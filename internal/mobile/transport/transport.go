// Package transport defines the side-effect boundary around the embedded VPN
// engine. Implementations may use Mihomo and an Android/Apple tunnel, while
// callers depend only on this lifecycle and probe contract.
package transport

type SocketProtector interface {
	Protect(fd int) bool
}

type Engine interface {
	Available() bool
	Init(home string, protector SocketProtector) string
	LoadConfig(config string) string
	StartTun(fd int, stack, gateway, dns string) string
	StopTun() string
	Traffic() string
	TestProxies(proxiesJSON, testURL string, timeoutMs, concurrency int) string
	TestTCP(proxiesJSON string, timeoutMs, concurrency int) string
	TestProxiesMulti(proxiesJSON, testURLsJSON string, timeoutMs, concurrency int) string
	FilterCountries(proxiesJSON, excludedJSON string, timeoutMs, concurrency int) string
	SpeedAvailable(testURL string, timeoutMs int) string
	ProbeConnectivity(allowlistURL, openInternetURL string, timeoutMs int) string
	TestSpeed(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64) string
	TestSpeedAdaptive(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64, sampleBytes int64) string
}
