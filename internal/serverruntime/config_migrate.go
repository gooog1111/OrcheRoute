package serverruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (runtime *Runtime) ensureWhitelistConfig(ctx context.Context) error {
	configPath := filepath.Join(runtime.Config.ConfigDirectory, "config.json")
	config := map[string]any{}
	if err := readJSON(configPath, &config); err != nil {
		return err
	}
	migrated, changed, err := addWhitelistProvider(config)
	if err != nil || !changed {
		return err
	}
	candidate := configPath + ".whitelist-new"
	if err := atomicJSON(candidate, migrated); err != nil {
		return err
	}
	defer os.Remove(candidate)
	if output, err := exec.CommandContext(ctx, runtime.Config.MihomoBinary, "-t", "-d", runtime.Config.StateDirectory, "-f", candidate).CombinedOutput(); err != nil {
		return fmt.Errorf("whitelist_config_validation_failed: %s", truncate(string(output), 1000))
	}
	return atomicJSON(configPath, migrated)
}

func addWhitelistProvider(config map[string]any) (map[string]any, bool, error) {
	payload, err := json.Marshal(config)
	if err != nil {
		return nil, false, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, false, err
	}
	providers, ok := result["proxy-providers"].(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("proxy_providers_missing")
	}
	if _, exists := providers["whitelist"]; exists {
		return result, false, nil
	}
	emergency, ok := providers["emergency"].(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("emergency_provider_missing")
	}
	providerPayload, _ := json.Marshal(emergency)
	whitelistProvider := map[string]any{}
	_ = json.Unmarshal(providerPayload, &whitelistProvider)
	if path, ok := whitelistProvider["path"].(string); ok {
		whitelistProvider["path"] = strings.Replace(path, "emergency.json", "whitelist.json", 1)
	}
	providers["whitelist"] = whitelistProvider
	groups, ok := result["proxy-groups"].([]any)
	if !ok {
		return nil, false, fmt.Errorf("proxy_groups_missing")
	}
	activeFound := false
	for _, raw := range groups {
		group, _ := raw.(map[string]any)
		if stringValue(group["name"]) != "ACTIVE" {
			continue
		}
		uses, _ := group["use"].([]any)
		uses = append(uses, "whitelist")
		group["use"] = uses
		activeFound = true
	}
	if !activeFound {
		return nil, false, fmt.Errorf("active_group_missing")
	}
	groups = append(groups, map[string]any{"name": "PROBE-WHITELIST", "type": "url-test", "use": []any{"whitelist"},
		"url": "https://www.gstatic.com/generate_204", "interval": float64(60), "timeout": float64(5000), "tolerance": float64(100),
		"lazy": false, "expected-status": float64(204)})
	result["proxy-groups"] = groups
	return result, true, nil
}
