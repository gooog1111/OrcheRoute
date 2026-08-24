package routing

import (
	"reflect"
	"testing"
)

func TestCompileOrdersBlockDirectProxyAndDefault(t *testing.T) {
	plan, err := Compile(Input{Default: "direct", Lists: map[string][]string{
		"block": {"ads.example"}, "direct": {"*.ru"}, "proxy": {":443"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DOMAIN-SUFFIX,ads.example,REJECT",
		"DOMAIN-SUFFIX,ru,DIRECT",
		"DST-PORT,443,ACTIVE",
		"MATCH,DIRECT",
	}
	if !reflect.DeepEqual(plan.Rules, want) {
		t.Fatalf("rules = %#v, want %#v", plan.Rules, want)
	}
}

func TestCompileRejectsInvalidDefault(t *testing.T) {
	_, err := Compile(Input{Default: "unknown", Lists: map[string][]string{"block": {}, "direct": {}, "proxy": {}}})
	if err == nil || err.Error() != "invalid_route_default" {
		t.Fatalf("error = %v", err)
	}
}
