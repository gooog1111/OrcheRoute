package mobilecore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/core/qualification"
)

const mobileSpeedURL = "https://cachefly.cachefly.net/10mb.test"

// QualificationObserver is implemented by the Android adapter. The Go core
// owns stage ordering; Android only renders progress and requests cancellation.
type QualificationObserver interface {
	OnProgress(stage string, current, total int)
	IsCancelled() bool
}

type mobileQualificationBackend struct {
	settings   map[string]any
	observer   QualificationObserver
	speedBytes int64
	baseline   qualification.Download
	baselineOK bool
}

type qualificationProbe struct {
	Name           string  `json:"name"`
	Alive          bool    `json:"alive"`
	DelayMS        int     `json:"delay_ms"`
	SpeedMbps      float64 `json:"speed_mbps"`
	StabilityRatio float64 `json:"stability_ratio"`
	Country        string  `json:"country"`
	Error          string  `json:"error"`
}

type qualificationEnvelope struct {
	OK     bool `json:"ok"`
	Result struct {
		Nodes     []qualificationProbe `json:"nodes"`
		Available bool                 `json:"available"`
		Baseline  float64              `json:"baseline_mbps"`
		Bytes     int64                `json:"recommended_bytes"`
	} `json:"result"`
	Error any `json:"error"`
}

// QualifyNodes is the stable gomobile facade for the shared qualification
// pipeline. Inputs and output use the same canonical node and policy models as
// Linux Server.
func QualifyNodes(pool, proxiesJSON, settingsJSON, sourcesJSON string, observer QualificationObserver) string {
	var proxies []map[string]any
	var settings map[string]any
	var sources map[string]qualification.Source
	if json.Unmarshal([]byte(proxiesJSON), &proxies) != nil || len(proxies) == 0 ||
		json.Unmarshal([]byte(settingsJSON), &settings) != nil {
		return qualificationError("invalid_qualification_request")
	}
	if strings.TrimSpace(sourcesJSON) != "" && json.Unmarshal([]byte(sourcesJSON), &sources) != nil {
		return qualificationError("invalid_qualification_sources")
	}
	if valueInt(settings["speed_candidates_per_source"], 0) > 0 {
		settings["speed_candidates"] = settings["speed_candidates_per_source"]
	}
	backend := &mobileQualificationBackend{settings: settings, observer: observer}
	result, err := qualification.Qualify(context.Background(), pool, proxies, settings, sources, backend, time.Now)
	if err != nil {
		return qualificationError(err.Error())
	}
	tests := make([]qualificationProbe, 0, len(result.Proxies))
	for _, proxy := range result.Proxies {
		name := fmt.Sprint(proxy["name"])
		metric := result.Metrics[name]
		tests = append(tests, qualificationProbe{Name: name, Alive: true, DelayMS: metric.DelayMS,
			SpeedMbps: metric.SpeedMbps, StabilityRatio: metric.StabilityRatio, Country: metric.Country})
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"proxies": result.Proxies, "tests": tests, "report": result.Report, "metrics": result.Metrics}})
}

func (backend *mobileQualificationBackend) TCP(ctx context.Context, proxies []map[string]any) ([]qualification.Latency, error) {
	probes, err := backend.probeBatches(ctx, "tcp", proxies, 128, func(payload string) string {
		return activeTransport.TestTCP(payload, valueInt(backend.settings["tcp_timeout_ms"], 2000), 128)
	})
	if err != nil {
		return nil, err
	}
	result := make([]qualification.Latency, len(probes))
	for index, probe := range probes {
		result[index] = qualification.Latency{Alive: probe.Alive, Seconds: float64(probe.DelayMS) / 1000}
	}
	return result, nil
}

func (backend *mobileQualificationBackend) URL(ctx context.Context, proxies []map[string]any) ([]qualification.Latency, error) {
	urls, _ := json.Marshal(backend.settings["url_test_urls"])
	probes, err := backend.probeBatches(ctx, "url_test", proxies, 80, func(payload string) string {
		return activeTransport.TestProxiesMulti(payload, string(urls), valueInt(backend.settings["url_timeout_ms"], 3000), 80)
	})
	if err != nil {
		return nil, err
	}
	result := make([]qualification.Latency, len(probes))
	for index, probe := range probes {
		result[index] = qualification.Latency{Alive: probe.Alive, Seconds: float64(probe.DelayMS) / 1000}
	}
	return result, nil
}

func (backend *mobileQualificationBackend) Geo(ctx context.Context, proxies []map[string]any) ([]qualification.GeoEvidence, error) {
	excluded, _ := json.Marshal(backend.settings["excluded_countries"])
	probes, err := backend.probeBatches(ctx, "geo", proxies, 32, func(payload string) string {
		return activeTransport.FilterCountries(payload, string(excluded), valueInt(backend.settings["geo_timeout_ms"], 5000), 12)
	})
	if err != nil {
		return nil, err
	}
	result := make([]qualification.GeoEvidence, len(probes))
	for index, probe := range probes {
		result[index] = qualification.GeoEvidence{OK: probe.Alive, Country: probe.Country, Status: probe.Error}
	}
	return result, nil
}

