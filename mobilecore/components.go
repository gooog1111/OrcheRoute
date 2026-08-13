package mobilecore

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	internalcomponents "github.com/gooog1111/orcheroute/internal/components"
	"github.com/metacubex/mihomo/component/geodata"
	C "github.com/metacubex/mihomo/constant"
)

const (
	geoIPURL   = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat"
	geoSiteURL = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
)

// EmbeddedMihomoVersion reports the module version actually linked into the
// APK instead of pretending that the embedded core is an independently
// replaceable executable.
func EmbeddedMihomoVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range info.Deps {
			if dependency.Path == "github.com/metacubex/mihomo" {
				if dependency.Replace != nil && dependency.Replace.Version != "" {
					return dependency.Replace.Version
				}
				if dependency.Version != "" {
					return dependency.Version
				}
			}
		}
	}
	return "embedded"
}

func GeoStatus(home string) string {
	if home == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "missing_mihomo_home"}})
	}
	return encode(map[string]any{"ok": true, "result": geoStatus(home)})
}

// GeoSources exposes the same source registry to every embedded frontend.
func GeoSources() string {
	return encode(map[string]any{"ok": true, "result": internalcomponents.GeoSources})
}

// ResolveGeoSource validates a built-in source or a pair of custom HTTPS URLs.
func ResolveGeoSource(id, geoIPURL, geoSiteURL string) string {
	source, err := internalcomponents.ResolveGeoSource(id, geoIPURL, geoSiteURL)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": source})
}

// GeoCatalog reads the actual categories from the installed binary databases.
func GeoCatalog(home string) string {
	if home == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "missing_mihomo_home"}})
	}
	geoIP, _ := internalcomponents.GeoCatalog(filepath.Join(home, "GeoIP.dat"))
	geoSite, _ := internalcomponents.GeoCatalog(filepath.Join(home, "GeoSite.dat"))
	if geoIP == nil {
		geoIP = []string{}
	}
	if geoSite == nil {
		geoSite = []string{}
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"geoip": geoIP, "geosite": geoSite}})
}

// UpdateGeo downloads into Mihomo's private directory. Mihomo validates both
// protobuf databases before replacing them. If the second update fails, both
// original files are restored so a partial pair is never reported as current.
func UpdateGeo(home string) string {
	return UpdateGeoFromSource(home, "metacubex", "", "")
}

// UpdateGeoFromSource resolves the selected source and atomically updates both
// databases. The existing pair is restored when either download is invalid.
func UpdateGeoFromSource(home, sourceID, geoIPCustomURL, geoSiteCustomURL string) string {
	return UpdateGeoFromSourceWithProgress(home, sourceID, geoIPCustomURL, geoSiteCustomURL, nil)
}

// GeoProgress is implemented by a platform adapter that displays byte-level
// download and validation progress.
type GeoProgress interface {
	OnGeoProgress(stage string, current, total int64)
}

func UpdateGeoFromSourceWithProgress(home, sourceID, geoIPCustomURL, geoSiteCustomURL string, progress GeoProgress) string {
	if home == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "missing_mihomo_home"}})
	}
	source, err := internalcomponents.ResolveGeoSource(sourceID, geoIPCustomURL, geoSiteCustomURL)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	C.SetHomeDir(home)
	C.GeositeName = "GeoSite.dat"
	C.GeoipName = "GeoIP.dat"
	geodata.SetGeoIpUrl(source.GeoIPURL)
	geodata.SetGeoSiteUrl(source.GeoSiteURL)

	geoIPPath := filepath.Join(home, C.GeoipName)
	geoSitePath := filepath.Join(home, C.GeositeName)
	backups := []fileBackup{snapshot(geoIPPath), snapshot(geoSitePath)}
	geoIPData, err := downloadGeo(source.GeoIPURL, "geoip_download", progress)
	if err != nil {
		restoreAll(backups)
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	geoSiteData, err := downloadGeo(source.GeoSiteURL, "geosite_download", progress)
	if err != nil {
		restoreAll(backups)
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	reportGeo(progress, "validation", 0, 2)
	loader, err := geodata.GetGeoDataLoader("standard")
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	if _, err = loader.LoadIPByBytes(geoIPData, "cn"); err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid GeoIP database: " + err.Error()}})
	}
	reportGeo(progress, "validation", 1, 2)
	if _, err = loader.LoadSiteByBytes(geoSiteData, "cn"); err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid GeoSite database: " + err.Error()}})
	}
	reportGeo(progress, "validation", 2, 2)
	reportGeo(progress, "install", 0, 2)
	if err = replaceGeoFile(geoIPPath, geoIPData); err != nil {
		restoreAll(backups)
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	reportGeo(progress, "install", 1, 2)
	if err = replaceGeoFile(geoSitePath, geoSiteData); err != nil {
		restoreAll(backups)
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	reportGeo(progress, "install", 2, 2)
	geodata.ClearGeoIPCache()
	geodata.ClearGeoSiteCache()
	result := geoStatus(home)
	result["source"] = source
	return encode(map[string]any{"ok": true, "result": result})
}

func downloadGeo(address, stage string, progress GeoProgress) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "OrcheRoute Mobile/0.4")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d", stage, response.StatusCode)
	}
	if response.ContentLength > 256<<20 {
		return nil, fmt.Errorf("%s: file_too_large", stage)
	}
	total := response.ContentLength
	reportGeo(progress, stage, 0, total)
	limited := io.LimitReader(response.Body, (256<<20)+1)
	data := make([]byte, 0, 1<<20)
	buffer := make([]byte, 64<<10)
	var current int64
	for {
		count, readErr := limited.Read(buffer)
		if count > 0 {
			data = append(data, buffer[:count]...)
			current += int64(count)
			reportGeo(progress, stage, current, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if len(data) == 0 || len(data) > 256<<20 {
		return nil, fmt.Errorf("%s: invalid_file_size", stage)
	}
	if total <= 0 {
		reportGeo(progress, stage, current, current)
	}
	return data, nil
}

func replaceGeoFile(path string, payload []byte) error {
	temporary := path + ".new"
	_ = os.Remove(temporary)
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func reportGeo(progress GeoProgress, stage string, current, total int64) {
	if progress != nil {
		progress.OnGeoProgress(stage, current, total)
	}
}

type fileBackup struct {
	path    string
	payload []byte
	mode    os.FileMode
	exists  bool
}

func snapshot(path string) fileBackup {
	result := fileBackup{path: path, mode: 0o600}
	if info, err := os.Stat(path); err == nil {
		result.exists = true
		result.mode = info.Mode().Perm()
		result.payload, _ = os.ReadFile(path)
	}
	return result
}

func restoreAll(backups []fileBackup) {
	for _, backup := range backups {
		if backup.exists {
			_ = os.WriteFile(backup.path, backup.payload, backup.mode)
		} else {
			_ = os.Remove(backup.path)
		}
	}
}

func geoStatus(home string) map[string]any {
	return map[string]any{
		"mihomo_version": EmbeddedMihomoVersion(),
		"geoip":          fileStatus(filepath.Join(home, "GeoIP.dat")),
		"geosite":        fileStatus(filepath.Join(home, "GeoSite.dat")),
		"checked_at":     time.Now().Unix(),
	}
}

func fileStatus(path string) map[string]any {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return map[string]any{"installed": false, "updated_at": int64(0), "size": int64(0)}
	}
	return map[string]any{"installed": true, "updated_at": info.ModTime().Unix(), "size": info.Size()}
}
