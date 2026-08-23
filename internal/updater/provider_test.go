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
