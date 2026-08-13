package subscriptions

import "testing"

func TestAggregateDeduplicatesAcrossSources(t *testing.T) {
	one := "vless://11111111-1111-1111-1111-111111111111@one.example:443?security=tls#One"
	two := "trojan://secret@two.example:443?sni=two.example#Two"
	pool := Aggregate([]SourceLinks{
		{ID: "first", Name: "First", Links: []string{one, "invalid://node"}},
		{ID: "second", Name: "Second", Links: []string{one, two}},
	})
	if pool.Fetched != 3 || pool.Sources != 2 || len(pool.Proxies) != 2 {
		t.Fatalf("unexpected pool: %#v", pool)
	}
	if pool.Errors["first:ValueError:unsupported protocol"] != 1 {
		t.Fatalf("unexpected errors: %#v", pool.Errors)
	}
	for _, proxy := range pool.Proxies {
		name := proxy["name"].(string)
		if _, ok := pool.SourceByNode[name]; !ok {
			t.Fatalf("missing source for %s", name)
		}
	}
}
