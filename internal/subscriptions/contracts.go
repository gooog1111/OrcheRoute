package subscriptions

import (
	"context"
	"time"
)

// Repository is implemented by platform storage (SQLite on the server,
// Room/Keychain-backed storage on mobile, or an application database on
// desktop). Secrets never need to cross the portable API boundary unmasked.
type Repository interface {
	List(ctx context.Context, includeSecret bool) ([]Subscription, error)
	Get(ctx context.Context, id string, includeSecret bool) (*Subscription, error)
	Create(ctx context.Context, subscription Subscription) (*Subscription, error)
	Update(ctx context.Context, id string, changes map[string]any) (*Subscription, error)
	Delete(ctx context.Context, id string) (bool, error)
}

type Fetcher interface {
	Fetch(ctx context.Context, subscription Subscription) ([]string, error)
}

// DetectingFetcher reports the adapter that actually accepted an automatic
// subscription. Platform updaters use this to persist a confirmed protocol
// instead of repeating detection on every refresh.
type DetectingFetcher interface {
	FetchDetected(ctx context.Context, subscription Subscription) (FetchResult, error)
}

type CacheRepository interface {
	Read(ctx context.Context, subscriptionID string) ([]string, error)
	Write(ctx context.Context, subscriptionID string, cache Cache) error
	Remove(ctx context.Context, subscriptionID string) error
}

type RefreshItem struct {
	Subscription Subscription `json:"subscription"`
	Fetch        bool         `json:"fetch"`
	Reason       string       `json:"reason"`
}

// PlanRefresh is the deterministic scheduling half of subscription updates.
// Fetching, locking and persistence remain adapters owned by each platform.
func PlanRefresh(all []Subscription, groups []Group, requestedIDs map[string]bool, cacheAvailable map[string]bool, force bool, now time.Time) []RefreshItem {
	selectedGroups := map[Group]bool{}
	for _, group := range groups {
		selectedGroups[group] = true
	}
	result := []RefreshItem{}
	for _, item := range all {
		if !item.Enabled || !selectedGroups[item.GroupName] || (len(requestedIDs) > 0 && !requestedIDs[item.ID]) {
			continue
		}
		reason := "cached"
		fetch := false
		switch {
		case force:
			fetch, reason = true, "forced"
		case !cacheAvailable[item.ID]:
			fetch, reason = true, "cache_missing"
		case now.Unix()-item.LastSuccess >= int64(item.IntervalSeconds):
			fetch, reason = true, "interval_elapsed"
		}
		result = append(result, RefreshItem{Subscription: item, Fetch: fetch, Reason: reason})
	}
	return result
}

func ShouldRebuildProvider(group Group, force bool, requestedGroups map[Group]bool, providerExists bool) bool {
	return force || requestedGroups[group] || !providerExists
}
