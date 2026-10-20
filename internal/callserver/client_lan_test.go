package callserver

import (
	"strings"
	"testing"
)

func TestClientLANPermissionSurvivesOtherEditsAndCanInherit(t *testing.T) {
	manager, _ := configuredManager(t)
	allow := true
	client, err := manager.CreateClient(CreateClientInput{AllowLAN: &allow})
	if err != nil {
		t.Fatal(err)
	}
	client, err = manager.UpdateClient(client.ID, UpdateClientInput{Name: "edited", Enabled: true})
	if err != nil || client.AllowLAN == nil || !*client.AllowLAN {
		t.Fatal(client, err)
	}
	if manager.PublicConfig().Clients[0].AllowLAN == nil {
		t.Fatal("public permission missing")
	}
	client, err = manager.UpdateClient(client.ID, UpdateClientInput{Enabled: true, SetLAN: true})
	if err != nil || client.AllowLAN != nil {
		t.Fatal("cannot restore inherited permission", err)
	}
}

func TestOrdinaryClientLANRulesPrecedeGlobalDefault(t *testing.T) {
	allow, deny := true, false
	rules := ordinaryClientEgressRules(OrdinarySnapshot{Clients: []OrdinaryClient{{ID: "allowed", AllowLAN: &allow}, {ID: "default"}}})
	if rules[0] != "IN-USER,allowed,DIRECT" || !strings.Contains(strings.Join(rules, "\n"), "IP-CIDR,10.0.0.0/8,REJECT-DROP") {
		t.Fatal(rules)
	}
	rules = ordinaryClientEgressRules(OrdinarySnapshot{AllowLAN: true, Clients: []OrdinaryClient{{ID: "denied", AllowLAN: &deny}}})
	if rules[0] != "AND,((IN-USER,denied),(IP-CIDR,10.0.0.0/8)),REJECT-DROP" || rules[len(rules)-1] != "MATCH,DIRECT" {
		t.Fatal(rules)
	}
}
