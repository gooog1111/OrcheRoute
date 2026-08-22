package serverruntime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gooog1111/orcheroute/internal/mobile/connectivity"
	"github.com/gooog1111/orcheroute/internal/updater"
	"github.com/gooog1111/orcheroute/internal/whitelist"
)

func (runtime *Runtime) startWhitelistScan(ids []string) (int, any) {
	physical := runtime.connectivitySnapshot()
	if physical.State != connectivity.Allowlist {
		return http.StatusConflict, map[string]any{"error": "allowlist_not_detected", "network_mode": physical.State}
	}
	operation := filepath.Join(runtime.Config.StateDirectory, "update-operation.json")
	var current map[string]any
	if readJSON(operation, &current) == nil && containsString([]string{"running", "cancelling"}, stringValue(current["status"])) && runtime.subscriptionUpdateProcessActive() {
		return http.StatusConflict, map[string]any{"error": "subscription_update_in_progress"}
	}
	if _, err := runtime.whitelistTransition(whitelist.Command{Operation: "begin"}); err != nil {
		return backendError(err)
	}
	if err := platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, false); err != nil {
		_, _ = runtime.whitelistTransition(whitelist.Command{Operation: "complete"})
		return backendError(err)
	}
	cancelPath := filepath.Join(runtime.Config.StateDirectory, "update-cancel.request")
	resultPath := filepath.Join(runtime.Config.StateDirectory, "whitelist-scan-result.json")
	_ = os.Remove(cancelPath)
	_ = os.Remove(resultPath)
	_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": "running", "phase": "whitelist", "message": "Формируем список серверов для белых списков", "updated_at": time.Now().Unix()})
	go runtime.runWhitelistScan(ids, operation, cancelPath, resultPath)
	return http.StatusAccepted, map[string]any{"accepted": true, "mode": "allowlist", "system_mutated": true}
}

func (runtime *Runtime) runWhitelistScan(ids []string, operation, cancelPath, resultPath string) {
	arguments := []string{"--state-dir", runtime.Config.ProductionState, "--output-state-dir", runtime.Config.StateDirectory,
		"--operation-path", operation, "--cancel-path", cancelPath, "--whitelist-result", resultPath,
		"--network-profile", filepath.Join(runtime.Config.StateDirectory, "network-active.json"),
		"--policy", filepath.Join(runtime.Config.StateDirectory, "qualification-policy.json"), "--mihomo", runtime.Config.MihomoBinary}
	for _, id := range ids {
		arguments = append(arguments, "--subscription-id", id)
	}
	command := exec.Command(runtime.Config.UpdateBinary, arguments...)
	output, commandErr := command.CombinedOutput()
	result := updater.WhitelistResult{}
	readErr := readJSON(resultPath, &result)
	if readErr == nil {
		_ = runtime.applyWhitelistResult(result)
	}
	transition, transitionErr := runtime.whitelistTransition(whitelist.Command{Operation: "complete"})
	cancelled := false
	if _, err := os.Stat(cancelPath); err == nil {
		cancelled = true
		_ = os.Remove(cancelPath)
	}
	if transitionErr == nil && len(transition.State.Nodes) > 0 {
		if control, err := runtime.Store.Control(context.Background()); err == nil && control.Enabled {
			if err := platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, true); err == nil {
				_ = runtime.selectWhitelistCandidate(transition.State)
			}
		}
	}
	status, message := "success", fmt.Sprintf("Список для белых списков готов: %d серверов", len(transition.State.Nodes))
	if cancelled {
		status, message = "cancelled", "Проверка остановлена; завершённые результаты сохранены"
	} else if commandErr != nil || readErr != nil || transitionErr != nil {
		status, message = "error", "Не удалось сформировать список для белых списков"
	} else if len(transition.State.Nodes) == 0 {
		status, message = "warning", "Доступных серверов для белых списков нет"
	}
	_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": status, "phase": "complete", "message": message,
		"failures": result.Failures, "output": truncate(string(output), 4000), "updated_at": time.Now().Unix()})
}

func (runtime *Runtime) applyWhitelistResult(result updater.WhitelistResult) error {
	for _, sourceID := range result.CompletedSources {
		if _, err := runtime.whitelistTransition(whitelist.Command{Operation: "replace_source", SourceID: sourceID, Nodes: result.Sources[sourceID]}); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) selectWhitelistCandidate(state whitelist.State) error {
	transition, err := runtime.whitelistTransition(whitelist.Command{Operation: "request"})
	if err != nil || transition.Candidate == nil {
		return err
	}
	name, _ := transition.Candidate.Proxy["name"].(string)
	if name == "" {
		return fmt.Errorf("whitelist_candidate_has_no_name")
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, reloadErr := runtime.mihomo(ctx, http.MethodPut, "/providers/proxies/whitelist", nil)
		if reloadErr == nil {
			_, selectErr := runtime.mihomo(ctx, http.MethodPut, "/proxies/ACTIVE", map[string]any{"name": name})
			cancel()
			if selectErr == nil {
				_, _ = runtime.whitelistTransition(whitelist.Command{Operation: "confirm", NodeID: transition.Candidate.ID})
				return nil
			}
		} else {
			cancel()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("mihomo_not_ready_for_whitelist")
}
