package serverruntime

import (
	"path/filepath"

	"github.com/gooog1111/orcheroute/internal/whitelist"
)

func (runtime *Runtime) whitelistState() whitelist.State {
	state := whitelist.State{}
	_ = readJSON(filepath.Join(runtime.Config.StateDirectory, "whitelist-state.json"), &state)
	return state
}

func (runtime *Runtime) whitelistTransition(command whitelist.Command) (whitelist.Result, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	result, err := whitelist.Transition(runtime.whitelistState(), command)
	if err != nil {
		return whitelist.Result{}, err
	}
	if result.Changed {
		if err := atomicJSON(filepath.Join(runtime.Config.StateDirectory, "whitelist-state.json"), result.State); err != nil {
			return whitelist.Result{}, err
		}
		if err := runtime.writeWhitelistProvider(result.State); err != nil {
			return whitelist.Result{}, err
		}
	}
	return result, nil
}

func (runtime *Runtime) writeWhitelistProvider(state whitelist.State) error {
	proxies := make([]map[string]any, 0, len(state.Nodes))
	metadata := map[string]any{}
	for _, node := range state.Nodes {
		if !node.Alive || node.Proxy == nil {
			continue
		}
		proxies = append(proxies, node.Proxy)
		name, _ := node.Proxy["name"].(string)
		if name != "" {
			metadata[name] = map[string]any{"id": node.SourceID, "name": node.SourceName, "delay_ms": node.DelayMS,
				"speed_mbps": node.SpeedMbps, "stability_ratio": node.StabilityRatio,
				"health_successes": node.HealthSuccesses, "health_failures": node.HealthFailures}
		}
	}
	providers := filepath.Join(runtime.Config.StateDirectory, "providers")
	if err := atomicJSON(filepath.Join(providers, "whitelist.json"), map[string]any{"proxies": proxies}); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(providers, "whitelist.sources.json"), map[string]any{"nodes": metadata})
}
