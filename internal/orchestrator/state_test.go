package orchestrator

import (
	"github.com/gooog1111/orcheroute/internal/whitelist"
	"testing"
)

func TestAllowlistStartsFullCachedScan(t *testing.T) {
	result, err := Transition(State{Mode: Normal, Enabled: true}, Event{Type: "probe", Mode: Allowlist})
	if err != nil || result.Action.Type != "scan_all_cached" || !result.State.Whitelist.ScanActive {
		t.Fatalf("unexpected: %#v %v", result, err)
	}
}
func TestFirstCandidateConnectsButScanContinues(t *testing.T) {
	node := whitelist.Node{ID: "one", Alive: true, SourceID: "a", Proxy: map[string]any{"name": "one"}}
	result, err := Transition(State{Mode: Allowlist, Enabled: true, Whitelist: whitelist.State{ScanActive: true}}, Event{Type: "add_source", SourceID: "a", Nodes: []whitelist.Node{node}})
	if err != nil || result.Action.Type != "connect" || result.Action.Candidate == nil || result.State.Whitelist.PendingNode != "one" || !result.State.Whitelist.ScanActive {
		t.Fatalf("unexpected: %#v %v", result, err)
	}
}
func TestFailedCandidateCannotRaceNext(t *testing.T) {
	nodes := []whitelist.Node{{ID: "one", Alive: true, Priority: 1, SourceID: "a", Proxy: map[string]any{"name": "one"}}, {ID: "two", Alive: true, Priority: 2, SourceID: "a", Proxy: map[string]any{"name": "two"}}}
	state := State{Mode: Allowlist, Enabled: true, Whitelist: whitelist.State{Nodes: nodes, SelectedNode: "one", PendingNode: "one"}}
	result, _ := Transition(state, Event{Type: "failed", NodeID: "one"})
	if result.Action.Candidate == nil || result.Action.Candidate.ID != "two" || result.State.Whitelist.PendingNode != "two" {
		t.Fatalf("unexpected: %#v", result)
	}
}
func TestEmptyCompletedScanStopsCleanly(t *testing.T) {
	result, _ := Transition(State{Mode: Allowlist, Enabled: true, Whitelist: whitelist.State{ScanActive: true}}, Event{Type: "scan_complete"})
	if result.Action.Type != "stop" || result.State.Enabled || result.State.LastError != "whitelist_pool_empty" {
		t.Fatalf("unexpected: %#v", result)
	}
}
