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
	if config["peer"] != "vpn.example.com:4443" || config["clientId"] != profile.VLESSUUID || proxy["mode"] != "tcp" || proxy["bond"] != true {
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

func TestOrcheRouteProfilePassesAllInvitationLinksToFreeTURN(t *testing.T) {
	profile := callprofile.Profile{
		Version: callprofile.Version, Transport: callprofile.Transport, Provider: callprofile.Provider,
		InvitationURL: "https://vk.com/call/join/first", InvitationURLs: []string{"https://vk.ru/call/join/second"},
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
	var config struct {
		VK struct {
			Links []string `json:"links"`
		} `json:"vk"`
	}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.VK.Links) != 2 || config.VK.Links[1] != "https://vk.com/call/join/second" {
		t.Fatalf("unexpected FreeTURN links: %#v", config.VK.Links)
	}
}

func TestPacketProfileBecomesObfuscatedAWGTunnelConfig(t *testing.T) {
	privateKey := base64.StdEncoding.EncodeToString(bytesOf(0x01, 32))
	publicKey := base64.StdEncoding.EncodeToString(bytesOf(0x02, 32))
	obfuscationKey := base64.RawURLEncoding.EncodeToString(bytesOf(0x31, 32))
	profile := callprofile.Profile{
		Version: callprofile.Version, Transport: callprofile.Transport, Provider: callprofile.Provider,
		InvitationURL: "https://vk.com/call/join/first", PeerAddress: "vpn.example.com:4443",
		PSK: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), VLESSUUID: "b831381d-6324-4d53-ad4f-8cda48b30811",
		PacketTunnel: &callprofile.PacketTunnel{Carrier: "vk-turn", Mode: "awg",
			Config:             "[Interface]\nPrivateKey = " + privateKey + "\nAddress = 10.77.0.2/32\nDNS = 1.1.1.1\n\n[Peer]\nPublicKey = " + publicKey + "\nAllowedIPs = 0.0.0.0/0\n",
			ObfuscationProfile: "rtpopus3", ObfuscationKey: obfuscationKey},
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
	proxy, tunnel, obf := config["proxy"].(map[string]any), config["tunnel"].(map[string]any), config["obf"].(map[string]any)
	if proxy["mode"] != "udp" || proxy["bond"] != false || tunnel["mode"] != "awg" || obf["profile"] != "rtpopus3" {
		t.Fatalf("unexpected packet config: %#v", config)
	}
	if got := ValidateConfig(configJSON); got != "" {
		t.Fatalf("generated config rejected: %s", got)
	}
	paramsJSON, err := TunnelParamsJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		t.Fatal(err)
	}
	if params["addresses"] != "10.77.0.2/32" || params["allowed_ips"] != "0.0.0.0/0" || params["mtu"] != float64(1280) {
		t.Fatalf("unexpected tunnel params: %#v", params)
	}
}

func TestUsesPacketTunnelKeepsLegacyProfilesSeparate(t *testing.T) {
	packet := encodedProfile(t, &callprofile.PacketTunnel{
		Carrier: "vk-turn", Mode: "awg",
		Config:             "[Interface]\nPrivateKey = " + base64.StdEncoding.EncodeToString(bytesOf(0x01, 32)) + "\nAddress = 10.77.0.2/32\n\n[Peer]\nPublicKey = " + base64.StdEncoding.EncodeToString(bytesOf(0x02, 32)) + "\nAllowedIPs = 0.0.0.0/0\n",
		ObfuscationProfile: "rtpopus3", ObfuscationKey: base64.RawURLEncoding.EncodeToString(bytesOf(0x31, 32)),
	})
	if !UsesPacketTunnel(packet) {
		t.Fatal("packet profile was not detected")
	}
	legacy := encodedProfile(t, nil)
	if UsesPacketTunnel(legacy) {
		t.Fatal("legacy profile was detected as a packet tunnel")
	}
}

func encodedProfile(t *testing.T, packet *callprofile.PacketTunnel) string {
	t.Helper()
	encoded, err := callprofile.Encode(callprofile.Profile{
		Version: callprofile.Version, Transport: callprofile.Transport, Provider: callprofile.Provider,
		InvitationURL: "https://vk.com/call/join/first", PeerAddress: "vpn.example.com:4443",
		PSK: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), VLESSUUID: "b831381d-6324-4d53-ad4f-8cda48b30811",
		PacketTunnel: packet,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}
