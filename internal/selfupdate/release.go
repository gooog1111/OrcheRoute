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
type githubAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}
type githubRelease struct {
	Tag        string        `json:"tag_name"`
	Prerelease bool          `json:"prerelease"`
	Draft      bool          `json:"draft"`
	PageURL    string        `json:"html_url"`
	Assets     []githubAsset `json:"assets"`
}

func Latest(ctx context.Context, client *http.Client, beta bool, arch string) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := "https://api.github.com/repos/" + Repository + "/releases/latest"
	if beta {
		url = "https://api.github.com/repos/" + Repository + "/releases?per_page=20"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "OrcheRoute self updater")
	res, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Release{}, fmt.Errorf("release_http_%d", res.StatusCode)
	}
	items := []githubRelease{}
	if beta {
		if err = json.NewDecoder(res.Body).Decode(&items); err != nil {
			return Release{}, err
		}
	} else {
		var one githubRelease
		if err = json.NewDecoder(res.Body).Decode(&one); err != nil {
			return Release{}, err
		}
		items = []githubRelease{one}
	}
	for _, item := range items {
		if item.Draft || (beta && !item.Prerelease) {
			continue
		}
		for _, asset := range item.Assets {
			lower := strings.ToLower(asset.Name)
			if strings.HasSuffix(lower, "-"+arch+".deb") && strings.Contains(lower, "server") && strings.HasPrefix(asset.Digest, "sha256:") {
				return Release{Version: strings.TrimPrefix(item.Tag, "v"), Prerelease: item.Prerelease, PageURL: item.PageURL, Asset: Asset{Name: asset.Name, URL: asset.URL, Digest: strings.TrimPrefix(asset.Digest, "sha256:"), Size: asset.Size}}, nil
			}
		}
	}
	return Release{}, fmt.Errorf("server_deb_asset_not_found")
}
