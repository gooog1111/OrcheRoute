//go:build windows

package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/components"
	"golang.org/x/net/proxy"
	"golang.org/x/sys/windows"
)

const releaseAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"

var versionRE = regexp.MustCompile(`\bv?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\b`)

type config struct {
	component, stateDirectory, productionState, configDirectory, mihomo, coreService, controllerService string
}

type release struct {
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
	config    config
	client    *http.Client
	operation string
}

func main() {
	root := os.Getenv("ProgramData")
	if root == "" {
		root = `C:\ProgramData`
	}
	root = filepath.Join(root, "OrcheRoute")
	current := config{}
	flag.StringVar(&current.component, "component", "all", "check, geo, core or all")
	flag.StringVar(&current.stateDirectory, "state-dir", filepath.Join(root, "state"), "operation and staging state")
	flag.StringVar(&current.productionState, "production-state", filepath.Join(root, "state"), "production data directory")
	flag.StringVar(&current.configDirectory, "config-dir", root, "configuration directory")
	flag.StringVar(&current.mihomo, "mihomo", filepath.Join(root, "bin", "mihomo.exe"), "Mihomo binary")
	flag.StringVar(&current.coreService, "core-service", "OrcheRouteMihomo", "Mihomo service")
	flag.StringVar(&current.controllerService, "controller-service", "OrcheRoute", "controller service")
	flag.Parse()
	if err := run(context.Background(), current); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	if cfg.component != "check" && cfg.component != "geo" && cfg.component != "core" && cfg.component != "all" {
		return fmt.Errorf("unknown_component")
	}
	if err := os.MkdirAll(cfg.stateDirectory, 0o700); err != nil {
		return err
	}
	releaseLock, acquired, err := lockFile(filepath.Join(cfg.stateDirectory, "component-update.lock"))
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("component_update_in_progress")
	}
	defer releaseLock()
	current := updater{config: cfg, client: &http.Client{Timeout: 5 * time.Minute}, operation: filepath.Join(cfg.stateDirectory, "component-operation.json")}
	rel, err := current.latest(ctx)
	if err != nil {
		_ = current.status("error", "failed", map[string]any{"message": "Проверка компонентов не выполнена", "error": err.Error()})
		return err
	}
	details := map[string]any{"component": cfg.component, "installed_version": rel.InstalledVersion, "latest_version": rel.LatestVersion, "update_available": rel.UpdateAvailable}
	if cfg.component == "check" {
		return current.status("success", "complete", with(details, "message", "Проверка завершена"))
	}
	if cfg.component == "geo" || cfg.component == "all" {
		if err := current.updateGeo(ctx); err != nil {
			_ = current.status("error", "failed", map[string]any{"message": "Обновление GEO не выполнено", "error": err.Error()})
			return err
		}
	}
	if cfg.component == "core" || cfg.component == "all" {
		updated, err := current.updateCore(ctx, rel)
		if err != nil {
			_ = current.status("error", "failed", map[string]any{"message": "Обновление Mihomo не выполнено", "error": err.Error()})
			return err
		}
		details["core_updated"] = updated
	}
	return current.status("success", "complete", with(details, "message", "Компоненты обновлены и проверены"))
}

func (current updater) latest(ctx context.Context) (release, error) {
	_ = current.status("running", "checking", map[string]any{"message": "Проверяем последнюю версию Mihomo"})
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	request.Header.Set("User-Agent", "OrcheRoute Go component updater")
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := current.client.Do(request)
	if err != nil {
		return release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("release_http_%d", response.StatusCode)
	}
	var payload releasePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return release{}, err
	}
	latest := strings.TrimPrefix(payload.TagName, "v")
	if !versionRE.MatchString(latest) {
		return release{}, fmt.Errorf("invalid_mihomo_release_tag")
	}
	architecture := map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
	if architecture == "" {
		return release{}, fmt.Errorf("unsupported_mihomo_architecture")
	}
	wanted := "mihomo-windows-" + architecture + "-" + payload.TagName + ".zip"
	compatible := "mihomo-windows-amd64-compatible-" + payload.TagName + ".zip"
	var asset releaseAsset
	for _, candidate := range payload.Assets {
		if candidate.Name == wanted || (asset.Name == "" && architecture == "amd64" && candidate.Name == compatible) {
			asset = candidate
		}
	}
	if asset.Name == "" {
		return release{}, fmt.Errorf("mihomo_release_asset_not_found")
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(asset.Digest) {
		return release{}, fmt.Errorf("mihomo_release_digest_missing")
	}
	installed, _ := binaryVersion(current.config.mihomo)
	result := release{CheckedAt: time.Now().Unix(), InstalledVersion: installed, LatestVersion: latest, UpdateAvailable: installed != latest,
		ReleaseURL: payload.HTMLURL, AssetName: asset.Name, AssetURL: asset.BrowserDownloadURL, AssetSize: asset.Size, AssetDigest: asset.Digest}
	if err := writeJSON(filepath.Join(current.config.stateDirectory, "component-latest.json"), result); err != nil {
		return release{}, err
	}
	return result, nil
}

