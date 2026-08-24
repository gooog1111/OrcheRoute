package mapper

import (
	"testing"

	"github.com/gooog1111/orcheroute/internal/core/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestQualificationMetadataPreservesHistoryAndMapsMetrics(t *testing.T) {
	result := qualification.Result{Metrics: map[string]qualification.NodeMetrics{
		"node": {DelayMS: 42, SpeedMbps: 18.5, StabilityRatio: .9, Country: "DE"},
	}}
	sources := map[string]subscriptions.SourceIdentity{"node": {ID: "source", Name: "Source"}}
	previous := map[string]map[string]any{"node": {"health_successes": 7, "health_failures": 2, "obsolete": true}}
	metadata := QualificationMetadata(result, sources, previous)["node"]
	if metadata["id"] != "source" || metadata["delay_ms"] != 42 || metadata["health_successes"] != 7 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if _, retained := metadata["obsolete"]; retained {
		t.Fatalf("obsolete metadata leaked: %#v", metadata)
	}
}
