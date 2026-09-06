package whitelist

import "testing"

func TestVerifiedNodePrecedesUnmeasuredCall(t *testing.T) {
	input := State{Nodes: []Node{
		{ID: "working", Alive: true, DelayMS: 1000, Proxy: map[string]any{"type": "vless"}},
		{ID: "call", ActivationRequired: true, Proxy: map[string]any{"type": "freeturn"}},
	}}
	requested, err := Transition(input, Command{Operation: "request"})
	if err != nil || requested.Candidate == nil || requested.Candidate.ID != "working" {
		t.Fatalf("selected: %#v %v", requested, err)
	}
	// Explicit user choice still permits activation of an unmeasured profile.
	manual, err := Transition(input, Command{Operation: "select", NodeID: "call"})
	if err != nil || manual.Candidate == nil || manual.Candidate.ID != "call" {
		t.Fatalf("manual: %#v %v", manual, err)
	}
	failed, err := Transition(requested.State, Command{Operation: "fail", NodeID: "working"})
	if err != nil || failed.Candidate == nil || failed.Candidate.ID != "call" {
		t.Fatalf("fallback: %#v %v", failed, err)
	}
}
