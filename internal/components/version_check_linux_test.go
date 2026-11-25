//go:build linux

package components

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type releaseCheckTransport func(*http.Request) (*http.Response, error)

func (f releaseCheckTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMihomoVersionCheckRefreshesCacheWithoutReplacingCore(t *testing.T) {
	architecture := map[string]string{"amd64": "amd64-compatible", "arm64": "arm64", "arm": "armv7"}[runtime.GOARCH]
	if architecture == "" {
		t.Skip("unsupported release architecture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "mihomo")
	original := []byte("#!/bin/sh\necho 'Mihomo Meta v1.19.30 linux amd64 with go1.26.7'\n")
	if err := os.WriteFile(binary, original, 0700); err != nil {
		t.Fatal(err)
	}
	current := updater{config: Config{Component: "check", StateDirectory: dir, Mihomo: binary}, operation: filepath.Join(dir, "component-operation.json")}
	requests := 0
	current.client = &http.Client{Transport: releaseCheckTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != releaseAPI || r.Method != http.MethodGet {
			t.Fatal(r.Method, r.URL)
		}
		payload, _ := json.Marshal(releasePayload{TagName: "v1.20.0", HTMLURL: "https://github.com/MetaCubeX/mihomo/releases/tag/v1.20.0", Assets: []releaseAsset{{Name: "mihomo-linux-" + architecture + "-v1.20.0.gz", BrowserDownloadURL: "https://example.invalid/mihomo.gz", Digest: "sha256:" + strings.Repeat("a", 64), Size: 123}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header)}, nil
	})}
	if err := current.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var release Release
	data, err := os.ReadFile(filepath.Join(dir, "component-latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &release); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || release.LatestVersion != "1.20.0" || release.InstalledVersion != "1.19.30" || !release.UpdateAvailable || release.CheckedAt <= 0 {
		t.Fatal(requests, release)
	}
	data, _ = os.ReadFile(binary)
	if string(data) != string(original) {
		t.Fatal("version check replaced the installed core")
	}
	var operation map[string]any
	data, _ = os.ReadFile(current.operation)
	if err := json.Unmarshal(data, &operation); err != nil {
		t.Fatal(err)
	}
	if operation["status"] != "success" || operation["component"] != "check" {
		t.Fatal(operation)
	}
	// A failed subsequent query must report the error, not erase the last result.
	before, _ := os.ReadFile(filepath.Join(dir, "component-latest.json"))
	current.client.Transport = releaseCheckTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})
	if err := current.run(context.Background()); err == nil {
		t.Fatal("upstream failure hidden")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "component-latest.json"))
	if string(before) != string(after) {
		t.Fatal("failed query erased the last known release")
	}
}
