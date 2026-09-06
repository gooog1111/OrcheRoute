package connectivity

import "time"

// StabilityWindow distinguishes selecting a transport from sustained successful
// traffic. Observations must refer to the same selected node and network epoch.
type StabilityWindow struct {
	key         string
	since, last time.Time
	successes   int
}

func (window *StabilityWindow) Observe(now time.Time, key string, successful bool, minimum time.Duration, samples int) bool {
	if key == "" || !successful {
		*window = StabilityWindow{}
		return false
	}
	if window.key != key || now.Before(window.last) {
		*window = StabilityWindow{key: key, since: now}
	}
	// Re-reading the same sample is not another independent success.
	if window.last.IsZero() || now.After(window.last) {
		window.successes++
	}
	window.last = now
	return window.successes >= samples && now.Sub(window.since) >= minimum
}
