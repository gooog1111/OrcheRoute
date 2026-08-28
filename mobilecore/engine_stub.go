//go:build !android

package mobilecore

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/gooog1111/orcheroute/internal/calltransport"
)

func embeddedEngineAvailable() bool { return false }

func engineInit(string, SocketProtector) string { return engineUnavailable() }
func engineLoadConfig(string) string            { return engineUnavailable() }
func engineStartTun(int, string, string, string) string {
	return engineUnavailable()
}
func engineStopTun() string                                             { return encode(map[string]any{"ok": true}) }
func engineTestProxies(string, string, int, int) string                 { return engineUnavailable() }
func engineTestTCP(string, int, int) string                             { return engineUnavailable() }
func engineTestProxiesMulti(string, string, int, int) string            { return engineUnavailable() }
func engineFilterCountries(string, string, int, int) string             { return engineUnavailable() }
func engineSpeedAvailable(string, int) string                           { return engineUnavailable() }
func platformProbeConnectivity(string, string, int) string              { return engineUnavailable() }
func engineTestSpeed(string, string, int, int, float64, float64) string { return engineUnavailable() }
func engineTestSpeedAdaptive(string, string, int, int, float64, float64, int64) string {
	return engineUnavailable()
}

func engineUnavailable() string {
	return encode(map[string]any{"ok": false, "error": map[string]string{"error": "embedded_engine_unavailable"}})
}

func platformCallHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

type standardCallUnderlay struct{}

func (standardCallUnderlay) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return (&net.ListenConfig{}).ListenPacket(ctx, network, address)
}

func (standardCallUnderlay) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
}

func platformCallUnderlay() calltransport.Underlay { return standardCallUnderlay{} }