func (current updater) updateGeo(ctx context.Context) error {
	settings := map[string]any{}
	_ = readJSON(filepath.Join(current.config.stateDirectory, "component-settings.json"), &settings)
	source, err := components.ResolveGeoSource(stringValue(settings["geo_source"]), stringValue(settings["geoip_url"]), stringValue(settings["geosite_url"]))
	if err != nil {
		return err
	}
	_ = current.status("running", "geo_download", map[string]any{"message": "Загружаем GeoIP и GeoSite из " + source.Name, "geo_source": source.ID})
	staging := filepath.Join(current.config.stateDirectory, "component-staging", "geo")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	for name, address := range map[string]string{"GeoIP.dat": source.GeoIPURL, "GeoSite.dat": source.GeoSiteURL} {
		if err := current.download(ctx, address, filepath.Join(staging, name), 256<<20); err != nil {
			return err
		}
	}
	geoIP, err := components.GeoCatalog(filepath.Join(staging, "GeoIP.dat"))
	if err != nil || len(geoIP) == 0 {
		return fmt.Errorf("geoip_catalog_invalid")
	}
	geoSite, err := components.GeoCatalog(filepath.Join(staging, "GeoSite.dat"))
	if err != nil || len(geoSite) == 0 {
		return fmt.Errorf("geosite_catalog_invalid")
	}
	_ = current.status("running", "geo_validation", map[string]any{"message": "Проверяем новые геобазы"})
	var mihomoConfig map[string]any
	if err := readJSON(filepath.Join(current.config.configDirectory, "config.json"), &mihomoConfig); err != nil {
		return err
	}
	candidateConfig := filepath.Join(staging, "config.json")
	if err := writeJSON(candidateConfig, mihomoConfig); err != nil {
		return err
	}
	if output, err := exec.CommandContext(ctx, current.config.mihomo, "-t", "-d", staging, "-f", candidateConfig).CombinedOutput(); err != nil {
		return fmt.Errorf("geo_validation_failed: %s", tail(string(output), 1000))
	}
	backups := map[string][]byte{}
	for _, name := range []string{"GeoIP.dat", "GeoSite.dat"} {
		target := filepath.Join(current.config.productionState, name)
		backups[name], _ = os.ReadFile(target)
		payload, readErr := os.ReadFile(filepath.Join(staging, name))
		if readErr != nil || writeAtomic(target, payload) != nil {
			for restoreName, restorePayload := range backups {
				if restorePayload != nil {
					_ = writeAtomic(filepath.Join(current.config.productionState, restoreName), restorePayload)
				}
			}
			if readErr != nil {
				return readErr
			}
			return fmt.Errorf("geo_install_failed")
		}
	}
	if err := writeJSON(filepath.Join(current.config.stateDirectory, "geo-installed-source.json"), map[string]any{"id": source.ID, "name": source.Name, "updated_at": time.Now().Unix()}); err != nil {
		return err
	}
	return writeJSON(filepath.Join(current.config.stateDirectory, "geo-catalog.json"), map[string]any{"geoip": geoIP, "geosite": geoSite})
}

