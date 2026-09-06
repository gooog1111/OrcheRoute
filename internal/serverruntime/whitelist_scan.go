package serverruntime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"time"

	"github.com/gooog1111/orcheroute/internal/core/connectivity"
	"github.com/gooog1111/orcheroute/internal/core/whitelist"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"github.com/gooog1111/orcheroute/internal/updater"
)

func (runtime *Runtime) startWhitelistScan(ids []string) (int, any) {
	physical := runtime.connectivitySnapshot()
	if physical.State != connectivity.Allowlist {
		return http.StatusConflict, map[string]any{"error": "allowlist_not_detected", "network_mode": physical.State}
	}
	configContext, cancelConfig := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelConfig()
	if err := runtime.ensureWhitelistConfig(configContext); err != nil {
		return backendError(err)
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
	_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": "running", "phase": "whitelist", "message": "Формируем список серверов для белых списков", "allowlist_scan": true, "connectivity": "allowlist", "updated_at": time.Now().Unix()})
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
	connected := false
	if transitionErr == nil && len(transition.State.Nodes) > 0 {
		if control, err := runtime.Store.Control(context.Background()); err == nil && control.Enabled {
			if err := platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, true); err == nil {
				connected = runtime.selectWhitelistCandidate(transition.State) == nil
			}
		}
	}
	if connected && len(ids) == 0 && !cancelled && commandErr == nil {
		_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": "running", "phase": "whitelist_stability",
			"message": "Проверяем устойчивость подключения перед обновлением подписок", "allowlist_scan": true, "updated_at": time.Now().Unix()})
		if runtime.awaitWhitelistStability(cancelPath) {
			runtime.refreshWhitelistSubscriptions(operation, cancelPath)
		}
		if _, err := os.Stat(cancelPath); err == nil {
			cancelled = true
			_ = os.Remove(cancelPath)
		}
		transition.State = runtime.whitelistState()
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
		"allowlist_scan": true, "connectivity": "allowlist", "failures": result.Failures, "output": truncate(string(output), 4000), "updated_at": time.Now().Unix()})
}

func (runtime *Runtime) refreshWhitelistSubscriptions(operation, cancelPath string) {
	selected := runtime.whitelistState().SelectedNode
	items, err := runtime.Store.List(context.Background(), false)
	if err != nil {
		return
	}
	cache := subscriptions.FileCache{Directory: filepath.Join(runtime.Config.StateDirectory, "subscription-cache")}
	includeEmergency := true
	policy := map[string]any{}
	if readJSON(filepath.Join(runtime.Config.StateDirectory, "qualification-policy.json"), &policy) == nil {
		if defaults, ok := policy["defaults"].(map[string]any); ok {
			if configured, valid := defaults["allowlist_use_emergency_subscriptions"].(bool); valid {
				includeEmergency = configured
			}
		}
	}
	enabled := make([]subscriptions.Subscription, 0, len(items))
	for _, item := range items {
		if item.Enabled && (includeEmergency || item.GroupName != subscriptions.Emergency) {
			enabled = append(enabled, item)
		}
	}
	internalOperation := filepath.Join(runtime.Config.StateDirectory, "whitelist-followup-operation.json")
	for index, item := range enabled {
		// A manually changed node or physical-network mode needs a new stability
		// window; do not carry a previous connection's approval to another one.
		if runtime.connectivitySnapshot().State != connectivity.Allowlist || runtime.whitelistState().SelectedNode != selected {
			return
		}
		if _, err := os.Stat(cancelPath); err == nil {
			return
		}
		_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": "running", "phase": "whitelist_subscriptions",
			"message": fmt.Sprintf("Обновляем подписку %d/%d · «%s»", index+1, len(enabled), item.Name),
			"current": index, "total": len(enabled), "allowlist_scan": true, "connectivity": "allowlist", "updated_at": time.Now().Unix()})
		before, _ := cache.Read(context.Background(), item.ID)
		fetchArgs := runtime.updateArguments(internalOperation, cancelPath, "--force", "--fetch-only", "--subscription-id", item.ID)
		if output, fetchErr := exec.Command(runtime.Config.UpdateBinary, fetchArgs...).CombinedOutput(); fetchErr != nil {
			_ = output
			continue
		}
		after, _ := cache.Read(context.Background(), item.ID)
		if reflect.DeepEqual(before, after) {
			continue
		}
		resultPath := filepath.Join(runtime.Config.StateDirectory, "whitelist-followup-"+item.ID+".json")
		_ = os.Remove(resultPath)
		scanArgs := runtime.updateArguments(internalOperation, cancelPath, "--whitelist-result", resultPath, "--subscription-id", item.ID)
		_, scanErr := exec.Command(runtime.Config.UpdateBinary, scanArgs...).CombinedOutput()
		partial := updater.WhitelistResult{}
		if readJSON(resultPath, &partial) == nil {
			_ = runtime.applyWhitelistResult(partial)
		}
		_ = os.Remove(resultPath)
		if scanErr != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = runtime.mihomo(ctx, http.MethodPut, "/providers/proxies/whitelist", nil)
		cancel()
		state := runtime.whitelistState()
		if len(state.Nodes) == 0 {
			_ = platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, false)
			return
		}
		if state.SelectedNode == "" {
			_ = runtime.selectWhitelistCandidate(state)
		}
	}
	_ = os.Remove(internalOperation)
}

