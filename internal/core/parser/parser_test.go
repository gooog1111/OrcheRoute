package parser

import (
	"encoding/base64"
	"testing"
)

func TestDecodeSubscriptionBody(t *testing.T) {
	want := "vless://id@example.com:443?type=tcp&security=tls#node"
	encoded := base64.StdEncoding.EncodeToString([]byte(want))
	links := DecodeSubscriptionBody(encoded)
	if len(links) != 1 || links[0] != want {
		t.Fatalf("decoded links = %#v", links)
	}
}

func TestParseSubscriptionRejectsMissingSource(t *testing.T) {
	if _, err := ParseSubscription([]string{"vless://ignored"}, ""); err == nil || err.Error() != "invalid_request" {
		t.Fatalf("error = %v, want invalid_request", err)
	}
}

func TestIsShareLinkCoversSupportedMobileProtocols(t *testing.T) {
	for _, value := range []string{"vless://node", "hy2://node", "wg://node", "amneziawg://node"} {
		if !IsShareLink(value) {
			t.Errorf("%q was not recognized", value)
		}
	}
	if IsShareLink("https://example.com/subscription") {
		t.Fatal("HTTP subscription was treated as an inline share link")
	}
}
