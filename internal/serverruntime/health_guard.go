package serverruntime

import "time"

// A pending outage must not be charged to the VPN nodes while the physical
// network monitor deliberately keeps its previous confirmed classification.
func deferNodeHealth(snapshot ConnectivitySnapshot, now int64, interval time.Duration) bool {
	maxAge := 3 * interval
	if maxAge < 30*time.Second {
		maxAge = 30 * time.Second
	}
	return snapshot.UpdatedAt == 0 || snapshot.Error != "" ||
		snapshot.State != snapshot.ObservedState ||
		now-snapshot.UpdatedAt > int64(maxAge.Seconds())
}
