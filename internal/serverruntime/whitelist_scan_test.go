package serverruntime

import (
	"testing"

	"github.com/gooog1111/orcheroute/internal/updater"
	"github.com/gooog1111/orcheroute/internal/whitelist"
)

func TestApplyWhitelistResultReplacesOnlyCompletedSources(t *testing.T) {
	runtime := cleanTestRuntime(t)
	oldA := whitelist.Node{DisplayName: "old-a", Alive: true, SourceID: "a", Proxy: map[string]any{"name": "old-a"}}
	oldB := whitelist.Node{DisplayName: "old-b", Alive: true, SourceID: "b", Proxy: map[string]any{"name": "old-b"}}
	if _, err := runtime.whitelistTransition(whitelist.Command{Operation: "add_source", SourceID: "a", Nodes: []whitelist.Node{oldA}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.whitelistTransition(whitelist.Command{Operation: "add_source", SourceID: "b", Nodes: []whitelist.Node{oldB}}); err != nil {
		t.Fatal(err)
	}
	newA := whitelist.Node{DisplayName: "new-a", Alive: true, SourceID: "a", Proxy: map[string]any{"name": "new-a"}}
	if err := runtime.applyWhitelistResult(updater.WhitelistResult{Sources: map[string][]whitelist.Node{"a": {newA}}, CompletedSources: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	state := runtime.whitelistState()
	seen := map[string]bool{}
	for _, node := range state.Nodes {
		seen[node.DisplayName] = true
	}
	if !seen["new-a"] || !seen["old-b"] || seen["old-a"] {
		t.Fatalf("atomic source replacement failed: %#v", state.Nodes)
	}
	provider := map[string]any{}
	if err := readJSON(runtime.Config.StateDirectory+"/providers/whitelist.json", &provider); err != nil {
		t.Fatal(err)
	}
	if len(provider["proxies"].([]any)) != 2 {
		t.Fatalf("provider not synchronized: %#v", provider)
	}
}
