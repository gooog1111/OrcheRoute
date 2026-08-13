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

func TestWindowsOmitsLinuxOnlyRoutingFields(t *testing.T) {
	input := testInput("system")
	input.Platform = "windows"
	config, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	tun := config["tun"].(map[string]any)
	for _, key := range []string{"auto-redirect", "iproute2-table-index", "iproute2-rule-index"} {
		if _, exists := tun[key]; exists {
			t.Fatalf("Windows TUN contains Linux-only %s", key)
		}
	}
	proxies := config["proxies"].([]any)
	for _, raw := range proxies {
		if _, exists := raw.(map[string]any)["routing-mark"]; exists {
			t.Fatal("Windows outbound contains Linux routing mark")
		}
	}
	providers := config["proxy-providers"].(map[string]any)
	override := providers["primary"].(map[string]any)["override"].(map[string]any)
	if _, exists := override["routing-mark"]; exists {
		t.Fatal("Windows provider contains Linux routing mark")
	}
}
