package subscriptions

import (
	"context"
	"errors"
	"testing"
)

type detectionFetcher struct {
	links []string
	err   error
	calls int
}

func (fetcher *detectionFetcher) Fetch(_ context.Context, _ Subscription) ([]string, error) {
	fetcher.calls++
	return fetcher.links, fetcher.err
}

func TestDetectAndFetchConfirmsBlackTempleByProtocol(t *testing.T) {
	standard := &detectionFetcher{links: []string{"vless://bootstrap"}}
	special := &detectionFetcher{links: []string{"vless://one", "vless://two"}}
	result, err := DetectAndFetch(context.Background(), Subscription{
		Parser: Standard,
		Secret: "https://provider.invalid/sub/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-",
	}, standard, special)
	if err != nil || result.Parser != BlackTemple || len(result.Links) != 2 || special.calls != 1 {
		t.Fatalf("unexpected detection: %#v err=%v calls=%d", result, err, special.calls)
	}
}

func TestDetectAndFetchKeepsStandardWhenProtocolRejectsToken(t *testing.T) {
	standard := &detectionFetcher{links: []string{"vless://ordinary"}}
	special := &detectionFetcher{err: errors.New("not_blacktemple")}
	result, err := DetectAndFetch(context.Background(), Subscription{
		Parser: Standard,
		Secret: "https://provider.invalid/sub/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-",
	}, standard, special)
	if err != nil || result.Parser != Standard || len(result.Links) != 1 {
		t.Fatalf("unexpected fallback: %#v err=%v", result, err)
	}
}

func TestDetectAndFetchDoesNotProbeOrdinarySubscription(t *testing.T) {
	standard := &detectionFetcher{links: []string{"vless://ordinary"}}
	special := &detectionFetcher{links: []string{"vless://wrong"}}
	result, err := DetectAndFetch(context.Background(), Subscription{
		Parser: Standard, Secret: "https://provider.invalid/sub/short-token",
	}, standard, special)
	if err != nil || result.Parser != Standard || special.calls != 0 {
		t.Fatalf("ordinary source was probed: %#v err=%v calls=%d", result, err, special.calls)
	}
}
