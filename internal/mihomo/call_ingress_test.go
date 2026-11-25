package mihomo

import (
	"encoding/json"
	"testing"
)

func TestCallIngressMigrationPreservesSettingsAndIsIdempotent(t *testing.T) {
	var config map[string]any
	_ = json.Unmarshal([]byte(`{"secret":"keep","rules":["MATCH,DIRECT"],"tun":{"device":"orcheroute0","exclude-interface":["docker0","orchecall0"],"include-interface":["eth0"],"mtu":9000}}`), &config)
	changed, err := CaptureCallServerIngress(config)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	tun := config["tun"].(map[string]any)
	excluded := tun["exclude-interface"].([]string)
	if len(excluded) != 1 || excluded[0] != "docker0" {
		t.Fatal(excluded)
	}
	included := tun["include-interface"].([]string)
	if len(included) != 2 || included[0] != "eth0" || included[1] != "orchecall0" || tun["mtu"] != float64(9000) || config["secret"] != "keep" {
		t.Fatal("settings replaced")
	}
	before, _ := json.Marshal(config)
	changed, err = CaptureCallServerIngress(config)
	after, _ := json.Marshal(config)
	if err != nil || changed || string(before) != string(after) {
		t.Fatal("migration is not idempotent")
	}
}

func TestCallIngressMigrationRestoresSystemCaptureWithoutModifyingForeignTun(t *testing.T) {
	config := map[string]any{"tun": map[string]any{"device": "orcheroute0", "exclude-interface": []string{"orchecall0"}}}
	if changed, err := CaptureCallServerIngress(config); err != nil || !changed {
		t.Fatal(changed, err)
	}
	tun := config["tun"].(map[string]any)
	if _, exists := tun["exclude-interface"]; exists {
		t.Fatal("server ingress remains excluded")
	}
	if _, exists := tun["include-interface"]; exists {
		t.Fatal("system capture was narrowed to an explicit interface")
	}
	for _, config := range []map[string]any{{}, {"tun": map[string]any{"device": "other"}}} {
		if changed, err := CaptureCallServerIngress(config); changed || err != nil {
			t.Fatal("foreign config changed")
		}
	}
}
