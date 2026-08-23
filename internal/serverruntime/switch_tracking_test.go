package serverruntime

import (
	"context"
	"testing"
)

func TestSnapshotTracksActualNodeOrPoolSwitch(t *testing.T) {
	runtime := cleanTestRuntime(t)
	if err := runtime.Store.SetSnapshot(context.Background(), map[string]any{
		"active": "node-a", "active_pool": "primary", "last_switch": int64(10),
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.setSnapshotTrackingSwitch(map[string]any{"active": "node-a", "active_pool": "primary"}, 20); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := runtime.Store.Snapshot(context.Background())
	if int64Value(unchanged.State["last_switch"]) != 10 {
		t.Fatalf("unchanged=%#v", unchanged.State)
	}
	if err := runtime.setSnapshotTrackingSwitch(map[string]any{"active": "node-b", "active_pool": "whitelist"}, 30); err != nil {
		t.Fatal(err)
	}
	changed, _ := runtime.Store.Snapshot(context.Background())
	if int64Value(changed.State["last_switch"]) != 30 {
		t.Fatalf("changed=%#v", changed.State)
	}
}

func TestIntValueAcceptsNativeTimestamps(t *testing.T) {
	if got := intValue(int64(1787471394)); got != 1787471394 {
		t.Fatalf("got=%d", got)
	}
}
