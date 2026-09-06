package callserver

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrdinaryMihomoConfigContainsWorkingListenersWithoutSecretsInPublicState(t *testing.T) {
	manager, _ := configuredManager(t)
	if _, err := manager.CreateClient(CreateClientInput{Name: "Phone"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := ordinaryMihomoConfig(snapshot.Ordinary)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	listeners, ok := config["listeners"].([]any)
	if !ok || len(listeners) != 3 {
		t.Fatalf("unexpected listeners: %#v", config["listeners"])
	}
	text := string(payload)
	for _, expected := range []string{`"type": "vless"`, `"type": "trojan"`, `"type": "hysteria2"`, `"dest": "m.vk.ru:443"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in %s", expected, text)
		}
	}
	public := mustJSON(t, manager.PublicConfig())
	for _, secret := range []string{manager.data.RealityPrivateKey, manager.data.TLSPrivateKey, manager.data.TLSCertificate} {
		if secret == "" || strings.Contains(public, secret) {
			t.Fatal("server identity leaked through public config")
		}
	}
}

func TestOrdinaryMihomoConfigAcceptedByRealBinary(t *testing.T) {
	binary := os.Getenv("ORCHEROUTE_TEST_MIHOMO")
	if binary == "" {
		t.Skip("ORCHEROUTE_TEST_MIHOMO is not set")
	}
	manager, _ := configuredManager(t)
	if _, err := manager.CreateClient(CreateClientInput{Name: "Phone"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := ordinaryMihomoConfig(snapshot.Ordinary)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "-t", "-d", directory, "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("mihomo rejected ordinary listeners: %v\n%s", err, output)
	}
}

func TestOrdinaryEgressRulesBlockPrivateRangesByDefault(t *testing.T) {
	rules := ordinaryEgressRules(false)
	if rules[len(rules)-1] != "MATCH,DIRECT" {
		t.Fatalf("MATCH,DIRECT must remain the final catch-all: %#v", rules)
	}
	for _, expected := range []string{
		"IP-CIDR,10.0.0.0/8,REJECT-DROP", "IP-CIDR,192.168.0.0/16,REJECT-DROP", "IP-CIDR,169.254.0.0/16,REJECT-DROP",
		"IP-CIDR6,fc00::/7,REJECT-DROP", "IP-CIDR6,fe80::/10,REJECT-DROP",
	} {
		found := false
		for _, rule := range rules[:len(rules)-1] {
			if rule == expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q ahead of the MATCH,DIRECT catch-all: %#v", expected, rules)
		}
	}
}

func TestOrdinaryEgressRulesAllowLANIsJustDirect(t *testing.T) {
	rules := ordinaryEgressRules(true)
	if len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Fatalf("allowLAN must skip the private-range rules entirely: %#v", rules)
	}
}

func TestVersionOneConfigMigratesToOrdinaryProtocols(t *testing.T) {
	config := DefaultConfig()
	config.Version = 1
	config.OrdinaryEnabled = false
	config.VLESSListenAddress, config.TrojanListenAddress, config.HysteriaListenAddress, config.FakeSNI = "", "", "", ""
	if err := config.Normalize(); err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentVersion || !config.OrdinaryEnabled || config.FakeSNI != "m.vk.ru" {
		t.Fatalf("version one config was not migrated: %#v", config)
	}
}
