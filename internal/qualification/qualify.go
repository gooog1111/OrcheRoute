package qualification

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const SpeedBytes = 10_485_760
const BaselineRatio = 0.10

type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Latency struct {
	Alive   bool    `json:"alive"`
	Seconds float64 `json:"seconds"`
}

type Download struct {
	OK             bool    `json:"ok"`
	HTTPCode       int     `json:"http_code"`
	Bytes          int64   `json:"bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`
}

type SpeedEvidence struct {
	CoreOK    bool       `json:"core_ok"`
	GeoStatus string     `json:"geo_status,omitempty"`
	Country   string     `json:"country,omitempty"`
	Downloads []Download `json:"downloads"`
}

type Measurement struct {
	Status         string  `json:"status"`
	BytesPerSecond float64 `json:"bytes_per_second"`
	Country        string  `json:"country,omitempty"`
}

type Backend interface {
	TCP(ctx context.Context, proxies []map[string]any) ([]Latency, error)
	URL(ctx context.Context, proxies []map[string]any) ([]Latency, error)
	Speed(ctx context.Context, proxies []map[string]any, geoEnabled bool) ([]SpeedEvidence, error)
}

type BaselineBackend interface {
	Baseline(ctx context.Context) (Download, error)
}

type SourceReport struct {
	Input       int            `json:"input"`
	TCPAlive    int            `json:"tcp_alive"`
	URLAlive    int            `json:"url_alive"`
	SpeedTested int            `json:"speed_tested"`
	Qualified   int            `json:"qualified"`
	Retained    int            `json:"retained"`
	Outcomes    map[string]int `json:"outcomes"`
}

type Report struct {
	Pool                string                   `json:"pool"`
	StartedAt           int64                    `json:"started_at"`
	FinishedAt          int64                    `json:"finished_at"`
	Input               int                      `json:"input"`
	TCPAlive            int                      `json:"tcp_alive"`
	URLAlive            int                      `json:"url_alive"`
	GeoEnabled          bool                     `json:"geo_enabled"`
	ExcludedCountries   []string                 `json:"excluded_countries"`
	ExcludedByCountry   map[string]int           `json:"excluded_by_country"`
	SpeedRuns           int                      `json:"speed_runs"`
	SpeedTested         int                      `json:"speed_tested"`
	Qualified           int                      `json:"qualified"`
	Retained            int                      `json:"retained"`
	ThresholdMbps       float64                  `json:"threshold_mbps"`
	BaselineMbps        float64                  `json:"baseline_mbps,omitempty"`
	BaselineRatio       float64                  `json:"baseline_ratio,omitempty"`
	ThresholdSource     string                   `json:"threshold_source"`
	StabilityRatio      float64                  `json:"stability_ratio"`
	FastestObservedMbps float64                  `json:"fastest_observed_mbps"`
	Outcomes            map[string]int           `json:"outcomes"`
	Sources             map[string]*SourceReport `json:"sources"`
}

type Result struct {
	Proxies []map[string]any `json:"proxies"`
	Report  Report           `json:"report"`
}

type rankedProxy struct {
	value float64
	proxy map[string]any
}