func (current updater) updateCore(ctx context.Context, rel release) (bool, error) {
	if !rel.UpdateAvailable {
		return false, nil
	}
	staging := filepath.Join(current.config.stateDirectory, "component-staging", "core")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return false, err
	}
	archive := filepath.Join(staging, rel.AssetName)
	if err := current.download(ctx, rel.AssetURL, archive, 256<<20); err != nil {
		return false, err
	}
	digest, err := fileSHA(archive)
	if err != nil || digest != strings.TrimPrefix(rel.AssetDigest, "sha256:") {
		return false, fmt.Errorf("mihomo_checksum_mismatch")
	}
	candidate := filepath.Join(staging, "mihomo.exe")
	if err := extractExecutable(archive, candidate); err != nil {
		return false, err
	}
	version, err := binaryVersion(candidate)
	if err != nil || version != rel.LatestVersion {
		return false, fmt.Errorf("mihomo_candidate_version_mismatch")
	}
	configPath := filepath.Join(current.config.configDirectory, "config.json")
	if output, err := exec.CommandContext(ctx, candidate, "-t", "-d", current.config.productionState, "-f", configPath).CombinedOutput(); err != nil {
		return false, fmt.Errorf("mihomo_candidate_config_invalid: %s", tail(string(output), 1000))
	}
	backup := filepath.Join(current.config.productionState, "backups", "mihomo-safe-"+time.Now().Format("20060102-150405")+"-"+rel.InstalledVersion+".exe")
	if payload, err := os.ReadFile(current.config.mihomo); err == nil {
		if err := writeAtomic(backup, payload); err != nil {
			return false, err
		}
	}
	if err := stopService(ctx, current.config.coreService); err != nil {
		return false, err
	}
	candidatePayload, err := os.ReadFile(candidate)
	if err != nil {
		return false, err
	}
	if err := writeAtomic(current.config.mihomo, candidatePayload); err != nil {
		_ = startService(context.Background(), current.config.coreService)
		return false, err
	}
	if err := startService(ctx, current.config.coreService); err != nil || !waitProxy(ctx) {
		if backupPayload, readErr := os.ReadFile(backup); readErr == nil {
			_ = stopService(context.Background(), current.config.coreService)
			_ = writeAtomic(current.config.mihomo, backupPayload)
			_ = startService(context.Background(), current.config.coreService)
		}
		return false, fmt.Errorf("mihomo_update_rolled_back")
	}
	return true, nil
}

func (current updater) download(ctx context.Context, address, path string, maximum int64) error {
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
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maximum+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written < 1024 || written > maximum {
		return fmt.Errorf("invalid_component_file_size")
	}
	return nil
}

func (current updater) status(state, phase string, fields map[string]any) error {
	value := map[string]any{"kind": "component_update", "status": state, "phase": phase, "updated_at": time.Now().Unix()}
	for key, item := range fields {
		value[key] = item
	}
	return writeJSON(current.operation, value)
}

func extractExecutable(path, target string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if !strings.HasSuffix(strings.ToLower(entry.Name), ".exe") || strings.Contains(entry.Name, "/") || strings.Contains(entry.Name, `\`) {
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		destination, err := os.Create(target)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(destination, io.LimitReader(source, 256<<20))
		destination.Close()
		source.Close()
		return copyErr
	}
	return fmt.Errorf("mihomo_executable_not_found")
}

func binaryVersion(path string) (string, error) {
	output, err := exec.Command(path, "-v").CombinedOutput()
	if err != nil {
		return "", err
	}
	match := versionRE.FindStringSubmatch(string(output))
	if len(match) < 2 {
		return "", fmt.Errorf("version_unavailable")
	}
	return match[1], nil
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

func lockFile(path string) (func(), bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false, err
	}
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	if err != nil {
		file.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return func() {}, false, nil
		}
		return func() {}, false, err
	}
	return func() { _ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped); _ = file.Close() }, true, nil
}

func stopService(ctx context.Context, name string) error {
	output, err := exec.CommandContext(ctx, "sc.exe", "stop", name).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "1062") {
		return fmt.Errorf("service_stop_failed: %s", tail(string(output), 500))
	}
	for count := 0; count < 40; count++ {
		query, _ := exec.CommandContext(ctx, "sc.exe", "query", name).CombinedOutput()
		if strings.Contains(string(query), "STOPPED") {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service_stop_timeout")
}

func startService(ctx context.Context, name string) error {
	output, err := exec.CommandContext(ctx, "sc.exe", "start", name).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "already running") {
		return fmt.Errorf("service_start_failed: %s", tail(string(output), 500))
	}
	return nil
}

func waitProxy(ctx context.Context) bool {
	for count := 0; count < 12; count++ {
		dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:21080", nil, &net.Dialer{Timeout: 5 * time.Second})
		if err == nil {
			transport := &http.Transport{DialContext: func(_ context.Context, networkName, address string) (net.Conn, error) {
				return dialer.Dial(networkName, address)
			}, TLSHandshakeTimeout: 8 * time.Second}
			client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.gstatic.com/generate_204", nil)
			response, requestErr := client.Do(request)
			if requestErr == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusNoContent {
					return true
				}
			}
			transport.CloseIdleConnections()
		}
		time.Sleep(3 * time.Second)
	}
	return false
}

func readJSON(path string, target any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}

func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(payload, '\n'))
}

func writeAtomic(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".new"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(temporary, path)
}

func stringValue(value any) string {
	current, _ := value.(string)
	return strings.TrimSpace(current)
}

func with(values map[string]any, key string, value any) map[string]any {
	values[key] = value
	return values
}

func tail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
