package serverruntime

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNextAppUpdateCheckSurvivesServiceRestart(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if got := nextAppUpdateCheck(int(now.Add(-time.Hour).Unix()), now); got != 5*time.Hour {
		t.Fatalf("got %s, want 5h", got)
	}
	if got := nextAppUpdateCheck(0, now); got != 20*time.Second {
		t.Fatalf("initial delay %s", got)
	}
}

func TestAppUpdateChannelIsPersistedWithoutStartingAnUpdate(t *testing.T) {
	directory := t.TempDir()
	runtime := &Runtime{Config: Config{StateDirectory: directory}}
	status, value := runtime.saveAppUpdateChannel(map[string]any{"beta_enabled": true})
	if status != 200 || value.(map[string]any)["beta_enabled"] != true {
		t.Fatalf("save returned %d %#v", status, value)
	}
	stored := map[string]any{}
	if err := readJSON(filepath.Join(directory, "app-update.json"), &stored); err != nil {
		t.Fatal(err)
	}
	if stored["beta_enabled"] != true || stored["state"] != "idle" || stored["active"] != false {
		t.Fatalf("channel was not persisted independently: %#v", stored)
	}
	_, loaded := runtime.getAppUpdate()
	if loaded.(map[string]any)["beta_enabled"] != true {
		t.Fatalf("saved channel was lost on polling: %#v", loaded)
	}
}
