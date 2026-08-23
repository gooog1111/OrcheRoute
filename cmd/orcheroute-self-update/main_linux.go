//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gooog1111/orcheroute/internal/selfupdate"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type status struct {
	State           string `json:"state"`
	Message         string `json:"message"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Active          bool   `json:"active"`
	Error           string `json:"error,omitempty"`
	UpdatedAt       int64  `json:"updated_at"`
	Beta            bool   `json:"beta_enabled"`
}

func main() {
	action := flag.String("action", "check", "")
	state := flag.String("state-dir", "/var/lib/orcheroute", "")
	beta := flag.Bool("beta", false, "")
	flag.Parse()
	if os.Geteuid() != 0 {
		fatal(*state, "root_required", *beta)
	}
	if err := run(*action, *state, *beta); err != nil {
		fatal(*state, err.Error(), *beta)
	}
}
func run(action, dir string, beta bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	current := installed()
	write(dir, status{State: "checking", Message: "Проверяем манифест обновлений", CurrentVersion: current, Active: true, UpdatedAt: time.Now().Unix(), Beta: beta})
	rel, err := selfupdate.Latest(ctx, nil, beta, "amd64")
	if err != nil {
		return err
	}
	available := newer(current, rel.Version)
	if action == "check" {
		state, message := "current", "Установлена актуальная версия"
		if available {
			state, message = "available", "Доступна версия "+rel.Version
		}
		write(dir, status{State: state, Message: message, CurrentVersion: current, LatestVersion: rel.Version, UpdateAvailable: available, UpdatedAt: time.Now().Unix(), Beta: beta})
		return nil
	}
	if action != "install" {
		return fmt.Errorf("invalid_action")
	}
	if !available {
		return fmt.Errorf("already_current")
	}
	rollback, err := ensureRollback(ctx, dir, current, beta)
	if err != nil {
		return err
	}
	staging := filepath.Join(dir, "self-update")
	os.MkdirAll(staging, 0700)
	candidate := filepath.Join(staging, rel.Asset.Name)
	write(dir, status{State: "downloading", Message: "Загружаем DEB", CurrentVersion: current, LatestVersion: rel.Version, Active: true, UpdatedAt: time.Now().Unix(), Beta: beta})
	if err = download(ctx, rel.Asset.URL, candidate, rel.Asset.Size); err != nil {
		return err
	}
	if digest(candidate) != rel.Asset.Digest {
		return fmt.Errorf("sha256_mismatch")
	}
	if debField(candidate, "Package") != "orcheroute" || debField(candidate, "Architecture") != "amd64" || debField(candidate, "Version") != rel.Version {
		return fmt.Errorf("invalid_deb_package")
	}
	backup := filepath.Join(dir, "backups", "before-self-update-"+time.Now().Format("20060102-150405")+".tar.gz")
	os.MkdirAll(filepath.Dir(backup), 0700)
	if out, backupErr := exec.Command("tar", "--exclude=/var/lib/orcheroute/backups", "--exclude=/var/lib/orcheroute/self-update", "--exclude=/var/lib/orcheroute/packages", "-czf", backup, "/etc/orcheroute", "/var/lib/orcheroute").CombinedOutput(); backupErr != nil {
		return fmt.Errorf("backup_failed:%s", tail(string(out)))
	}
	core := exec.Command("systemctl", "is-active", "--quiet", "orcheroute-core.service").Run() == nil
	write(dir, status{State: "installing", Message: "Устанавливаем и проверяем пакет", CurrentVersion: current, LatestVersion: rel.Version, Active: true, UpdatedAt: time.Now().Unix(), Beta: beta})
	if out, e := exec.CommandContext(ctx, "dpkg", "-i", candidate).CombinedOutput(); e != nil {
		return rollbackInstall(ctx, rollback, fmt.Errorf("dpkg_failed:%s", tail(string(out))))
	}
	time.Sleep(3 * time.Second)
	if exec.Command("systemctl", "is-active", "--quiet", "orcheroute-go.service").Run() != nil || (core && exec.Command("systemctl", "is-active", "--quiet", "orcheroute-core.service").Run() != nil) || !health() {
		return rollbackInstall(ctx, rollback, fmt.Errorf("healthcheck_failed"))
	}
	os.MkdirAll(filepath.Dir(rollback), 0700)
	if err = os.Rename(candidate, rollback+".new"); err != nil {
		return err
	}
	if err = os.Rename(rollback+".new", rollback); err != nil {
		return err
	}
	write(dir, status{State: "current", Message: "OrcheRoute обновлён", CurrentVersion: rel.Version, LatestVersion: rel.Version, UpdatedAt: time.Now().Unix(), Beta: beta})
	return nil
}

func ensureRollback(ctx context.Context, dir, current string, beta bool) (string, error) {
	rollback := filepath.Join(dir, "packages", "current.deb")
	if validRollback(rollback, current) {
		return rollback, nil
	}
	if strings.TrimSpace(current) == "" {
		return "", fmt.Errorf("rollback_package_missing")
	}
	if err := os.MkdirAll(filepath.Dir(rollback), 0700); err != nil {
		return "", err
	}
	temporary := rollback + ".download"
	_ = os.Remove(temporary)
	defer os.Remove(temporary)
	if err := download(ctx, rollbackAssetURL(current, beta), temporary, 0); err != nil {
		return "", fmt.Errorf("rollback_package_download_failed:%w", err)
	}
	if !validRollback(temporary, current) {
		return "", fmt.Errorf("rollback_package_invalid")
	}
	if err := os.Rename(temporary, rollback); err != nil {
		return "", err
	}
	return rollback, nil
}

func validRollback(path, version string) bool {
	return debField(path, "Package") == "orcheroute" && debField(path, "Architecture") == "amd64" && debField(path, "Version") == version
}

func rollbackAssetURL(version string, beta bool) string {
	tag := "v" + version
	if beta {
		tag = "server-beta"
	}
	name := "OrcheRoute-Linux-Server-" + version + "-amd64.deb"
	return "https://github.com/" + selfupdate.Repository + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}
func rollbackInstall(ctx context.Context, p string, cause error) error {
	_, _ = exec.CommandContext(ctx, "dpkg", "-i", p).CombinedOutput()
	_ = exec.Command("systemctl", "restart", "orcheroute-go.service").Run()
	return fmt.Errorf("%v; rolled_back", cause)
}
func installed() string {
	b, _ := exec.Command("dpkg-query", "-W", "-f=${Version}", "orcheroute").Output()
	return strings.TrimSpace(string(b))
}
func newer(current, candidate string) bool {
	return exec.Command("dpkg", "--compare-versions", current, "lt", candidate).Run() == nil
}
func debField(p, field string) string {
	b, e := exec.Command("dpkg-deb", "-f", p, field).Output()
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func download(ctx context.Context, u, p string, size int64) error {
	q, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	q.Header.Set("User-Agent", "OrcheRoute self updater")
	r, e := http.DefaultClient.Do(q)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("download_http_%d", r.StatusCode)
	}
	if r.ContentLength > 512<<20 {
		return fmt.Errorf("asset_too_large")
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(f, io.LimitReader(r.Body, 512<<20))
	c := f.Close()
	if e != nil {
		return e
	}
	return c
}
func digest(p string) string {
	f, e := os.Open(p)
	if e != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}
func health() bool {
	client := &http.Client{Timeout: 10 * time.Second}
	r, e := client.Get("http://127.0.0.1:19100/healthz")
	if e != nil {
		return false
	}
	defer r.Body.Close()
	return r.StatusCode == 200
}
func write(dir string, s status) {
	os.MkdirAll(dir, 0700)
	b, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(filepath.Join(dir, "app-update.json"), append(b, '\n'), 0600)
}
func fatal(dir, msg string, beta bool) {
	write(dir, status{State: "error", Message: "Обновление не выполнено", CurrentVersion: installed(), Error: msg, UpdatedAt: time.Now().Unix(), Beta: beta})
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
func tail(s string) string {
	if len(s) > 1000 {
		return s[len(s)-1000:]
	}
	return s
}
