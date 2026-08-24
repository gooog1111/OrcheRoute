package updater

import (
	"context"
	"fmt"

	coremapper "github.com/gooog1111/orcheroute/internal/core/mapper"
	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"github.com/gooog1111/orcheroute/internal/whitelist"
)

// WhitelistResult contains complete, source-scoped replacements. A caller may
// persist CompletedSources even when a later source is cancelled; a partially
// tested source is never returned and therefore cannot erase known-good nodes.
type WhitelistResult struct {
	Sources          map[string][]whitelist.Node `json:"sources"`
	CompletedSources []string                    `json:"completed_sources"`
	Failures         map[string]string           `json:"failures"`
}

type WhitelistRequest struct {
	SubscriptionIDs map[string]bool
	Policy          map[string]any
}

// RunWhitelist qualifies every enabled cached subscription independently.
// It deliberately performs no fetch: restricted-network discovery must first
// build a stable usable list before attempting subscription updates.
func RunWhitelist(ctx context.Context, dependencies Dependencies, request WhitelistRequest) (WhitelistResult, error) {
	result := WhitelistResult{Sources: map[string][]whitelist.Node{}, CompletedSources: []string{}, Failures: map[string]string{}}
	if dependencies.Repository == nil || dependencies.Cache == nil || dependencies.Qualifier == nil {
		return result, fmt.Errorf("incomplete whitelist dependencies")
	}
	items, err := dependencies.Repository.List(ctx, true)
	if err != nil {
		return result, err
	}
	eligible := make([]subscriptions.Subscription, 0, len(items))
	for _, item := range items {
		if item.Enabled && (len(request.SubscriptionIDs) == 0 || request.SubscriptionIDs[item.ID]) {
			eligible = append(eligible, item)
		}
	}
	policy := cloneMap(request.Policy)
	policy["excluded_countries"] = []any{}
	policy["url_limit"], policy["speed_candidates"], policy["keep"] = float64(0), float64(0), float64(0)
	policy["skip_speed"] = true
	for index, item := range eligible {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		report(dependencies.Progress, Progress{Phase: "whitelist", Message: "Проверяем подписку «" + item.Name + "»", Current: index + 1, Total: len(eligible), Pool: whitelist.Pool})
		links, readErr := dependencies.Cache.Read(ctx, item.ID)
		if readErr != nil || len(links) == 0 {
			reason := "cache_unavailable"
			result.Failures[item.ID] = reason
			updateOneQualificationStatus(ctx, dependencies, item.ID, "error", 0, 0, &reason)
			continue
		}
		aggregated := coremapper.Subscriptions([]subscriptions.SourceLinks{{ID: item.ID, Name: item.Name, Links: links}})
		if len(aggregated.Proxies) == 0 {
			reason := "no_supported_nodes"
			result.Failures[item.ID] = reason
			updateOneQualificationStatus(ctx, dependencies, item.ID, "error", 0, 0, &reason)
			continue
		}
		sources := make(map[string]qualification.Source, len(aggregated.SourceByNode))
		for name, source := range aggregated.SourceByNode {
			sources[name] = qualification.Source{ID: source.ID, Name: source.Name}
		}
		qualified, qualifyErr := dependencies.Qualifier.Qualify(ctx, whitelist.Pool, aggregated.Proxies, policy, sources)
		if qualifyErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			reason := errorName(qualifyErr)
			result.Failures[item.ID] = reason
			updateOneQualificationStatus(ctx, dependencies, item.ID, "error", len(aggregated.Proxies), 0, &reason)
			continue
		}
		tested, available := qualificationCounts(qualified.Report, len(aggregated.Proxies), len(qualified.Proxies))
		status := "ok"
		var statusError *string
		if available == 0 {
			reason := "no_available_servers"
			status, statusError = "unavailable", &reason
		}
		updateOneQualificationStatus(ctx, dependencies, item.ID, status, tested, available, statusError)
		nodes := make([]whitelist.Node, 0, len(qualified.Proxies))
		for _, proxy := range qualified.Proxies {
			name, _ := proxy["name"].(string)
			metric := qualified.Metrics[name]
			nodes = append(nodes, whitelist.Node{DisplayName: name, OriginPool: string(item.GroupName), Alive: true,
				DelayMS: metric.DelayMS, SpeedMbps: metric.SpeedMbps, StabilityRatio: metric.StabilityRatio,
				SourceID: item.ID, SourceName: item.Name, Proxy: proxy})
		}
		result.Sources[item.ID] = nodes
		result.CompletedSources = append(result.CompletedSources, item.ID)
	}
	return result, nil
}

func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input)+4)
	for key, value := range input {
		result[key] = value
	}
	return result
}