func (runtime *Runtime) updateArguments(operation, cancelPath string, extra ...string) []string {
	arguments := []string{"--state-dir", runtime.Config.ProductionState, "--output-state-dir", runtime.Config.StateDirectory,
		"--operation-path", operation, "--cancel-path", cancelPath,
		"--network-profile", filepath.Join(runtime.Config.StateDirectory, "network-active.json"),
		"--policy", filepath.Join(runtime.Config.StateDirectory, "qualification-policy.json"), "--mihomo", runtime.Config.MihomoBinary}
	return append(arguments, extra...)
}

func (runtime *Runtime) applyWhitelistResult(result updater.WhitelistResult) error {
	for _, sourceID := range result.ExcludedSources {
		if _, err := runtime.whitelistTransition(whitelist.Command{Operation: "remove_source", SourceID: sourceID}); err != nil {
			return err
		}
	}
	for _, sourceID := range result.CompletedSources {
		if _, err := runtime.whitelistTransition(whitelist.Command{Operation: "replace_source", SourceID: sourceID, Nodes: result.Sources[sourceID]}); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) selectWhitelistCandidate(state whitelist.State) error {
	transition, err := runtime.whitelistTransition(whitelist.Command{Operation: "request"})
	if err != nil {
		return err
	}
	if transition.Candidate == nil {
		return fmt.Errorf("whitelist_candidate_unavailable")
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
				return nil
			}
		} else {
			cancel()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("mihomo_not_ready_for_whitelist")
}

func (runtime *Runtime) awaitWhitelistStability(cancelPath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var window connectivity.StabilityWindow
	for {
		if _, err := os.Stat(cancelPath); err == nil {
			return false
		}
		physical := runtime.connectivitySnapshot()
		if physical.State != connectivity.Allowlist || physical.Error != "" {
			return false
		}
		control, err := runtime.Store.Control(ctx)
		if err != nil || !control.Enabled {
			return false
		}
		selected := runtime.whitelistState().SelectedNode
		if selected == "" {
			return false
		}
		probeCtx, stopProbe := context.WithTimeout(ctx, 8*time.Second)
		ok := runtime.activeAvailable(probeCtx)
		stopProbe()
		// Refuse a sample that completed after a concurrent selection change.
		if runtime.whitelistState().SelectedNode != selected {
			ok = false
		}
		after := runtime.connectivitySnapshot()
		if after.State != physical.State || after.DirectInterface != physical.DirectInterface || after.ConfirmedAt != physical.ConfirmedAt || after.Error != "" {
			ok = false
		}
		key := fmt.Sprintf("%s/%s/%d", selected, physical.DirectInterface, physical.ConfirmedAt)
		if window.Observe(time.Now(), key, ok, 30*time.Second, 3) {
			_, err := runtime.whitelistTransition(whitelist.Command{Operation: "confirm", NodeID: selected})
			return err == nil
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
