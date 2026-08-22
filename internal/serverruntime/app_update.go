package serverruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func (r *Runtime) RunAppUpdateMonitor(ctx context.Context) {
	if runtime.GOOS != "linux" {
		return
	}
	timer := time.NewTimer(20 * time.Second)
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

func (r *Runtime) getAppUpdate() (int, any) {
	v := map[string]any{"state": "idle", "message": "Обновления не проверялись", "active": false, "updated_at": 0, "supported": runtime.GOOS == "linux", "beta_enabled": false}
	_ = readJSON(filepath.Join(r.Config.StateDirectory, "app-update.json"), &v)
	v["active"] = containsString([]string{"checking", "downloading", "installing"}, stringValue(v["state"]))
	return 200, v
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
	if err := atomicJSON(filepath.Join(r.Config.StateDirectory, "app-update.json"), map[string]any{"state": "checking", "message": "Запускаем проверку обновления", "active": true, "updated_at": time.Now().Unix()}); err != nil {
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
