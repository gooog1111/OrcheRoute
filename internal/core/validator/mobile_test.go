package validator

import "testing"

func validMobileProfile() map[string]any {
	dns := map[string]any{
		"direct": []any{"1.1.1.1"}, "proxy": []any{"https://1.1.1.1/dns-query"},
		"vpn_underlay": []any{"1.1.1.1"}, "bootstrap": []any{"1.1.1.1"}, "cache_algorithm": "arc",
	}
	return map[string]any{
		"roles": map[string]any{
			"direct": map[string]any{"interface": "auto"}, "vpn_underlay": map[string]any{"interface": "wifi"},
		},
		"capture": map[string]any{"mode": "system"}, "dns": dns,
	}
}

func TestMobileNetworkProfile(t *testing.T) {
	if err := MobileNetworkProfile(validMobileProfile()); err != nil {
		t.Fatal(err)
	}
	invalid := validMobileProfile()
	invalid["roles"].(map[string]any)["direct"] = map[string]any{"interface": "pppoe"}
	if err := MobileNetworkProfile(invalid); err == nil || err.Error() != "invalid_android_transport" {
		t.Fatalf("unexpected transport validation: %v", err)
	}
}

func TestMobileDNSProfile(t *testing.T) {
	dns := validMobileProfile()["dns"].(map[string]any)
	if err := MobileDNSProfile(dns); err != nil {
		t.Fatal(err)
	}
	dns["proxy"] = []any{" "}
	if err := MobileDNSProfile(dns); err == nil || err.Error() != "invalid_dns_value" {
		t.Fatalf("unexpected DNS validation: %v", err)
	}
}
