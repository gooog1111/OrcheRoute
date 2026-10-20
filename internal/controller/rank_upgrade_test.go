package controller

import "testing"

func TestManagedRankUpgradeRequiresTwoObservations(t *testing.T) {
	input := Observation{Now: 100, WAN: true, ActiveOK: true, Active: "slow", ActivePool: Primary, Control: Control{Enabled: true}, Nodes: []Node{
		{Name: "slow", Pool: Primary, Alive: true, Delay: 500, SpeedMbps: 1},
		{Name: "fast", Pool: Primary, Alive: true, Delay: 30, SpeedMbps: 50},
	}}
	state, first := Step(State{}, input, ManagedPolicy())
	if first.Action != "keep" {
		t.Fatal(first)
	}
	input.Now = 110
	state, early := Step(state, input, ManagedPolicy())
	if early.Action != "keep" || state.RankStreak != 1 {
		t.Fatal(state, early)
	}
	input.Now = 120
	_, second := Step(state, input, ManagedPolicy())
	if second.Action != "select" || second.Target != "fast" || second.Reason != "higher_rank_stable" {
		t.Fatal(second)
	}
	_, sticky := Step(State{}, input, DefaultPolicy())
	if sticky.Action != "keep" {
		t.Fatal("default platform policy changed", sticky)
	}
	input.Control.Mode, input.Control.ManualNode = "manual", "slow"
	_, manual := Step(state, input, ManagedPolicy())
	if manual.Target == "fast" {
		t.Fatal("manual selection was overridden", manual)
	}
}

func TestSmallRankDifferenceDoesNotCauseFlapping(t *testing.T) {
	input := Observation{Now: 100, WAN: true, ActiveOK: true, Active: "a", ActivePool: Primary, Control: Control{Enabled: true}, Nodes: []Node{
		{Name: "a", Pool: Primary, Alive: true, Delay: 30, SpeedMbps: 50},
		{Name: "b", Pool: Primary, Alive: true, Delay: 29, SpeedMbps: 51},
	}}
	state := State{}
	for i := 0; i < 4; i++ {
		var decision Decision
		state, decision = Step(state, input, ManagedPolicy())
		if decision.Action != "keep" {
			t.Fatal(decision)
		}
		input.Now += 20
	}
}
