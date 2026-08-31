package serverruntime

import (
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/callserver"
)

const defaultCallServerPort = "4443"

func (runtime *Runtime) autoConfigureCallServer(body map[string]any) (int, any) {
	if runtime.CallServer == nil {
		return 503, map[string]any{"error": "call_server_unavailable", "message": runtime.callServerError}
	}
	snapshot := runtime.identitySnapshot()
	if snapshot.UpdatedAt == 0 || time.Now().Unix()-snapshot.UpdatedAt > 120 {
		return 409, map[string]any{"error": "call_server_direct_ip_stale", "message": "Ожидаем свежее определение внешнего Direct IP"}
	}
	identity := snapshot.Direct
	if identity == nil || strings.TrimSpace(identity.IP) == "" {
		return 409, map[string]any{"error": "call_server_direct_ip_unavailable", "message": "Внешний Direct IP ещё не определён"}
	}
	address, err := netip.ParseAddr(strings.TrimSpace(identity.IP))
	if err != nil || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return 409, map[string]any{"error": "call_server_direct_ip_not_public", "message": "Direct IP не является публичным адресом"}
	}

	current := runtime.CallServer.PublicConfig()
	port := callServerPort(current)
	publicEndpoint := net.JoinHostPort(address.String(), port)
	if current.PublicEndpoint != "" && current.PublicEndpoint != publicEndpoint && len(current.Clients) > 0 {
		return 409, map[string]any{"error": "call_server_public_ip_changed", "message": "Внешний IP изменился; существующим клиентам потребуются новые профили"}
	}
	subscriptionBaseURL := automaticSubscriptionBaseURL(stringValue(body["browser_origin"]))
	if subscriptionBaseURL == "" {
		subscriptionBaseURL = current.SubscriptionBaseURL
	}
	config := callserver.Config{
		Version: current.Version, Enabled: current.Enabled,
		ListenAddress:       "0.0.0.0:" + port,
		PublicEndpoint:      publicEndpoint,
		BackendAddress:      "127.0.0.1:18443",
		SubscriptionBaseURL: subscriptionBaseURL,
		OrdinaryEnabled:     current.OrdinaryEnabled, VLESSListenAddress: current.VLESSListenAddress,
		TrojanListenAddress: current.TrojanListenAddress, HysteriaListenAddress: current.HysteriaListenAddress,
		FakeSNI: current.FakeSNI,
	}
	updated, err := runtime.CallServer.UpdatePublicConfig(config, false, false)
	if err != nil {
		return backendError(err)
	}
	warnings := []string{"external_tcp_unverified"}
	if subscriptionBaseURL == "" {
		warnings = append(warnings, "subscription_url_optional")
	}
	return 200, map[string]any{
		"configured": true, "config": updated, "source": "direct_identity",
		"public_ip": address.String(), "tcp_port": port, "warnings": warnings,
	}
}

func callServerPort(config callserver.PublicConfig) string {
	for _, endpoint := range []string{config.PublicEndpoint, config.ListenAddress} {
		_, port, err := net.SplitHostPort(endpoint)
		if err == nil && port != "" {
			return port
		}
	}
	return defaultCallServerPort
}

func automaticSubscriptionBaseURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return (&url.URL{Scheme: "https", Host: parsed.Host}).String()
}
