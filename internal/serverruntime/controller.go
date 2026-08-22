package serverruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gooog1111/orcheroute/internal/controller"
	mobileconnectivity "github.com/gooog1111/orcheroute/internal/mobile/connectivity"
	"github.com/gooog1111/orcheroute/internal/serverstate"
	"github.com/gooog1111/orcheroute/internal/whitelist"
)

func (runtime *Runtime) RunController(ctx context.Context) {
	ticker := time.NewTicker(runtime.Config.ControllerEvery)
	defer ticker.Stop()
	runtime.controllerCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime.controllerCycle(ctx)
		}
	}
}

func (runtime *Runtime) controllerCycle(ctx context.Context) {
	cycleContext, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	previous := controller.State{}
	statePath := filepath.Join(runtime.Config.StateDirectory, "controller-state.json")
	var rawPrevious map[string]any
	_ = readJSON(statePath, &rawPrevious)
	if _, migrated := rawPrevious["wan_available"]; migrated {
		payload, _ := json.Marshal(rawPrevious)
		_ = json.Unmarshal(payload, &previous)
	}
	controlValue, err := runtime.Store.Control(cycleContext)
	if err != nil {
		runtime.recordControllerError(err)
		return
	}
	physical := runtime.connectivitySnapshot()
	if controlValue.Enabled && (physical.State == mobileconnectivity.Allowlist || physical.State == mobileconnectivity.Offline) {
		runtime.restrictedNetworkCycle(cycleContext, physical.State)
		return
	}
	if controlValue.Enabled && physical.State != mobileconnectivity.Normal {
		return
	}
	nodes, _, err := runtime.liveNodes(cycleContext)
	transportAvailable := err == nil
	if err != nil {
		if controlValue.Enabled && physical.State == mobileconnectivity.Normal {
			if startErr := platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, true); startErr != nil {
				runtime.recordControllerError(startErr)
				return
			}
			_ = runtime.Store.SetSnapshot(context.Background(), map[string]any{"status": "starting", "mode": controlValue.Mode,
				"active": "", "active_pool": "", "last_cycle": time.Now().Unix(), "wan_available": true})
			return
		}
		// The direct connection and disabled state do not depend on Mihomo.
		// Keep observing the host while the transport is stopped or has not yet
		// produced a configuration; pools will simply remain empty meanwhile.
		nodes = []PublicNode{}
	}
	if physical.State == mobileconnectivity.Normal {
		derived := runtime.whitelistState()
		if derived.SelectedNode != "" || derived.PendingNode != "" {
			_, _ = runtime.whitelistTransition(whitelist.Command{Operation: "deactivate"})
		}
	}
	active, activePool := "", ""
	controllerNodes := make([]controller.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Pool == whitelist.Pool {
			continue
		}
		delay := 0
		if node.Delay != nil {
			delay = *node.Delay
		}
		controllerNodes = append(controllerNodes, controller.Node{Name: node.FullName, Pool: node.Pool, Alive: node.Alive, Delay: delay, SpeedMbps: node.SpeedMbps, StabilityRatio: node.StabilityRatio, HealthSuccesses: node.HealthSuccesses, HealthFailures: node.HealthFailures})
		if node.Selected {
			active, activePool = node.FullName, node.Pool
		}
	}
	wan, activeOK := physical.State == mobileconnectivity.Normal, false
	if controlValue.Enabled && transportAvailable {
		activeOK = runtime.activeAvailable(cycleContext)
	}
	manualNode := ""
	if controlValue.ManualNode != nil {
		manualNode = *controlValue.ManualNode
	}
	observation := controller.Observation{Now: time.Now().Unix(), WAN: wan, ActiveOK: activeOK, Active: active, ActivePool: activePool, Nodes: controllerNodes, Control: controller.Control{Enabled: controlValue.Enabled, Mode: controlValue.Mode, ManualNode: manualNode, ManualUntil: controlValue.ManualUntil}}
	state, decision := controller.Step(previous, observation, controller.DefaultPolicy())
	if !transportAvailable && !controlValue.Enabled && decision.Action == "select" {
		decision = controller.Decision{Action: "keep", Reason: "service_disabled_transport_stopped"}
	}
	if err := runtime.executeDecision(cycleContext, decision); err != nil {
		runtime.recordControllerError(err)
		return
	}
	if err := atomicJSON(filepath.Join(runtime.Config.StateDirectory, "controller-state.json"), state); err != nil {
		runtime.recordControllerError(err)
		return
	}
	stateMap := map[string]any{}
	payload, _ := json.Marshal(state)
	_ = json.Unmarshal(payload, &stateMap)
	_ = runtime.Store.SetSnapshot(context.Background(), stateMap)
	runtime.mu.Lock()
	changed := runtime.lastDecision != decision
	runtime.lastDecision, runtime.lastObservation = decision, observation
	runtime.mu.Unlock()
	if changed {
		_ = runtime.Store.AddEvent(context.Background(), serverstate.EventInput{EventType: "controller_decision", Severity: "info", Pool: stringPointer(decision.Pool), Reason: stringPointer(decision.Reason), Details: map[string]any{"action": decision.Action, "target": decision.Target, "observed_active": active, "wan_available": wan, "active_ok": activeOK}})
	}
}

