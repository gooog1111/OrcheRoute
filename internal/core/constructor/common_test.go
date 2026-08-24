package constructor

import "testing"

func TestCommonConfigKeepsPlatformSpecificFieldsOut(t *testing.T) {
	config := CommonConfig(false, "standard", false)
	if config["mode"] != "rule" || config["geodata-loader"] != "standard" {
		t.Fatalf("unexpected common config: %#v", config)
	}
	for _, key := range []string{"tun", "external-controller", "proxy-providers", "dns"} {
		if _, exists := config[key]; exists {
			t.Fatalf("platform field %q leaked into common config", key)
		}
	}
	if _, exists := Sniffer(false)["skip-dst-address"]; exists {
		t.Fatal("mobile sniffer unexpectedly skips private destinations")
	}
	if _, exists := Sniffer(true)["skip-dst-address"]; !exists {
		t.Fatal("server sniffer lost private destination bypass")
	}
}
