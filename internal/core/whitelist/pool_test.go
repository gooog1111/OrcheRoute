package whitelist

import "testing"

func TestRemoveSelectedNodeKeepsRemainingPool(t *testing.T) {
	state := State{Nodes: []Node{
		{ID: "one", SourceID: "source", Alive: true, Proxy: map[string]any{"name": "one"}},
		{ID: "two", SourceID: "source", Alive: true, Proxy: map[string]any{"name": "two"}},
	}, SelectedNode: "one", PendingNode: "one"}
	result, err := Transition(state, Command{Operation: "remove_node", NodeID: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.Nodes) != 1 || result.State.Nodes[0].ID != "two" {
		t.Fatalf("unexpected nodes: %#v", result.State.Nodes)
	}
	if result.State.SelectedNode != "" || result.State.PendingNode != "" {
		t.Fatalf("selection was not cleared: %#v", result.State)
	}
}

func TestRemovedNodeCanReturnOnSourceRefresh(t *testing.T) {
	state := State{Nodes: []Node{testNode("one", "source", 1)}}
	removed, err := Transition(state, Command{Operation: "remove_node", NodeID: "one"})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := Transition(removed.State, Command{
		Operation: "replace_source",
		SourceID:  "source",
		Nodes:     []Node{testNode("one", "source", 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.State.Nodes) != 1 || refreshed.State.Nodes[0].ID != "one" {
		t.Fatalf("source refresh did not restore removed node: %#v", refreshed.State.Nodes)
	}
}

func testNode(id, source string, priority int) Node {
	return Node{ID: id, SourceID: source, DisplayName: id, Alive: true, Priority: priority, Proxy: map[string]any{"name": id}}
}

func TestPendingCandidatePreventsRace(t *testing.T) {
	state := State{Nodes: []Node{testNode("one", "a", 1), testNode("two", "a", 2)}}
	first, _ := Transition(state, Command{Operation: "request"})
	second, _ := Transition(first.State, Command{Operation: "request"})
	if first.Candidate == nil || second.Candidate == nil || first.Candidate.ID != "one" || second.Candidate.ID != "one" || second.State.PendingNode != "one" {
		t.Fatalf("candidate race: %#v %#v", first, second)
	}
}
func TestFailureSelectsNext(t *testing.T) {
	requested, _ := Transition(State{Nodes: []Node{testNode("one", "a", 1), testNode("two", "a", 2)}}, Command{Operation: "request"})
	failed, _ := Transition(requested.State, Command{Operation: "fail", NodeID: "one"})
	if failed.Candidate == nil || failed.Candidate.ID != "two" || len(failed.State.Nodes) != 1 || failed.State.PendingNode != "two" {
		t.Fatalf("failover: %#v", failed)
	}
}
func TestReplaceSourceKeepsOtherSources(t *testing.T) {
	state := State{Nodes: []Node{testNode("old", "a", 1), testNode("keep", "b", 2)}, SelectedNode: "old"}
	result, err := Transition(state, Command{Operation: "replace_source", SourceID: "a", Nodes: []Node{testNode("new", "a", 1)}})
	if err != nil || len(result.State.Nodes) != 2 || result.State.SelectedNode != "" || find(result.State.Nodes, "new") == nil || find(result.State.Nodes, "keep") == nil || find(result.State.Nodes, "old") != nil {
		t.Fatalf("replace: %#v %v", result, err)
	}
}
func TestConfirmRequiresPendingNode(t *testing.T) {
	requested, _ := Transition(State{Nodes: []Node{testNode("one", "a", 1)}}, Command{Operation: "request"})
	wrong, _ := Transition(requested.State, Command{Operation: "confirm", NodeID: "other"})
	if wrong.State.PendingNode != "one" {
		t.Fatal("unrelated node confirmed")
	}
	confirmed, _ := Transition(wrong.State, Command{Operation: "confirm", NodeID: "one"})
	if confirmed.State.PendingNode != "" || confirmed.State.SelectedNode != "one" {
		t.Fatalf("confirm: %#v", confirmed.State)
	}
}

func TestBeginPreservesWorkingPoolUntilSourcesAreReplaced(t *testing.T) {
	state := State{Nodes: []Node{testNode("one", "a", 1), testNode("two", "b", 2)}, SelectedNode: "one"}
	result, err := Transition(state, Command{Operation: "begin"})
	if err != nil || !result.State.ScanActive || result.State.Generation != 1 || len(result.State.Nodes) != 2 || result.State.SelectedNode != "one" {
		t.Fatalf("begin destroyed stable pool: %#v %v", result, err)
	}
}
