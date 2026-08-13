//go:build linux

package components

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const releaseAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"

var (
	versionRE       = regexp.MustCompile(`\bv?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\b`)
	mihomoVersionRE = regexp.MustCompile(`(?i)\bmihomo(?:\s+meta)?\s+v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\b`)
)

type Config struct {
	Component, StateDirectory, ProductionState, ConfigDirectory, Mihomo, CoreService, ControllerService string
}
type Release struct {
	CheckedAt        int64  `json:"checked_at"`
	InstalledVersion string `json:"installed_version"`
	LatestVersion    string `json:"latest_version"`
	UpdateAvailable  bool   `json:"update_available"`
	ReleaseURL       string `json:"release_url"`
	AssetName        string `json:"asset_name"`
	AssetURL         string `json:"asset_url"`
	AssetSize        int64  `json:"asset_size"`
	AssetDigest      string `json:"asset_digest"`
}
type releasePayload struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}
type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}
type updater struct {
	config    Config
	client    *http.Client
	operation string
}

func Run(ctx context.Context, config Config) error {
	if config.Component == "" {
		config.Component = "all"
	}
	if config.Component != "check" && config.Component != "geo" && config.Component != "core" && config.Component != "all" {
		return fmt.Errorf("unknown_component")
	}
	if err := os.MkdirAll(config.StateDirectory, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(config.StateDirectory, "component-update.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("component_update_in_progress")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	current := updater{config: config, client: &http.Client{Timeout: 5 * time.Minute}, operation: filepath.Join(config.StateDirectory, "component-operation.json")}
	return current.run(ctx)
}
func (current updater) run(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			_ = current.status("error", "failed", map[string]any{"message": "Обновление компонентов не выполнено", "error": err.Error(), "component": current.config.Component})
		}
	}()
	details := map[string]any{"component": current.config.Component}
	release := Release{}
	if current.config.Component != "geo" {
		var err error
		release, err = current.latest(ctx)
		if err != nil {
			return err
		}
		details["installed_version"], details["latest_version"], details["update_available"] = release.InstalledVersion, release.LatestVersion, release.UpdateAvailable
		if current.config.Component == "check" {
			return current.status("success", "complete", merge(details, map[string]any{"message": "Проверка завершена"}))
		}
	}
	if current.config.Component == "geo" || current.config.Component == "all" {
		if err := current.updateGeo(ctx); err != nil {
			return err
		}
	}
	if current.config.Component == "core" || current.config.Component == "all" {
		updated, err := current.updateCore(ctx, release)
		if err != nil {
			return err
		}
		details["core_updated"] = updated
	}
	return current.status("success", "complete", merge(details, map[string]any{"message": "Компоненты обновлены и проверены", "system_mutated": true}))
}
func (current updater) latest(ctx context.Context) (Release, error) {
	_ = current.status("running", "checking", map[string]any{"message": "Проверяем последнюю версию Mihomo", "component": current.config.Component})
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	request.Header.Set("User-Agent", "OrcheRoute Go component updater")
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := current.client.Do(request)
	if err != nil {
		return Release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Release{}, fmt.Errorf("release_http_%d", response.StatusCode)
	}
	var payload releasePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Release{}, err
	}
	latest := strings.TrimPrefix(payload.TagName, "v")
	if !versionRE.MatchString(latest) {
		return Release{}, fmt.Errorf("invalid_mihomo_release_tag")
	}
	architecture := map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "armv7"}[runtime.GOARCH]
	if architecture == "" {
		return Release{}, fmt.Errorf("unsupported_mihomo_architecture")
	}
	asset := selectLinuxAsset(payload.Assets, architecture, payload.TagName)
	if asset.Name == "" {
		return Release{}, fmt.Errorf("mihomo_release_asset_not_found")
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(asset.Digest) {
		return Release{}, fmt.Errorf("mihomo_release_digest_missing")
	}
	installed, _ := binaryVersion(current.config.Mihomo)
	result := Release{CheckedAt: time.Now().Unix(), InstalledVersion: installed, LatestVersion: latest, UpdateAvailable: installed != latest, ReleaseURL: payload.HTMLURL, AssetName: asset.Name, AssetURL: asset.BrowserDownloadURL, AssetSize: asset.Size, AssetDigest: asset.Digest}
	if err := writeJSON(filepath.Join(current.config.StateDirectory, "component-latest.json"), result); err != nil {
		return Release{}, err
	}
	return result, nil
}

