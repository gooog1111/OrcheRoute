package selfupdate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const Repository = "gooog1111/OrcheRoute"
const StableManifestURL = "https://github.com/" + Repository + "/releases/latest/download/server-update.json"
const BetaManifestURL = "https://github.com/" + Repository + "/releases/download/server-beta/server-update.json"

const MaxAssetBytes int64 = 512 << 20

// HTTPClient forbids HTTPS downgrades and redirects outside GitHub's release CDN.
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !trustedDownloadURL(req.URL) {
			return fmt.Errorf("untrusted_download_redirect")
		}
		return nil
	}}
}

func trustedDownloadURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

func ValidAssetURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && trustedDownloadURL(u) && u.Hostname() == "github.com" &&
		strings.HasPrefix(u.Path, "/"+Repository+"/releases/download/") && u.Path == path.Clean(u.Path) && u.RawPath == "" && u.RawQuery == "" && u.Fragment == ""
}

type Asset struct {
	Name, URL, Digest string
	Size              int64
}
type Release struct {
	Version    string
	Prerelease bool
	PageURL    string
	Asset      Asset
}

type manifest struct {
	Version    string `json:"version"`
	Prerelease bool   `json:"prerelease"`
	PageURL    string `json:"page_url"`
	Assets     map[string]struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"assets"`
}

// Latest reads a small update manifest from GitHub's raw CDN. It deliberately
// avoids api.github.com, whose anonymous quota must not affect installed apps.
func Latest(ctx context.Context, client *http.Client, beta bool, arch string) (Release, error) {
	if client == nil {
		client = HTTPClient(15 * time.Second)
	}
	manifestURL := StableManifestURL
	if beta {
		manifestURL = BetaManifestURL
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	req.Header.Set("User-Agent", "OrcheRoute self updater")
	res, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("release_manifest_http_%d", res.StatusCode)
	}
	var item manifest
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&item); err != nil {
		return Release{}, err
	}
	asset, ok := item.Assets[arch]
	if !ok {
		return Release{}, fmt.Errorf("server_deb_asset_not_found")
	}
	version, digest := strings.TrimPrefix(strings.TrimSpace(item.Version), "v"), strings.ToLower(strings.TrimSpace(asset.SHA256))
	decodedDigest, digestErr := hex.DecodeString(digest)
	assetURL, _ := url.Parse(asset.URL)
	if version == "" || digestErr != nil || len(decodedDigest) != 32 || asset.Size <= 0 || asset.Size > MaxAssetBytes ||
		asset.Name == "" || strings.ContainsAny(asset.Name, "/\\\x00") || !strings.HasSuffix(asset.Name, ".deb") ||
		!ValidAssetURL(asset.URL) || assetURL == nil || path.Base(assetURL.Path) != asset.Name {
		return Release{}, fmt.Errorf("invalid_release_manifest")
	}
	if beta != item.Prerelease {
		return Release{}, fmt.Errorf("release_channel_mismatch")
	}
	return Release{Version: version, Prerelease: item.Prerelease, PageURL: item.PageURL,
		Asset: Asset{Name: asset.Name, URL: asset.URL, Digest: digest, Size: asset.Size}}, nil
}
