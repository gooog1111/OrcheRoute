package serverruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var installedVersionOnce sync.Once
var installedVersion string

func currentPackageVersion() string {
	installedVersionOnce.Do(func() {
		if runtime.GOOS != "linux" {
			return
		}
		output, err := exec.Command("dpkg-query", "-W", "-f=${Version}", "orcheroute").Output()
		if err == nil {
			installedVersion = strings.TrimSpace(string(output))
		}
	})
	return installedVersion
}

func (r *Runtime) RunAppUpdateMonitor(ctx context.Context) {
	if runtime.GOOS != "linux" {
		return
	}
	_, initial := r.getAppUpdate()
	current := initial.(map[string]any)
	timer := time.NewTimer(nextAppUpdateCheck(intValue(current["updated_at"]), time.Now()))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_, value := r.getAppUpdate()
			current := value.(map[string]any)
			if !current["active"].(bool) {
				_, _ = r.startAppUpdate("check", map[string]any{"beta_enabled": current["beta_enabled"]})
			}
			timer.Reset(6 * time.Hour)
		}
	}
}

func nextAppUpdateCheck(updatedAt int, now time.Time) time.Duration {
	const interval = 6 * time.Hour
	if updatedAt <= 0 {
		return 20 * time.Second
	}
	remaining := interval - now.Sub(time.Unix(int64(updatedAt), 0))
	if remaining < 20*time.Second {
		return 20 * time.Second
	}
	return remaining
}

func (r *Runtime) getAppUpdate() (int, any) {
	v := map[string]any{"state": "idle", "message": "Обновления не проверялись", "active": false, "updated_at": 0, "supported": runtime.GOOS == "linux", "beta_enabled": false}
	_ = readJSON(filepath.Join(r.Config.StateDirectory, "app-update.json"), &v)
	if version := currentPackageVersion(); version != "" {
		v["current_version"] = version
		v["current_prerelease"] = strings.Contains(version, "-")
	}
	v["active"] = containsString([]string{"checking", "downloading", "installing"}, stringValue(v["state"]))
	return 200, v
}

func (r *Runtime) saveAppUpdateChannel(body map[string]any) (int, any) {
	enabled, ok := body["beta_enabled"].(bool)
	if !ok {
		return 400, map[string]any{"error": "invalid_update_channel"}
	}
	_, value := r.getAppUpdate()
	current := value.(map[string]any)
	if current["active"] == true {
		return 409, map[string]any{"error": "app_update_in_progress"}
	}
	current["beta_enabled"] = enabled
	current["state"] = "idle"
	current["message"] = map[bool]string{true: "Выбран канал Beta", false: "Выбран канал Stable"}[enabled]
	current["active"] = false
	current["updated_at"] = time.Now().Unix()
	delete(current, "latest_version")
	delete(current, "update_available")
	delete(current, "error")
	if err := atomicJSON(filepath.Join(r.Config.StateDirectory, "app-update.json"), current); err != nil {
		return backendError(err)
	}
	return 200, current
}

func (r *Runtime) startAppUpdate(action string, body map[string]any) (int, any) {
	if runtime.GOOS != "linux" {
		return 409, map[string]any{"error": "deb_update_unsupported"}
	}
	_, current := r.getAppUpdate()
	if current.(map[string]any)["active"] == true {
		return 409, map[string]any{"error": "app_update_in_progress"}
	}
	args := []string{"--action", action, "--state-dir", r.Config.StateDirectory}
	if enabled, _ := body["beta_enabled"].(bool); enabled {
		args = append(args, "--beta")
	}
	beta, _ := body["beta_enabled"].(bool)
	if err := atomicJSON(filepath.Join(r.Config.StateDirectory, "app-update.json"), map[string]any{"state": "checking", "message": "Запускаем проверку обновления", "active": true, "beta_enabled": beta, "updated_at": time.Now().Unix()}); err != nil {
		return backendError(err)
	}
	cmd := exec.Command(r.Config.SelfUpdateBinary, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return backendError(err)
	}
	go cmd.Wait()
	return 202, map[string]any{"accepted": true}
}
