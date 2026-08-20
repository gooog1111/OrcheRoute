// Package mapper maps parsed subscription sources to persistent pool models.
// It owns cross-source deduplication and source identity, but performs no
// fetching, syntax parsing, qualification, routing, or transport operations.
package mapper

import "github.com/gooog1111/orcheroute/internal/subscriptions"

// Subscriptions combines parsed source links into the canonical node pool.
func Subscriptions(sources []subscriptions.SourceLinks) subscriptions.Pool {
	return subscriptions.Aggregate(sources)
}

// RetainSources removes source metadata for nodes no longer present after a
// validator/qualification stage.
func RetainSources(sourceByNode map[string]subscriptions.SourceIdentity, proxies []map[string]any) map[string]subscriptions.SourceIdentity {
	return subscriptions.RetainSources(sourceByNode, proxies)
}
