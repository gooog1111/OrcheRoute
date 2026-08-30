//go:build linux

package serverruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/callserver"
	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
	"github.com/gooog1111/orcheroute/internal/core/connectivity"
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
	runtime.callServerProbe = func(context.Context) (callserver.ProviderProbeResult, error) {
		return callserver.ProviderProbeResult{Provider: "vk", TURNEndpoint: "turn.example:3478", Network: "udp", ExpiresAt: time.Now().Add(8 * time.Minute).Unix()}, nil
	}
	callServerAPI(t, runtime, http.MethodGet, "/v1/reverse-vpn", nil, http.StatusNotFound)

	settings := map[string]any{
		"version": 2, "enabled": false, "ordinary_enabled": false, "listen_address": freeServerAddress(t, "udp"),
		"public_endpoint": "203.0.113.25:4443", "backend_address": freeServerAddress(t, "tcp"),
		"invitation_url": "https://vk.com/call/join/test-invite", "subscription_base_url": "https://vpn.example",
	}
	callServerAPI(t, runtime, http.MethodPut, "/v1/call-server", settings, http.StatusOK)
	delete(settings, "invitation_url")
	callServerAPI(t, runtime, http.MethodPut, "/v1/call-server", settings, http.StatusOK)
	probe := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/test", map[string]any{}, http.StatusOK)
	if probe["ok"] != true || probe["result"].(map[string]any)["turn_endpoint"] != "turn.example:3478" {
		t.Fatalf("provider probe did not return connection evidence: %#v", probe)
	}
	runtime.callServerProbe = func(context.Context) (callserver.ProviderProbeResult, error) {
		return callserver.ProviderProbeResult{}, errors.New("call_transport_vk_client_identity_required")
	}
	probeFailure := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/test", map[string]any{}, http.StatusServiceUnavailable)
	if probeFailure["error"] != "call_transport_vk_client_identity_required" {
		t.Fatalf("provider error was hidden: %#v", probeFailure)
	}
	created := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/clients", map[string]any{"name": "Phone", "traffic_limit_bytes": 1024}, http.StatusCreated)
	client := created["client"].(map[string]any)
	path := created["subscription_path"].(string)
	if !strings.HasPrefix(path, "/subscription/") || strings.HasPrefix(path, "/subscription/call/") {
		t.Fatalf("unexpected subscription path: %s", path)
	}

	read := callServerAPI(t, runtime, http.MethodGet, "/v1/call-server", nil, http.StatusOK)
	encoded, _ := json.Marshal(read)
	for _, secretName := range []string{"invitation_url", "subscription_token", "psk", "vless_uuid"} {
		if bytes.Contains(encoded, []byte(secretName)) {
			t.Fatalf("GET exposed %s: %s", secretName, encoded)
		}
	}
	profileResponse := callServerAPI(t, runtime, http.MethodGet, "/v1/call-server/clients/"+client["id"].(string)+"/profile", nil, http.StatusOK)
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
	if disposition := subscriptionResponse.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "inline") {
		t.Fatalf("subscription is not browser-readable: %q", disposition)
	}
	legacyRequest := httptest.NewRequest(http.MethodGet, strings.Replace(path, "/subscription/", "/subscription/call/", 1), nil)
	legacyResponse := httptest.NewRecorder()
	runtime.WebHandler().ServeHTTP(legacyResponse, legacyRequest)
	if legacyResponse.Code != http.StatusOK || legacyResponse.Body.String() != subscriptionResponse.Body.String() {
		t.Fatalf("legacy subscription URL is no longer compatible: %d %s", legacyResponse.Code, legacyResponse.Body.String())
	}
	callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/apply", map[string]any{}, http.StatusOK)
	active := callServerAPI(t, runtime, http.MethodGet, "/v1/call-server", nil, http.StatusOK)
	if !active["status"].(map[string]any)["active"].(bool) {
		t.Fatal("embedded call server did not become active")
	}
	callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/disable", map[string]any{}, http.StatusOK)
}

func TestDefaultCallServerSourceIncludesVKApplicationIdentity(t *testing.T) {
	source := defaultCallServerSource()
	if source.Identity.ID == "" || source.Identity.Secret == "" || source.Name == "" {
		t.Fatalf("default server VK source is incomplete: %#v", source)
	}
}

func TestCallServerAutoConfigureUsesDirectIPAndTreatsDomainAsOptional(t *testing.T) {
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
	if err := atomicJSON(runtime.identityPath(), IdentitySnapshot{Direct: &connectivity.Identity{IP: "8.8.8.8"}, UpdatedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}

	result := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/auto-configure", map[string]any{"browser_origin": "http://192.168.1.5:19110"}, http.StatusOK)
	configured := result["config"].(map[string]any)
	if configured["public_endpoint"] != "8.8.8.8:4443" || configured["listen_address"] != "0.0.0.0:4443" || configured["backend_address"] != "127.0.0.1:18443" {
		t.Fatalf("unexpected automatic config: %#v", configured)
	}
	if value, exists := configured["subscription_base_url"]; exists && value != "" {
		t.Fatalf("domain must stay optional, got %#v", value)
	}

	httpsResult := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/auto-configure", map[string]any{"browser_origin": "https://vpn.example/settings?ignored=1"}, http.StatusOK)
	httpsConfig := httpsResult["config"].(map[string]any)
	if httpsConfig["subscription_base_url"] != "https://vpn.example" {
		t.Fatalf("HTTPS origin was not normalized: %#v", httpsConfig)
	}
	preserved := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/auto-configure", map[string]any{"browser_origin": "http://192.168.1.5:19110"}, http.StatusOK)
	if preserved["config"].(map[string]any)["subscription_base_url"] != "https://vpn.example" {
		t.Fatalf("LAN setup erased an existing HTTPS address: %#v", preserved)
	}
	if err := atomicJSON(runtime.identityPath(), IdentitySnapshot{Direct: &connectivity.Identity{IP: "8.8.8.8"}, UpdatedAt: time.Now().Add(-3 * time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	stale := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/auto-configure", map[string]any{}, http.StatusConflict)
	if stale["error"] != "call_server_direct_ip_stale" {
		t.Fatalf("stale Direct IP was accepted: %#v", stale)
	}
	if err := atomicJSON(runtime.identityPath(), IdentitySnapshot{Direct: &connectivity.Identity{IP: "192.168.1.10"}, UpdatedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	rejected := callServerAPI(t, runtime, http.MethodPost, "/v1/call-server/auto-configure", map[string]any{}, http.StatusConflict)
	if rejected["error"] != "call_server_direct_ip_not_public" {
		t.Fatalf("private Direct IP was not rejected: %#v", rejected)
	}
}

func callServerAPI(t *testing.T, runtime *Runtime, method, path string, body any, expected int) map[string]any {
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

func freeServerAddress(t *testing.T, network string) string {
	t.Helper()
	if network == "udp" {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.LocalAddr().String()
		_ = listener.Close()
		return address
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}
