package subscriptions

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInlineFetcherNormalizesDuplicateLinks(t *testing.T) {
	link := "vless://id@example.test:443?security=tls"
	links, err := (InlineFetcher{}).Fetch(context.Background(), Subscription{Parser: Inline, Secret: link + "\n" + link})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0] != link {
		t.Fatalf("unexpected inline links: %#v", links)
	}
}

func TestHTTPFetcher(t *testing.T) {
	body := base64.RawURLEncoding.EncodeToString([]byte("vless://id@example.test:443\n"))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.UserAgent() != "test-agent" {
			t.Errorf("unexpected agent %q", request.UserAgent())
		}
		_, _ = response.Write([]byte(body))
	}))
	defer server.Close()
	links, err := (HTTPFetcher{UserAgent: "test-agent"}).Fetch(context.Background(), Subscription{Parser: Standard, Secret: server.URL})
	if err != nil || len(links) != 1 {
		t.Fatalf("unexpected result: %#v %v", links, err)
	}
}

func TestWireGuardFetcher(t *testing.T) {
	config := "[Interface]\nAddress=10.0.0.2/32\nPrivateKey=private\n[Peer]\nPublicKey=public\nEndpoint=example.test:51820"
	links, err := (WireGuardFetcher{}).Fetch(context.Background(), Subscription{Parser: WireGuard, Secret: config})
	if err != nil || len(links) != 1 || !strings.HasPrefix(links[0], "wireguard://") {
		t.Fatalf("unexpected wireguard result: %#v %v", links, err)
	}
}
