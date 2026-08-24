package constructor

import (
	"testing"

	"github.com/gooog1111/orcheroute/internal/core/routing"
)

func TestBuildKeepsRoutingAndDNSInputs(t *testing.T) {
	plan, err := routing.Compile(routing.Input{Default: "proxy", Lists: map[string][]string{
		"block": {"ads.example"}, "direct": {}, "proxy": {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dns := DefaultDNS()
	dns.Direct = []string{"9.9.9.9"}
	result, err := Build(Request{Proxy: map[string]any{"name": "NODE", "type": "trojan"}, Routing: plan, DNS: dns})
	if err != nil {
		t.Fatal(err)
	}
	if result.Node != "NODE" {
		t.Fatalf("node = %q", result.Node)
	}
	rules := result.Config["rules"].([]string)
	if rules[0] != "DOMAIN-SUFFIX,ads.example,REJECT" || rules[len(rules)-1] != "MATCH,ACTIVE" {
		t.Fatalf("rules = %#v", rules)
	}
	dnsConfig := result.Config["dns"].(map[string]any)
	if dnsConfig["direct-nameserver"].([]string)[0] != "9.9.9.9" {
		t.Fatalf("dns = %#v", dnsConfig)
	}
}

func TestBuildRejectsIncompleteDNS(t *testing.T) {
	plan, err := routing.Compile(routing.DefaultInput())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(Request{Proxy: map[string]any{"name": "NODE"}, Routing: plan})
	if err == nil || err.Error() != "invalid_dns_profile" {
		t.Fatalf("error = %v", err)
	}
}
