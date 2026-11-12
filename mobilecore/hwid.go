package mobilecore

import (
	"encoding/json"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func DeviceHWID(seed string) string {
	value, err := subscriptions.DeviceHWID(seed)
	if err != nil {
		return ""
	}
	return value
}

func ValidateHWIDSettings(input string) string {
	var settings subscriptions.HWIDSettings
	if json.Unmarshal([]byte(input), &settings) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_hwid_settings"}})
	}
	settings.Current = ""
	value, err := subscriptions.NormalizeHWIDSettings(settings)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": value})
}

func ValidateHWIDSelection(mode, value string) string {
	if err := subscriptions.ValidateHWIDSelection(mode, value); err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true})
}
