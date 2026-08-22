package serverruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/gooog1111/orcheroute/internal/controller"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/serverstate"
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
	nodes, _, err := runtime.liveNodes(cycleContext)
	transportAvailable := err == nil
	if err != nil {
		// The direct connection and disabled state do not depend on Mihomo.
		// Keep observing the host while the transport is stopped or has not yet
		// produced a configuration; pools will simply remain empty meanwhile.
		nodes = []PublicNode{}
	}
	active, activePool := "", ""
	controllerNodes := make([]controller.Node, 0, len(nodes))
	for _, node := range nodes {
		delay := 0
		if node.Delay != nil {
			delay = *node.Delay
		}
		controllerNodes = append(controllerNodes, controller.Node{Name: node.FullName, Pool: node.Pool, Alive: node.Alive, Delay: delay, SpeedMbps: node.SpeedMbps, StabilityRatio: node.StabilityRatio, HealthSuccesses: node.HealthSuccesses, HealthFailures: node.HealthFailures})
		if node.Selected {
			active, activePool = node.FullName, node.Pool
		}
	}
	profile := network.Profile{}
	if err := readJSON(filepath.Join(runtime.Config.StateDirectory, "network-active.json"), &profile); err != nil {
		runtime.recordControllerError(err)
		return
	}
	directRole := profile.Roles["direct"]
	wan, activeOK := false, false
	var wait sync.WaitGroup
	wait.Add(1)
	// Binding to the selected egress already keeps this probe off a system-wide
	// VPN. A policy mark is deliberately not used here: on a clean install its
	// routing table does not exist until the first network apply.
	go func() { defer wait.Done(); wan = runtime.directAvailable(cycleContext, directRole.Interface, 0) }()
	if controlValue.Enabled && transportAvailable {
		wait.Add(1)
		go func() { defer wait.Done(); activeOK = runtime.activeAvailable(cycleContext) }()
	}
	wait.Wait()
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
