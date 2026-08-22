package qualification

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var countryCodeRE = regexp.MustCompile(`^[A-Z]{2}$`)

var Pools = []string{"primary", "emergency"}

func DefaultPolicy() map[string]any {
	return map[string]any{
		"version": float64(1),
		"defaults": map[string]any{
			"excluded_countries": []any{},
			"min_speed_mbps":     float64(10),
			"stability_ratio":    0.65,
			"tcp_timeout_ms":     float64(2000),
			"url_timeout_ms":     float64(3000),
			"geo_timeout_ms":     float64(5000),
			"speed_timeout_ms":   float64(15000),
			"url_test_urls": []any{
				"https://www.gstatic.com/generate_204",
				"https://cp.cloudflare.com/generate_204",
				"https://www.msftconnecttest.com/connecttest.txt",
			},
			"allowlist_probe_url":     "https://ya.ru/",
			"open_internet_probe_url": "https://www.cloudflare.com/cdn-cgi/trace",
		},
		"pools": map[string]any{
			"primary":   map[string]any{"url_limit": float64(0), "speed_candidates": float64(0), "speed_candidates_per_source": float64(0), "keep": float64(0)},
			"emergency": map[string]any{"url_limit": float64(1200), "speed_candidates": float64(60), "speed_candidates_per_source": float64(100), "keep": float64(20)},
		},
	}
}

type ValidationError struct{ Code string }

func (err *ValidationError) Error() string { return err.Code }

func Validate(policy map[string]any) (map[string]any, error) {
	result, err := clone(policy)
	if err != nil || !valueEqualsOne(result["version"]) {
		return nil, validation("unsupported_policy_version")
	}
	defaults, okDefaults := result["defaults"].(map[string]any)
	pools, okPools := result["pools"].(map[string]any)
	if !okDefaults || !okPools {
		return nil, validation("invalid_policy_shape")
	}
	countries, ok := anySlice(defaults["excluded_countries"])
	if !ok || len(countries) > 64 {
		return nil, validation("invalid_excluded_countries")
	}
	normalized := []any{}
	seen := map[string]bool{}
	for _, country := range countries {
		code := strings.ToUpper(strings.TrimSpace(valueString(country)))
		if !countryCodeRE.MatchString(code) {
			return nil, validation("invalid_country_code")
		}
		if !seen[code] {
			seen[code] = true
			normalized = append(normalized, code)
		}
	}
	defaults["excluded_countries"] = normalized
	speed, err := valueFloat(valueOr(defaults, "min_speed_mbps", float64(0)))
	if err != nil || speed < 0.1 || speed > 10000 || math.IsNaN(speed) {
		return nil, validation("min_speed_mbps_out_of_range")
	}
	defaults["min_speed_mbps"] = speed
	ratio, err := valueFloat(valueOr(defaults, "stability_ratio", float64(0)))
	if err != nil || ratio < 0.1 || ratio > 1 || math.IsNaN(ratio) {
		return nil, validation("stability_ratio_out_of_range")
	}
	defaults["stability_ratio"] = ratio
	timeoutRanges := map[string][2]int{
		"tcp_timeout_ms":   {500, 10000},
		"url_timeout_ms":   {1000, 30000},
		"geo_timeout_ms":   {1000, 15000},
		"speed_timeout_ms": {5000, 120000},
	}
	defaultValues := DefaultPolicy()["defaults"].(map[string]any)
	for key, limits := range timeoutRanges {
		value, conversionErr := valueInt(valueOr(defaults, key, defaultValues[key]))
		if conversionErr != nil {
			return nil, validation(key + "_invalid")
		}
		if value < limits[0] || value > limits[1] {
			return nil, validation(key + "_out_of_range")
		}
		defaults[key] = float64(value)
	}
	testURLs, ok := anySlice(valueOr(defaults, "url_test_urls", defaultValues["url_test_urls"]))
	if !ok || len(testURLs) == 0 || len(testURLs) > 16 {
		return nil, validation("invalid_url_test_urls")
	}
	normalizedURLs := make([]any, 0, len(testURLs))
	seenURLs := map[string]bool{}
	for _, raw := range testURLs {
		value := strings.TrimSpace(valueString(raw))
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return nil, validation("invalid_url_test_url")
		}
		if !seenURLs[value] {
			seenURLs[value] = true
			normalizedURLs = append(normalizedURLs, value)
		}
	}
	if len(normalizedURLs) == 0 {
		return nil, validation("invalid_url_test_urls")
	}
	defaults["url_test_urls"] = normalizedURLs
	for _, key := range []string{"allowlist_probe_url", "open_internet_probe_url"} {
		value := strings.TrimSpace(valueString(valueOr(defaults, key, defaultValues[key])))
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return nil, validation("invalid_" + key)
		}
		defaults[key] = value
	}

	for _, pool := range Pools {
		settings, ok := pools[pool].(map[string]any)
		if !ok {
			return nil, validation("missing_pool_policy")
		}
		for _, key := range []string{"url_limit", "speed_candidates", "speed_candidates_per_source", "keep"} {
			fallback := float64(0)
			if key == "speed_candidates_per_source" && pool == "emergency" {
				fallback = float64(100)
			}
			value, conversionErr := valueInt(valueOr(settings, key, fallback))
			if conversionErr != nil {
				return nil, validation(key + "_invalid")
			}
			if value < 0 || value > 10000 {
				return nil, validation(key + "_out_of_range")
			}
			settings[key] = float64(value)
		}
		if override, exists := settings["excluded_countries"]; exists && override != nil {
			values, valid := anySlice(override)
			if !valid {
				return nil, validation("invalid_pool_countries")
			}
			normalizedOverride := make([]any, 0, len(values))
			for _, value := range values {
				code := strings.ToUpper(strings.TrimSpace(valueString(value)))
				if !countryCodeRE.MatchString(code) {
					return nil, validation("invalid_pool_country_code")
				}
				normalizedOverride = append(normalizedOverride, code)
			}
			settings["excluded_countries"] = normalizedOverride
		}
		if value, exists := settings["min_speed_mbps"]; exists && value != nil {
			current, conversionErr := valueFloat(value)
			if conversionErr != nil || current < 0.1 || current > 10000 || math.IsNaN(current) {
				return nil, validation("pool_min_speed_mbps_out_of_range")
			}
			settings["min_speed_mbps"] = current
		}
		if value, exists := settings["stability_ratio"]; exists && value != nil {
			current, conversionErr := valueFloat(value)
			if conversionErr != nil || current < 0.1 || current > 1 || math.IsNaN(current) {
				return nil, validation("pool_stability_ratio_out_of_range")
			}
			settings["stability_ratio"] = current
		}
	}
	return result, nil
}

