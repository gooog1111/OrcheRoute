//go:build linux

package serverruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	coreparser "github.com/gooog1111/orcheroute/internal/core/parser"
	"github.com/gooog1111/orcheroute/internal/reversevpn"
)

type reverseVPNFakeAdapter struct{ active bool }

func (adapter *reverseVPNFakeAdapter) Apply(context.Context, reversevpn.Config) error {
	adapter.active = true
	return nil
}
func (adapter *reverseVPNFakeAdapter) Disable(context.Context, string) error {
	adapter.active = false
	return nil
}
func (adapter *reverseVPNFakeAdapter) Active(context.Context, string) bool { return adapter.active }

func TestReverseVPNAPIKeepsSecretsOutOfConfig(t *testing.T) {
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
	manager, err := reversevpn.Open(filepath.Join(directory, "reverse-api-test.json"), &reverseVPNFakeAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReverseVPN = manager

	settings := manager.PublicConfig()
	settings.PublicEndpoint = "vpn.example:51820"
	callReverseVPNAPI(t, runtime, http.MethodPut, "/v1/reverse-vpn", settings, http.StatusOK)
	created := callReverseVPNAPI(t, runtime, http.MethodPost, "/v1/reverse-vpn/clients", map[string]any{"name": "Phone"}, http.StatusCreated)
	client := created["client"].(map[string]any)
	if client["private_key"] == "" {
		t.Fatal("new client did not return its private key")
	}
	callReverseVPNAPI(t, runtime, http.MethodPost, "/v1/reverse-vpn/apply", map[string]any{}, http.StatusOK)

	read := callReverseVPNAPI(t, runtime, http.MethodGet, "/v1/reverse-vpn", nil, http.StatusOK)
	encoded, _ := json.Marshal(read)
	if bytes.Contains(encoded, []byte("private_key")) {
		t.Fatalf("GET exposed private key: %s", encoded)
	}
	profile := callReverseVPNAPI(t, runtime, http.MethodGet, "/v1/reverse-vpn/clients/"+client["id"].(string)+"/profile", nil, http.StatusOK)
	if profile["profile"] == "" {
		t.Fatal("client profile is empty")
	}
	subscriptionRequest := httptest.NewRequest(http.MethodGet, "/subscription/reverse/"+client["subscription_token"].(string), nil)
	subscriptionResponse := httptest.NewRecorder()
	runtime.WebHandler().ServeHTTP(subscriptionResponse, subscriptionRequest)
	if subscriptionResponse.Code != http.StatusOK || !bytes.Contains(subscriptionResponse.Body.Bytes(), []byte("[Interface]")) {
		t.Fatalf("public subscription returned %d: %s", subscriptionResponse.Code, subscriptionResponse.Body.String())
	}
	if subscriptionResponse.Header().Get("Subscription-Userinfo") == "" {
		t.Fatal("subscription traffic metadata missing")
	}
	links := coreparser.DecodeSubscriptionBody(subscriptionResponse.Body.String())
	if len(links) != 1 || !bytes.HasPrefix([]byte(links[0]), []byte("wireguard://")) {
		t.Fatalf("Android/shared subscription parser rejected generated profile: %#v", links)
	}
}

func callReverseVPNAPI(t *testing.T, runtime *Runtime, method, path string, body any, expected int) map[string]any {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	runtime.APIHandler().ServeHTTP(response, request)
	if response.Code != expected {
		t.Fatalf("%s %s returned %d: %s", method, path, response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
