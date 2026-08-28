//go:build linux

package serverruntime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
)

func TestCallServerAPIIssuesSecretFreePublicStateAndClientProfile(t *testing.T) {
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

	settings := map[string]any{
		"version": 1, "enabled": false, "listen_address": "0.0.0.0:4443",
		"public_endpoint": "203.0.113.25:4443", "backend_address": "127.0.0.1:18443",
		"invitation_url": "https://vk.com/call/join/test-invite", "subscription_base_url": "https://vpn.example",
	}
	callReverseVPNAPI(t, runtime, http.MethodPut, "/v1/call-server", settings, http.StatusOK)
	created := callReverseVPNAPI(t, runtime, http.MethodPost, "/v1/call-server/clients", map[string]any{"name": "Phone", "traffic_limit_bytes": 1024}, http.StatusCreated)
	client := created["client"].(map[string]any)
	path := created["subscription_path"].(string)
	if !strings.HasPrefix(path, "/subscription/call/") {
		t.Fatalf("unexpected subscription path: %s", path)
	}

	read := callReverseVPNAPI(t, runtime, http.MethodGet, "/v1/call-server", nil, http.StatusOK)
	encoded, _ := json.Marshal(read)
	for _, secretName := range []string{"invitation_url", "subscription_token", "psk", "vless_uuid"} {
		if bytes.Contains(encoded, []byte(secretName)) {
			t.Fatalf("GET exposed %s: %s", secretName, encoded)
		}
	}
	profileResponse := callReverseVPNAPI(t, runtime, http.MethodGet, "/v1/call-server/clients/"+client["id"].(string)+"/profile", nil, http.StatusOK)
	profileURI := profileResponse["profile"].(string)
	if _, err := callprofile.Decode(profileURI, time.Now()); err != nil {
		t.Fatalf("API returned invalid call profile: %v", err)
	}

	subscriptionRequest := httptest.NewRequest(http.MethodGet, path, nil)
	subscriptionResponse := httptest.NewRecorder()
	runtime.WebHandler().ServeHTTP(subscriptionResponse, subscriptionRequest)
	if subscriptionResponse.Code != http.StatusOK || strings.TrimSpace(subscriptionResponse.Body.String()) != profileURI {
		t.Fatalf("public subscription returned %d: %s", subscriptionResponse.Code, subscriptionResponse.Body.String())
	}
	if subscriptionResponse.Header().Get("Subscription-Userinfo") == "" {
		t.Fatal("subscription traffic metadata missing")
	}
}
