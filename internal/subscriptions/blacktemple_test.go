package subscriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBlackTempleFetcherAuthenticatesAndCachesCredentials(t *testing.T) {
	var mutex sync.Mutex
	authCalls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-App-Id") == "" || request.Header.Get("X-Signature") == "" || request.Header.Get("X-Timestamp") == "" {
			t.Errorf("missing signed headers for %s", request.URL.Path)
		}
		switch {
		case request.URL.Path == "/api/auth":
			mutex.Lock()
			authCalls++
			mutex.Unlock()
			_ = json.NewEncoder(response).Encode(map[string]string{"jwt": "bearer", "confirm": "refresh"})
		case strings.HasPrefix(request.URL.Path, "/api/refresh/"):
			_ = json.NewEncoder(response).Encode(map[string]any{"ok": true})
		case strings.HasPrefix(request.URL.Path, "/sub/"):
			_ = json.NewEncoder(response).Encode(map[string]any{"sub": map[string]any{"servers": []any{map[string]any{"key": "vless://id@example.test:443"}, map[string]any{"key": "vless://id@example.test:443"}}}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "credentials.json")
	fetcher := BlackTempleFetcher{Client: server.Client(), Bases: []string{server.URL}, CredentialsPath: path, Now: func() time.Time { return time.Unix(1000, 0) }}
	subscription := Subscription{Parser: BlackTemple, Secret: "https://provider.example/sub/token-value"}
	for index := 0; index < 2; index++ {
		links, err := fetcher.Fetch(context.Background(), subscription)
		if err != nil || len(links) != 1 {
			t.Fatalf("fetch %d: %#v %v", index, links, err)
		}
	}
	if authCalls != 1 {
		t.Fatalf("credentials were not reused: auth calls=%d", authCalls)
	}
}

func TestBlackTempleSignaturesMatchProtocolReference(t *testing.T) {
	if got := blackTempleSignature("GET", "/sub/token", "1000", "app"); got != "26fb9e0313ce8617c94e464dc72fed340a650ffcb5861a2697a70c548833d064" {
		t.Fatalf("signature changed: %s", got)
	}
}

func TestNormalizeBlackTempleAppLinks(t *testing.T) {
	for input, expected := range map[string]string{
		"blacktemple://token-value":                          "token-value",
		"blacktemple://import/token-value":                   "token-value",
		"blacktemple://import?token=token-value":             "token-value",
		"blacktemple://import?url=https%3A%2F%2Fx%2Fsub%2Ft": "t",
	} {
		if actual := normalizeSubscriptionToken(input); actual != expected {
			t.Errorf("normalizeSubscriptionToken(%q) = %q, want %q", input, actual, expected)
		}
	}
}
