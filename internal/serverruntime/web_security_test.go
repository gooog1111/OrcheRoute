package serverruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

func TestWebMutationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, origin, site, content string
		want                        int
	}{
		{"same origin", "https://panel.example", "same-origin", "application/json; charset=utf-8", 401},
		{"CLI", "", "", "application/json", 401},
		{"foreign origin", "https://attacker.example", "", "application/json", 403},
		{"null origin", "null", "", "application/json", 403},
		{"cross site", "", "cross-site", "application/json", 403},
		{"same site subdomain", "", "same-site", "application/json", 403},
		{"simple request", "", "", "text/plain", 415},
		{"missing content type", "", "", "", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &Runtime{Config: Config{RuntimeEnv: t.TempDir() + "/missing"}}
			req := httptest.NewRequest(http.MethodPost, "http://panel.example/api/v1/whitelist/transition", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.site)
			req.Header.Set("Content-Type", tc.content)
			res := httptest.NewRecorder()
			runtime.WebHandler().ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d want %d", res.Code, tc.want)
			}
		})
	}
}

func TestAuthenticatedWebMutationRejectsForeignOriginWithoutBreakingUI(t *testing.T) {
	dir := t.TempDir()
	salt := []byte("test-only-salt")
	hash := hex.EncodeToString(pbkdf2.Key([]byte("test-only-password"), salt, 100, 32, sha256.New))
	env := filepath.Join(dir, "runtime.env")
	if err := os.WriteFile(env, []byte("webui_username=test\nwebui_password_hash=pbkdf2_sha256$100$"+hex.EncodeToString(salt)+"$"+hash+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{Config: Config{StateDirectory: dir, RuntimeEnv: env, RequireAPIAuth: true}, apiToken: "test-only-token"}
	api := httptest.NewServer(runtime.APIHandler())
	defer api.Close()
	runtime.Config.Listen = strings.TrimPrefix(api.URL, "http://")
	runtime.client = api.Client()
	for _, tc := range []struct {
		origin string
		want   int
	}{
		{"https://attacker.example", 403}, {"https://panel.example", 200},
	} {
		req := httptest.NewRequest(http.MethodPost, "http://panel.example/api/v1/whitelist/transition", bytes.NewBufferString(`{"operation":"begin"}`))
		req.RemoteAddr = "127.0.0.1:1234"
		req.SetBasicAuth("test", "test-only-password")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", tc.origin)
		res := httptest.NewRecorder()
		runtime.WebHandler().ServeHTTP(res, req)
		if res.Code != tc.want || runtime.whitelistState().ScanActive != (tc.want == 200) {
			t.Fatalf("origin=%s status=%d body=%s", tc.origin, res.Code, res.Body.String())
		}
	}
}

func TestSubscriptionResponsesNeverCache(t *testing.T) {
	runtime := &Runtime{}
	res := httptest.NewRecorder()
	runtime.WebHandler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "http://panel.example/subscription/test", nil))
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("subscription response can be cached")
	}
}

func TestWebAuthSuccessfulPollingPreservesCredentials(t *testing.T) {
	dir := t.TempDir()
	salt := []byte("test-only-salt")
	hash := hex.EncodeToString(pbkdf2.Key([]byte("test-password"), salt, 100, 32, sha256.New))
	env := filepath.Join(dir, "runtime.env")
	before := []byte("webui_username=test\nwebui_password_hash=pbkdf2_sha256$100$" + hex.EncodeToString(salt) + "$" + hash + "\n")
	if err := os.WriteFile(env, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("test UI"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{Config: Config{RuntimeEnv: env, WebRoot: dir}}
	handler := runtime.WebHandler()
	for i := 0; i < 2*webAuthBurst; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://panel.example/", nil)
		req.RemoteAddr = "127.0.0.1:1000"
		req.SetBasicAuth("test", "test-password")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != 200 {
			t.Fatalf("valid request %d: status=%d", i, res.Code)
		}
	}
	after, err := os.ReadFile(env)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("authentication modified credentials")
	}
}
