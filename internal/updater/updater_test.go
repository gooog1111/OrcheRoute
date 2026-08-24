package updater

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

type fakeRepository struct {
	items   []subscriptions.Subscription
	updates map[string]map[string]any
}

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
func (repository *fakeRepository) Update(_ context.Context, id string, changes map[string]any) (*subscriptions.Subscription, error) {
	if repository.updates == nil {
		repository.updates = map[string]map[string]any{}
	}
	repository.updates[id] = changes
	for index := range repository.items {
		if repository.items[index].ID != id {
			continue
		}
		if parser, ok := changes["parser"].(string); ok {
			repository.items[index].Parser = subscriptions.Parser(parser)
		}
		copy := repository.items[index]
		return &copy, nil
	}
	return nil, nil
}
func (repository *fakeRepository) Delete(context.Context, string) (bool, error) { return false, nil }

type fakeFetcher struct {
	calls []string
	links map[string][]string
}

type detectingFakeFetcher struct {
	result subscriptions.FetchResult
}

func (fetcher *detectingFakeFetcher) Fetch(context.Context, subscriptions.Subscription) ([]string, error) {
	return fetcher.result.Links, nil
}

func (fetcher *detectingFakeFetcher) FetchDetected(context.Context, subscriptions.Subscription) (subscriptions.FetchResult, error) {
	return fetcher.result, nil
}

func (fetcher *fakeFetcher) Fetch(_ context.Context, item subscriptions.Subscription) ([]string, error) {
	fetcher.calls = append(fetcher.calls, item.ID)
	return fetcher.links[item.ID], nil
}

type fakeQualifier struct{}

func (fakeQualifier) Qualify(_ context.Context, pool string, proxies []map[string]any, _ map[string]any, _ map[string]qualification.Source) (qualification.Result, error) {
	return qualification.Result{Proxies: proxies, Report: qualification.Report{Pool: pool, Input: len(proxies), Retained: len(proxies)}}, nil
}

type recordingQualifier struct {
	calls int
	input int
}

func (qualifier *recordingQualifier) Qualify(_ context.Context, pool string, proxies []map[string]any, _ map[string]any, sources map[string]qualification.Source) (qualification.Result, error) {
	qualifier.calls++
	qualifier.input += len(proxies)
	reports := map[string]*qualification.SourceReport{}
	for _, source := range sources {
		reports[source.ID] = &qualification.SourceReport{Input: len(proxies), Qualified: len(proxies), Retained: len(proxies)}
	}
	return qualification.Result{Proxies: proxies, Report: qualification.Report{Pool: pool, Input: len(proxies), Qualified: len(proxies), Retained: len(proxies), Sources: reports}}, nil
}

func TestCheckOnlyMergesRequestedSubscriptionWithoutReplacingOtherSources(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	cache := subscriptions.FileCache{Directory: filepath.Join(directory, "cache")}
	one := "trojan://one@one.example:443#One"
	two := "trojan://two@two.example:443#Two"
	if err := cache.Write(ctx, "one", subscriptions.NewCache([]string{one}, time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := cache.Write(ctx, "two", subscriptions.NewCache([]string{two}, time.Now())); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{items: []subscriptions.Subscription{
		{ID: "one", Name: "One", GroupName: subscriptions.Primary, Enabled: true},
		{ID: "two", Name: "Two", GroupName: subscriptions.Primary, Enabled: true},
	}}
	providers := FileProviderStore{ProvidersDirectory: filepath.Join(directory, "providers")}
	if err := providers.Write("primary", qualification.Result{Proxies: []map[string]any{{"name": "existing"}}}, map[string]subscriptions.SourceIdentity{
		"existing": {ID: "two", Name: "Two"},
	}); err != nil {
		t.Fatal(err)
	}
	qualifier := &recordingQualifier{}
	statusID, tested, available := "", 0, 0
	result, err := Run(ctx, Dependencies{
		Repository: repository, Cache: cache, Fetcher: &fakeFetcher{}, Qualifier: qualifier, Providers: providers,
		UpdateQualificationStatus: func(_ context.Context, id, _ string, currentTested, currentAvailable int, _ *string) error {
			statusID, tested, available = id, currentTested, currentAvailable
			return nil
		},
	}, Request{Groups: []subscriptions.Group{subscriptions.Primary}, SubscriptionIDs: map[string]bool{"one": true}, SkipFetch: true, CheckOnly: true, Policies: map[subscriptions.Group]map[string]any{subscriptions.Primary: {}}})
	if err != nil {
		t.Fatal(err)
	}
	if qualifier.calls != 1 || qualifier.input != 1 || statusID != "one" || tested != 1 || available != 1 {
		t.Fatalf("calls=%d input=%d status=%q tested=%d available=%d", qualifier.calls, qualifier.input, statusID, tested, available)
	}
	if result.Pools[subscriptions.Primary].Accepted != 1 {
		t.Fatalf("result=%#v", result)
	}
	payload, err := os.ReadFile(filepath.Join(directory, "providers", "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provider struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := json.Unmarshal(payload, &provider); err != nil {
		t.Fatal(err)
	}
	if len(provider.Proxies) != 2 {
		t.Fatalf("merged provider=%#v", provider.Proxies)
	}
	metadataPayload, err := os.ReadFile(filepath.Join(directory, "providers", "primary.sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Nodes map[string]map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(metadataPayload, &metadata); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, value := range metadata.Nodes {
		counts[value["id"].(string)]++
	}
	if counts["one"] != 1 || counts["two"] != 1 {
		t.Fatalf("source counts=%#v", counts)
	}
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

func TestSuccessfulAutomaticDetectionPersistsConfirmedParser(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository := &fakeRepository{items: []subscriptions.Subscription{{
		ID: "black", Name: "BlackTemple", GroupName: subscriptions.Primary,
		Parser: subscriptions.Standard, Secret: "https://example.test/opaque", Enabled: true,
		IntervalSeconds: 300, LastSuccess: 1,
	}}}
	link := "vless://11111111-1111-1111-1111-111111111111@one.example:443#One"
	_, err := Run(ctx, Dependencies{
		Repository: repository,
		Cache:      subscriptions.FileCache{Directory: filepath.Join(directory, "cache")},
		Fetcher:    &detectingFakeFetcher{result: subscriptions.FetchResult{Parser: subscriptions.BlackTemple, Links: []string{link}}},
		Qualifier:  fakeQualifier{},
		Providers:  FileProviderStore{ProvidersDirectory: filepath.Join(directory, "providers")},
		Now:        func() time.Time { return time.Unix(1000, 0) },
	}, Request{Groups: []subscriptions.Group{subscriptions.Primary}, RequestedGroups: map[subscriptions.Group]bool{subscriptions.Primary: true}, Policies: map[subscriptions.Group]map[string]any{subscriptions.Primary: {}}})
	if err != nil {
		t.Fatal(err)
	}
	if repository.items[0].Parser != subscriptions.BlackTemple {
		t.Fatalf("parser was not persisted: %#v", repository.updates)
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
