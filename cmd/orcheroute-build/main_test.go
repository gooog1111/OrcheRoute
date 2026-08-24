package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceDirectoryCopiesFreshTreeAndRemovesStaleFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "webui", "out")
	destination := filepath.Join(root, "dist", "verify", "linux-server", "webui")
	if err := os.MkdirAll(filepath.Join(source, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "index.html"), []byte("fresh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets", "app.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "stale.js"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "stale.js")); !os.IsNotExist(err) {
		t.Fatalf("stale file was not removed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "assets", "app.js"))
	if err != nil || string(data) != "js" {
		t.Fatalf("copied asset = %q, %v", data, err)
	}
}

func TestReplaceDirectoryRejectsUnrelatedDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "webui", "out")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceDirectory(source, filepath.Join(root, "important")); err == nil {
		t.Fatal("unsafe destination was accepted")
	}
}

func TestReplaceDirectoryAllowsVerifiedServerWebUI(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "webui", "out")
	destination := filepath.Join(root, "dist", "verify", "linux-server", "webui")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "index.html"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "index.html"))
	if err != nil || string(data) != "shared" {
		t.Fatalf("server WebUI = %q, %v", data, err)
	}
}

func TestAndroidEnvironmentUsesConfiguredSDK(t *testing.T) {
	sdk := t.TempDir()
	t.Setenv("ANDROID_HOME", sdk)
	t.Setenv("ANDROID_SDK_ROOT", "")
	values, err := androidEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	want := "ANDROID_HOME=" + sdk
	if values[0] != want {
		t.Fatalf("ANDROID_HOME = %q, want %q", values[0], want)
	}
}

func TestNPMCommandMatchesHost(t *testing.T) {
	want := "npm"
	if runtime.GOOS == "windows" {
		want = "npm.cmd"
	}
	if got := npmCommand(); got != want {
		t.Fatalf("npm command = %q, want %q", got, want)
	}
}

func TestVerifyArchiveEntryRequiresEmbeddedAndroidIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.apk")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("assets/web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyArchiveEntry(path, "assets/web/index.html"); err != nil {
		t.Fatal(err)
	}
	if err := verifyArchiveEntry(path, "assets/index.html"); err == nil {
		t.Fatal("missing root asset was accepted")
	}
}

func TestVerifyArchiveFileMatchesCanonicalWebUI(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "app.apk")
	source := filepath.Join(directory, "index.html")
	if err := os.WriteFile(source, []byte("canonical"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("assets/web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("canonical")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyArchiveFileMatches(path, "assets/web/index.html", source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyArchiveFileMatches(path, "assets/web/index.html", source); err == nil {
		t.Fatal("stale Android WebUI was accepted")
	}
}
