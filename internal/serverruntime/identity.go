package serverruntime

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"

	mobileconnectivity "github.com/gooog1111/orcheroute/internal/mobile/connectivity"
	"github.com/gooog1111/orcheroute/internal/network"
	"golang.org/x/net/proxy"
)

const identityURL = "https://www.cloudflare.com/cdn-cgi/trace"

type IdentitySnapshot struct {
	Direct    *mobileconnectivity.Identity `json:"direct,omitempty"`
	Proxy     *mobileconnectivity.Identity `json:"proxy,omitempty"`
	UpdatedAt int64                        `json:"updated_at"`
	Error     string                       `json:"error,omitempty"`
}

func (runtime *Runtime) RunIdentityMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	runtime.identityCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime.identityCycle(ctx)
		}
	}
}

func (runtime *Runtime) identityCycle(ctx context.Context) {
	cycle, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	value := IdentitySnapshot{UpdatedAt: time.Now().Unix()}
	profile := network.Profile{}
	if err := readJSON(filepath.Join(runtime.Config.StateDirectory, "network-active.json"), &profile); err != nil {
		value.Error = err.Error()
		_ = atomicJSON(runtime.identityPath(), value)
		return
	}
	if identity, err := directIdentity(cycle, profile.Roles["direct"].Interface); err == nil {
		value.Direct = &identity
	} else {
		value.Error = err.Error()
	}
	if control, err := runtime.Store.Control(cycle); err == nil && control.Enabled {
		if identity, err := proxyIdentity(cycle); err == nil {
			value.Proxy = &identity
		}
	}
	_ = atomicJSON(runtime.identityPath(), value)
}

func (runtime *Runtime) identityPath() string {
	return filepath.Join(runtime.Config.StateDirectory, "connection-identity.json")
}
func (runtime *Runtime) identitySnapshot() IdentitySnapshot {
	value := IdentitySnapshot{}
	_ = readJSON(runtime.identityPath(), &value)
	return value
}

func directIdentity(ctx context.Context, interfaceName string) (mobileconnectivity.Identity, error) {
	dialer := platformDialer(interfaceName, 0)
	dialer.Timeout = 6 * time.Second
	return fetchIdentity(ctx, dialer.DialContext)
}

func proxyIdentity(ctx context.Context) (mobileconnectivity.Identity, error) {
	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:21080", nil, proxy.Direct)
	if err != nil {
		return mobileconnectivity.Identity{}, err
	}
	return fetchIdentity(ctx, func(_ context.Context, network, address string) (net.Conn, error) {
		return dialer.Dial(network, address)
	})
}

func fetchIdentity(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error)) (mobileconnectivity.Identity, error) {
	transport := &http.Transport{DialContext: dial, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 7 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, identityURL, nil)
	if err != nil {
		return mobileconnectivity.Identity{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return mobileconnectivity.Identity{}, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return mobileconnectivity.Identity{}, err
	}
	return mobileconnectivity.ParseTraceIdentity(string(payload))
}
