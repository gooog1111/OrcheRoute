package mobilecore

import (
	"encoding/json"
	"slices"
	"testing"

	mobiletransport "github.com/gooog1111/orcheroute/internal/core/transport"
)

type qualificationTransport struct{ speedAvailable bool }

type qualificationObserver struct {
	cancelled bool
	stages    []string
}

func (observer *qualificationObserver) OnProgress(stage string, _, _ int) {
	observer.stages = append(observer.stages, stage)
}
func (observer *qualificationObserver) IsCancelled() bool { return observer.cancelled }

func (qualificationTransport) Available() bool { return true }
func (qualificationTransport) Init(string, mobiletransport.SocketProtector) string {
	return `{"ok":true}`
}
func (qualificationTransport) LoadConfig(string) string                    { return `{"ok":true}` }
func (qualificationTransport) StartTun(int, string, string, string) string { return `{"ok":true}` }
func (qualificationTransport) StopTun() string                             { return `{"ok":true}` }
func (qualificationTransport) Traffic() string                             { return `{"ok":true}` }
func (qualificationTransport) TestProxies(string, string, int, int) string { return `{"ok":true}` }
func (qualificationTransport) TestTCP(payload string, _, _ int) string {
	return probeReply(payload, 20, 0)
}
func (qualificationTransport) TestProxiesMulti(payload, _ string, _, _ int) string {
	return probeReply(payload, 40, 0)
}
func (qualificationTransport) FilterCountries(payload, _ string, _, _ int) string {
	return probeReply(payload, 40, 0)
}
func (transport qualificationTransport) SpeedAvailable(string, int) string {
	return encode(map[string]any{"ok": true, "result": map[string]any{"available": transport.speedAvailable, "baseline_mbps": 30.0, "recommended_bytes": 524288}})
}
func (qualificationTransport) TestSpeed(string, string, int, int, float64, float64) string {
	return `{"ok":false}`
}
func (qualificationTransport) TestSpeedAdaptive(payload, _ string, _, _ int, _, _ float64, _ int64) string {
	return probeReply(payload, 40, 20)
}

func probeReply(payload string, delay int, speed float64) string {
	var proxies []map[string]any
	_ = json.Unmarshal([]byte(payload), &proxies)
	nodes := make([]map[string]any, 0, len(proxies))
	for _, proxy := range proxies {
		node := map[string]any{"name": proxy["name"], "alive": true, "delay_ms": delay, "country": "DE"}
		if speed > 0 {
			node["speed_mbps"] = speed
			node["stability_ratio"] = .9
		}
		nodes = append(nodes, node)
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"nodes": nodes}})
}

func TestQualifyNodesUsesSharedPipelineAndSkipsUnavailableSpeed(t *testing.T) {
	previous := activeTransport
	activeTransport = qualificationTransport{speedAvailable: false}
	defer func() { activeTransport = previous }()
	proxies := `[{"name":"A"},{"name":"B"}]`
	settings := `{"min_speed_mbps":10,"stability_ratio":0.65,"excluded_countries":[],"url_limit":0,"speed_candidates":0,"keep":0,"tcp_timeout_ms":2000,"url_timeout_ms":3000,"url_test_urls":["https://example.com"]}`
	var envelope map[string]any
	if err := json.Unmarshal([]byte(QualifyNodes("primary", proxies, settings, `{}`, nil)), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != true {
		t.Fatalf("qualification failed: %#v", envelope)
	}
	result := envelope["result"].(map[string]any)
	if len(result["proxies"].([]any)) != 2 || len(result["tests"].([]any)) != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestQualifyNodesUsesDynamicBaselineAndReportsProgress(t *testing.T) {
	previous := activeTransport
	activeTransport = qualificationTransport{speedAvailable: true}
	defer func() { activeTransport = previous }()
	observer := &qualificationObserver{}
	settings := `{"min_speed_mbps":10,"stability_ratio":0.65,"excluded_countries":[],"url_limit":0,"speed_candidates":0,"keep":0,"tcp_timeout_ms":2000,"url_timeout_ms":3000,"speed_timeout_ms":15000,"url_test_urls":["https://example.com"]}`
	var envelope map[string]any
	if err := json.Unmarshal([]byte(QualifyNodes("primary", `[{"name":"A"}]`, settings, `{}`, observer)), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != true {
		t.Fatalf("qualification failed: %#v", envelope)
	}
	result := envelope["result"].(map[string]any)
	report := result["report"].(map[string]any)
	if report["threshold_source"] != "wan_baseline" || report["threshold_mbps"] != float64(3) {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(observer.stages) == 0 {
		t.Fatal("progress was not reported")
	}
	tcp := slices.Index(observer.stages, "tcp")
	url := slices.Index(observer.stages, "url_test")
	baseline := slices.Index(observer.stages, "baseline")
	speed := slices.Index(observer.stages, "speed_test")
	if tcp < 0 || url <= tcp || baseline <= url || speed <= baseline {
		t.Fatalf("unexpected qualification order: %v", observer.stages)
	}
}

func TestQualifyNodesHonorsCancellationBeforeTransportBatch(t *testing.T) {
	previous := activeTransport
	activeTransport = qualificationTransport{speedAvailable: false}
	defer func() { activeTransport = previous }()
	observer := &qualificationObserver{cancelled: true}
	settings := `{"min_speed_mbps":10,"stability_ratio":0.65,"excluded_countries":[],"url_limit":0,"speed_candidates":0,"keep":0,"tcp_timeout_ms":2000,"url_timeout_ms":3000,"url_test_urls":["https://example.com"]}`
	var envelope map[string]any
	if err := json.Unmarshal([]byte(QualifyNodes("primary", `[{"name":"A"}]`, settings, `{}`, observer)), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != false {
		t.Fatalf("cancelled qualification succeeded: %#v", envelope)
	}
}

var _ mobiletransport.Engine = qualificationTransport{}
