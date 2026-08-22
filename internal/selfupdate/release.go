package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const Repository = "gooog1111/OrcheRoute"
const StableManifestURL = "https://github.com/" + Repository + "/releases/latest/download/server-update.json"
const BetaManifestURL = "https://github.com/" + Repository + "/releases/download/server-beta/server-update.json"

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
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := StableManifestURL
	if beta { url = BetaManifestURL }
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	if err := json.NewDecoder(res.Body).Decode(&item); err != nil {
		return Release{}, err
	}
	asset, ok := item.Assets[arch]
	if !ok {
		return Release{}, fmt.Errorf("server_deb_asset_not_found")
	}
	version, digest := strings.TrimPrefix(strings.TrimSpace(item.Version), "v"), strings.ToLower(strings.TrimSpace(asset.SHA256))
	if version == "" || len(digest) != 64 || asset.Size <= 0 || !strings.HasPrefix(asset.URL, "https://") {
		return Release{}, fmt.Errorf("invalid_release_manifest")
	}
	if beta != item.Prerelease {
		return Release{}, fmt.Errorf("release_channel_mismatch")
	}
	return Release{Version: version, Prerelease: item.Prerelease, PageURL: item.PageURL,
		Asset: Asset{Name: asset.Name, URL: asset.URL, Digest: digest, Size: asset.Size}}, nil
}
