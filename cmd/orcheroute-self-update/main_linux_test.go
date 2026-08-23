//go:build linux

package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInstallAndBackupDetachFromControlPlaneService(t *testing.T) {
	original, present := os.LookupEnv("ORCHEROUTE_SELF_UPDATE_TRANSIENT")
	t.Cleanup(func() {
		if present {
			_ = os.Setenv("ORCHEROUTE_SELF_UPDATE_TRANSIENT", original)
		} else {
			_ = os.Unsetenv("ORCHEROUTE_SELF_UPDATE_TRANSIENT")
		}
	})
	_ = os.Unsetenv("ORCHEROUTE_SELF_UPDATE_TRANSIENT")
	if !shouldDetach("install") || !shouldDetach("backup") || shouldDetach("check") {
		t.Fatal("only mutating self-update actions must detach")
	}
	_ = os.Setenv("ORCHEROUTE_SELF_UPDATE_TRANSIENT", "1")
	if shouldDetach("install") || shouldDetach("backup") {
		t.Fatal("transient service must not detach recursively")
	}
}

func TestRollbackAssetURL(t *testing.T) {
	tests := []struct {
		version string
		beta    bool
		want    string
	}{
		{"0.5.11-beta.7", true, "https://github.com/gooog1111/OrcheRoute/releases/download/server-beta/OrcheRoute-Linux-Server-0.5.11-beta.7-amd64.deb"},
		{"0.5.12", false, "https://github.com/gooog1111/OrcheRoute/releases/download/v0.5.12/OrcheRoute-Linux-Server-0.5.12-amd64.deb"},
	}
	for _, test := range tests {
		if got := rollbackAssetURL(test.version, test.beta); got != test.want {
			t.Fatalf("rollbackAssetURL(%q, %t)=%q want %q", test.version, test.beta, got, test.want)
		}
	}
}

func TestBackupExcluded(t *testing.T) {
	for _, name := range []string{"backups", "self-update", "packages", "app-update.json", "state.db", "state.db-wal", "state.db-shm"} {
		if !backupExcluded(name) {
			t.Fatalf("%s must be excluded", name)
		}
	}
	if backupExcluded("routes.json") {
		t.Fatal("persistent configuration must be copied")
	}
}

func TestBackupSQLiteCreatesConsistentCopy(t *testing.T) {
	directory := t.TempDir()
	source, target := filepath.Join(directory, "source.db"), filepath.Join(directory, "target.db")
	database, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("CREATE TABLE values_table(value TEXT); INSERT INTO values_table VALUES ('saved')"); err != nil {
		t.Fatal(err)
	}
	if err := backupSQLite(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	copyDatabase, err := sql.Open("sqlite", target)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDatabase.Close()
	var value string
	if err := copyDatabase.QueryRow("SELECT value FROM values_table").Scan(&value); err != nil || value != "saved" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}
