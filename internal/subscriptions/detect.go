package subscriptions

import (
	"context"
	"net/url"
	"strings"
)

// FetchResult keeps the user-facing automatic parser separate from the
// internal adapter that successfully handled the source.
type FetchResult struct {
	Parser Parser
	Links  []string
}

// DetectAndFetch confirms special protocols by executing their adapter. It
// never assigns BlackTemple from a hostname, subscription name or node label.
// Ordinary HTTP remains the fallback when the special protocol rejects the
// opaque token.
func DetectAndFetch(ctx context.Context, subscription Subscription, standard, blackTemple Fetcher) (FetchResult, error) {
	if subscription.Parser == BlackTemple {
		links, err := blackTemple.Fetch(ctx, subscription)
		return FetchResult{Parser: BlackTemple, Links: links}, err
	}
	links, err := standard.Fetch(ctx, subscription)
	if err != nil {
		return FetchResult{}, err
	}
	result := FetchResult{Parser: Standard, Links: links}
	if blackTemple == nil || len(links) > 1 || !hasOpaqueProtocolToken(subscription.Secret) {
		return result, nil
	}
	candidate := subscription
	candidate.Parser = BlackTemple
	if specialLinks, specialErr := blackTemple.Fetch(ctx, candidate); specialErr == nil && len(specialLinks) > 0 {
		return FetchResult{Parser: BlackTemple, Links: specialLinks}, nil
	}
	return result, nil
}

// The long opaque credential only selects candidates for active protocol
// verification. It is not sufficient to classify a source by itself.
func hasOpaqueProtocolToken(value string) bool {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		for _, key := range []string{"token", "url", "link", "subscription"} {
			if nested := strings.TrimSpace(parsed.Query().Get(key)); nested != "" {
				return hasOpaqueProtocolToken(nested)
			}
		}
	}
	token := normalizeSubscriptionToken(value)
	if len(token) < 56 {
		return false
	}
	for _, character := range token {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}