func (runtime *Runtime) restrictedNetworkCycle(ctx context.Context, mode mobileconnectivity.State) {
	control, err := runtime.Store.Control(ctx)
	if err != nil || !control.Enabled {
		return
	}
	now := time.Now().Unix()
	if mode == mobileconnectivity.Offline {
		_ = platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, false)
		runtime.setRestrictedSnapshot("internet_down", "", "", now)
		return
	}
	state := runtime.whitelistState()
	if state.ScanActive {
		runtime.setRestrictedSnapshot("whitelist_scanning", state.SelectedNode, whitelist.Pool, now)
		return
	}
	if len(state.Nodes) == 0 {
		operation := map[string]any{}
		retryDue := true
		if readJSON(filepath.Join(runtime.Config.StateDirectory, "update-operation.json"), &operation) == nil {
			retryDue = now-int64(intValue(operation["updated_at"])) >= 300
		}
		if retryDue {
			_, _ = runtime.startWhitelistScan(nil)
			runtime.setRestrictedSnapshot("whitelist_scanning", "", whitelist.Pool, now)
		} else {
			runtime.setRestrictedSnapshot("whitelist_unavailable", "", whitelist.Pool, now)
		}
		return
	}
	nodes, _, liveErr := runtime.liveNodes(ctx)
	if liveErr != nil {
		if err := platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, true); err != nil {
			runtime.recordControllerError(err)
			return
		}
		if err := runtime.selectWhitelistCandidate(state); err != nil {
			runtime.setRestrictedSnapshot("whitelist_connecting", "", whitelist.Pool, now)
			return
		}
		state = runtime.whitelistState()
		active := ""
		for _, node := range state.Nodes {
			if node.ID == state.SelectedNode {
				active = stringValue(node.Proxy["name"])
				break
			}
		}
		runtime.setRestrictedSnapshot("whitelist_connecting", active, whitelist.Pool, now)
		return
	}
	activeName, activeID, activeOK := "", "", false
	for _, node := range nodes {
		if node.Pool == whitelist.Pool && node.Selected {
			activeName, activeID = node.FullName, node.ID
			break
		}
	}
	if activeName != "" {
		activeOK = runtime.activeAvailable(ctx)
	}
	if !activeOK {
		if activeID != "" {
			failed, _ := runtime.whitelistTransition(whitelist.Command{Operation: "fail", NodeID: activeID})
			state = failed.State
		}
		if len(state.Nodes) == 0 {
			_ = platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, false)
			_, _ = runtime.startWhitelistScan(nil)
			runtime.setRestrictedSnapshot("whitelist_scanning", "", whitelist.Pool, now)
			return
		}
		if err := platformSetTransportEnabled(context.Background(), runtime.Config.CoreService, true); err != nil {
			runtime.recordControllerError(err)
			return
		}
		if err := runtime.selectWhitelistCandidate(state); err != nil {
			runtime.setRestrictedSnapshot("whitelist_connecting", "", whitelist.Pool, now)
			return
		}
		state = runtime.whitelistState()
		if selected := state.SelectedNode; selected != "" {
			for _, node := range state.Nodes {
				if node.ID == selected {
					activeName = stringValue(node.Proxy["name"])
					break
				}
			}
		}
	}
	runtime.setRestrictedSnapshot("proxy_ok", activeName, whitelist.Pool, now)
}

func (runtime *Runtime) setRestrictedSnapshot(status, active, pool string, now int64) {
	state := map[string]any{"status": status, "mode": "auto", "active": active, "active_pool": pool,
		"failure_streak": 0, "last_cycle": now, "wan_available": status != "internet_down"}
	_ = runtime.Store.SetSnapshot(context.Background(), state)
	runtime.mu.Lock()
	runtime.lastDecision = controller.Decision{Action: "keep", Pool: pool, Reason: status}
	runtime.lastObservation = controller.Observation{Now: now, WAN: status != "internet_down", Active: active, ActivePool: pool}
	runtime.mu.Unlock()
}

func (runtime *Runtime) executeDecision(ctx context.Context, decision controller.Decision) error {
	switch decision.Action {
	case "keep", "freeze":
		return nil
	case "select":
		if decision.Target == "" {
			return fmt.Errorf("controller_selected_empty_target")
		}
		_, err := runtime.mihomo(ctx, http.MethodPut, "/proxies/ACTIVE", map[string]any{"name": decision.Target})
		return err
	case "refresh":
		operation := filepath.Join(runtime.Config.StateDirectory, "update-operation.json")
		var current map[string]any
		if readJSON(operation, &current) == nil && stringValue(current["status"]) == "running" {
			return nil
		}
		arguments := []string{"--state-dir", runtime.Config.StateDirectory, "--output-state-dir", runtime.Config.StateDirectory, "--operation-path", operation, "--force", "--cached-only"}
		if decision.Pool != "" {
			arguments = append(arguments, "--group", decision.Pool)
		}
		command := exec.Command(runtime.Config.UpdateBinary, arguments...)
		if err := command.Start(); err != nil {
			return err
		}
		go command.Wait()
		return nil
	default:
		return fmt.Errorf("unknown_controller_action:%s", decision.Action)
	}
}

func (runtime *Runtime) recordControllerError(err error) {
	payload := map[string]any{"status": "controller_error", "last_error": fmt.Sprintf("%T", err), "message": err.Error(), "updated_at": time.Now().Unix()}
	_ = atomicJSON(filepath.Join(runtime.Config.StateDirectory, "controller-error.json"), payload)
	_ = runtime.Store.AddEvent(context.Background(), serverstate.EventInput{EventType: "controller_error", Severity: "error", Reason: stringPointer(err.Error())})
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (runtime *Runtime) Decision() (controller.Decision, controller.Observation) {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.lastDecision, runtime.lastObservation
}
