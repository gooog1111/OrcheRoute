package subscriptions

import (
	"strings"

	"github.com/gooog1111/orcheroute/internal/nodes"
)

type SourceLinks struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Links []string `json:"links"`
}

type SourceIdentity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Pool struct {
	Proxies      []map[string]any          `json:"proxies"`
	Errors       map[string]int            `json:"errors"`
	SourceByNode map[string]SourceIdentity `json:"source_by_node"`
	Fetched      int                       `json:"fetched"`
	Sources      int                       `json:"sources"`
}

// Aggregate performs source-order-preserving, cross-subscription deduplication
// and node conversion. Network qualification is intentionally a separate
// platform adapter.
func Aggregate(sources []SourceLinks) Pool {
	result := Pool{Proxies: []map[string]any{}, Errors: map[string]int{}, SourceByNode: map[string]SourceIdentity{}}
	seenLinks := map[string]bool{}
	for _, source := range sources {
		uniqueLinks := []string{}
		for _, link := range source.Links {
			if !seenLinks[link] {
				seenLinks[link] = true
				uniqueLinks = append(uniqueLinks, link)
			}
		}
		if len(uniqueLinks) > 0 {
			result.Sources++
		}
		result.Fetched += len(uniqueLinks)
		converted := nodes.ConvertLinks(uniqueLinks, source.ID)
		result.Proxies = append(result.Proxies, converted.Proxies...)
		for _, proxy := range converted.Proxies {
			if name, ok := proxy["name"].(string); ok {
				result.SourceByNode[name] = SourceIdentity{ID: source.ID, Name: source.Name}
			}
		}
		for reason, count := range converted.Errors {
			key := source.ID + ":" + reason
			result.Errors[key] += count
		}
	}
	return result
}

func RetainSources(sourceByNode map[string]SourceIdentity, proxies []map[string]any) map[string]SourceIdentity {
	result := map[string]SourceIdentity{}
	for _, proxy := range proxies {
		name, _ := proxy["name"].(string)
		if source, ok := sourceByNode[name]; ok && strings.TrimSpace(name) != "" {
			result[name] = source
		}
	}
	return result
}
