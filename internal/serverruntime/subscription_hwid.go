package serverruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func (runtime *Runtime) loadHWIDSettings() (subscriptions.HWIDSettings, error) {
	runtime.hwidMu.Lock()
	defer runtime.hwidMu.Unlock()
	return runtime.loadHWIDSettingsLocked()
}

func (runtime *Runtime) loadHWIDSettingsLocked() (subscriptions.HWIDSettings, error) {
	path := filepath.Join(runtime.Config.StateDirectory, "subscription-hwid.json")
	settings, err := subscriptions.ReadHWIDSettings(path)
	if err == nil {
		return settings, nil
	}
	if !os.IsNotExist(err) {
		return settings, err
	}
	seed, _ := os.ReadFile("/etc/machine-id")
	settings.Current, err = subscriptions.DeviceHWID(strings.TrimSpace(string(seed)))
	if err != nil {
		return settings, err
	}
	settings.Saved = []subscriptions.SavedHWID{}
	return settings, atomicJSON(path, settings)
}

func (runtime *Runtime) saveHWIDSettings(body map[string]any) (int, any) {
	payload, err := json.Marshal(body)
	if err != nil {
		return validationError(err)
	}
	var settings subscriptions.HWIDSettings
	if json.Unmarshal(payload, &settings) != nil {
		return 400, map[string]any{"error": "invalid_hwid_settings"}
	}
	settings.Current = ""
	settings, err = subscriptions.NormalizeHWIDSettings(settings)
	if err != nil {
		return 400, map[string]any{"error": err.Error()}
	}
	runtime.hwidMu.Lock()
	defer runtime.hwidMu.Unlock()
	old, err := runtime.loadHWIDSettingsLocked()
	if err != nil {
		return backendError(err)
	}
	settings.Current = old.Current
	if err = atomicJSON(filepath.Join(runtime.Config.StateDirectory, "subscription-hwid.json"), settings); err != nil {
		return backendError(err)
	}
	return 200, settings
}
