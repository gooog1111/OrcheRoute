// Package mapper maps parsed subscription sources to persistent pool models.
// It owns cross-source deduplication and source identity, but performs no
// fetching, syntax parsing, qualification, routing, or transport operations.
package mapper

import (
	"github.com/gooog1111/orcheroute/internal/core/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

// Subscriptions combines parsed source links into the canonical node pool.
func Subscriptions(sources []subscriptions.SourceLinks) subscriptions.Pool {
	return subscriptions.Aggregate(sources)
}

// RetainSources removes source metadata for nodes no longer present after a
// validator/qualification stage.
func RetainSources(sourceByNode map[string]subscriptions.SourceIdentity, proxies []map[string]any) map[string]subscriptions.SourceIdentity {
	return subscriptions.RetainSources(sourceByNode, proxies)
}

// QualificationMetadata maps shared qualification evidence to persistent
// node metadata while preserving platform-owned health history.
func QualificationMetadata(result qualification.Result, sources map[string]subscriptions.SourceIdentity, previous map[string]map[string]any) map[string]map[string]any {
	nodes := make(map[string]map[string]any, len(sources))
	for name, source := range sources {
		value := map[string]any{"id": source.ID, "name": source.Name}
		if history := previous[name]; history != nil {
			for _, key := range []string{"health_successes", "health_failures"} {
				if current, ok := history[key]; ok {
					value[key] = current
				}
			}
		}
		if metrics, ok := result.Metrics[name]; ok {
			value["delay_ms"] = metrics.DelayMS
			value["speed_mbps"] = metrics.SpeedMbps
			value["stability_ratio"] = metrics.StabilityRatio
			value["country"] = metrics.Country
		}
		if result.Report.FinishedAt > 0 {
			value["last_tested_at"] = result.Report.FinishedAt
		}
		nodes[name] = value
	}
	return nodes
}
