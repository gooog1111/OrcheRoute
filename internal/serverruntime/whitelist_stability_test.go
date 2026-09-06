package serverruntime

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	mobileconnectivity "github.com/gooog1111/orcheroute/internal/core/connectivity"
	"github.com/gooog1111/orcheroute/internal/core/whitelist"
)

func TestWhitelistSelectionIsNotConnectionConfirmation(t *testing.T) {
	runtime := cleanTestRuntime(t)
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected method %s", r.Method)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer api.Close()
	runtime.Config.MihomoAPI = api.URL
	result, err := runtime.whitelistTransition(whitelist.Command{Operation: "add_source", SourceID: "test", Nodes: []whitelist.Node{{ID: "node", Alive: true, Proxy: map[string]any{"name": "test node", "type": "vless"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.selectWhitelistCandidate(result.State); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || runtime.whitelistState().PendingNode != "node" {
		t.Fatal("controller acknowledgement was mistaken for traffic confirmation")
	}
}

func TestWhitelistStabilityWaitHonorsCancellationBeforeProbing(t *testing.T) {
	runtime := cleanTestRuntime(t)
	path := filepath.Join(t.TempDir(), "cancel")
	if err := os.WriteFile(path, []byte("cancel"), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.awaitWhitelistStability(path) {
		t.Fatal("cancel ignored")
	}
}

func TestConnectivityErrorDoesNotRefreshSuccessfulTimestamp(t *testing.T) {
	runtime := cleanTestRuntime(t)
	runtime.recordConnectivityError(ConnectivitySnapshot{State: mobileconnectivity.Normal, UpdatedAt: 123, ConfirmedAt: 100}, errors.New("probe failed"))
	got := runtime.connectivitySnapshot()
	if got.UpdatedAt != 123 || got.AttemptedAt <= 123 || got.Error == "" {
		t.Fatalf("snapshot=%#v", got)
	}
}
