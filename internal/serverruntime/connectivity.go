package serverruntime

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	mobileconnectivity "github.com/gooog1111/orcheroute/internal/core/connectivity"
	corevalidator "github.com/gooog1111/orcheroute/internal/core/validator"
	"github.com/gooog1111/orcheroute/internal/network"
)

// ConnectivitySnapshot is owned by the physical-network monitor. Controllers
// and transports consume it but must never run their own connectivity probes.
type ConnectivitySnapshot struct {
	State           mobileconnectivity.State  `json:"state"`
	ObservedState   mobileconnectivity.State  `json:"observed_state"`
	CandidateState  mobileconnectivity.State  `json:"candidate_state,omitempty"`
	CandidateCount  int                       `json:"candidate_count"`
	Changed         bool                      `json:"changed"`
	UpdatedAt       int64                     `json:"updated_at"`
	ConfirmedAt     int64                     `json:"confirmed_at"`
	DirectInterface string                    `json:"direct_interface,omitempty"`
	Observation     mobileconnectivity.Result `json:"observation"`
	Error           string                    `json:"error,omitempty"`
}

type connectivityProbeFactory func(interfaceName string, timeout time.Duration) mobileconnectivity.Probe

func (runtime *Runtime) RunConnectivityMonitor(ctx context.Context) {
	ticker := time.NewTicker(runtime.Config.ConnectivityEvery)
	defer ticker.Stop()
	runtime.connectivityCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime.connectivityCycle(ctx)
		}
	}
}

func (runtime *Runtime) connectivityCycle(ctx context.Context) {
	cycle, cancel := context.WithTimeout(ctx, runtime.Config.ConnectivityTimeout)
	defer cancel()
	previous := runtime.connectivitySnapshot()
	profile := network.Profile{}
	if err := readJSON(filepath.Join(runtime.Config.StateDirectory, "network-active.json"), &profile); err != nil {
		runtime.recordConnectivityError(previous, err)
		return
	}
	policy := corevalidator.DefaultQualificationPolicy()
	var stored map[string]any
	if readJSON(filepath.Join(runtime.Config.StateDirectory, "qualification-policy.json"), &stored) == nil {
		validated, err := corevalidator.QualificationPolicy(stored)
		if err != nil {
			runtime.recordConnectivityError(previous, err)
			return
		}
		policy = validated
	}
	defaults, _ := policy["defaults"].(map[string]any)
	allowlistURL := stringValue(defaults["allowlist_probe_url"])
	openURL := stringValue(defaults["open_internet_probe_url"])
	timeout := time.Duration(intValue(defaults["url_timeout_ms"])) * time.Millisecond
	if timeout <= 0 || timeout > runtime.Config.ConnectivityTimeout {
		timeout = runtime.Config.ConnectivityTimeout
	}
	interfaceName := profile.Roles["direct"].Interface
	factory := runtime.connectivityProbeFactory
	if factory == nil {
		factory = boundConnectivityProbe
	}
	observed, err := mobileconnectivity.Diagnose(cycle, mobileconnectivity.Config{
		AllowlistURL: allowlistURL, OpenInternetURL: openURL,
	}, factory(interfaceName, timeout))
	if err != nil {
		runtime.recordConnectivityError(previous, err)
		return
	}
	confirmed, err := mobileconnectivity.Confirm(mobileconnectivity.ConfirmationInput{
		ConfirmedState: previous.State, CandidateState: previous.CandidateState,
		CandidateCount: previous.CandidateCount, ObservedState: observed.State,
	})
	if err != nil {
		runtime.recordConnectivityError(previous, err)
		return
	}
	now := time.Now().Unix()
	confirmedAt := previous.ConfirmedAt
	if confirmed.Changed || confirmedAt == 0 {
		confirmedAt = now
	}
	snapshot := ConnectivitySnapshot{
		State: confirmed.State, ObservedState: observed.State,
		CandidateState: confirmed.CandidateState, CandidateCount: confirmed.CandidateCount,
		Changed: confirmed.Changed, UpdatedAt: now, ConfirmedAt: confirmedAt,
		DirectInterface: interfaceName, Observation: observed,
	}
	_ = atomicJSON(runtime.connectivityPath(), snapshot)
}

func (runtime *Runtime) connectivityPath() string {
	return filepath.Join(runtime.Config.StateDirectory, "connectivity-state.json")
}

func (runtime *Runtime) connectivitySnapshot() ConnectivitySnapshot {
	value := ConnectivitySnapshot{State: "unknown"}
	_ = readJSON(runtime.connectivityPath(), &value)
	if value.State == "" {
		value.State = "unknown"
	}
	return value
}

func (runtime *Runtime) recordConnectivityError(previous ConnectivitySnapshot, err error) {
	previous.Changed = false
	previous.UpdatedAt = time.Now().Unix()
	previous.Error = err.Error()
	_ = atomicJSON(runtime.connectivityPath(), previous)
}

func boundConnectivityProbe(interfaceName string, timeout time.Duration) mobileconnectivity.Probe {
	return func(ctx context.Context, target mobileconnectivity.Target) bool {
		resolverDialer := platformDialer(interfaceName, 0)
		dialer := platformDialer(interfaceName, 0)
		dialer.Timeout = timeout
		dialer.Resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return resolverDialer.DialContext(ctx, network, address)
		}}
		transport := &http.Transport{
			DialContext:       dialer.DialContext,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			DisableKeepAlives: true,
		}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: timeout}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
		if err != nil {
			return false
		}
		request.Header.Set("Cache-Control", "no-cache, no-store")
		request.Header.Set("Pragma", "no-cache")
		request.Header.Set("User-Agent", "OrcheRoute Server connectivity monitor")
		response, err := client.Do(request)
		if err != nil {
			return false
		}
		response.Body.Close()
		if target.ExpectNoContent {
			return response.StatusCode == http.StatusNoContent
		}
		return response.StatusCode >= 200 && response.StatusCode < 300
	}
}

func (snapshot ConnectivitySnapshot) validate() error {
	if snapshot.State != mobileconnectivity.Normal && snapshot.State != mobileconnectivity.Allowlist &&
		snapshot.State != mobileconnectivity.Offline && snapshot.State != "unknown" {
		return fmt.Errorf("invalid_connectivity_snapshot")
	}
	return nil
}