func (backend *mobileQualificationBackend) Baseline(context.Context) (qualification.Download, error) {
	if backend.baselineOK {
		return backend.baseline, nil
	}
	backend.progress("baseline", 0, 1)
	var envelope qualificationEnvelope
	if err := json.Unmarshal([]byte(activeTransport.SpeedAvailable(mobileSpeedURL, 8000)), &envelope); err != nil || !envelope.OK || !envelope.Result.Available || envelope.Result.Baseline <= 0 {
		backend.progress("baseline", 1, 1)
		return qualification.Download{}, fmt.Errorf("speed_endpoint_unavailable")
	}
	bytesPerSecond := envelope.Result.Baseline * 1_000_000 / 8
	backend.speedBytes = envelope.Result.Bytes
	backend.baseline = qualification.Download{OK: true, HTTPCode: 206, Bytes: qualification.SpeedBytes,
		ExpectedBytes: qualification.SpeedBytes, BytesPerSecond: bytesPerSecond}
	backend.baselineOK = true
	backend.progress("baseline", 1, 1)
	return backend.baseline, nil
}

func (backend *mobileQualificationBackend) SetSpeedBytes(value int64) { backend.speedBytes = value }

func (backend *mobileQualificationBackend) Speed(ctx context.Context, proxies []map[string]any) ([]qualification.SpeedEvidence, error) {
	minimum := valueFloat64(backend.settings["min_speed_mbps"], 10)
	stability := valueFloat64(backend.settings["stability_ratio"], .65)
	probes, err := backend.probeBatches(ctx, "speed_test", proxies, 6, func(payload string) string {
		return activeTransport.TestSpeedAdaptive(payload, mobileSpeedURL, valueInt(backend.settings["speed_timeout_ms"], 15000), 6, minimum, stability, backend.speedBytes)
	})
	if err != nil {
		return nil, err
	}
	result := make([]qualification.SpeedEvidence, len(probes))
	for index, probe := range probes {
		evidence := qualification.SpeedEvidence{CoreOK: probe.SpeedMbps > 0 || probe.Alive}
		if evidence.CoreOK {
			slowest := probe.SpeedMbps * 1_000_000 / 8
			fastest := slowest
			if probe.StabilityRatio > 0 {
				fastest = slowest / probe.StabilityRatio
			}
			expected := backend.speedBytes
			if expected <= 0 {
				expected = qualification.MinSpeedSampleBytes
			}
			evidence.Downloads = []qualification.Download{
				{OK: true, HTTPCode: 206, Bytes: expected, ExpectedBytes: expected, BytesPerSecond: slowest},
				{OK: true, HTTPCode: 206, Bytes: expected, ExpectedBytes: expected, BytesPerSecond: fastest},
			}
		}
		result[index] = evidence
	}
	return result, nil
}

func (backend *mobileQualificationBackend) probeBatches(ctx context.Context, stage string, proxies []map[string]any, batchSize int, run func(string) string) ([]qualificationProbe, error) {
	result := make([]qualificationProbe, 0, len(proxies))
	backend.progress(stage, 0, len(proxies))
	for offset := 0; offset < len(proxies); offset += batchSize {
		if err := backend.cancelled(ctx); err != nil {
			return nil, err
		}
		end := offset + batchSize
		if end > len(proxies) {
			end = len(proxies)
		}
		payload, _ := json.Marshal(proxies[offset:end])
		var envelope qualificationEnvelope
		if err := json.Unmarshal([]byte(run(string(payload))), &envelope); err != nil || !envelope.OK {
			return nil, fmt.Errorf("%s_failed", stage)
		}
		if len(envelope.Result.Nodes) != end-offset {
			return nil, fmt.Errorf("%s_result_count_mismatch", stage)
		}
		result = append(result, envelope.Result.Nodes...)
		backend.progress(stage, end, len(proxies))
	}
	return result, nil
}

func (backend *mobileQualificationBackend) cancelled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if backend.observer != nil && backend.observer.IsCancelled() {
		return context.Canceled
	}
	return nil
}

func (backend *mobileQualificationBackend) progress(stage string, current, total int) {
	if backend.observer != nil {
		backend.observer.OnProgress(stage, current, total)
	}
}

func valueInt(value any, fallback int) int {
	switch current := value.(type) {
	case float64:
		return int(current)
	case int:
		return current
	default:
		return fallback
	}
}

func valueFloat64(value any, fallback float64) float64 {
	if current, ok := value.(float64); ok && !math.IsNaN(current) && !math.IsInf(current, 0) {
		return current
	}
	return fallback
}

func valueBool(value any) bool {
	current, _ := value.(bool)
	return current
}

func qualificationError(message string) string {
	return encode(map[string]any{"ok": false, "error": map[string]string{"error": message}})
}
