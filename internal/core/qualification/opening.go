package qualification

import (
	"context"
	"net/http"
)

// OpeningAllowed uses the supplied per-candidate proxy client, never a direct
// fallback. HTTP errors including 403 do not mean that a site opened.
func OpeningAllowed(ctx context.Context, client *http.Client, target, mode string) bool {
	if ctx.Err() != nil {
		return false
	}
	if mode == "off" || mode == "" {
		return true
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	opened := false
	if err == nil {
		response, requestErr := client.Do(request)
		if response != nil {
			opened = requestErr == nil && response.StatusCode >= 200 && response.StatusCode < 300
			response.Body.Close()
		}
	}
	if mode == "require" {
		return ctx.Err() == nil && opened
	}
	return ctx.Err() == nil && mode == "exclude" && !opened
}
