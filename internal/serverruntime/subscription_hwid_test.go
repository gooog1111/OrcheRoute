package serverruntime

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestHWIDCatalogAndSubscriptionSelection(t *testing.T) {
	runtime := cleanTestRuntime(t)
	current := callServerAPI(t, runtime, http.MethodGet, "/v1/subscription-hwid", nil, 200)["current"].(string)
	if !subscriptions.ValidHWID(current) {
		t.Fatal("device identifier missing")
	}
	settings := callServerAPI(t, runtime, http.MethodPut, "/v1/subscription-hwid", map[string]any{"current": "forged-current", "saved": []map[string]any{{"name": "Phone", "value": "owned-phone-hwid"}}}, 200)
	if settings["current"] != current {
		t.Fatal("current device identifier was overwritten")
	}
	stored, err := subscriptions.ReadHWIDSettings(filepath.Join(runtime.Config.StateDirectory, "subscription-hwid.json"))
	if err != nil || stored.Current != current || len(stored.Saved) != 1 {
		t.Fatal("catalog not persisted")
	}
	info, _ := os.Stat(filepath.Join(runtime.Config.StateDirectory, "subscription-hwid.json"))
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("identifier settings exposed to other users")
	}
	created := callServerAPI(t, runtime, http.MethodPost, "/v1/subscriptions", map[string]any{"name": "HWID source", "group": "primary", "parser": "standard", "secret": "https://example.test/sub", "enabled": true, "interval_seconds": 3600, "hwid_mode": "custom", "hwid": "owned-phone-hwid"}, 201)
	item := created["subscription"].(map[string]any)
	id := item["id"].(string)
	if item["hwid_mode"] != "custom" || item["hwid"] != "owned-phone-hwid" {
		t.Fatal("identifier omitted from subscription")
	}
	callServerAPI(t, runtime, http.MethodPatch, "/v1/subscriptions/"+id, map[string]any{"hwid_mode": "current", "hwid": ""}, 200)
	callServerAPI(t, runtime, http.MethodPatch, "/v1/subscriptions/"+id, map[string]any{"hwid_mode": "custom", "hwid": ""}, 400)
	itemStored, err := runtime.Store.Get(context.Background(), id, true)
	if err != nil || itemStored.HWIDMode != "current" {
		t.Fatal("invalid update replaced working selection")
	}
	again, err := runtime.loadHWIDSettings()
	if err != nil || again.Current != current {
		t.Fatal("identifier rotated between loads")
	}
}

func TestCorruptHWIDSettingsDoNotDisableControlPlane(t *testing.T) {
	runtime := cleanTestRuntime(t)
	path := filepath.Join(runtime.Config.StateDirectory, "subscription-hwid.json")
	if err := os.WriteFile(path, []byte("broken-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runtime.bootstrap(context.Background(), false); err != nil {
		t.Fatal("optional HWID settings disabled the control plane", err)
	}
	callServerAPI(t, runtime, http.MethodGet, "/v1/subscription-hwid", nil, 503)
	content, _ := os.ReadFile(path)
	if string(content) != "broken-json" {
		t.Fatal("unreadable saved identifiers were overwritten")
	}
}
