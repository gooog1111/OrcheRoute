package reversevpn

import (
	"strings"
	"testing"
)

func TestWireGuardServerConfigIsScoped(t *testing.T) {
	config := DefaultConfig()
	config.PrivateKey = "private"
	config.PublicKey = "public"
	config.Clients = []Client{{ID: "phone", Name: "Phone", Address: "10.77.0.2/32", PublicKey: "peer", Enabled: true}}
	text, err := WireGuardServerConfig(config, "eth0")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Address = 10.77.0.1/24", "ListenPort = 51820", "-s 10.77.0.0/24 -o eth0", "AllowedIPs = 10.77.0.2/32"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
}
