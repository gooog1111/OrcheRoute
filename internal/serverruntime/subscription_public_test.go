//go:build linux

package serverruntime

import (
	"testing"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestSubscriptionPublicIncludesNextUpdate(t *testing.T) {
	item := subscriptions.Subscription{
		Enabled:         true,
		LastSuccess:     1_000,
		IntervalSeconds: 900,
	}

	got := subscriptionPublic(item)
	if got["next_update"] != int64(1_900) {
		t.Fatalf("next_update = %#v, want 1900", got["next_update"])
	}

	item.Enabled = false
	got = subscriptionPublic(item)
	if got["next_update"] != nil {
		t.Fatalf("disabled next_update = %#v, want nil", got["next_update"])
	}
}
