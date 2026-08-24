package serverruntime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanRuntimeReturnsEmptyPoolsWhenTransportIsNotStarted(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("api_token=test-token\ncontroller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory = directory
	config.ProductionState = directory
	config.ConfigDirectory = directory
	config.RuntimeEnv = runtimeEnv
	config.MihomoAPI = "http://127.0.0.1:1"
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	for _, path := range []string{
		filepath.Join(directory, "providers", "primary.json"),
		filepath.Join(directory, "providers", "emergency.json"),
		filepath.Join(directory, "rules", "direct.txt"),
		filepath.Join(directory, "rules", "proxy.txt"),
		filepath.Join(directory, "rules", "block.txt"),
		filepath.Join(directory, "routes.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("clean runtime did not seed %s: %v", path, err)
		}
	}

	for _, path := range []string{"/v1/pools", "/v1/nodes"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer test-token")
		response := httptest.NewRecorder()
		runtime.APIHandler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if available, _ := payload["transport_available"].(bool); available {
			t.Fatalf("%s reported unavailable transport as available", path)
		}
	}
}

func TestBootstrapDoesNotOverwriteExistingRouteProvider(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("controller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rulesDirectory := filepath.Join(directory, "rules")
	if err := os.MkdirAll(rulesDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	directPath := filepath.Join(rulesDirectory, "direct.txt")
	const existing = "DOMAIN-SUFFIX,example.test\n"
	if err := os.WriteFile(directPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	config.RequireAPIAuth = false
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	payload, err := os.ReadFile(directPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != existing {
		t.Fatalf("bootstrap overwrote existing route provider: %q", payload)
	}
}

func TestRoutesSaveSucceedsWhileTransportIsStopped(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("api_token=test-token\ncontroller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	config.MihomoAPI = "http://127.0.0.1:1"
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	body := []byte(`{"revision":0,"default":"proxy","lists":{"direct":["*.example.test"],"proxy":[],"block":[]}}`)
	request := httptest.NewRequest(http.MethodPut, "/v1/routes", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	runtime.APIHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("routes save returned %d: %s", response.Code, response.Body.String())
	}
	var value map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value["updated"] != true || value["applied"] != false || value["apply_pending"] != true {
		t.Fatalf("unexpected stopped-transport response: %#v", value)
	}
	payload, err := os.ReadFile(filepath.Join(directory, "rules", "direct.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte("example.test")) {
		t.Fatalf("compiled route was not persisted: %s", payload)
	}
}

func TestLoopbackAPIWorksWithoutBearerToken(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("controller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	config.RequireAPIAuth = false
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	response := httptest.NewRecorder()
	runtime.APIHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("local API returned %d: %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["connectivity"] != "disabled" {
		t.Fatalf("clean desktop must be ready but disabled: %#v", payload)
	}
}

func TestComponentSourceCanBeSelectedAndReadBack(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("api_token=test-token\ncontroller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	config.MihomoBinary = filepath.Join(directory, "missing-mihomo")
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	body := []byte(`{"geo_auto_update":true,"geo_interval_hours":48,"geo_source":"runetfreedom"}`)
	request := httptest.NewRequest(http.MethodPut, "/v1/components/settings", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-OrcheRoute-UI", "1")
	response := httptest.NewRecorder()
	runtime.APIHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("settings returned %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/components", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	runtime.APIHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("components returned %d: %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["geo_source"] != "runetfreedom" || payload["interval_hours"] != float64(48) {
		t.Fatalf("component settings were not persisted: %#v", payload)
	}
}

func TestQualificationPolicyIsPersistedAndReadBack(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("api_token=test-token\ncontroller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	body := []byte(`{"defaults":{"excluded_countries":["DE"],"min_speed_mbps":7.5,"stability_ratio":0.8,"allowlist_probe_url":"https://ya.ru/","open_internet_probe_url":"https://www.gstatic.com/generate_204"},"pools":{"emergency":{"speed_candidates_per_source":42}}}`)
	request := httptest.NewRequest(http.MethodPut, "/v1/qualification/policy", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-OrcheRoute-UI", "1")
	response := httptest.NewRecorder()
	runtime.APIHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save policy returned %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/qualification", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	runtime.APIHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("read policy returned %d: %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	policy := payload["policy"].(map[string]any)
	defaults := policy["defaults"].(map[string]any)
	if defaults["min_speed_mbps"] != 7.5 || defaults["stability_ratio"] != 0.8 {
		t.Fatalf("saved policy was not returned: %#v", policy)
	}
	emergency := policy["pools"].(map[string]any)["emergency"].(map[string]any)
	if emergency["speed_candidates_per_source"] != float64(42) {
		t.Fatalf("saved emergency policy was not returned: %#v", emergency)
	}
	if _, err := os.Stat(filepath.Join(directory, "qualification-policy.json")); err != nil {
		t.Fatalf("policy file was not persisted: %v", err)
	}
}
