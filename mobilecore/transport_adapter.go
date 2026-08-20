package mobilecore

import (
	mobiletransport "github.com/gooog1111/orcheroute/internal/mobile/transport"
	"github.com/metacubex/mihomo/tunnel/statistic"
)

// embeddedTransport is the only bridge from the stable gomobile facade to the
// platform-tagged Mihomo driver in engine_android.go/engine_stub.go.
type embeddedTransport struct{}

var activeTransport mobiletransport.Engine = embeddedTransport{}

func (embeddedTransport) Available() bool { return embeddedEngineAvailable() }
func (embeddedTransport) Init(home string, protector mobiletransport.SocketProtector) string {
	return engineInit(home, protector)
}
func (embeddedTransport) LoadConfig(config string) string { return engineLoadConfig(config) }
func (embeddedTransport) StartTun(fd int, stack, gateway, dns string) string {
	return engineStartTun(fd, stack, gateway, dns)
}
func (embeddedTransport) StopTun() string { return engineStopTun() }
func (embeddedTransport) Traffic() string {
	upload, download := statistic.DefaultManager.Now()
	uploadTotal, downloadTotal := statistic.DefaultManager.Total()
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"upload_bps": upload, "download_bps": download,
		"upload_total": uploadTotal, "download_total": downloadTotal,
	}})
}
func (embeddedTransport) TestProxies(proxiesJSON, testURL string, timeoutMs, concurrency int) string {
	return engineTestProxies(proxiesJSON, testURL, timeoutMs, concurrency)
}
func (embeddedTransport) TestTCP(proxiesJSON string, timeoutMs, concurrency int) string {
	return engineTestTCP(proxiesJSON, timeoutMs, concurrency)
}
func (embeddedTransport) TestProxiesMulti(proxiesJSON, testURLsJSON string, timeoutMs, concurrency int) string {
	return engineTestProxiesMulti(proxiesJSON, testURLsJSON, timeoutMs, concurrency)
}
func (embeddedTransport) FilterCountries(proxiesJSON, excludedJSON string, timeoutMs, concurrency int) string {
	return engineFilterCountries(proxiesJSON, excludedJSON, timeoutMs, concurrency)
}
func (embeddedTransport) SpeedAvailable(testURL string, timeoutMs int) string {
	return engineSpeedAvailable(testURL, timeoutMs)
}
func (embeddedTransport) TestSpeed(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64) string {
	return engineTestSpeed(proxiesJSON, testURL, timeoutMs, concurrency, minimumMbps, stabilityRatio)
}
func (embeddedTransport) TestSpeedAdaptive(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64, sampleBytes int64) string {
	return engineTestSpeedAdaptive(proxiesJSON, testURL, timeoutMs, concurrency, minimumMbps, stabilityRatio, sampleBytes)
}
