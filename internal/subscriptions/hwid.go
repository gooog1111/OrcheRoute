package subscriptions

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type SavedHWID struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
}
type HWIDSettings struct {
	Current string      `json:"current"`
	Saved   []SavedHWID `json:"saved"`
}

var hwidPattern = regexp.MustCompile(`^[A-Za-z0-9=+/_:.-]{1,256}$`)

func ValidHWID(value string) bool { return hwidPattern.MatchString(value) }

func DeviceHWID(seed string) (string, error) {
	if seed == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		return hex.EncodeToString(raw), nil
	}
	digest := sha256.Sum256([]byte("OrcheRoute/HWID/v1:" + seed))
	return hex.EncodeToString(digest[:]), nil
}

func NormalizeHWIDSettings(settings HWIDSettings) (HWIDSettings, error) {
	if len(settings.Saved) > 64 {
		return HWIDSettings{}, fmt.Errorf("too_many_saved_hwids")
	}
	result := HWIDSettings{Current: settings.Current, Saved: []SavedHWID{}}
	if result.Current != "" && !ValidHWID(result.Current) {
		return result, fmt.Errorf("invalid_hwid")
	}
	seen := map[string]bool{}
	for _, item := range settings.Saved {
		item.Name = strings.TrimSpace(item.Name)
		item.Value = strings.TrimSpace(item.Value)
		if item.Name == "" || len([]rune(item.Name)) > 64 || !ValidHWID(item.Value) {
			return result, fmt.Errorf("invalid_saved_hwid")
		}
		if seen[item.Value] {
			return result, fmt.Errorf("duplicate_hwid")
		}
		seen[item.Value] = true
		digest := sha256.Sum256([]byte(item.Value))
		item.ID = hex.EncodeToString(digest[:12])
		result.Saved = append(result.Saved, item)
	}
	return result, nil
}

func ReadHWIDSettings(path string) (HWIDSettings, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return HWIDSettings{}, err
	}
	var settings HWIDSettings
	if json.Unmarshal(payload, &settings) != nil {
		return settings, fmt.Errorf("invalid_hwid_settings")
	}
	return NormalizeHWIDSettings(settings)
}

func ValidateHWIDSelection(mode, value string) error {
	value = strings.TrimSpace(value)
	if value != "" && !ValidHWID(value) {
		return fmt.Errorf("invalid_hwid")
	}
	switch mode {
	case "", "none", "current":
		return nil
	case "custom":
		if ValidHWID(strings.TrimSpace(value)) {
			return nil
		}
	}
	return fmt.Errorf("invalid_hwid_selection")
}

func SelectedHWID(item Subscription, current string) (string, error) {
	if err := ValidateHWIDSelection(item.HWIDMode, item.HWID); err != nil {
		return "", err
	}
	switch item.HWIDMode {
	case "current":
		if !ValidHWID(current) {
			return "", fmt.Errorf("current_hwid_unavailable")
		}
		return current, nil
	case "custom":
		return strings.TrimSpace(item.HWID), nil
	}
	return "", nil
}
