package mihomo

import (
	"encoding/json"
	"testing"
)

func TestCallIngressMigrationPreservesSettingsAndIsIdempotent(t *testing.T) {
	var config map[string]any
	_ = json.Unmarshal([]byte(`{"secret":"keep","rules":["MATCH,DIRECT"],"tun":{"device":"orcheroute0","exclude-interface":["docker0"],"include-interface":["orchecall0","eth0"],"mtu":9000}}`), &config)
	changed, err := ExcludeCallServerIngress(config)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	tun := config["tun"].(map[string]any)
	excluded := tun["exclude-interface"].([]string)
	if len(excluded) != 2 || excluded[0] != "docker0" || excluded[1] != "orchecall0" {
		t.Fatal(excluded)
	}
	included := tun["include-interface"].([]string)
	if len(included) != 1 || included[0] != "eth0" || tun["mtu"] != float64(9000) || config["secret"] != "keep" {
		t.Fatal("settings replaced")
	}
	before, _ := json.Marshal(config)
	changed, err = ExcludeCallServerIngress(config)
	after, _ := json.Marshal(config)
	if err != nil || changed || string(before) != string(after) {
		t.Fatal("migration is not idempotent")
	}
}

func TestCallIngressMigrationDoesNotExpandCaptureOrModifyForeignTun(t *testing.T) {
	config := map[string]any{"tun": map[string]any{"device": "orcheroute0", "include-interface": []string{"orchecall0"}}}
	if _, err := ExcludeCallServerIngress(config); err == nil {
		t.Fatal("empty include would expand capture to other interfaces")
	}
	for _, config := range []map[string]any{{}, {"tun": map[string]any{"device": "other"}}} {
		if changed, err := ExcludeCallServerIngress(config); changed || err != nil {
			t.Fatal("foreign config changed")
		}
	}
}
