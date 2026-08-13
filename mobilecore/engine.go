package mobilecore

import "github.com/metacubex/mihomo/tunnel/statistic"

// SocketProtector is implemented by Android's VpnService. Every outbound
// socket opened by Mihomo must be protected from re-entering the VPN tunnel.
type SocketProtector interface {
	Protect(fd int) bool
}

func EngineInit(home string, protector SocketProtector) string {
	return engineInit(home, protector)
}

func EngineLoadConfig(configYAML string) string {
	return engineLoadConfig(configYAML)
}

func EngineStartTun(fd int, stack, gateway, dns string) string {
	return engineStartTun(fd, stack, gateway, dns)
}

func EngineStopTun() string {
	return engineStopTun()
}

// EngineTraffic reports Mihomo's measured payload rate for the last second.
func EngineTraffic() string {
	upload, download := statistic.DefaultManager.Now()
	uploadTotal, downloadTotal := statistic.DefaultManager.Total()
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"upload_bps": upload, "download_bps": download,
		"upload_total": uploadTotal, "download_total": downloadTotal,
	}})
}

// EngineTestProxies performs real HTTPS URL tests through parsed Mihomo
// proxies without replacing the currently active tunnel configuration.
func EngineTestProxies(proxiesJSON, testURL string, timeoutMs, concurrency int) string {
	return engineTestProxies(proxiesJSON, testURL, timeoutMs, concurrency)
}

// EngineTestTCP performs the inexpensive reachability stage before proxy
// handshakes. UDP-only transports pass through to the URL stage.
func EngineTestTCP(proxiesJSON string, timeoutMs, concurrency int) string {
	return engineTestTCP(proxiesJSON, timeoutMs, concurrency)
}

// EngineTestProxiesMulti requires a majority of the supplied URL probes.
func EngineTestProxiesMulti(proxiesJSON, testURLsJSON string, timeoutMs, concurrency int) string {
	return engineTestProxiesMulti(proxiesJSON, testURLsJSON, timeoutMs, concurrency)
}

// EngineFilterCountries resolves the actual proxy egress country and removes
// countries excluded by the user's normal-mode policy.
func EngineFilterCountries(proxiesJSON, excludedJSON string, timeoutMs, concurrency int) string {
	return engineFilterCountries(proxiesJSON, excludedJSON, timeoutMs, concurrency)
}

// EngineSpeedAvailable checks the speed endpoint over the protected direct
// underlay before spending traffic on per-proxy measurements.
func EngineSpeedAvailable(testURL string, timeoutMs int) string {
	return engineSpeedAvailable(testURL, timeoutMs)
}

// EngineProbeConnectivity distinguishes a missing underlay, an allowlist-only
// network, and ordinary Internet access using user-configurable known targets.
func EngineProbeConnectivity(allowlistURL, openInternetURL string, timeoutMs int) string {
	return engineProbeConnectivity(allowlistURL, openInternetURL, timeoutMs)
}

// EngineTestSpeed performs two downloads per proxy and applies the requested
// throughput and stability thresholds.
func EngineTestSpeed(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64) string {
	return engineTestSpeed(proxiesJSON, testURL, timeoutMs, concurrency, minimumMbps, stabilityRatio)
}

func EngineTestSpeedAdaptive(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64, sampleBytes int64) string {
	return engineTestSpeedAdaptive(proxiesJSON, testURL, timeoutMs, concurrency, minimumMbps, stabilityRatio, sampleBytes)
}
