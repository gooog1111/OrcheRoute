package updater

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

type whitelistQualifier struct {
	policies []map[string]any
}

func (qualifier *whitelistQualifier) Qualify(_ context.Context, pool string, proxies []map[string]any, policy map[string]any, _ map[string]qualification.Source) (qualification.Result, error) {
	qualifier.policies = append(qualifier.policies, policy)
	return qualification.Result{Proxies: proxies, Report: qualification.Report{Pool: pool}, Metrics: map[string]qualification.NodeMetrics{}}, nil
}

func TestRunWhitelistUsesEachEnabledCacheAsAtomicSource(t *testing.T) {
	ctx := context.Background()
	cache := subscriptions.FileCache{Directory: filepath.Join(t.TempDir(), "cache")}
	linkA := "vless://11111111-1111-1111-1111-111111111111@one.example:443#One"
	linkB := "trojan://pw@two.example:443#Two"
	if err := cache.Write(ctx, "a", subscriptions.NewCache([]string{linkA}, time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := cache.Write(ctx, "b", subscriptions.NewCache([]string{linkB}, time.Now())); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{items: []subscriptions.Subscription{
		{ID: "a", Name: "A", GroupName: subscriptions.Primary, Enabled: true},
		{ID: "b", Name: "B", GroupName: subscriptions.Emergency, Enabled: true},
		{ID: "off", Name: "Off", GroupName: subscriptions.Primary, Enabled: false},
	}}
	qualifier := &whitelistQualifier{}
	result, err := RunWhitelist(ctx, Dependencies{Repository: repository, Cache: cache, Qualifier: qualifier}, WhitelistRequest{Policy: map[string]any{"excluded_countries": []any{"RU"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CompletedSources) != 2 || len(result.Sources["a"]) != 1 || len(result.Sources["b"]) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Sources["a"][0].OriginPool != "primary" || result.Sources["b"][0].OriginPool != "emergency" {
		t.Fatalf("origin pools were lost: %#v", result.Sources)
	}
	for _, policy := range qualifier.policies {
		if policy["skip_speed"] != true || len(policy["excluded_countries"].([]any)) != 0 {
			t.Fatalf("unsafe whitelist policy: %#v", policy)
		}
	}
}

func TestRunWhitelistDoesNotCompleteMissingCache(t *testing.T) {
	result, err := RunWhitelist(context.Background(), Dependencies{Repository: &fakeRepository{items: []subscriptions.Subscription{{ID: "missing", Enabled: true}}}, Cache: subscriptions.FileCache{Directory: t.TempDir()}, Qualifier: &whitelistQualifier{}}, WhitelistRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CompletedSources) != 0 || result.Failures["missing"] != "cache_unavailable" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
