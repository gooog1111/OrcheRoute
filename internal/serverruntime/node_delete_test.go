package serverruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gooog1111/orcheroute/internal/core/whitelist"
)

func TestDeletePrimaryNodeUpdatesProviderAndMetadata(t *testing.T) {
	runtime := cleanTestRuntime(t)
	var lock sync.Mutex
	putPaths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPut {
			lock.Lock()
			putPaths = append(putPaths, request.URL.Path)
			lock.Unlock()
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/providers/proxies":
			_ = json.NewEncoder(writer).Encode(map[string]any{"providers": map[string]any{
				"primary": map[string]any{"proxies": []any{
					map[string]any{"name": "node-a Alpha", "alive": true},
					map[string]any{"name": "node-b Beta", "alive": true},
				}},
				"emergency": map[string]any{"proxies": []any{}},
			}})
		case "/proxies/ACTIVE":
			_ = json.NewEncoder(writer).Encode(map[string]any{"now": "node-b Beta"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	runtime.Config.MihomoAPI = server.URL
	providerPath := filepath.Join(runtime.Config.StateDirectory, "providers", "primary.json")
	if err := atomicJSON(providerPath, map[string]any{"proxies": []any{
		map[string]any{"name": "node-a Alpha"}, map[string]any{"name": "node-b Beta"},
	}}); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(runtime.Config.StateDirectory, "providers", "primary.sources.json")
	if err := atomicJSON(metadataPath, map[string]any{"nodes": map[string]any{
		"node-a Alpha": map[string]any{"id": "source-a"}, "node-b Beta": map[string]any{"id": "source-b"},
	}}); err != nil {
		t.Fatal(err)
	}

	status, payload := runtime.deletePoolNode(context.Background(), "node-a")
	if status != 200 || payload.(map[string]any)["deleted"] != true {
		t.Fatalf("status=%d payload=%#v", status, payload)
	}
	provider := map[string]any{}
	if err := readJSON(providerPath, &provider); err != nil {
		t.Fatal(err)
	}
	proxies := provider["proxies"].([]any)
	if len(proxies) != 1 || stringValue(proxies[0].(map[string]any)["name"]) != "node-b Beta" {
		t.Fatalf("provider=%#v", provider)
	}
	metadata := map[string]any{}
	if err := readJSON(metadataPath, &metadata); err != nil {
		t.Fatal(err)
	}
	if _, exists := metadata["nodes"].(map[string]any)["node-a Alpha"]; exists {
		t.Fatalf("metadata still contains deleted node: %#v", metadata)
	}
	lock.Lock()
	defer lock.Unlock()
	if len(putPaths) != 1 || putPaths[0] != "/providers/proxies/primary" {
		t.Fatalf("provider reloads=%#v", putPaths)
	}
}

func TestDeleteWhitelistNodeUsesDerivedPool(t *testing.T) {
	runtime := cleanTestRuntime(t)
	node := whitelist.Node{ID: "whitelist:one", DisplayName: "One", Alive: true, SourceID: "source", Proxy: map[string]any{"name": "one"}}
	if _, err := runtime.whitelistTransition(whitelist.Command{Operation: "add_source", SourceID: "source", Nodes: []whitelist.Node{node}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/providers/proxies":
			_ = json.NewEncoder(writer).Encode(map[string]any{"providers": map[string]any{
				"primary": map[string]any{"proxies": []any{}}, "emergency": map[string]any{"proxies": []any{}},
			}})
		case "/proxies/ACTIVE":
			_ = json.NewEncoder(writer).Encode(map[string]any{})
		}
	}))
	defer server.Close()
	runtime.Config.MihomoAPI = server.URL
	status, payload := runtime.deletePoolNode(context.Background(), node.ID)
	if status != 200 || payload.(map[string]any)["remaining"] != 0 {
		t.Fatalf("status=%d payload=%#v", status, payload)
	}
}

func TestClearPrimaryPoolWorksWhileTransportStopped(t *testing.T) {
	runtime := cleanTestRuntime(t)
	runtime.Config.MihomoAPI = "http://127.0.0.1:1"
	providerPath := filepath.Join(runtime.Config.StateDirectory, "providers", "primary.json")
	metadataPath := filepath.Join(runtime.Config.StateDirectory, "providers", "primary.sources.json")
	if err := atomicJSON(providerPath, map[string]any{"proxies": []any{map[string]any{"name": "node-a Alpha"}}}); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(metadataPath, map[string]any{"nodes": map[string]any{"node-a Alpha": map[string]any{"id": "source-a"}}}); err != nil {
		t.Fatal(err)
	}
	status, payload := runtime.clearPool(context.Background(), "primary")
	if status != 200 || payload.(map[string]any)["remaining"] != 0 {
		t.Fatalf("status=%d payload=%#v", status, payload)
	}
	provider := map[string]any{}
	if err := readJSON(providerPath, &provider); err != nil || len(provider["proxies"].([]any)) != 0 {
		t.Fatalf("provider not cleared: %#v %v", provider, err)
	}
}
