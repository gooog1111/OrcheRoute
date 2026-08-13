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
	}
	return result, nil
}
