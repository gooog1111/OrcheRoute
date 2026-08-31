package mobilecore

import (
	"encoding/json"
	"net"
	"strconv"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
)

// BuildFreeTURNProfileConfig creates the local VLESS hop used by the isolated
// FreeTURN runtime. Transport state and provider credentials belong to the
// upstream FreeTURN mobile runtime, not mobilecore.
func BuildFreeTURNProfileConfig(encodedProfile, endpoint, routesJSON, dnsJSON string) string {
	profile, err := callprofile.Decode(encodedProfile, time.Now())
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	host, portValue, err := net.SplitHostPort(endpoint)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "freeturn_invalid_local_listener"}})
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "freeturn_invalid_local_listener"}})
	}
	proxy, err := callxray.MihomoProxy(callxray.MihomoInput{Name: profile.Name, LocalAddress: host, LocalPort: port, ClientID: profile.VLESSUUID})
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	proxyJSON, err := json.Marshal(proxy)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "freeturn_proxy_encode"}})
	}
	return buildMobileProxyConfig(string(proxyJSON), routesJSON, dnsJSON)
}