func Qualify(ctx context.Context, pool string, proxies []map[string]any, settings map[string]any, sourceByNode map[string]Source, backend Backend, now func() time.Time) (Result, error) {
	if backend == nil {
		return Result{}, fmt.Errorf("qualification backend is required")
	}
	if now == nil {
		now = time.Now
	}
	started := now().Unix()
	minimumMbps, err := valueFloat(settings["min_speed_mbps"])
	if err != nil {
		return Result{}, validation("invalid_min_speed_mbps")
	}
	thresholdSource := "configured_fallback"
	baselineMbps := float64(0)
	if provider, ok := backend.(BaselineBackend); ok {
		if baseline, baselineErr := provider.Baseline(ctx); baselineErr == nil && baseline.OK && baseline.BytesPerSecond > 0 {
			baselineMbps = baseline.BytesPerSecond * 8 / 1_000_000
			minimumMbps = baselineMbps * BaselineRatio
			thresholdSource = "wan_baseline"
		}
	}
	stabilityRatio, err := valueFloat(settings["stability_ratio"])
	if err != nil {
		return Result{}, validation("invalid_stability_ratio")
	}
	urlLimit, err := valueInt(valueOr(settings, "url_limit", float64(0)))
	if err != nil {
		return Result{}, validation("invalid_url_limit")
	}
	speedCandidates, err := valueInt(valueOr(settings, "speed_candidates", float64(0)))
	if err != nil {
		return Result{}, validation("invalid_speed_candidates")
	}
	keep, err := valueInt(valueOr(settings, "keep", float64(0)))
	if err != nil {
		return Result{}, validation("invalid_keep")
	}
	excludedValues, _ := anySlice(valueOr(settings, "excluded_countries", []any{}))
	excluded := map[string]bool{}
	for _, value := range excludedValues {
		excluded[strings.ToUpper(valueString(value))] = true
	}
	excludedList := make([]string, 0, len(excluded))
	for code := range excluded {
		excludedList = append(excludedList, code)
	}
	sort.Strings(excludedList)

	sourceReports := map[string]*SourceReport{}
	sourceID := func(proxy map[string]any) string {
		name, _ := proxy["name"].(string)
		if source, ok := sourceByNode[name]; ok && source.ID != "" {
			return source.ID
		}
		return "unknown"
	}
	getSourceReport := func(proxy map[string]any) *SourceReport {
		id := sourceID(proxy)
		if sourceReports[id] == nil {
			sourceReports[id] = &SourceReport{Outcomes: map[string]int{}}
		}
		return sourceReports[id]
	}
	for _, proxy := range proxies {
		getSourceReport(proxy).Input++
	}

	tcpResults, err := backend.TCP(ctx, proxies)
	if err != nil {
		return Result{}, err
	}
	if len(tcpResults) != len(proxies) {
		return Result{}, fmt.Errorf("tcp result count mismatch")
	}
	tcpAlive := []rankedProxy{}
	for index, result := range tcpResults {
		if result.Alive {
			tcpAlive = append(tcpAlive, rankedProxy{result.Seconds, proxies[index]})
		}
	}
	sort.SliceStable(tcpAlive, func(i, j int) bool { return tcpAlive[i].value < tcpAlive[j].value })
	for _, item := range tcpAlive {
		getSourceReport(item.proxy).TCPAlive++
	}

	urlSource := tcpAlive
	if urlLimit > 0 && len(urlSource) > urlLimit {
		urlSource = urlSource[:urlLimit]
	}
	urlProxies := proxiesFromRanked(urlSource)
	urlResults, err := backend.URL(ctx, urlProxies)
	if err != nil {
		return Result{}, err
	}
	if len(urlResults) != len(urlProxies) {
		return Result{}, fmt.Errorf("url result count mismatch")
	}
	urlAlive := []rankedProxy{}
	for index, result := range urlResults {
		if result.Alive {
			urlAlive = append(urlAlive, rankedProxy{result.Seconds, urlProxies[index]})
		}
	}
	sort.SliceStable(urlAlive, func(i, j int) bool { return urlAlive[i].value < urlAlive[j].value })
	for _, item := range urlAlive {
		getSourceReport(item.proxy).URLAlive++
	}

	speedSource := urlAlive
	if speedCandidates > 0 && len(speedSource) > speedCandidates {
		speedSource = speedSource[:speedCandidates]
	}
	speedProxies := proxiesFromRanked(speedSource)
	evidence, err := backend.Speed(ctx, speedProxies, len(excluded) > 0)
	if err != nil {
		return Result{}, err
	}
	if len(evidence) != len(speedProxies) {
		return Result{}, fmt.Errorf("speed result count mismatch")
	}
	threshold := int(minimumMbps * 1_000_000 / 8)
	measurements := make([]Measurement, len(evidence))
	for index, current := range evidence {
		measurements[index] = EvaluateSpeed(current, threshold, stabilityRatio, excluded)
	}
	outcomes := map[string]int{}
	excludedCounts := map[string]int{}
	fast := []rankedProxy{}
	fastest := float64(0)
	for index, measurement := range measurements {
		proxy := speedProxies[index]
		outcomes[measurement.Status]++
		if measurement.Status == "country_excluded" && measurement.Country != "" {
			excludedCounts[measurement.Country]++
		}
		source := getSourceReport(proxy)
		source.SpeedTested++
		source.Outcomes[measurement.Status]++
		if measurement.BytesPerSecond > fastest {
			fastest = measurement.BytesPerSecond
		}
		if measurement.Status == "qualified" {
			fast = append(fast, rankedProxy{measurement.BytesPerSecond, proxy})
		}
	}
	sort.SliceStable(fast, func(i, j int) bool { return fast[i].value > fast[j].value })
	for _, item := range fast {
		getSourceReport(item.proxy).Qualified++
	}
	retained := fast
	if keep > 0 && len(retained) > keep {
		retained = retained[:keep]
	}
	qualified := proxiesFromRanked(retained)
	for _, proxy := range qualified {
		getSourceReport(proxy).Retained++
	}

	report := Report{
		Pool: pool, StartedAt: started, FinishedAt: now().Unix(), Input: len(proxies), TCPAlive: len(tcpAlive), URLAlive: len(urlAlive),
		GeoEnabled: len(excluded) > 0, ExcludedCountries: excludedList, ExcludedByCountry: excludedCounts,
		SpeedRuns: 2, SpeedTested: len(measurements), Qualified: len(fast), Retained: len(qualified),
		ThresholdMbps: math.Round(minimumMbps*100) / 100, BaselineMbps: math.Round(baselineMbps*100) / 100,
		BaselineRatio: BaselineRatio, ThresholdSource: thresholdSource, StabilityRatio: stabilityRatio,
		FastestObservedMbps: math.Round(fastest*8/1_000_000*100) / 100,
		Outcomes:            outcomes, Sources: sourceReports,
	}
	return Result{Proxies: qualified, Report: report}, nil
}

