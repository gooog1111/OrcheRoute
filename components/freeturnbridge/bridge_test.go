package freeturnbridge

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
)

func TestDefaultConfigAndIdleStateAreValidJSON(t *testing.T) {
	for name, value := range map[string]string{
		"config": DefaultConfigJSON(),
		"state":  StateJSON(),
	} {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(value), &decoded); err != nil {
			t.Fatalf("%s is not JSON: %v", name, err)
		}
	}
}

func TestOrcheRouteProfileBecomesTCPFreeTURNConfig(t *testing.T) {
	profile := callprofile.Profile{
		Version: callprofile.Version, Transport: callprofile.Transport, Provider: callprofile.Provider,
		Name: "Phone", InvitationURL: "https://vk.com/call/join/abcdefghijklmnop",
		PeerAddress: "vpn.example.com:4443", PSK: base64.RawURLEncoding.EncodeToString(make([]byte, 32)),
		VLESSUUID: "b831381d-6324-4d53-ad4f-8cda48b30811",
	}
	encoded, err := callprofile.Encode(profile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	configJSON, err := ConfigFromOrcheRouteProfile(encoded, "127.0.0.1:19000")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatal(err)
	}
	proxy := config["proxy"].(map[string]any)
	if config["peer"] != "vpn.example.com:4443" || config["clientId"] != profile.VLESSUUID || proxy["mode"] != "tcp" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if got := ValidateConfig(configJSON); got != "" {
		t.Fatalf("generated config rejected: %s", got)
	}
}

func TestInvalidConfigIsRejectedWithoutStartingTransport(t *testing.T) {
	if got := ValidateConfig(`{"clientId":""}`); got == "" {
		t.Fatal("invalid config was accepted")
	}
}
