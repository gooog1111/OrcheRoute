package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type desktopConfig struct {
	APIURL     string `json:"api_url"`
	APIToken   string `json:"api_token"`
	RuntimeEnv string `json:"runtime_env"`
}

func loadConfig(apiURLFlag, runtimeEnvFlag string) (desktopConfig, error) {
	config := desktopConfig{APIURL: "http://127.0.0.1:19100"}
	if directory, err := os.UserConfigDir(); err == nil {
		path := filepath.Join(directory, "OrcheRoute", "desktop.json")
		if payload, readErr := os.ReadFile(path); readErr == nil {
			if err := json.Unmarshal(payload, &config); err != nil {
				return config, err
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv("ORCHEROUTE_API_URL")); value != "" {
		config.APIURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ORCHEROUTE_RUNTIME_ENV")); value != "" {
		config.RuntimeEnv = value
	}
	if value := strings.TrimSpace(os.Getenv("ORCHEROUTE_API_TOKEN")); value != "" {
		config.APIToken = value
	}
	if strings.TrimSpace(apiURLFlag) != "" {
		config.APIURL = strings.TrimSpace(apiURLFlag)
	}
	if strings.TrimSpace(runtimeEnvFlag) != "" {
		config.RuntimeEnv = strings.TrimSpace(runtimeEnvFlag)
	}
	if config.APIToken == "" && config.RuntimeEnv != "" {
		values, err := readEnvironment(config.RuntimeEnv)
		if err == nil {
			config.APIToken = values["api_token"]
		} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			return config, err
		}
	}
	return config, nil
}

func defaultRuntimeEnv() string {
	switch runtime.GOOS {
	case "windows":
		if root := os.Getenv("ProgramData"); root != "" {
			return filepath.Join(root, "OrcheRoute", "runtime.env")
		}
		return `C:\ProgramData\OrcheRoute\runtime.env`
	default:
		return "/etc/orcheroute/runtime.env"
	}
}

func readEnvironment(path string) (map[string]string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, raw := range strings.Split(strings.TrimPrefix(string(payload), "\ufeff"), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		result[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return result, nil
}
