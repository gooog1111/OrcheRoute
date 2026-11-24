//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gooog1111/orcheroute/internal/mihomo"
)

func migrateCallIngress(ctx context.Context, current options) (bool, error) {
	original, err := os.ReadFile(current.configPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var config map[string]any
	if err := json.Unmarshal(original, &config); err != nil {
		return false, err
	}
	changed, err := mihomo.ExcludeCallServerIngress(config)
	if err != nil || !changed {
		return false, err
	}
	file, err := os.CreateTemp(filepath.Dir(current.configPath), ".call-ingress-*.json")
	if err != nil {
		return false, err
	}
	candidate := file.Name()
	_ = file.Close()
	defer os.Remove(candidate)
	if err := writeJSON(candidate, config); err != nil {
		return false, err
	}
	validation, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(validation, current.mihomoBinary, "-t", "-d", current.stateDirectory, "-f", candidate).CombinedOutput(); err != nil {
		return false, fmt.Errorf("call_ingress_config_invalid: %w: %s", err, tail(string(output), 1000))
	}
	digest := sha256.Sum256(original)
	backup := fmt.Sprintf("%s.before-call-ingress-%x", current.configPath, digest[:6])
	saved, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		payload, readErr := os.ReadFile(backup)
		if readErr != nil || !bytes.Equal(payload, original) {
			return false, fmt.Errorf("call_ingress_backup_conflict")
		}
	} else if err != nil {
		return false, err
	} else {
		_, writeErr := saved.Write(original)
		if writeErr == nil {
			writeErr = saved.Sync()
		}
		closeErr := saved.Close()
		if writeErr != nil {
			return false, writeErr
		}
		if closeErr != nil {
			return false, closeErr
		}
	}
	latest, err := os.ReadFile(current.configPath)
	if err != nil || !bytes.Equal(latest, original) {
		return false, fmt.Errorf("call_ingress_config_changed_during_migration")
	}
	if err := os.Rename(candidate, current.configPath); err != nil {
		return false, err
	}
	return true, nil
}
