//go:build linux

package components

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryVersionPrefersMihomoVersionOverGoToolchain(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "mihomo")
	payload := "#!/bin/sh\nprintf '%s\\n' 'Mihomo Meta v1.19.29 linux amd64 with go1.26.5'\n"
	if err := os.WriteFile(path, []byte(payload), 0o755); err != nil {
		t.Fatal(err)
	}
	version, err := binaryVersion(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.19.29" {
		t.Fatalf("version = %q, want 1.19.29", version)
	}
}

func TestNormalizeVersion(t *testing.T) {
	for input, want := range map[string]string{"v1.19.29": "1.19.29", " 1.19.29 ": "1.19.29", "v1.20.0-alpha.1": "1.20.0-alpha.1"} {
		if got := normalizeVersion(input); got != want {
			t.Fatalf("normalizeVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSelectLinuxAssetAlwaysPrefersCompatibleAMD64(t *testing.T) {
	assets := []releaseAsset{
		{Name: "mihomo-linux-amd64-compatible-v1.19.29.gz"},
		{Name: "mihomo-linux-amd64-v1.19.29.gz"},
	}
	if got := selectLinuxAsset(assets, "amd64", "v1.19.29").Name; got != assets[0].Name {
		t.Fatalf("selected %q, want compatible asset", got)
	}
	assets[0], assets[1] = assets[1], assets[0]
	if got := selectLinuxAsset(assets, "amd64", "v1.19.29").Name; got != "mihomo-linux-amd64-compatible-v1.19.29.gz" {
		t.Fatalf("selected %q after reorder, want compatible asset", got)
	}
}

func TestFormatDownloadProgress(t *testing.T) {
	if got := formatDownloadProgress("Загружаем GeoIP", 5<<20, 20<<20); got != "Загружаем GeoIP · 5.0 МБ / 20.0 МБ" {
		t.Fatalf("unexpected progress text: %q", got)
	}
}
