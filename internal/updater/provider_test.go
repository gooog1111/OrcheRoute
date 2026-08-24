package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestProviderMetadataRetainsQualificationMetrics(t *testing.T) {
	directory := t.TempDir()
	store := FileProviderStore{ProvidersDirectory: directory}
	result := qualification.Result{
		Proxies: []map[string]any{{"name": "node-a"}},
		Metrics: map[string]qualification.NodeMetrics{
			"node-a": {DelayMS: 87, SpeedMbps: 42.5, StabilityRatio: .91, Country: "NL"},
		},
	}
	if err := store.Write("primary", result, map[string]subscriptions.SourceIdentity{
		"node-a": {ID: "source-a", Name: "Source A"},
	}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(directory, "primary.sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{}
	if err := json.Unmarshal(payload, &metadata); err != nil {
		t.Fatal(err)
	}
	node := metadata["nodes"].(map[string]any)["node-a"].(map[string]any)
	if node["id"] != "source-a" || node["delay_ms"] != float64(87) || node["speed_mbps"] != 42.5 || node["stability_ratio"] != .91 || node["country"] != "NL" {
		t.Fatalf("metadata=%#v", node)
	}
}

func TestProviderRanksCombinedNodesByQualificationScore(t *testing.T) {
	directory := t.TempDir()
	store := FileProviderStore{ProvidersDirectory: directory}
	result := qualification.Result{
		Proxies: []map[string]any{{"name": "slow"}, {"name": "fast"}},
		Metrics: map[string]qualification.NodeMetrics{
			"slow": {DelayMS: 350, SpeedMbps: 12, StabilityRatio: .7},
			"fast": {DelayMS: 55, SpeedMbps: 80, StabilityRatio: .95},
		},
	}
	sources := map[string]subscriptions.SourceIdentity{
		"slow": {ID: "source", Name: "Source"},
		"fast": {ID: "source", Name: "Source"},
	}
	if err := store.Write("primary", result, sources); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(directory, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provider struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := json.Unmarshal(payload, &provider); err != nil {
		t.Fatal(err)
	}
	if len(provider.Proxies) != 2 || provider.Proxies[0]["name"] != "fast" {
		t.Fatalf("provider is not ranked: %#v", provider.Proxies)
	}
}

func TestMergeSourceRemovesStaleNodesFromOnlyCheckedSubscription(t *testing.T) {
	directory := t.TempDir()
	store := FileProviderStore{ProvidersDirectory: directory}
	initial := qualification.Result{
		Proxies: []map[string]any{{"name": "SOURCE-A-old"}, {"name": "SOURCE-B-keep"}},
		Metrics: map[string]qualification.NodeMetrics{
			"SOURCE-A-old":  {DelayMS: 300, SpeedMbps: 12, StabilityRatio: .7},
			"SOURCE-B-keep": {DelayMS: 80, SpeedMbps: 40, StabilityRatio: .93},
		},
	}
	if err := store.Write("primary", initial, map[string]subscriptions.SourceIdentity{
		"SOURCE-A-old":  {ID: "source-a", Name: "A"},
		"SOURCE-B-keep": {ID: "source-b", Name: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	result := qualification.Result{Proxies: []map[string]any{{"name": "SOURCE-A-new"}}, Metrics: map[string]qualification.NodeMetrics{"SOURCE-A-new": {DelayMS: 40, SpeedMbps: 50, StabilityRatio: .9}}}
	if err := store.MergeSource("primary", "source-a", result, map[string]subscriptions.SourceIdentity{"SOURCE-A-new": {ID: "source-a", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	payload, _ := os.ReadFile(filepath.Join(directory, "primary.json"))
	var provider struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := json.Unmarshal(payload, &provider); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, proxy := range provider.Proxies {
		names[proxy["name"].(string)] = true
	}
	if names["SOURCE-A-old"] || !names["SOURCE-A-new"] || !names["SOURCE-B-keep"] || len(names) != 2 {
		t.Fatalf("unexpected merged names: %#v", names)
	}
	metadataPayload, err := os.ReadFile(filepath.Join(directory, "primary.sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Nodes map[string]map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(metadataPayload, &metadata); err != nil {
		t.Fatal(err)
	}
	retained := metadata.Nodes["SOURCE-B-keep"]
	if retained["delay_ms"] != float64(80) || retained["speed_mbps"] != float64(40) || retained["stability_ratio"] != .93 {
		t.Fatalf("other source rating data changed: %#v", retained)
	}
}

func TestMergeSourceWithNoAvailableNodesRemovesOnlyThatSource(t *testing.T) {
	directory := t.TempDir()
	store := FileProviderStore{ProvidersDirectory: directory}
	if err := store.Write("primary", qualification.Result{Proxies: []map[string]any{{"name": "SOURCE-A-old"}, {"name": "SOURCE-B-keep"}}}, map[string]subscriptions.SourceIdentity{
		"SOURCE-A-old":  {ID: "source-a", Name: "A"},
		"SOURCE-B-keep": {ID: "source-b", Name: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MergeSource("primary", "source-a", qualification.Result{}, map[string]subscriptions.SourceIdentity{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(directory, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provider struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := json.Unmarshal(payload, &provider); err != nil {
		t.Fatal(err)
	}
	if len(provider.Proxies) != 1 || provider.Proxies[0]["name"] != "SOURCE-B-keep" {
		t.Fatalf("unexpected provider after empty result: %#v", provider.Proxies)
	}
}
