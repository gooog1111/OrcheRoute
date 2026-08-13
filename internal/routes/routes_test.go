package routes

import (
	"reflect"
	"testing"
)

func TestParseEntryCompatibility(t *testing.T) {
	tests := []struct {
		entry      string
		normalized string
		rules      []string
	}{
		{"10.0.0.1", "10.0.0.1", []string{"IP-CIDR,10.0.0.1/32"}},
		{"10.0.1.7/23", "10.0.0.0/23", []string{"IP-CIDR,10.0.0.0/23"}},
		{"10.0.0.0-11.0.0.0", "10.0.0.0-11.0.0.0", []string{"IP-CIDR,10.0.0.0/8", "IP-CIDR,11.0.0.0/32"}},
		{"Example.COM.", ".example.com", []string{"DOMAIN-SUFFIX,example.com"}},
		{"=Example.COM.", "=example.com", []string{"DOMAIN,example.com"}},
		{"*.ru", ".ru", []string{"DOMAIN-SUFFIX,ru"}},
		{"*yandex.ru", ".yandex.ru", []string{"DOMAIN-SUFFIX,yandex.ru"}},
		{"api.*.example.org", "api.*.example.org", []string{"DOMAIN-WILDCARD,api.*.example.org"}},
		{"geoip:ru", "geoip:RU", []string{"GEOIP,RU"}},
		{"geosite:category-ads-all", "geosite:category-ads-all", []string{"GEOSITE,category-ads-all"}},
		{":5000,5002,5005", ":5000,5002,5005", []string{"DST-PORT,5000", "DST-PORT,5002", "DST-PORT,5005"}},
		{"53/udp", "udp:53", []string{"AND,((NETWORK,UDP),(DST-PORT,53))"}},
		{"udp:*", "udp:*", []string{"NETWORK,UDP"}},
		{"preset:torrent", "preset:torrent", []string{"AND,((NETWORK,TCP),(DST-PORT,6881-6889))", "AND,((NETWORK,UDP),(DST-PORT,6881-6889))"}},
	}
	for _, test := range tests {
		t.Run(test.entry, func(t *testing.T) {
			result, err := ParseEntry(test.entry)
			if err != nil {
				t.Fatal(err)
			}
			if result.Normalized != test.normalized || !reflect.DeepEqual(result.Rules, test.rules) {
				t.Fatalf("got %#v", result)
			}
		})
	}
}

func TestCompileListsDeduplicates(t *testing.T) {
	result, err := CompileLists(map[string][]string{
		"direct": {"EXAMPLE.com", "example.com"},
		"proxy":  {},
		"block":  {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats["direct"].DuplicatesRemoved != 1 {
		t.Fatalf("unexpected stats: %#v", result.Stats["direct"])
	}
	if !reflect.DeepEqual(result.Compiled["direct"], []string{"DOMAIN-SUFFIX,example.com"}) {
		t.Fatalf("unexpected rules: %#v", result.Compiled["direct"])
	}
}

func TestValidationLocation(t *testing.T) {
	_, err := CompileLists(map[string][]string{"direct": {"10.0.0.5-foo"}, "proxy": {}, "block": {}})
	validation, ok := err.(*ValidationError)
	if !ok || validation.Code != "invalid_ip_range" || validation.List != "direct" || validation.Index == nil || *validation.Index != 0 {
		t.Fatalf("unexpected error: %#v", err)
	}
}
