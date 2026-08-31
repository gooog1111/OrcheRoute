package mobilecore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
)

func TestBuildFreeTURNProfileConfig(t *testing.T) {
	now := time.Now()
	profile, err := callprofile.Encode(callprofile.Profile{
		Version:       callprofile.Version,
		Transport:     callprofile.Transport,
		Provider:      callprofile.Provider,
		Name:          "FreeTURN",
		PeerAddress:   "vpn.example:4443",
		InvitationURL: "https://vk.com/call/join/example",
		VLESSUUID:     "11111111-1111-4111-8111-111111111111",
		PSK:           "legacy-not-used-by-freeturn",
		ExpiresAt:     now.Add(time.Hour).Unix(),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	routes := `{"default":"proxy","lists":{"direct":[],"proxy":[],"block":[]}}`
	dns := `{"direct":["1.1.1.1"],"proxy":["https://1.1.1.1/dns-query"],"vpn_underlay":["1.1.1.1"],"bootstrap":["1.1.1.1"]}`
	raw := BuildFreeTURNProfileConfig(profile, "127.0.0.1:19000", routes, dns)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != true || !strings.Contains(raw, `127.0.0.1`) || !strings.Contains(raw, `19000`) {
		t.Fatalf("unexpected FreeTURN config: %s", raw)
	}
}
