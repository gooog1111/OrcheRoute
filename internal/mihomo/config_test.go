package mihomo

import "testing"

func testInput(mode string) Input {
	return Input{
		StateDir: "/var/lib/orcheroute", TestURL: "https://example.test/204", Secret: "secret", RouteDefault: "proxy",
		Network: Network{
			Profile: Profile{Capture: Capture{Mode: mode, Interfaces: []string{"lan0"}, DNSHijack: true, StrictRoute: true}},
			ResolvedRoles: map[string]Role{
				"direct": {Interface: "wan0", Mark: 0x5351}, "vpn_underlay": {Interface: "vpnwan", Mark: 0x5352},
			},
			EffectiveBypassCIDRs: []string{"10.0.0.0/8", "::1/128"},
			DNS: DNS{
				Config: DNSConfig{IPv6: false, CacheAlgorithm: "arc", UseHosts: true},
				Effective: DNSEffective{
					Bootstrap: []string{"1.1.1.1"}, Proxy: []string{"1.1.1.1#ACTIVE"},
					Direct: []string{"1.1.1.1#DIRECT-EGRESS"}, VPNUnderlay: []string{"1.1.1.1#VPN-UNDERLAY-DNS"},
				},
			},
		},
	}
}

func TestSystemIncludesTun(t *testing.T) {
	config, err := Build(testInput("system"))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := config["tun"]; !exists {
		t.Fatal("system mode must include tun")
	}
}

func TestAllowlistProviderIsSelectable(t *testing.T) {
	config, err := Build(testInput("system"))
	if err != nil {
		t.Fatal(err)
	}
	providers := config["proxy-providers"].(map[string]any)
	if _, ok := providers["whitelist"]; !ok {
		t.Fatal("whitelist provider missing")
	}
	groups := config["proxy-groups"].([]any)
	active := groups[0].(map[string]any)
	uses := active["use"].([]string)
	found := false
	for _, value := range uses {
		if value == "whitelist" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ACTIVE cannot select whitelist provider: %#v", uses)
	}
}

func TestInterfaceCaptureIncludesTun(t *testing.T) {
	config, err := Build(testInput("interfaces"))
	if err != nil {
		t.Fatal(err)
	}
	tun := config["tun"].(map[string]any)
	interfaces := tun["include-interface"].([]string)
	if len(interfaces) != 1 || interfaces[0] != "lan0" {
		t.Fatalf("unexpected interfaces: %#v", interfaces)
	}
	excluded := tun["route-exclude-address"].([]string)
	if len(excluded) != 1 || excluded[0] != "10.0.0.0/8" {
		t.Fatalf("IPv6 bypass must be filtered: %#v", excluded)
	}
}

func TestInvalidDefaultFails(t *testing.T) {
	input := testInput("system")
	input.RouteDefault = "invalid"
	if _, err := Build(input); err == nil || err.Error() != "invalid_default_action" {
		t.Fatalf("unexpected error: %v", err)
	}
}
