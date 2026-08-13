package updater

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

type fakeRepository struct{ items []subscriptions.Subscription }

func (repository *fakeRepository) List(context.Context, bool) ([]subscriptions.Subscription, error) {
	return append([]subscriptions.Subscription{}, repository.items...), nil
}
func (repository *fakeRepository) Get(_ context.Context, id string, _ bool) (*subscriptions.Subscription, error) {
	for _, item := range repository.items {
		if item.ID == id {
			copy := item
			return &copy, nil
		}
	}
	return nil, nil
}
func (repository *fakeRepository) Create(context.Context, subscriptions.Subscription) (*subscriptions.Subscription, error) {
	return nil, nil
}
func (repository *fakeRepository) Update(context.Context, string, map[string]any) (*subscriptions.Subscription, error) {
	return nil, nil
}
func (repository *fakeRepository) Delete(context.Context, string) (bool, error) { return false, nil }

type fakeFetcher struct {
	calls []string
	links map[string][]string
}

func (fetcher *fakeFetcher) Fetch(_ context.Context, item subscriptions.Subscription) ([]string, error) {
	fetcher.calls = append(fetcher.calls, item.ID)
	return fetcher.links[item.ID], nil
}

type fakeQualifier struct{}

func (fakeQualifier) Qualify(_ context.Context, pool string, proxies []map[string]any, _ map[string]any, _ map[string]qualification.Source) (qualification.Result, error) {
	return qualification.Result{Proxies: proxies, Report: qualification.Report{Pool: pool, Input: len(proxies), Retained: len(proxies)}}, nil
}

func TestRunUsesFreshCacheAndRebuildsRequestedPool(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	cache := subscriptions.FileCache{Directory: filepath.Join(directory, "cache")}
	one := "vless://11111111-1111-1111-1111-111111111111@one.example:443#One"
	two := "trojan://pw@two.example:443#Two"
	if err := cache.Write(ctx, "fresh", subscriptions.NewCache([]string{one}, time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{items: []subscriptions.Subscription{
		{ID: "fresh", Name: "Fresh", GroupName: subscriptions.Primary, Parser: subscriptions.Standard, Secret: "https://one", Enabled: true, IntervalSeconds: 900, LastSuccess: 900},
		{ID: "due", Name: "Due", GroupName: subscriptions.Primary, Parser: subscriptions.Standard, Secret: "https://two", Enabled: true, IntervalSeconds: 300, LastSuccess: 1},
	}}
	fetcher := &fakeFetcher{links: map[string][]string{"due": {two}}}
	statusCalls := 0
	providers := FileProviderStore{ProvidersDirectory: filepath.Join(directory, "providers"), ReportsDirectory: filepath.Join(directory, "reports")}
	result, err := Run(ctx, Dependencies{Repository: repository, Cache: cache, Fetcher: fetcher, UpdateStatus: func(context.Context, string, string, *int, *string, bool) error { statusCalls++; return nil }, Qualifier: fakeQualifier{}, Providers: providers, Now: func() time.Time { return time.Unix(1000, 0) }}, Request{Groups: []subscriptions.Group{subscriptions.Primary}, RequestedGroups: map[subscriptions.Group]bool{subscriptions.Primary: true}, Policies: map[subscriptions.Group]map[string]any{subscriptions.Primary: {}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetcher.calls) != 1 || fetcher.calls[0] != "due" || statusCalls != 1 {
		t.Fatalf("unexpected fetch calls: %#v status=%d", fetcher.calls, statusCalls)
	}
	pool := result.Pools[subscriptions.Primary]
	if pool.Fetched != 2 || pool.Accepted != 2 || pool.Cached {
		t.Fatalf("unexpected pool: %#v", pool)
	}
	if !providers.Exists("primary") {
		t.Fatal("provider not written")
	}
}

func TestSuccessfulFetchReplacesPreviousCache(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	cache := subscriptions.FileCache{Directory: filepath.Join(directory, "cache")}
	oldLink := "trojan://old@old.example:443#Old"
	newLink := "trojan://new@new.example:443#New"
	if err := cache.Write(ctx, "due", subscriptions.NewCache([]string{oldLink}, time.Unix(1, 0))); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{items: []subscriptions.Subscription{{
		ID: "due", Name: "Due", GroupName: subscriptions.Primary, Parser: subscriptions.Standard,
		Secret: "https://example.test/sub", Enabled: true, IntervalSeconds: 300, LastSuccess: 1,
	}}}
	fetcher := &fakeFetcher{links: map[string][]string{"due": {newLink}}}
	_, err := Run(ctx, Dependencies{
		Repository: repository, Cache: cache, Fetcher: fetcher, Qualifier: fakeQualifier{},
		Providers: FileProviderStore{ProvidersDirectory: filepath.Join(directory, "providers")},
		Now:       func() time.Time { return time.Unix(1000, 0) },
	}, Request{Groups: []subscriptions.Group{subscriptions.Primary}, RequestedGroups: map[subscriptions.Group]bool{subscriptions.Primary: true}, Policies: map[subscriptions.Group]map[string]any{subscriptions.Primary: {}}})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := cache.Read(ctx, "due")
	if err != nil || len(stored) != 1 || stored[0] != newLink {
		t.Fatalf("cache=%v err=%v", stored, err)
	}
}

func TestScheduledRunKeepsHealthyProvider(t *testing.T) {
	directory := t.TempDir()
	providers := FileProviderStore{ProvidersDirectory: directory}
	if err := atomicJSON(filepath.Join(directory, "primary.json"), map[string]any{"proxies": []any{}}); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Dependencies{Repository: &fakeRepository{}, Cache: subscriptions.FileCache{Directory: filepath.Join(directory, "cache")}, Fetcher: &fakeFetcher{}, Qualifier: fakeQualifier{}, Providers: providers}, Request{Groups: []subscriptions.Group{subscriptions.Primary}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pools[subscriptions.Primary].Cached {
		t.Fatal("healthy provider was rebuilt")
	}
}
