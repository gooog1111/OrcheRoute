package subscriptions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxSubscriptionBody = 64 << 20

type HTTPFetcher struct {
	Client    *http.Client
	UserAgent string
}

type InlineFetcher struct{}
type WireGuardFetcher struct{}

func (InlineFetcher) Fetch(_ context.Context, subscription Subscription) ([]string, error) {
	if subscription.Parser != Inline {
		return nil, fmt.Errorf("unsupported_parser")
	}
	links := unique(Decode([]byte(subscription.Secret)))
	if len(links) == 0 {
		return nil, fmt.Errorf("subscription_returned_no_supported_links")
	}
	return links, nil
}

func (WireGuardFetcher) Fetch(_ context.Context, subscription Subscription) ([]string, error) {
	if subscription.Parser != WireGuard {
		return nil, fmt.Errorf("unsupported_parser")
	}
	links := unique(Decode([]byte(subscription.Secret)))
	if len(links) == 0 || !strings.HasPrefix(links[0], "wireguard://") {
		return nil, fmt.Errorf("invalid_wireguard_config")
	}
	return links, nil
}

func (fetcher HTTPFetcher) Fetch(ctx context.Context, subscription Subscription) ([]string, error) {
	if subscription.Parser != Standard {
		return nil, fmt.Errorf("unsupported_parser")
	}
	client := fetcher.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, subscription.Secret, nil)
	if err != nil {
		return nil, err
	}
	userAgent := fetcher.UserAgent
	if userAgent == "" {
		userAgent = "orcheroute/0.2"
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("subscription_http_%d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxSubscriptionBody+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxSubscriptionBody {
		return nil, fmt.Errorf("subscription_too_large")
	}
	links := Decode(payload)
	if len(links) == 0 {
		return nil, fmt.Errorf("subscription_returned_no_supported_links")
	}
	return unique(links), nil
}

type FetcherMap map[Parser]Fetcher

func (fetchers FetcherMap) Fetch(ctx context.Context, subscription Subscription) ([]string, error) {
	result, err := fetchers.FetchDetected(ctx, subscription)
	return result.Links, err
}

func (fetchers FetcherMap) FetchDetected(ctx context.Context, subscription Subscription) (FetchResult, error) {
	if subscription.Parser == Standard {
		standard := fetchers[Standard]
		if standard == nil {
			return FetchResult{}, fmt.Errorf("unsupported_parser")
		}
		return DetectAndFetch(ctx, subscription, standard, fetchers[BlackTemple])
	}
	fetcher := fetchers[subscription.Parser]
	if fetcher == nil {
		return FetchResult{}, fmt.Errorf("unsupported_parser")
	}
	links, err := fetcher.Fetch(ctx, subscription)
	return FetchResult{Parser: subscription.Parser, Links: links}, err
}
