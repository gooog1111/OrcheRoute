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
	State            mobileconnectivity.State  `json:"state"`
	ObservedState    mobileconnectivity.State  `json:"observed_state"`
	CandidateState   mobileconnectivity.State  `json:"candidate_state,omitempty"`
	CandidateCount   int                       `json:"candidate_count"`
	CandidateSinceMS int64                     `json:"candidate_since_ms,omitempty"`
	Changed          bool                      `json:"changed"`
	UpdatedAt        int64                     `json:"updated_at"`
	AttemptedAt      int64                     `json:"attempted_at"`
	ConfirmedAt      int64                     `json:"confirmed_at"`
	DirectInterface  string                    `json:"direct_interface,omitempty"`
	DirectNetwork    string                    `json:"direct_network,omitempty"`
	Observation      mobileconnectivity.Result `json:"observation"`
	Error            string                    `json:"error,omitempty"`
}

type connectivityProbeFactory func(interfaceName string, timeout time.Duration) mobileconnectivity.Probe
type connectivityTopologyLookup func(context.Context) (network.Topology, error)

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
	topologyLookup := runtime.connectivityTopologyLookup
	if topologyLookup == nil {
		topologyLookup = discoverTopology
	}
	// The interface name alone survives a DHCP renewal, an AP roam, or a
	// USB-tether reconnect that keeps the same name but attaches to a
	// different physical network. Fold in the interface's current address and
	// gateway so those cases also reset a stale allowlist candidate, instead
	// of only a rename to a different interface.
	networkID := interfaceName
	if topology, topoErr := topologyLookup(cycle); topoErr == nil {
		networkID = directNetworkFingerprint(topology, interfaceName)
	} else if previous.DirectNetwork != "" {
		networkID = previous.DirectNetwork
	}
	if previous.DirectInterface != interfaceName || (previous.DirectNetwork != "" && previous.DirectNetwork != networkID) {
		previous.CandidateState, previous.CandidateCount, previous.CandidateSinceMS = "", 0, 0
	}
	confirmed, err := mobileconnectivity.Confirm(mobileconnectivity.ConfirmationInput{
		ConfirmedState: previous.State, CandidateState: previous.CandidateState,
		CandidateCount: previous.CandidateCount, ObservedState: observed.State,
		NowMS: time.Now().UnixMilli(), CandidateSinceMS: previous.CandidateSinceMS,
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
		CandidateSinceMS: confirmed.CandidateSinceMS,
		Changed:          confirmed.Changed, UpdatedAt: now, AttemptedAt: now, ConfirmedAt: confirmedAt,
		DirectInterface: interfaceName, DirectNetwork: networkID, Observation: observed,
	}
	_ = atomicJSON(runtime.connectivityPath(), snapshot)
}

// directNetworkFingerprint identifies the physical network currently attached
// to interfaceName, not just the interface's name. A stable name can outlive
// the attached network across a DHCP renewal, a Wi-Fi AP roam or a tether
// reconnect, so the fingerprint also folds in the interface's current
// address and default gateway.
func directNetworkFingerprint(topology network.Topology, interfaceName string) string {
	for _, iface := range topology.Interfaces {
		if iface.Name != interfaceName {
			continue
		}
		address := ""
		for _, candidate := range iface.Addresses {
			if candidate.Family == "inet" {
				address = candidate.CIDR
				break
			}
		}
		gateway := ""
		for _, route := range iface.DefaultRoutes {
			if route.Gateway != nil {
				gateway = *route.Gateway
				break
			}
		}
		return interfaceName + "|" + address + "|" + gateway
	}
	return interfaceName
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
	previous.AttemptedAt = time.Now().Unix()
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
