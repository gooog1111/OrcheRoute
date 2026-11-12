package subscriptions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const maxSubscriptionBody = 64 << 20

type HTTPFetcher struct {
	Client     *http.Client
	UserAgent  string
	DeviceHWID string
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
	hwid, err := SelectedHWID(subscription, fetcher.DeviceHWID)
	if err != nil {
		return nil, err
	}
	if hwid != "" {
		if request.URL.Scheme != "https" {
			return nil, fmt.Errorf("hwid_requires_https")
		}
		request.Header.Set("X-Hwid", hwid)
		deviceOS := runtime.GOOS
		if deviceOS == "android" {
			deviceOS = "Android"
		} else if deviceOS == "linux" {
			deviceOS = "Linux"
		}
		request.Header.Set("X-Device-OS", deviceOS)
		copyClient := *client
		previous := copyClient.CheckRedirect
		copyClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if previous != nil {
				if err := previous(next, via); err != nil {
					return err
				}
			}
			if next.URL.Scheme != request.URL.Scheme || next.URL.Host != request.URL.Host {
				next.Header.Del("X-Hwid")
				next.Header.Del("X-Device-OS")
			}
			if next.URL.Scheme != "https" {
				return fmt.Errorf("hwid_redirect_requires_https")
			}
			if previous == nil && len(via) >= 10 {
				return fmt.Errorf("too_many_redirects")
			}
			return nil
		}
		client = &copyClient
	}
	response, err := client.Do(request)
	if err != nil {
		if failure, ok := err.(*url.Error); ok {
			return nil, fmt.Errorf("subscription_request_failed: %w", failure.Err)
		}
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
	if strings.EqualFold(response.Header.Get("X-Hwid-Not-Supported"), "true") || hwidNotice(links) {
		if hwid != "" {
			return nil, fmt.Errorf("subscription_hwid_rejected")
		}
		return nil, fmt.Errorf("subscription_hwid_required")
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("subscription_returned_no_supported_links")
	}
	return unique(links), nil
}

func hwidNotice(links []string) bool {
	if len(links) != 1 {
		return false
	}
	parsed, err := url.Parse(links[0])
	if err != nil {
		return false
	}
	return parsed.Hostname() == "0.0.0.0" && parsed.Port() == "1" && strings.Contains(strings.ToLower(parsed.Fragment), "hwid")
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
