package qualification

import (
	"context"
	"testing"
	"time"
)

type fakeBackend struct {
	tcp        []Latency
	url        []Latency
	speed      []SpeedEvidence
	speedCalls int
	speedBytes int64
}

type baselineBackend struct {
	fakeBackend
	baseline Download
}

func (backend baselineBackend) Baseline(context.Context) (Download, error) {
	return backend.baseline, nil
}

func (backend fakeBackend) TCP(context.Context, []map[string]any) ([]Latency, error) {
	return backend.tcp, nil
}
func (backend fakeBackend) URL(context.Context, []map[string]any) ([]Latency, error) {
	return backend.url, nil
}

func (backend *fakeBackend) Speed(context.Context, []map[string]any, bool) ([]SpeedEvidence, error) {
	backend.speedCalls++
	return backend.speed, nil
}
func (backend *fakeBackend) SetSpeedBytes(value int64) { backend.speedBytes = value }

func goodDownloads(first, second float64) []Download {
	return []Download{{OK: true, HTTPCode: 200, Bytes: SpeedBytes, BytesPerSecond: first}, {OK: true, HTTPCode: 200, Bytes: SpeedBytes, BytesPerSecond: second}}
}

func TestEvaluateSpeed(t *testing.T) {
	excluded := map[string]bool{"RU": true}
	cases := []struct {
		evidence SpeedEvidence
		want     string
	}{
		{SpeedEvidence{CoreOK: false}, "core_failed"},
		{SpeedEvidence{CoreOK: true, Country: "RU"}, "country_excluded"},
		{SpeedEvidence{CoreOK: true, Country: "DE", Downloads: goodDownloads(2_000_000, 500_000)}, "slow"},
		{SpeedEvidence{CoreOK: true, Country: "DE", Downloads: goodDownloads(2_000_000, 1_000_000)}, "unstable"},
		{SpeedEvidence{CoreOK: true, Country: "DE", Downloads: goodDownloads(2_000_000, 1_500_000)}, "qualified"},
	}
	for _, item := range cases {
		if got := EvaluateSpeed(item.evidence, 1_000_000, 0.65, excluded); got.Status != item.want {
			t.Fatalf("got %s, want %s", got.Status, item.want)
		}
	}
}

func TestQualificationPipelineAndReport(t *testing.T) {
	proxies := []map[string]any{{"name": "A"}, {"name": "B"}, {"name": "C"}}
	backend := &fakeBackend{
		tcp: []Latency{{Alive: true, Seconds: .3}, {Alive: false}, {Alive: true, Seconds: .1}},
		url: []Latency{{Alive: true, Seconds: .2}, {Alive: true, Seconds: .1}},
		speed: []SpeedEvidence{
			{CoreOK: true, Country: "DE", Downloads: goodDownloads(2_000_000, 1_800_000)},
			{CoreOK: true, Country: "NL", Downloads: goodDownloads(3_000_000, 2_800_000)},
		},
	}
	settings := map[string]any{"min_speed_mbps": float64(10), "stability_ratio": .65, "excluded_countries": []any{"RU"}, "url_limit": float64(0), "speed_candidates": float64(0), "keep": float64(1)}
	times := []time.Time{time.Unix(100, 0), time.Unix(110, 0)}
	now := func() time.Time { value := times[0]; times = times[1:]; return value }
	result, err := Qualify(context.Background(), "primary", proxies, settings, map[string]Source{"A": {ID: "one"}, "B": {ID: "one"}, "C": {ID: "two"}}, backend, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Proxies) != 1 || result.Proxies[0]["name"] != "C" {
		t.Fatalf("unexpected retained proxies: %#v", result.Proxies)
	}
	if result.Report.TCPAlive != 2 || result.Report.URLAlive != 2 || result.Report.GeoPassed != 2 || result.Report.Qualified != 2 || result.Report.Retained != 1 {
		t.Fatalf("unexpected report: %#v", result.Report)
	}
	if result.Report.Sources["two"].Retained != 1 || result.Report.Sources["one"].Qualified != 1 {
		t.Fatalf("unexpected sources: %#v", result.Report.Sources)
	}
	if result.Metrics["C"].DelayMS != 200 || result.Metrics["C"].SpeedMbps != 22.4 || result.Metrics["C"].StabilityRatio < 0.93 || result.Metrics["C"].Country != "NL" {
		t.Fatalf("unexpected retained metrics: %#v", result.Metrics["C"])
	}
}

func TestQualificationUsesTenPercentOfWANBaseline(t *testing.T) {
	backend := baselineBackend{
		fakeBackend: fakeBackend{
			tcp:   []Latency{{Alive: true}},
			url:   []Latency{{Alive: true}},
			speed: []SpeedEvidence{{CoreOK: true, Downloads: goodDownloads(1_200_000, 1_200_000)}},
		},
		baseline: Download{OK: true, HTTPCode: 200, Bytes: SpeedBytes, BytesPerSecond: 12_500_000},
	}
	settings := map[string]any{"min_speed_mbps": float64(1), "stability_ratio": .65, "excluded_countries": []any{}, "url_limit": float64(0), "speed_candidates": float64(0), "keep": float64(0)}
	result, err := Qualify(context.Background(), "primary", []map[string]any{{"name": "A"}}, settings, nil, &backend, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.BaselineMbps != 100 || result.Report.ThresholdMbps != 10 || result.Report.ThresholdSource != "wan_baseline" {
		t.Fatalf("unexpected dynamic threshold: %#v", result.Report)
	}
	if result.Report.Qualified != 0 || result.Report.Outcomes["slow"] != 1 {
		t.Fatalf("candidate should be below the dynamic threshold: %#v", result.Report)
	}
	if backend.speedBytes != MaxSpeedSampleBytes {
		t.Fatalf("adaptive sample=%d want %d", backend.speedBytes, MaxSpeedSampleBytes)
	}
}

func TestQualificationCanSkipSpeedForAllowlist(t *testing.T) {
	backend := &fakeBackend{
		tcp: []Latency{{Alive: true, Seconds: .2}, {Alive: true, Seconds: .1}},
		url: []Latency{{Alive: true, Seconds: .3}, {Alive: false}},
	}
	settings := map[string]any{
		"min_speed_mbps": float64(10), "stability_ratio": .65,
		"excluded_countries": []any{"RU"}, "url_limit": float64(0),
		"speed_candidates": float64(0), "keep": float64(0), "skip_speed": true,
	}
	result, err := Qualify(context.Background(), "whitelist", []map[string]any{{"name": "A"}, {"name": "B"}}, settings, nil, backend, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if backend.speedCalls != 0 {
		t.Fatalf("speed backend was called %d times", backend.speedCalls)
	}
	if len(result.Proxies) != 1 || result.Proxies[0]["name"] != "B" {
		t.Fatalf("unexpected retained proxies: %#v", result.Proxies)
	}
	if result.Report.SpeedRuns != 0 || result.Report.Qualified != 1 || result.Report.ThresholdSource != "skipped" {
		t.Fatalf("unexpected allowlist report: %#v", result.Report)
	}
}

func TestAdaptiveSpeedBytes(t *testing.T) {
	tests := []struct {
		speed float64
		want  int64
	}{
		{100_000, MinSpeedSampleBytes},
		{1_250_000, 625_000},
		{3_750_000, 1_875_000},
		{100_000_000, MaxSpeedSampleBytes},
	}
	for _, test := range tests {
		if got := AdaptiveSpeedBytes(test.speed); got != test.want {
			t.Fatalf("AdaptiveSpeedBytes(%v)=%d want %d", test.speed, got, test.want)
		}
	}
}
