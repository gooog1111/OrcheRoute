package controller

import "testing"

func TestInternetLossFreezesSelection(t *testing.T) {
	state, decision := Step(State{Status: "proxy_ok", Active: "one"}, Observation{Now: 100, WAN: false, Active: "one", Control: Control{Enabled: true}}, DefaultPolicy())
	if state.Status != "internet_down" || decision.Action != "freeze" {
		t.Fatalf("state=%+v decision=%+v", state, decision)
	}
}

func TestFailureChoosesPrimaryBeforeEmergency(t *testing.T) {
	input := Observation{Now: 100, WAN: true, Active: "old", ActivePool: Primary, ActiveOK: false, Control: Control{Enabled: true}, Nodes: []Node{
		{Name: "emergency-fast", Pool: Emergency, Alive: true, Delay: 10},
		{Name: "primary-slow", Pool: Primary, Alive: true, Delay: 100},
	}}
	state, decision := Step(State{Status: "proxy_degraded", FailureStreak: 1}, input, DefaultPolicy())
	if decision.Target != "primary-slow" || decision.Action != "select" {
		t.Fatalf("state=%+v decision=%+v", state, decision)
	}
}

func TestAutomaticModeUsesEmergencyWhenPrimaryUnavailable(t *testing.T) {
	input := Observation{Now: 100, WAN: true, Active: "DIRECT-EGRESS", ActiveOK: false, Control: Control{Enabled: true, Mode: "auto"}, Nodes: []Node{
		{Name: "primary-dead", Pool: Primary, Alive: false, Delay: 0},
		{Name: "backup", Pool: Emergency, Alive: true, Delay: 30},
	}}
	state, decision := Step(State{}, input, DefaultPolicy())
	if state.Mode != "auto" || decision.Action != "select" || decision.Target != "backup" || decision.Pool != Emergency {
		t.Fatalf("state=%+v decision=%+v", state, decision)
	}
}

func TestFailbackRequiresStableChecks(t *testing.T) {
	policy := DefaultPolicy()
	input := Observation{Now: 100, WAN: true, Active: "backup", ActivePool: Emergency, ActiveOK: true, Control: Control{Enabled: true}, Nodes: []Node{{Name: "main", Pool: Primary, Alive: true, Delay: 20, LastTestedAt: 99}}}
	state, first := Step(State{}, input, policy)
	if first.Action != "refresh" || first.Pool != Primary {
		t.Fatalf("%+v", first)
	}
	input.Nodes[0].LastTestedAt = state.RecoveryRequestedAt
	input.Now = state.NextRecoveryCheck
	state, second := Step(state, input, policy)
	if second.Action != "keep" || second.Reason != "failback_probe" {
		t.Fatalf("%+v", second)
	}
	input.Now = state.NextRecoveryCheck
	_, third := Step(state, input, policy)
	if third.Action != "select" || third.Target != "main" {
		t.Fatalf("%+v", third)
	}
}

func TestFailbackNeverUsesStalePrimaryAfterRefreshRequest(t *testing.T) {
	policy := DefaultPolicy()
	input := Observation{Now: 100, WAN: true, Active: "backup", ActivePool: Emergency, ActiveOK: true, Control: Control{Enabled: true}, Nodes: []Node{{Name: "stale", Pool: Primary, Alive: true, LastTestedAt: 10}}}
	state, first := Step(State{}, input, policy)
	if first.Action != "refresh" {
		t.Fatalf("first=%+v", first)
	}
	input.Now = state.NextRecoveryCheck
	_, second := Step(state, input, policy)
	if second.Action == "select" {
		t.Fatalf("stale primary selected: %+v", second)
	}
}

func TestEmergencyModeNeverSelectsPrimary(t *testing.T) {
	input := Observation{Now: 100, WAN: true, Active: "main", ActivePool: Primary, ActiveOK: true, Control: Control{Enabled: true, Mode: "emergency"}, Nodes: []Node{
		{Name: "main", Pool: Primary, Alive: true, Delay: 5},
		{Name: "backup", Pool: Emergency, Alive: true, Delay: 30},
	}}
	state, decision := Step(State{}, input, DefaultPolicy())
	if state.Mode != "emergency" || decision.Action != "select" || decision.Target != "backup" {
		t.Fatalf("state=%+v decision=%+v", state, decision)
	}
}

func TestEmergencyModeKeepsHealthyEmergency(t *testing.T) {
	input := Observation{Now: 100, WAN: true, Active: "backup", ActivePool: Emergency, ActiveOK: true, Control: Control{Enabled: true, Mode: "emergency"}, Nodes: []Node{
		{Name: "main", Pool: Primary, Alive: true, Delay: 5},
		{Name: "backup", Pool: Emergency, Alive: true, Delay: 30},
	}}
	state, decision := Step(State{}, input, DefaultPolicy())
	if state.Status != "emergency_proxy_ok" || decision.Action != "keep" {
		t.Fatalf("state=%+v decision=%+v", state, decision)
	}
}

func TestCandidatesUseSharedLatencySpeedAndStabilityRating(t *testing.T) {
	input := Observation{Now: 100, WAN: true, Active: "old", ActivePool: Primary, ActiveOK: false, Control: Control{Enabled: true}, Nodes: []Node{
		{Name: "low-ping-unstable", Pool: Primary, Alive: true, Delay: 20, SpeedMbps: 3, HealthSuccesses: 1, HealthFailures: 10},
		{Name: "balanced", Pool: Primary, Alive: true, Delay: 55, SpeedMbps: 60, StabilityRatio: .9, HealthSuccesses: 20},
	}}
	_, decision := Step(State{Status: "proxy_degraded", FailureStreak: 1}, input, DefaultPolicy())
	if decision.Target != "balanced" {
		t.Fatalf("decision=%+v", decision)
	}
}
