package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestManifestRejectsUntrustedAssets(t *testing.T) {
	base := "https://github.com/" + Repository + "/releases/download/v1.2.3/"
	for _, tc := range []struct {
		name, file, address, hash string
		size                      int64
	}{
		{"parent", "../../outside.deb", base + "outside.deb", strings.Repeat("a", 64), 42},
		{"windows path", "..\\outside.deb", base + "outside.deb", strings.Repeat("a", 64), 42},
		{"foreign host", "ok.deb", "https://evil.example/ok.deb", strings.Repeat("a", 64), 42},
		{"foreign repo", "ok.deb", "https://github.com/evil/repo/releases/download/v1/ok.deb", strings.Repeat("a", 64), 42},
		{"wrong hash", "ok.deb", base + "ok.deb", strings.Repeat("z", 64), 42},
		{"oversized", "ok.deb", base + "ok.deb", strings.Repeat("a", 64), MaxAssetBytes + 1},
		{"mismatched filename", "ok.deb", base + "other.deb", strings.Repeat("a", 64), 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"version": "1.2.3", "assets": map[string]any{"amd64": map[string]any{"name": tc.file, "url": tc.address, "sha256": tc.hash, "size": tc.size}}})
			}))
			defer s.Close()
			if _, err := Latest(context.Background(), &http.Client{Transport: rewrite{s.URL}}, false, "amd64"); err == nil {
				t.Fatal("unsafe manifest accepted")
			}
		})
	}
}

func TestDownloadRedirectPolicy(t *testing.T) {
	client := HTTPClient(time.Second)
	for _, tc := range []struct {
		address string
		allowed bool
	}{
		{"https://release-assets.githubusercontent.com/path?signature=test", true},
		{"https://objects.githubusercontent.com/path", true},
		{"http://github.com/path", false}, {"https://github.com.evil.test/path", false},
		{"https://user:password@github.com/path", false}, {"https://github.com:8443/path", false},
	} {
		u, _ := url.Parse(tc.address)
		err := client.CheckRedirect(&http.Request{URL: u}, nil)
		if (err == nil) != tc.allowed {
			t.Fatalf("redirect %s: %v", tc.address, err)
		}
	}
}
