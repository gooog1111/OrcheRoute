package serverruntime

import (
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/core/connectivity"
)

func TestNodeHealthWaitsForReliablePhysicalObservation(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot ConnectivitySnapshot
		wait     bool
	}{
		{"normal", ConnectivitySnapshot{State: connectivity.Normal, ObservedState: connectivity.Normal, UpdatedAt: 100}, false},
		{"transient outage", ConnectivitySnapshot{State: connectivity.Normal, ObservedState: connectivity.Offline, UpdatedAt: 100}, true},
		{"pending allowlist", ConnectivitySnapshot{State: connectivity.Normal, ObservedState: connectivity.Allowlist, UpdatedAt: 100}, true},
		{"pending recovery", ConnectivitySnapshot{State: connectivity.Allowlist, ObservedState: connectivity.Normal, UpdatedAt: 100}, true},
		{"probe error", ConnectivitySnapshot{State: connectivity.Normal, ObservedState: connectivity.Normal, UpdatedAt: 100, Error: "probe failed"}, true},
		{"stale", ConnectivitySnapshot{State: connectivity.Normal, ObservedState: connectivity.Normal, UpdatedAt: 50}, true},
		{"startup", ConnectivitySnapshot{}, true},
		{"confirmed offline", ConnectivitySnapshot{State: connectivity.Offline, ObservedState: connectivity.Offline, UpdatedAt: 100}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := deferNodeHealth(test.snapshot, 105, 10*time.Second); got != test.wait {
				t.Fatalf("got %v, want %v", got, test.wait)
			}
		})
	}
}
