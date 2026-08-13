package serverstate

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	control, err := store.Control(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if control.Mode != "auto" || !control.Enabled {
		t.Fatalf("unexpected control: %#v", control)
	}
	if err := store.SetSnapshot(ctx, map[string]any{"connectivity": "proxy_ok"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State["connectivity"] != "proxy_ok" || snapshot.UpdatedAt == 0 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	pool := "primary"
	if err := store.AddEvent(ctx, EventInput{EventType: "qualification_complete", Pool: &pool, Details: map[string]any{"accepted": float64(2)}}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Severity != "info" || events[0].Details["accepted"] != float64(2) {
		t.Fatalf("unexpected events: %#v", events)
	}
	created, err := store.Create(ctx, subscriptions.Subscription{Name: "Main", GroupName: subscriptions.Primary, Parser: subscriptions.Standard, Secret: "https://example.test/sub", Enabled: true, IntervalSeconds: 900})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.LastStatus != "pending" || created.Secret == "" {
		t.Fatalf("unexpected subscription: %#v", created)
	}
	public, err := store.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || public[0].Secret != "" {
		t.Fatalf("secret leaked: %#v", public)
	}
	links := 5
	if err := store.UpdateSubscriptionStatus(ctx, created.ID, "ok", &links, nil, true); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(ctx, created.ID, map[string]any{"group_name": "emergency"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GroupName != subscriptions.Emergency || updated.LastSuccess != 0 || updated.LastStatus != "pending" || updated.LastLinks != 0 {
		t.Fatalf("status was not reset: %#v", updated)
	}
	deleted, err := store.Delete(ctx, created.ID)
	if err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
}

func TestMigratesLegacyControlTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE control (id INTEGER PRIMARY KEY CHECK(id=1), mode TEXT NOT NULL, manual_node TEXT, manual_until INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL);
INSERT INTO control(id,mode,manual_node,manual_until,updated_at) VALUES(1,'auto',NULL,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	control, err := store.Control(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !control.Enabled {
		t.Fatalf("legacy control should default enabled: %#v", control)
	}
}

func TestMigratesLegacySubscriptionParserConstraint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-subscriptions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE subscriptions (
        id TEXT PRIMARY KEY, name TEXT NOT NULL,
        group_name TEXT NOT NULL CHECK (group_name IN ('primary', 'emergency')),
        parser TEXT NOT NULL CHECK (parser IN ('standard', 'blacktemple')),
        secret TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
        interval_seconds INTEGER NOT NULL DEFAULT 3600, last_attempt INTEGER NOT NULL DEFAULT 0,
        last_success INTEGER NOT NULL DEFAULT 0, last_status TEXT NOT NULL DEFAULT 'pending',
        last_error TEXT, last_links INTEGER NOT NULL DEFAULT 0,
        created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
    );
    INSERT INTO subscriptions(id,name,group_name,parser,secret,enabled,interval_seconds,created_at,updated_at)
    VALUES('legacy','Legacy','primary','standard','https://example.test',1,900,1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.List(context.Background(), true)
	if err != nil || len(items) != 1 || items[0].ID != "legacy" {
		t.Fatalf("legacy data was not preserved: %#v %v", items, err)
	}
	created, err := store.Create(context.Background(), subscriptions.Subscription{Name: "WG", GroupName: subscriptions.Primary, Parser: subscriptions.WireGuard, Secret: "config", Enabled: false, IntervalSeconds: 900})
	if err != nil || created.Parser != subscriptions.WireGuard {
		t.Fatalf("wireguard parser was not enabled: %#v %v", created, err)
	}
}
