package qualification

import "testing"

func TestDefaultPolicyDoesNotExcludeAnyCountry(t *testing.T) {
	defaults := DefaultPolicy()["defaults"].(map[string]any)
	if countries := defaults["excluded_countries"].([]any); len(countries) != 0 {
		t.Fatalf("default excluded countries = %#v, want empty", countries)
	}
}

func TestPolicyValidationAndEffectiveOverrides(t *testing.T) {
	policy := DefaultPolicy()
	changes := map[string]any{
		"defaults": map[string]any{"excluded_countries": []any{" ru ", "BY", "RU"}, "min_speed_mbps": "20"},
		"pools":    map[string]any{"emergency": map[string]any{"min_speed_mbps": float64(5), "excluded_countries": []any{}}},
	}
	updated, err := Update(policy, changes)
	if err != nil {
		t.Fatal(err)
	}
	defaults := updated["defaults"].(map[string]any)
	if defaults["min_speed_mbps"] != float64(20) || len(defaults["excluded_countries"].([]any)) != 2 {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}
	effective, err := Effective(updated, "emergency")
	if err != nil {
		t.Fatal(err)
	}
	if effective["min_speed_mbps"] != float64(5) || len(effective["excluded_countries"].([]any)) != 0 {
		t.Fatalf("unexpected effective policy: %#v", effective)
	}
}

func TestPolicyRejectsRanges(t *testing.T) {
	policy := DefaultPolicy()
	policy["defaults"].(map[string]any)["stability_ratio"] = float64(2)
	_, err := Validate(policy)
	if err == nil || err.Error() != "stability_ratio_out_of_range" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPolicyValidatesQualificationTimeouts(t *testing.T) {
	policy := DefaultPolicy()
	defaults := policy["defaults"].(map[string]any)
	want := map[string]float64{
		"tcp_timeout_ms": 2000, "url_timeout_ms": 3000,
		"geo_timeout_ms": 5000, "speed_timeout_ms": 15000,
	}
	for key, value := range want {
		if defaults[key] != value {
			t.Fatalf("%s = %#v, want %v", key, defaults[key], value)
		}
	}
	defaults["url_timeout_ms"] = float64(999)
	if _, err := Validate(policy); err == nil || err.Error() != "url_timeout_ms_out_of_range" {
		t.Fatalf("unexpected timeout validation: %v", err)
	}
}

func TestPolicyValidatesAndNormalizesURLTestURLs(t *testing.T) {
	policy := DefaultPolicy()
	defaults := policy["defaults"].(map[string]any)
	defaults["url_test_urls"] = []any{" https://one.example/ping ", "https://two.example/204", "https://one.example/ping"}
	validated, err := Validate(policy)
	if err != nil {
		t.Fatal(err)
	}
	urls := validated["defaults"].(map[string]any)["url_test_urls"].([]any)
	if len(urls) != 2 || urls[0] != "https://one.example/ping" || urls[1] != "https://two.example/204" {
		t.Fatalf("URL-test URLs = %#v", urls)
	}
	extracted, err := URLTestURLs(validated)
	if err != nil || len(extracted) != 2 || extracted[1] != "https://two.example/204" {
		t.Fatalf("extracted URL-test URLs = %#v, err=%v", extracted, err)
	}
	validated["defaults"].(map[string]any)["url_test_urls"] = []any{}
	if _, err := Validate(validated); err == nil || err.Error() != "invalid_url_test_urls" {
		t.Fatalf("unexpected empty URL list validation: %v", err)
	}
	validated["defaults"].(map[string]any)["url_test_urls"] = []any{"file:///etc/passwd"}
	if _, err := Validate(validated); err == nil || err.Error() != "invalid_url_test_url" {
		t.Fatalf("unexpected URL validation: %v", err)
	}
}

func TestPolicyValidatesConnectivityURLsAndEmergencyPerSourceLimit(t *testing.T) {
	policy := DefaultPolicy()
	validated, err := Validate(policy)
	if err != nil {
		t.Fatal(err)
	}
	defaults := validated["defaults"].(map[string]any)
	if defaults["allowlist_probe_url"] == "" || defaults["open_internet_probe_url"] == "" {
		t.Fatalf("probe URLs missing: %#v", defaults)
	}
	emergency := validated["pools"].(map[string]any)["emergency"].(map[string]any)
	if emergency["speed_candidates_per_source"] != float64(100) {
		t.Fatalf("per-source limit = %#v", emergency["speed_candidates_per_source"])
	}
	defaults["open_internet_probe_url"] = "file:///etc/passwd"
	if _, err := Validate(validated); err == nil || err.Error() != "invalid_open_internet_probe_url" {
		t.Fatalf("unexpected URL validation: %v", err)
	}
}

func TestLegacyPoolMigration(t *testing.T) {
	policy := DefaultPolicy()
	policy["pools"] = map[string]any{"new": map[string]any{"keep": 1}, "legacy": map[string]any{"keep": 2}, "ebrasha": map[string]any{"keep": 3}}
	migrated := MigrateLegacyPools(policy)
	pools := migrated["pools"].(map[string]any)
	if len(pools) != 2 || pools["primary"].(map[string]any)["keep"].(float64) != 1 || pools["emergency"].(map[string]any)["keep"].(float64) != 3 {
		t.Fatalf("unexpected migration: %#v", pools)
	}
}
