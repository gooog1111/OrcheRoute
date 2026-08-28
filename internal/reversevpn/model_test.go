package reversevpn

import "testing"

func TestDefaultConfigIsSafeAndValid(t *testing.T) {
	config := DefaultConfig()
	if config.Enabled {
		t.Fatal("reverse VPN must be disabled by default")
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("default config: %v", err)
	}
}

func TestPublicConfigNeverExposesPrivateKeys(t *testing.T) {
	config := DefaultConfig()
	config.PrivateKey = "server-secret"
	config.PublicKey = "server-public"
	config.Clients = []Client{{ID: "phone", Name: "Phone", Address: "10.77.0.2/32", PrivateKey: "client-secret", PublicKey: "client-public", Enabled: true}}
	public := config.Public()
	if public.PublicKey != "server-public" || len(public.Clients) != 1 {
		t.Fatal("public fields missing")
	}
}

func TestValidateRejectsOverlapsAndUnsupportedTransport(t *testing.T) {
	config := DefaultConfig()
	config.Clients = []Client{
		{ID: "one", Name: "One", Address: "10.77.0.2/32", PublicKey: "key-one", Enabled: true},
		{ID: "two", Name: "Two", Address: "10.77.0.2/32", PublicKey: "key-two", Enabled: true},
	}
	if err := config.Validate(); err == nil || err.Error() != "duplicate_client_address" {
		t.Fatalf("unexpected error: %v", err)
	}
	config.Clients = nil
	config.Transport = "olcrtc"
	if err := config.Validate(); err == nil || err.Error() != "unsupported_transport" {
		t.Fatalf("unexpected transport error: %v", err)
	}
}
