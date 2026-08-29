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

func TestNormalizeInlineDeduplicatesWithoutChangingOrder(t *testing.T) {
	value, duplicates := NormalizeInline("vless://one\nvless://one\ntrojan://two")
	if duplicates != 1 || value != "vless://one\ntrojan://two" {
		t.Fatalf("value=%q duplicates=%d", value, duplicates)
	}
}

func TestParseSubscriptionRejectsMissingSource(t *testing.T) {
	if _, err := ParseSubscription([]string{"vless://ignored"}, ""); err == nil || err.Error() != "invalid_request" {
		t.Fatalf("error = %v, want invalid_request", err)
	}
}

func TestIsShareLinkCoversSupportedMobileProtocols(t *testing.T) {
	for _, value := range []string{"vless://node", "hy2://node", "wg://node", "amneziawg://node", "orcheroute://call/profile"} {
		if !IsShareLink(value) {
			t.Errorf("%q was not recognized", value)
		}
	}
	if IsShareLink("https://example.com/subscription") {
		t.Fatal("HTTP subscription was treated as an inline share link")
	}
}
