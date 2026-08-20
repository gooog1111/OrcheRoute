//go:build !android && !ios

package mobilecore

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
