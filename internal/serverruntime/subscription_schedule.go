package serverruntime

import (
	"context"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"time"
)

// A timer must read the current registry, not a schedule cached at startup.
func scheduledSubscriptionIDs(items []subscriptions.Subscription, now int64) []string {
	ids := []string{}
	for _, item := range items {
		if !item.Enabled || item.IntervalSeconds <= 0 {
			continue
		}
		due := item.LastSuccess + int64(item.IntervalSeconds)
		if item.LastSuccess == 0 {
			due = 0
		}
		// Failed downloads retry at most once in five minutes, preserving old data.
		if item.LastAttempt > item.LastSuccess && item.LastAttempt+300 > due {
			due = item.LastAttempt + 300
		}
		if now >= due {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func (r *Runtime) RunSubscriptionMonitor(ctx context.Context) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			items, err := r.Store.List(ctx, false)
			if err == nil {
				if ids := scheduledSubscriptionIDs(items, time.Now().Unix()); len(ids) > 0 {
					_, _ = r.startUpdate(ids, "scheduled")
				}
			}
			timer.Reset(30 * time.Second)
		}
	}
}