func selectLinuxAsset(assets []releaseAsset, architecture, tag string) releaseAsset {
	// The unqualified amd64 release may require x86-64-v3. Desktop/server
	// packages must run on older AMD64 CPUs, so prefer compatible whenever the
	// upstream release provides it, regardless of GitHub asset ordering.
	wanted := "mihomo-linux-" + architecture + "-" + tag + ".gz"
	preferred := wanted
	if architecture == "amd64" {
		preferred = "mihomo-linux-amd64-compatible-" + tag + ".gz"
	}
	for _, name := range []string{preferred, wanted} {
		for _, candidate := range assets {
			if candidate.Name == name {
				return candidate
			}
		}
	}
	return releaseAsset{}
}
func (current updater) updateGeo(ctx context.Context) error {
	settings := map[string]any{}
	if payload, err := os.ReadFile(filepath.Join(current.config.StateDirectory, "component-settings.json")); err == nil {
		_ = json.Unmarshal(payload, &settings)
	}
	sourceID, _ := settings["geo_source"].(string)
	geoIPURL, _ := settings["geoip_url"].(string)
	geoSiteURL, _ := settings["geosite_url"].(string)
	source, err := ResolveGeoSource(sourceID, geoIPURL, geoSiteURL)
	if err != nil {
		return err
	}
	_ = current.status("running", "geo_download", map[string]any{"message": "Загружаем GeoIP и GeoSite из " + source.Name, "component": current.config.Component, "geo_source": source.ID})
	staging := filepath.Join(current.config.StateDirectory, "component-staging", "geo")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	downloads := []struct{ name, address, phase, label string }{
		{"GeoIP.dat", source.GeoIPURL, "geoip_download", "GeoIP"},
		{"GeoSite.dat", source.GeoSiteURL, "geosite_download", "GeoSite"},
	}
	for _, download := range downloads {
		name, address, phase, label := download.name, download.address, download.phase, download.label
		if err := current.download(ctx, address, filepath.Join(staging, name), 256<<20, func(downloaded, total int64) {
			_ = current.status("running", phase, map[string]any{
				"message":   formatDownloadProgress("Загружаем "+label, downloaded, total),
				"component": current.config.Component, "geo_source": source.ID,
				"file": name, "current": downloaded, "total": total,
			})
		}); err != nil {
			return err
		}
	}
	geoIPCatalog, err := GeoCatalog(filepath.Join(staging, "GeoIP.dat"))
	if err != nil || len(geoIPCatalog) == 0 {
		return fmt.Errorf("geoip_catalog_invalid")
	}
	geoSiteCatalog, err := GeoCatalog(filepath.Join(staging, "GeoSite.dat"))
	if err != nil || len(geoSiteCatalog) == 0 {
		return fmt.Errorf("geosite_catalog_invalid")
	}
	_ = current.status("running", "geo_validation", map[string]any{"message": "Проверяем GeoIP и GeoSite · 0 / 2", "component": current.config.Component, "current": 0, "total": 2})
	configPayload, err := os.ReadFile(filepath.Join(current.config.ConfigDirectory, "config.json"))
	if err == nil {
		var config map[string]any
		if err := json.Unmarshal(configPayload, &config); err != nil {
			return err
		}
		rules, _ := config["rules"].([]any)
		insert := len(rules)
		if insert > 0 {
			insert--
		}
		updated := append([]any{}, rules[:insert]...)
		updated = append(updated, "GEOIP,LAN,DIRECT-EGRESS", "GEOSITE,category-ads-all,REJECT")
		updated = append(updated, rules[insert:]...)
		config["rules"] = updated
		candidate := filepath.Join(staging, "config.json")
		if err := writeJSON(candidate, config); err != nil {
			return err
		}
		validationCommand := exec.CommandContext(ctx, current.config.Mihomo, "-t", "-d", staging, "-f", candidate)
		validationCommand.Env = append(os.Environ(), "SAFE_PATHS="+current.config.ProductionState+":"+staging)
		if output, err := validationCommand.CombinedOutput(); err != nil {
			return fmt.Errorf("geo_validation_failed: %w: %s", err, tail(string(output), 1000))
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	_ = current.status("running", "geo_validation", map[string]any{"message": "GeoIP и GeoSite проверены · 2 / 2", "component": current.config.Component, "current": 2, "total": 2})
	names := []string{"GeoIP.dat", "GeoSite.dat"}
	_ = current.status("running", "geo_install", map[string]any{"message": "Устанавливаем геобазы · 0 / 2", "component": current.config.Component, "current": 0, "total": 2})
	for _, name := range names {
		target, backup := filepath.Join(current.config.ProductionState, name), filepath.Join(current.config.ProductionState, name)+".previous"
		_ = os.Remove(backup)
		if _, err := os.Stat(target); err == nil {
			if err := os.Rename(target, backup); err != nil {
				for _, previous := range names {
					previousTarget, previousBackup := filepath.Join(current.config.ProductionState, previous), filepath.Join(current.config.ProductionState, previous)+".previous"
					if _, backupErr := os.Stat(previousBackup); backupErr == nil {
						_ = os.Rename(previousBackup, previousTarget)
					}
				}
				return err
			}
		}
	}
	for index, name := range names {
		source, target := filepath.Join(staging, name), filepath.Join(current.config.ProductionState, name)
		if err := copyAtomic(source, target, 0o600); err != nil {
			for _, previous := range names {
				previousTarget, previousBackup := filepath.Join(current.config.ProductionState, previous), filepath.Join(current.config.ProductionState, previous)+".previous"
				_ = os.Remove(previousTarget)
				if _, backupErr := os.Stat(previousBackup); backupErr == nil {
					_ = os.Rename(previousBackup, previousTarget)
				}
			}
			return err
		}
		_ = current.status("running", "geo_install", map[string]any{"message": fmt.Sprintf("Устанавливаем геобазы · %d / 2", index+1), "component": current.config.Component, "current": index + 1, "total": 2})
	}
	if err := writeJSON(filepath.Join(current.config.StateDirectory, "geo-installed-source.json"), map[string]any{"id": source.ID, "name": source.Name, "updated_at": time.Now().Unix()}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(current.config.StateDirectory, "geo-catalog.json"), map[string]any{"geoip": geoIPCatalog, "geosite": geoSiteCatalog}); err != nil {
		return err
	}
	return nil
}
func (current updater) updateCore(ctx context.Context, release Release) (bool, error) {
	if !release.UpdateAvailable {
		return false, nil
	}
	staging := filepath.Join(current.config.StateDirectory, "component-staging", "core")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return false, err
	}
	archive := filepath.Join(staging, release.AssetName)
	if err := current.download(ctx, release.AssetURL, archive, 256<<20, func(downloaded, total int64) {
		_ = current.status("running", "core_download", map[string]any{"message": formatDownloadProgress("Загружаем Mihomo", downloaded, total), "component": current.config.Component, "current": downloaded, "total": total})
	}); err != nil {
		return false, err
	}
	digest, err := fileSHA(archive)
	if err != nil {
		return false, err
	}
	if digest != strings.TrimPrefix(release.AssetDigest, "sha256:") {
		return false, fmt.Errorf("mihomo_checksum_mismatch")
	}
	source, err := os.Open(archive)
	if err != nil {
		return false, err
	}
	compressed, err := gzip.NewReader(source)
	if err != nil {
		source.Close()
		return false, err
	}
	// Astra and other hardened distributions commonly mount /var with noexec.
	// Validate the candidate next to the installed executable: the service can
	// already execute from and atomically update this directory.
	destination, err := os.CreateTemp(filepath.Dir(current.config.Mihomo), ".mihomo-candidate-*")
	if err != nil {
		compressed.Close()
		source.Close()
		return false, err
	}
	candidate := destination.Name()
	defer os.Remove(candidate)
	_, copyErr := io.Copy(destination, compressed)
	closeErr := destination.Close()
	compressed.Close()
	source.Close()
	if copyErr != nil {
		return false, copyErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if err := os.Chmod(candidate, 0o755); err != nil {
		return false, err
	}
	version, err := binaryVersion(candidate)
	if err != nil {
		return false, fmt.Errorf("mihomo_candidate_not_executable: %w", err)
	}
	if normalizeVersion(version) != normalizeVersion(release.LatestVersion) {
		return false, fmt.Errorf("mihomo_candidate_version_mismatch: expected=%s actual=%s", release.LatestVersion, version)
	}
	if output, err := exec.CommandContext(ctx, candidate, "-t", "-d", current.config.ProductionState, "-f", filepath.Join(current.config.ConfigDirectory, "config.json")).CombinedOutput(); err != nil {
		return false, fmt.Errorf("mihomo_candidate_config_invalid: %w: %s", err, tail(string(output), 1000))
	}
	backup := filepath.Join(current.config.ProductionState, "backups", "mihomo-safe-"+time.Now().Format("20060102-150405")+"-"+release.InstalledVersion)
	if err := copyAtomic(current.config.Mihomo, backup, 0o700); err != nil {
		return false, err
	}
	if err := copyAtomic(candidate, current.config.Mihomo, 0o755); err != nil {
		return false, err
	}
	if err := restart(ctx, current.config.CoreService, current.config.ControllerService); err != nil {
		_ = copyAtomic(backup, current.config.Mihomo, 0o755)
		_ = restart(context.Background(), current.config.CoreService, current.config.ControllerService)
		return false, fmt.Errorf("mihomo_update_rolled_back: %w", err)
	}
	return true, nil
}
func (current updater) download(ctx context.Context, address, path string, maximum int64, progress func(current, total int64)) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	request.Header.Set("User-Agent", "OrcheRoute Go component updater")
	response, err := current.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download_http_%d", response.StatusCode)
	}
	if response.ContentLength > maximum {
		return fmt.Errorf("component_file_too_large")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	total := response.ContentLength
	if progress != nil {
		progress(0, total)
	}
	limited := io.LimitReader(response.Body, maximum+1)
	buffer := make([]byte, 64<<10)
	var written int64
	lastReport := time.Now()
	var copyErr error
	for {
		count, readErr := limited.Read(buffer)
		if count > 0 {
			writtenCount, writeErr := file.Write(buffer[:count])
			written += int64(writtenCount)
			if writeErr != nil {
				copyErr = writeErr
				break
			}
			if writtenCount != count {
				copyErr = io.ErrShortWrite
				break
			}
			if progress != nil && time.Since(lastReport) >= 250*time.Millisecond {
				progress(written, total)
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			copyErr = readErr
			break
		}
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maximum {
		return fmt.Errorf("component_file_too_large")
	}
	if written < 1024 {
		return fmt.Errorf("component_file_too_small")
	}
	if progress != nil {
		if total <= 0 {
			total = written
		}
		progress(written, total)
	}
	return nil
}

func formatDownloadProgress(label string, current, total int64) string {
	if total > 0 {
		return fmt.Sprintf("%s · %s / %s", label, formatBytes(current), formatBytes(total))
	}
	return fmt.Sprintf("%s · %s", label, formatBytes(current))
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d Б", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.1f КБ", float64(value)/1024)
	}
	return fmt.Sprintf("%.1f МБ", float64(value)/(1024*1024))
}
func (current updater) status(state, phase string, fields map[string]any) error {
	value := map[string]any{"kind": "component_update", "status": state, "phase": phase, "updated_at": time.Now().Unix()}
	for key, item := range fields {
		value[key] = item
	}
	return writeJSON(current.operation, value)
}
func binaryVersion(path string) (string, error) {
	output, err := exec.Command(path, "-v").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, tail(strings.TrimSpace(string(output)), 500))
	}
	match := mihomoVersionRE.FindStringSubmatch(string(output))
	if len(match) < 2 {
		match = versionRE.FindStringSubmatch(string(output))
	}
	if len(match) < 2 {
		return "", fmt.Errorf("version_unavailable: %s", tail(strings.TrimSpace(string(output)), 500))
	}
	return normalizeVersion(match[1]), nil
}

func normalizeVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}
func restart(ctx context.Context, services ...string) error {
	for _, service := range services {
		if service == "" {
			continue
		}
		if output, err := exec.CommandContext(ctx, "systemctl", "restart", service).CombinedOutput(); err != nil {
			return fmt.Errorf("restart %s: %w: %s", service, err, tail(string(output), 500))
		}
	}
	return nil
}
func fileSHA(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
func copyAtomic(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	temporary := target + ".new"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(temporary, mode); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}
func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return copyBytes(path, append(payload, '\n'), 0o600)
}
func copyBytes(path string, payload []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, payload, mode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
func merge(left, right map[string]any) map[string]any {
	for key, value := range right {
		left[key] = value
	}
	return left
}
func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
