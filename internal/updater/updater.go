package updater

import (
	"context"
	"fmt"
	"time"

	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

type StatusUpdater func(ctx context.Context, id, status string, links *int, statusError *string, success bool) error
type QualificationStatusUpdater func(ctx context.Context, id, status string, tested, available int, statusError *string) error
type EventRecorder func(ctx context.Context, eventType, severity, pool, reason string, details map[string]any) error
type ProgressReporter func(Progress)

type Qualifier interface {
	Qualify(ctx context.Context, pool string, proxies []map[string]any, settings map[string]any, sources map[string]qualification.Source) (qualification.Result, error)
}

type ProviderStore interface {
	Exists(pool string) bool
	WriteReport(pool string, report qualification.Report) error
	Write(pool string, result qualification.Result, sources map[string]subscriptions.SourceIdentity) error
	MergeSource(pool, sourceID string, result qualification.Result, sources map[string]subscriptions.SourceIdentity) error
}

type Dependencies struct {
	Repository                subscriptions.Repository
	Cache                     subscriptions.CacheRepository
	Fetcher                   subscriptions.Fetcher
	UpdateStatus              StatusUpdater
	UpdateQualificationStatus QualificationStatusUpdater
	RecordEvent               EventRecorder
	Qualifier                 Qualifier
	Providers                 ProviderStore
	Progress                  ProgressReporter
	Now                       func() time.Time
}

type Request struct {
	Groups          []subscriptions.Group
	RequestedGroups map[subscriptions.Group]bool
	SubscriptionIDs map[string]bool
	Force           bool
	FetchOnly       bool
	SkipFetch       bool
	CheckOnly       bool
	Policies        map[subscriptions.Group]map[string]any
}

type Progress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
	Pool    string `json:"pool,omitempty"`
}

type PoolResult struct {
	Sources  int            `json:"sources"`
	Fetched  int            `json:"fetched"`
	Accepted int            `json:"accepted"`
	Rejected int            `json:"rejected"`
	Errors   map[string]int `json:"errors"`
	Cached   bool           `json:"cached"`
	Reason   string         `json:"reason,omitempty"`
}

type Result struct {
	Failures []string                           `json:"failures"`
	Pools    map[subscriptions.Group]PoolResult `json:"pools"`
}

