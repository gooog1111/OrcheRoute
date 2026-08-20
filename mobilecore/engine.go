package mobilecore

// SocketProtector is implemented by Android's VpnService. Every outbound
// socket opened by Mihomo must be protected from re-entering the VPN tunnel.
type SocketProtector interface {
	Protect(fd int) bool
}

func EngineInit(home string, protector SocketProtector) string {
	return activeTransport.Init(home, protector)
}

func EngineLoadConfig(configYAML string) string {
	return activeTransport.LoadConfig(configYAML)
}

func EngineStartTun(fd int, stack, gateway, dns string) string {
	return activeTransport.StartTun(fd, stack, gateway, dns)
}

func EngineStopTun() string {
	return activeTransport.StopTun()
}

// EngineTraffic reports Mihomo's measured payload rate for the last second.
func EngineTraffic() string {
	return activeTransport.Traffic()
}

// EngineTestProxies performs real HTTPS URL tests through parsed Mihomo
// proxies without replacing the currently active tunnel configuration.
func EngineTestProxies(proxiesJSON, testURL string, timeoutMs, concurrency int) string {
	return activeTransport.TestProxies(proxiesJSON, testURL, timeoutMs, concurrency)
}

// EngineTestTCP performs the inexpensive reachability stage before proxy
// handshakes. UDP-only transports pass through to the URL stage.
func EngineTestTCP(proxiesJSON string, timeoutMs, concurrency int) string {
	return activeTransport.TestTCP(proxiesJSON, timeoutMs, concurrency)
}

// EngineTestProxiesMulti requires a majority of the supplied URL probes.
func EngineTestProxiesMulti(proxiesJSON, testURLsJSON string, timeoutMs, concurrency int) string {
	return activeTransport.TestProxiesMulti(proxiesJSON, testURLsJSON, timeoutMs, concurrency)
}

// EngineFilterCountries resolves the actual proxy egress country and removes
// countries excluded by the user's normal-mode policy.
func EngineFilterCountries(proxiesJSON, excludedJSON string, timeoutMs, concurrency int) string {
	return activeTransport.FilterCountries(proxiesJSON, excludedJSON, timeoutMs, concurrency)
}

// EngineSpeedAvailable checks the speed endpoint over the protected direct
// underlay before spending traffic on per-proxy measurements.
func EngineSpeedAvailable(testURL string, timeoutMs int) string {
	return activeTransport.SpeedAvailable(testURL, timeoutMs)
}

// EngineProbeConnectivity is kept for gomobile API compatibility. Connectivity
// diagnosis is a separate layer and does not depend on Mihomo or TUN state.
func EngineProbeConnectivity(allowlistURL, openInternetURL string, timeoutMs int) string {
	return ProbeConnectivity(allowlistURL, openInternetURL, timeoutMs)
}

// ProbeConnectivity distinguishes a missing underlay, an allowlist-only
// network, and ordinary Internet access using physical-network probes.
func ProbeConnectivity(allowlistURL, openInternetURL string, timeoutMs int) string {
	return platformProbeConnectivity(allowlistURL, openInternetURL, timeoutMs)
}

// EngineTestSpeed performs two downloads per proxy and applies the requested
// throughput and stability thresholds.
func EngineTestSpeed(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64) string {
	return activeTransport.TestSpeed(proxiesJSON, testURL, timeoutMs, concurrency, minimumMbps, stabilityRatio)
}

func EngineTestSpeedAdaptive(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64, sampleBytes int64) string {
	return activeTransport.TestSpeedAdaptive(proxiesJSON, testURL, timeoutMs, concurrency, minimumMbps, stabilityRatio, sampleBytes)
}
