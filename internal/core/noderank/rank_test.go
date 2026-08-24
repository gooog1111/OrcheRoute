package noderank

import "testing"

func TestRankCombinesLatencySpeedAndHealth(t *testing.T) {
	nodes := []Node{
		{ID: "unstable-fast", Alive: true, DelayMS: 30, SpeedMbps: 100, HealthSuccesses: 1, HealthFailures: 12},
		{ID: "stable", Alive: true, DelayMS: 55, SpeedMbps: 60, HealthSuccesses: 30, HealthFailures: 1},
		{ID: "dead", Alive: false, DelayMS: 10, SpeedMbps: 500},
	}
	ranked := Rank(nodes)
	if ranked[0].ID != "stable" || ranked[len(ranked)-1].ID != "dead" {
		t.Fatalf("unexpected order: %#v", ranked)
	}
}

func TestMissingSpeedIsNeutral(t *testing.T) {
	unknown := Score(Node{Alive: true, DelayMS: 40})
	knownSlow := Score(Node{Alive: true, DelayMS: 40, SpeedMbps: 1})
	if unknown <= knownSlow {
		t.Fatalf("missing evidence treated as failure: unknown=%v slow=%v", unknown, knownSlow)
	}
}

func TestEmergencyModeNeverSelectsPrimary(t *testing.T) {
	nodes := []Node{
		{ID: "primary", Pool: "primary", Alive: true, DelayMS: 5},
		{ID: "emergency", Pool: "emergency", Alive: true, DelayMS: 100},
	}
	selected := Select(nodes, "emergency")
	if selected == nil || selected.ID != "emergency" {
		t.Fatalf("emergency mode escaped its pool: %#v", selected)
	}
}

func TestAutoPrefersPrimaryBeforeHigherRatedEmergency(t *testing.T) {
	nodes := []Node{
		{ID: "primary", Pool: "primary", Alive: true, DelayMS: 300, SpeedMbps: 5},
		{ID: "emergency", Pool: "emergency", Alive: true, DelayMS: 5, SpeedMbps: 100},
	}
	selected := Select(nodes, "auto")
	if selected == nil || selected.ID != "primary" {
		t.Fatalf("auto did not prefer primary: %#v", selected)
	}
}