func Run(ctx context.Context, dependencies Dependencies, request Request) (Result, error) {
	if dependencies.Repository == nil || dependencies.Cache == nil || dependencies.Fetcher == nil || dependencies.Qualifier == nil || dependencies.Providers == nil {
		return Result{}, fmt.Errorf("incomplete updater dependencies")
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	groups := request.Groups
	if len(groups) == 0 {
		groups = []subscriptions.Group{subscriptions.Primary, subscriptions.Emergency}
	}
	result := Result{Failures: []string{}, Pools: map[subscriptions.Group]PoolResult{}}
	all, err := dependencies.Repository.List(ctx, true)
	if err != nil {
		return result, err
	}
	selected := map[subscriptions.Group]bool{}
	for _, group := range groups {
		selected[group] = true
	}
	eligible := []subscriptions.Subscription{}
	for _, item := range all {
		if item.Enabled && selected[item.GroupName] && (len(request.SubscriptionIDs) == 0 || request.SubscriptionIDs[item.ID]) {
			eligible = append(eligible, item)
		}
	}
	for index, item := range eligible {
		if request.SkipFetch {
			break
		}
		report(dependencies.Progress, Progress{Phase: "fetching", Message: "Получаем подписку «" + item.Name + "»", Current: index + 1, Total: len(eligible)})
		cached, cacheErr := dependencies.Cache.Read(ctx, item.ID)
		if cacheErr != nil {
			cached = []string{}
		}
		if !subscriptions.RefreshDue(now(), item.LastSuccess, item.IntervalSeconds, request.Force, cached) {
			continue
		}
		detectedParser := item.Parser
		var links []string
		var fetchErr error
		if detector, ok := dependencies.Fetcher.(subscriptions.DetectingFetcher); ok {
			var detected subscriptions.FetchResult
			detected, fetchErr = detector.FetchDetected(ctx, item)
			links, detectedParser = detected.Links, detected.Parser
		} else {
			links, fetchErr = dependencies.Fetcher.Fetch(ctx, item)
		}
		if fetchErr == nil && len(links) == 0 {
			fetchErr = fmt.Errorf("parser returned no links")
		}
		if fetchErr != nil {
			failure := item.ID
			result.Failures = appendUnique(result.Failures, failure)
			message := errorName(fetchErr)
			if dependencies.UpdateStatus != nil {
				_ = dependencies.UpdateStatus(ctx, item.ID, "error", nil, &message, false)
			}
			continue
		}
		// A complete non-empty fetch is authoritative. The previous cache is only
		// retained when fetching or parsing fails before this point.
		cache := subscriptions.NewCache(links, now())
		if err := dependencies.Cache.Write(ctx, item.ID, cache); err != nil {
			result.Failures = appendUnique(result.Failures, item.ID)
			continue
		}
		if detectedParser != "" && detectedParser != item.Parser {
			// Detection is already confirmed by a successful protocol fetch. Cache
			// remains usable even if this best-effort metadata migration fails.
			_, _ = dependencies.Repository.Update(ctx, item.ID, map[string]any{"parser": string(detectedParser)})
		}
		count := len(cache.Links)
		if dependencies.UpdateStatus != nil {
			_ = dependencies.UpdateStatus(ctx, item.ID, "ok", &count, nil, true)
		}
	}
	all, err = dependencies.Repository.List(ctx, true)
	if err != nil {
		return result, err
	}
	if request.FetchOnly {
		return result, nil
	}
	if request.CheckOnly && len(request.SubscriptionIDs) > 0 {
		return checkSubscriptions(ctx, dependencies, request, all)
	}
	for index, group := range groups {
		report(dependencies.Progress, Progress{Phase: "qualifying", Message: "Проверяем серверы пула " + string(group), Current: index + 1, Total: len(groups), Pool: string(group)})
		if !subscriptions.ShouldRebuildProvider(group, request.Force, request.RequestedGroups, dependencies.Providers.Exists(string(group))) {
			result.Pools[group] = PoolResult{Cached: true}
			continue
		}
		sources := []subscriptions.SourceLinks{}
		for _, item := range all {
			if !item.Enabled || item.GroupName != group {
				continue
			}
			links, cacheErr := dependencies.Cache.Read(ctx, item.ID)
			if cacheErr == nil && len(links) > 0 {
				sources = append(sources, subscriptions.SourceLinks{ID: item.ID, Name: item.Name, Links: links})
			}
		}
		aggregated := subscriptions.Aggregate(sources)
		poolResult := PoolResult{Sources: aggregated.Sources, Fetched: aggregated.Fetched, Rejected: sumErrors(aggregated.Errors), Errors: aggregated.Errors}
		if len(aggregated.Proxies) == 0 {
			if err := poolFailure(ctx, dependencies, result.Failures, group, "group_has_no_cached_links", poolResult); err != nil {
				return result, err
			}
			result.Failures = appendUnique(result.Failures, string(group))
			result.Pools[group] = poolResult
			continue
		}
		sourceMap := map[string]qualification.Source{}
		for name, source := range aggregated.SourceByNode {
			sourceMap[name] = qualification.Source{ID: source.ID, Name: source.Name}
		}
		policy := request.Policies[group]
		qualified, qualifyErr := dependencies.Qualifier.Qualify(ctx, string(group), aggregated.Proxies, policy, sourceMap)
		updateSourceQualificationStatus(ctx, dependencies, qualified, qualifyErr)
		if qualified.Report.Pool != "" {
			if reportErr := dependencies.Providers.WriteReport(string(group), qualified.Report); reportErr != nil {
				return result, reportErr
			}
		}
		if qualifyErr != nil || len(qualified.Proxies) == 0 {
			reason := "qualification_failed"
			if qualifyErr != nil {
				reason = errorName(qualifyErr)
			} else if qualified.Report.Input > 0 && qualified.Report.TCPAlive == 0 {
				reason = "all_servers_unavailable_tcp"
			} else if qualified.Report.URLAlive == 0 {
				reason = "all_servers_unavailable_url_test"
			} else if qualified.Report.Qualified == 0 {
				reason = "all_servers_failed_speed_or_stability"
			}
			poolResult.Reason = reason
			if err := poolFailure(ctx, dependencies, result.Failures, group, reason, poolResult); err != nil {
				return result, err
			}
			result.Failures = appendUnique(result.Failures, string(group))
			result.Pools[group] = poolResult
			continue
		}
		retainedSources := subscriptions.RetainSources(aggregated.SourceByNode, qualified.Proxies)
		if err := dependencies.Providers.Write(string(group), qualified, retainedSources); err != nil {
			return result, err
		}
		poolResult.Accepted = len(qualified.Proxies)
		result.Pools[group] = poolResult
		if dependencies.RecordEvent != nil {
			_ = dependencies.RecordEvent(ctx, "qualification_complete", "info", string(group), "subscription_aggregation", map[string]any{"sources": poolResult.Sources, "fetched": poolResult.Fetched, "accepted": poolResult.Accepted, "rejected": poolResult.Rejected})
		}
	}
	return result, nil
}

func checkSubscriptions(ctx context.Context, dependencies Dependencies, request Request, all []subscriptions.Subscription) (Result, error) {
	result := Result{Failures: []string{}, Pools: map[subscriptions.Group]PoolResult{}}
	selectedGroups := map[subscriptions.Group]bool{}
	for _, group := range request.Groups {
		selectedGroups[group] = true
	}
	if len(selectedGroups) == 0 {
		selectedGroups[subscriptions.Primary] = true
		selectedGroups[subscriptions.Emergency] = true
	}
	for _, item := range all {
		if !item.Enabled || !request.SubscriptionIDs[item.ID] || !selectedGroups[item.GroupName] {
			continue
		}
		pool := result.Pools[item.GroupName]
		pool.Sources++
		if pool.Errors == nil {
			pool.Errors = map[string]int{}
		}
		report(dependencies.Progress, Progress{Phase: "qualifying", Message: "Проверяем подписку «" + item.Name + "»", Current: 1, Total: 1, Pool: string(item.GroupName)})
		links, readErr := dependencies.Cache.Read(ctx, item.ID)
		if readErr != nil || len(links) == 0 {
			reason := "cache_unavailable"
			pool.Errors[reason]++
			pool.Reason = reason
			result.Failures = appendUnique(result.Failures, item.ID)
			updateOneQualificationStatus(ctx, dependencies, item.ID, "error", 0, 0, &reason)
			result.Pools[item.GroupName] = pool
			continue
		}
		aggregated := subscriptions.Aggregate([]subscriptions.SourceLinks{{ID: item.ID, Name: item.Name, Links: links}})
		pool.Fetched += aggregated.Fetched
		pool.Rejected += sumErrors(aggregated.Errors)
		for reason, count := range aggregated.Errors {
			pool.Errors[reason] += count
		}
		if len(aggregated.Proxies) == 0 {
			reason := "no_supported_nodes"
			pool.Errors[reason]++
			pool.Reason = reason
			result.Failures = appendUnique(result.Failures, item.ID)
			updateOneQualificationStatus(ctx, dependencies, item.ID, "error", 0, 0, &reason)
			result.Pools[item.GroupName] = pool
			continue
		}
		sourceMap := make(map[string]qualification.Source, len(aggregated.SourceByNode))
		for name, source := range aggregated.SourceByNode {
			sourceMap[name] = qualification.Source{ID: source.ID, Name: source.Name}
		}
		qualified, qualifyErr := dependencies.Qualifier.Qualify(ctx, string(item.GroupName), aggregated.Proxies, request.Policies[item.GroupName], sourceMap)
		tested, available := qualificationCounts(qualified.Report, len(aggregated.Proxies), len(qualified.Proxies))
		status := "ok"
		var statusError *string
		if qualifyErr != nil {
			reason := errorName(qualifyErr)
			status, statusError = "error", &reason
			pool.Reason = reason
			pool.Errors[reason]++
			result.Failures = appendUnique(result.Failures, item.ID)
		} else if available == 0 {
			reason := "no_available_servers"
			status, statusError = "unavailable", &reason
			pool.Reason = reason
			pool.Errors[reason]++
			result.Failures = appendUnique(result.Failures, item.ID)
		}
		if qualifyErr == nil {
			retainedSources := subscriptions.RetainSources(aggregated.SourceByNode, qualified.Proxies)
			if mergeErr := dependencies.Providers.MergeSource(string(item.GroupName), item.ID, qualified, retainedSources); mergeErr != nil {
				return result, fmt.Errorf("merge checked subscription %s: %w", item.ID, mergeErr)
			}
		}
		updateOneQualificationStatus(ctx, dependencies, item.ID, status, tested, available, statusError)
		pool.Accepted += available
		if tested > available {
			pool.Rejected += tested - available
		}
		result.Pools[item.GroupName] = pool
	}
	return result, nil
}

func updateSourceQualificationStatus(ctx context.Context, dependencies Dependencies, result qualification.Result, qualifyErr error) {
	if dependencies.UpdateQualificationStatus == nil {
		return
	}
	for id, source := range result.Report.Sources {
		tested, available := qualificationCountsFromSource(source)
		status := "ok"
		var statusError *string
		if qualifyErr != nil {
			reason := errorName(qualifyErr)
			status, statusError = "error", &reason
		} else if available == 0 {
			reason := "no_available_servers"
			status, statusError = "unavailable", &reason
		}
		updateOneQualificationStatus(ctx, dependencies, id, status, tested, available, statusError)
	}
}

func updateOneQualificationStatus(ctx context.Context, dependencies Dependencies, id, status string, tested, available int, statusError *string) {
	if dependencies.UpdateQualificationStatus != nil {
		_ = dependencies.UpdateQualificationStatus(ctx, id, status, tested, available, statusError)
	}
}

func qualificationCounts(report qualification.Report, testedFallback, availableFallback int) (int, int) {
	tested, available := report.Input, report.Qualified
	if tested == 0 {
		tested = testedFallback
	}
	if available == 0 && report.Qualified == 0 && report.Retained > 0 {
		available = report.Retained
	}
	if available == 0 && report.Input == 0 {
		available = availableFallback
	}
	return tested, available
}

func qualificationCountsFromSource(report *qualification.SourceReport) (int, int) {
	if report == nil {
		return 0, 0
	}
	available := report.Qualified
	if available == 0 && report.Retained > 0 {
		available = report.Retained
	}
	return report.Input, available
}

func poolFailure(ctx context.Context, dependencies Dependencies, _ []string, group subscriptions.Group, reason string, details PoolResult) error {
	if dependencies.RecordEvent != nil {
		_ = dependencies.RecordEvent(ctx, "qualification_failed", "warning", string(group), reason, map[string]any{"sources": details.Sources, "fetched": details.Fetched, "rejected": details.Rejected})
	}
	if !dependencies.Providers.Exists(string(group)) {
		return fmt.Errorf("%s: %s", group, reason)
	}
	return nil
}
func report(callback ProgressReporter, value Progress) {
	if callback != nil {
		callback(value)
	}
}
func sumErrors(values map[string]int) int {
	result := 0
	for _, value := range values {
		result += value
	}
	return result
}
func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}
func errorName(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
