package serverruntime

import (
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"reflect"
	"testing"
)

func TestScheduledSubscriptionsHonorIntervalsAndFailureBackoff(t *testing.T) {
	items := []subscriptions.Subscription{
		{ID: "due", Enabled: true, IntervalSeconds: 60, LastSuccess: 1000, LastAttempt: 1000},
		{ID: "fresh", Enabled: true, IntervalSeconds: 3600, LastSuccess: 1000},
		{ID: "failed", Enabled: true, IntervalSeconds: 60, LastSuccess: 1000, LastAttempt: 1070},
		{ID: "disabled", IntervalSeconds: 60, LastSuccess: 1000},
		{ID: "new", Enabled: true, IntervalSeconds: 60},
	}
	if got := scheduledSubscriptionIDs(items, 1100); !reflect.DeepEqual(got, []string{"due", "new"}) {
		t.Fatal(got)
	}
	if got := scheduledSubscriptionIDs(items, 1370); !reflect.DeepEqual(got, []string{"due", "failed", "new"}) {
		t.Fatal(got)
	}
}