func Update(policy, changes map[string]any) (map[string]any, error) {
	current, err := Validate(policy)
	if err != nil {
		return nil, err
	}
	if defaultsChange, exists := changes["defaults"]; exists {
		values, ok := defaultsChange.(map[string]any)
		if !ok {
			return nil, validation("invalid_defaults_update")
		}
		merge(current["defaults"].(map[string]any), values)
	}
	if poolsChange, exists := changes["pools"]; exists {
		values, ok := poolsChange.(map[string]any)
		if !ok {
			return nil, validation("invalid_pools_update")
		}
		currentPools := current["pools"].(map[string]any)
		for pool, raw := range values {
			settings, valid := raw.(map[string]any)
			if !contains(Pools, pool) || !valid {
				return nil, validation("invalid_pool_update")
			}
			merge(currentPools[pool].(map[string]any), settings)
		}
	}
	return Validate(current)
}

func Effective(policy map[string]any, pool string) (map[string]any, error) {
	validated, err := Validate(policy)
	if err != nil {
		return nil, err
	}
	if !contains(Pools, pool) {
		return nil, validation("invalid_pool")
	}
	result := map[string]any{}
	merge(result, validated["defaults"].(map[string]any))
	for key, value := range validated["pools"].(map[string]any)[pool].(map[string]any) {
		if value != nil {
			result[key] = value
		}
	}
	return result, nil
}

// URLTestURLs returns the normalized global URL-test list from a validated
// policy. Qualification backends use the same list on every platform.
func URLTestURLs(policy map[string]any) ([]string, error) {
	validated, err := Validate(policy)
	if err != nil {
		return nil, err
	}
	values, _ := anySlice(validated["defaults"].(map[string]any)["url_test_urls"])
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, valueString(value))
	}
	return result, nil
}

func MigrateLegacyPools(policy map[string]any) map[string]any {
	result, err := clone(policy)
	if err != nil {
		return policy
	}
	pools, ok := result["pools"].(map[string]any)
	if !ok || len(pools) != 3 || pools["new"] == nil || pools["legacy"] == nil || pools["ebrasha"] == nil {
		return result
	}
	result["pools"] = map[string]any{"primary": pools["new"], "emergency": pools["ebrasha"]}
	return result
}

func validation(code string) error { return &ValidationError{Code: code} }
func merge(target, values map[string]any) {
	for key, value := range values {
		target[key] = value
	}
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func valueOr(values map[string]any, key string, fallback any) any {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}

func clone(value map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(payload, &result)
	return result, err
}

func anySlice(value any) ([]any, bool) {
	switch current := value.(type) {
	case []any:
		return current, true
	case []string:
		result := make([]any, len(current))
		for index, item := range current {
			result[index] = item
		}
		return result, true
	default:
		return nil, false
	}
}

func valueEqualsOne(value any) bool {
	switch current := value.(type) {
	case bool:
		return current
	case float64:
		return current == 1
	case int:
		return current == 1
	default:
		return false
	}
}

func valueString(value any) string {
	switch current := value.(type) {
	case nil:
		return "None"
	case bool:
		if current {
			return "True"
		}
		return "False"
	case string:
		return current
	default:
		return fmt.Sprint(current)
	}
}

func valueFloat(value any) (float64, error) {
	switch current := value.(type) {
	case float64:
		return current, nil
	case float32:
		return float64(current), nil
	case int:
		return float64(current), nil
	case bool:
		if current {
			return 1, nil
		}
		return 0, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(current), 64)
	default:
		return 0, fmt.Errorf("invalid float")
	}
}

func valueInt(value any) (int, error) {
	switch current := value.(type) {
	case float64:
		if math.IsNaN(current) || math.IsInf(current, 0) {
			return 0, fmt.Errorf("invalid int")
		}
		return int(current), nil
	case int:
		return current, nil
	case bool:
		if current {
			return 1, nil
		}
		return 0, nil
	case string:
		return strconv.Atoi(strings.TrimSpace(current))
	default:
		return 0, fmt.Errorf("invalid int")
	}
}
