//go:build linux

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		{"0.5.12", true, "https://github.com/gooog1111/OrcheRoute/releases/download/v0.5.12/OrcheRoute-Linux-Server-0.5.12-amd64.deb"},
		{"0.5.11-beta.7", false, "https://github.com/gooog1111/OrcheRoute/releases/download/server-beta/OrcheRoute-Linux-Server-0.5.11-beta.7-amd64.deb"},
	}
	for _, test := range tests {
		if got := rollbackAssetURL(test.version, test.beta); got != test.want {
			t.Fatalf("rollbackAssetURL(%q, %t)=%q want %q", test.version, test.beta, got, test.want)
		}
	}
}

func TestAssetSizeValidation(t *testing.T) {
	for _, size := range []int64{2, 3, 4} {
		var dst bytes.Buffer
		err := copyAsset(&dst, strings.NewReader("abc"), size)
		if (err == nil) != (size == 3) {
			t.Fatalf("size=%d error=%v", size, err)
		}
	}
}

func TestRollbackReportsEveryFailureAndVerifiesDataRestore(t *testing.T) {
	for failAt := 0; failAt <= 5; failAt++ {
		calls := 0
		step := func(context.Context) error {
			calls++
			if calls == failAt {
				return errors.New("injected failure")
			}
			return nil
		}
		cause := errors.New("original install failed")
		err := runRollback(context.Background(), cause, step, step, step, step, step)
		if !errors.Is(err, cause) {
			t.Fatal("lost original failure")
		}
		if failAt == 0 {
			if calls != 5 || !strings.Contains(err.Error(), "package_and_data_rollback_verified") {
				t.Fatal(err)
			}
		} else if calls != failAt || !strings.Contains(err.Error(), "rollback_failed:") || strings.Contains(err.Error(), "rollback_verified") {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
}

func TestRestoreSnapshotTreeRestoresCredentialsAndDatabase(t *testing.T) {
	root := t.TempDir()
	snapshot := filepath.Join(root, "snapshot")
	etcSource := filepath.Join(snapshot, "etc", "orcheroute")
	stateSource := filepath.Join(snapshot, "var", "lib", "orcheroute")
	etcTarget := filepath.Join(root, "etc-target")
	stateTarget := filepath.Join(root, "state-target")
	for _, directory := range []string{etcSource, stateSource, etcTarget, stateTarget, filepath.Join(stateTarget, "self-update")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(etcSource, "runtime.env"), "AUTH=before\n")
	writeTestFile(t, filepath.Join(stateSource, "state.db"), "database-before")
	writeTestFile(t, filepath.Join(stateSource, "routes.json"), "routes-before")
	writeTestFile(t, filepath.Join(etcTarget, "runtime.env"), "AUTH=changed\n")
	writeTestFile(t, filepath.Join(stateTarget, "state.db"), "database-changed")
	writeTestFile(t, filepath.Join(stateTarget, "state.db-wal"), "stale-wal")
	writeTestFile(t, filepath.Join(stateTarget, "state.db-shm"), "stale-shm")
	writeTestFile(t, filepath.Join(stateTarget, "routes.json"), "routes-changed")
	writeTestFile(t, filepath.Join(stateTarget, "self-update", "candidate.deb"), "preserve-update")

	if err := restoreSnapshotTree(context.Background(), snapshot, etcTarget, stateTarget); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(etcTarget, "runtime.env"):                    "AUTH=before\n",
		filepath.Join(stateTarget, "state.db"):                     "database-before",
		filepath.Join(stateTarget, "routes.json"):                  "routes-before",
		filepath.Join(stateTarget, "self-update", "candidate.deb"): "preserve-update",
	} {
		if contents, err := os.ReadFile(path); err != nil || string(contents) != want {
			t.Fatalf("%s=%q err=%v want=%q", path, contents, err, want)
		}
	}
	for _, name := range []string{"state.db-wal", "state.db-shm"} {
		if _, err := os.Stat(filepath.Join(stateTarget, name)); !os.IsNotExist(err) {
			t.Fatalf("stale SQLite sidecar %s survived: %v", name, err)
		}
	}
}

func TestRestoreBackupArchiveUsesCreatedSnapshotLayout(t *testing.T) {
	root := t.TempDir()
	snapshot := filepath.Join(root, "snapshot")
	etcSource := filepath.Join(snapshot, "etc", "orcheroute")
	stateSource := filepath.Join(snapshot, "var", "lib", "orcheroute")
	for _, directory := range []string{etcSource, stateSource} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(etcSource, "runtime.env"), "AUTH=archive\n")
	writeTestFile(t, filepath.Join(stateSource, "state.db"), "archive-db")
	archive := filepath.Join(root, "snapshot.tar.gz")
	if output, err := exec.Command("tar", "-C", snapshot, "-czf", archive, "etc", "var").CombinedOutput(); err != nil {
		t.Fatalf("create archive: %v: %s", err, output)
	}
	etcTarget, stateTarget := filepath.Join(root, "etc-target"), filepath.Join(root, "state-target")
	if err := restoreBackupTargets(context.Background(), archive, etcTarget, stateTarget); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(etcTarget, "runtime.env"): "AUTH=archive\n",
		filepath.Join(stateTarget, "state.db"):  "archive-db",
	} {
		if contents, err := os.ReadFile(path); err != nil || string(contents) != want {
			t.Fatalf("%s=%q err=%v want=%q", path, contents, err, want)
		}
	}
}

func TestValidSnapshotMemberRejectsTraversal(t *testing.T) {
	for _, member := range []string{"etc/runtime.env", "var/lib/orcheroute/state.db", "var/", "var/lib/"} {
		if !validSnapshotMember(member) {
			t.Fatalf("valid member rejected: %q", member)
		}
	}
	for _, member := range []string{"../etc/shadow", "/etc/shadow", "home/user/file", "var/lib/other/file"} {
		if validSnapshotMember(member) {
			t.Fatalf("invalid member accepted: %q", member)
		}
	}
}

func writeTestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPackageInstallUsesAPTToResolveDependencies(t *testing.T) {
	command, arguments := packageInstallCommand("/var/lib/orcheroute/self-update/update.deb")
	if command != "apt-get" {
		t.Fatalf("installer=%q", command)
	}
	want := []string{"install", "--yes", "--no-remove", "/var/lib/orcheroute/self-update/update.deb"}
	if len(arguments) != len(want) {
		t.Fatalf("arguments=%q", arguments)
	}
	for index := range want {
		if arguments[index] != want[index] {
			t.Fatalf("arguments[%d]=%q want %q", index, arguments[index], want[index])
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
