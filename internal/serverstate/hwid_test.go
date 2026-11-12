package serverstate

import (
	"context"
	"database/sql"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"path/filepath"
	"testing"
)

func TestHWIDMigratesExistingStoreAndPreservesSubscriptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(`INSERT INTO subscriptions(id,name,group_name,parser,secret,enabled,last_links,last_success,created_at,updated_at) VALUES('old','Existing','primary','standard','https://example.test/private',1,23,99,1,1)`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	item, err := store.Get(ctx, "old", true)
	if err != nil || item.LastLinks != 23 || item.LastSuccess != 99 || item.HWID != "" || item.HWIDMode != "" {
		t.Fatal("migration lost existing subscription")
	}
	_, err = store.Update(ctx, "old", map[string]any{"hwid_mode": "custom", "hwid": "owned-device-hwid"})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item, err = store.Get(ctx, "old", true)
	if err != nil || item.HWID != "owned-device-hwid" || item.HWIDMode != "custom" || item.LastLinks != 23 || item.Secret != "https://example.test/private" {
		t.Fatal("HWID or subscription not preserved after reopening")
	}
	if item.LastSuccess != 0 {
		t.Fatal("changed identifier did not schedule a new fetch")
	}
	_, err = store.Update(ctx, "old", map[string]any{"hwid": "bad\r\nheader"})
	if err == nil {
		t.Fatal("invalid identifier accepted")
	}
	item, _ = store.Get(ctx, "old", true)
	if item.HWID != "owned-device-hwid" {
		t.Fatal("invalid update replaced working identifier")
	}
}

func TestCreateHWIDSelectionValidation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.Create(context.Background(), subscriptions.Subscription{ID: "invalid", HWIDMode: "custom"})
	if err == nil {
		t.Fatal("empty custom identifier accepted")
	}
}
