package mobilecore

import mobiletransport "github.com/gooog1111/orcheroute/internal/mobile/transport"

type transportContractCheck struct{}

func (transportContractCheck) Available() bool { return false }
func (transportContractCheck) Init(string, mobiletransport.SocketProtector) string {
	return "init"
}
func (transportContractCheck) LoadConfig(string) string                    { return "load" }
func (transportContractCheck) StartTun(int, string, string, string) string { return "start" }
func (transportContractCheck) StopTun() string                             { return "stop" }
func (transportContractCheck) Traffic() string                             { return "traffic" }
func (transportContractCheck) TestProxies(string, string, int, int) string { return "url" }
func (transportContractCheck) TestTCP(string, int, int) string             { return "tcp" }
func (transportContractCheck) TestProxiesMulti(string, string, int, int) string {
	return "urls"
}
func (transportContractCheck) FilterCountries(string, string, int, int) string { return "geo" }
func (transportContractCheck) SpeedAvailable(string, int) string               { return "speed-available" }
func (transportContractCheck) TestSpeed(string, string, int, int, float64, float64) string {
	return "speed"
}
func (transportContractCheck) TestSpeedAdaptive(string, string, int, int, float64, float64, int64) string {
	return "adaptive"
}

var _ mobiletransport.Engine = transportContractCheck{}
