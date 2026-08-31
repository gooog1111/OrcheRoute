// Package freeturnbridge is the isolated gomobile boundary for the optional
// VK TURN transport. It deliberately exposes only primitives and JSON so the
// upstream dependency can evolve without becoming part of OrcheRoute's core
// API or dependency graph.
package freeturnbridge

import (
	"encoding/json"
	"strings"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
	_ "github.com/gooog1111/orcheroute/mobilecore"
	upstream "github.com/samosvalishe/free-turn-proxy/mobile"
)

type EventSink interface {
	OnState(state string, streams, total int, errMsg string)
	OnLog(level, msg string, unixMillis int64)
	OnCaptcha(url string)
}

type Protector interface {
	Protect(fd int) bool
}

func Version() string { return upstream.Version() }

func DefaultConfigJSON() string { return upstream.DefaultConfigJSON() }

func ValidateConfig(configJSON string) string { return upstream.ValidateConfig(configJSON) }

func ConfigFromOrcheRouteProfile(encodedProfile, listenAddress string) (string, error) {
	profile, err := callprofile.Decode(encodedProfile, time.Now())
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(listenAddress) == "" {
		listenAddress = "127.0.0.1:19000"
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(upstream.DefaultConfigJSON()), &config); err != nil {
		return "", err
	}
	config["peer"] = profile.PeerAddress
	config["clientId"] = profile.VLESSUUID
	config["provider"] = "vk"
	config["routes"] = false
	config["proxy"] = map[string]any{"mode": "tcp", "listen": listenAddress}
	config["vk"] = map[string]any{
		"links": []string{profile.InvitationURL}, "manualCaptcha": true,
		"platform": "mobile", "streamsPerCred": 10,
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func Start(configJSON string) error { return upstream.Start(configJSON) }

func Restart(configJSON string) error { return upstream.Restart(configJSON, 0) }

func Stop() { upstream.Stop() }

func Wake() { upstream.Wake() }

func Reconnect() { upstream.Reconnect() }

func SetStateDir(path string) { upstream.SetStateDir(path) }

func SetDNSServers(servers string) { upstream.SetDNSServers(servers) }

func SetEventSink(s EventSink) { upstream.SetEventSink(s) }

func SetProtect(p Protector) { upstream.SetProtect(p) }

func DumpLogs() string { return upstream.DumpLogs() }

func ClearLogs() { upstream.ClearLogs() }

func StateJSON() string {
	state := upstream.GetState()
	payload, err := json.Marshal(map[string]any{
		"state": state.State, "streams": state.Streams, "total": state.Total,
		"error": state.ErrMsg, "tx_total": state.TxTotal, "rx_total": state.RxTotal,
		"tx_rate": state.TxRate, "rx_rate": state.RxRate,
	})
	if err != nil {
		return `{"state":"error","error":"state_encode_failed"}`
	}
	return string(payload)
}
