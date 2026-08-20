package mapper

import (
	"testing"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestSubscriptionsDeduplicatesAcrossSourcesAndPreservesOwner(t *testing.T) {
	link := "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls&type=tcp#node"
	result := Subscriptions([]subscriptions.SourceLinks{
		{ID: "first", Name: "First", Links: []string{link}},
		{ID: "second", Name: "Second", Links: []string{link}},
	})
	if result.Fetched != 1 || result.Sources != 1 || len(result.Proxies) != 1 {
		t.Fatalf("mapped pool = %#v", result)
	}
	name, _ := result.Proxies[0]["name"].(string)
	if owner := result.SourceByNode[name]; owner.ID != "first" {
		t.Fatalf("node owner = %#v, want first source", owner)
	}
}

func TestRetainSourcesDropsRejectedNodes(t *testing.T) {
	sources := map[string]subscriptions.SourceIdentity{
		"alive": {ID: "one"},
		"dead":  {ID: "two"},
	}
	result := RetainSources(sources, []map[string]any{{"name": "alive"}})
	if len(result) != 1 || result["alive"].ID != "one" {
		t.Fatalf("retained sources = %#v", result)
	}
}