func EvaluateSpeed(evidence SpeedEvidence, threshold int, stabilityRatio float64, excluded map[string]bool) Measurement {
	if !evidence.CoreOK {
		return Measurement{Status: "core_failed"}
	}
	country := strings.ToUpper(evidence.Country)
	if len(excluded) > 0 {
		if evidence.GeoStatus != "" {
			return Measurement{Status: evidence.GeoStatus, Country: country}
		}
		if country == "" {
			return Measurement{Status: "country_unknown"}
		}
		if excluded[country] {
			return Measurement{Status: "country_excluded", Country: country}
		}
	}
	speeds := []float64{}
	for _, download := range evidence.Downloads {
		fastest := maxFloat(speeds)
		if !download.OK {
			return Measurement{Status: "speed_failed", BytesPerSecond: fastest, Country: country}
		}
		if download.HTTPCode != 200 || download.Bytes < SpeedBytes {
			return Measurement{Status: "speed_incomplete", BytesPerSecond: fastest, Country: country}
		}
		if math.IsNaN(download.BytesPerSecond) || math.IsInf(download.BytesPerSecond, 0) {
			return Measurement{Status: "speed_invalid", BytesPerSecond: fastest, Country: country}
		}
		speeds = append(speeds, download.BytesPerSecond)
	}
	if len(speeds) != 2 {
		return Measurement{Status: "speed_invalid", BytesPerSecond: maxFloat(speeds), Country: country}
	}
	slowest, fastest := math.Min(speeds[0], speeds[1]), math.Max(speeds[0], speeds[1])
	if slowest < float64(threshold) {
		return Measurement{Status: "slow", BytesPerSecond: fastest, Country: country}
	}
	if fastest != 0 && slowest/fastest < stabilityRatio {
		return Measurement{Status: "unstable", BytesPerSecond: fastest, Country: country}
	}
	return Measurement{Status: "qualified", BytesPerSecond: slowest, Country: country}
}

func proxiesFromRanked(values []rankedProxy) []map[string]any {
	result := make([]map[string]any, len(values))
	for index, value := range values {
		result[index] = value.proxy
	}
	return result
}
func maxFloat(values []float64) float64 {
	result := float64(0)
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}
