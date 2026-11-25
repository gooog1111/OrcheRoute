//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCallIngressConfigMigrationRetainsExactBackup(t *testing.T) {
	dir := t.TempDir()
	current := options{configPath: filepath.Join(dir, "config.json"), stateDirectory: dir, mihomoBinary: filepath.Join(dir, "validator")}
	original := []byte(`{"secret":"keep","rules":["MATCH,DIRECT"],"tun":{"device":"orcheroute0","exclude-interface":["docker0","orchecall0"]}}`)
	if err := os.WriteFile(current.configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current.mihomoBinary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	changed, err := migrateCallIngress(context.Background(), current)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	backups, _ := filepath.Glob(current.configPath + ".before-call-ingress-*")
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	payload, _ := os.ReadFile(backups[0])
	if string(payload) != string(original) {
		t.Fatal("backup differs from original")
	}
	changed, err = migrateCallIngress(context.Background(), current)
	if err != nil || changed {
		t.Fatal("repeated migration changed config")
	}
}

func TestCallIngressConfigValidationFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	current := options{configPath: filepath.Join(dir, "config.json"), stateDirectory: dir, mihomoBinary: filepath.Join(dir, "validator")}
	if changed, err := migrateCallIngress(context.Background(), current); err != nil || changed {
		t.Fatal("missing clean-install config must be skipped")
	}
	original := []byte(`{"tun":{"device":"orcheroute0","exclude-interface":["orchecall0"]}}`)
	_ = os.WriteFile(current.configPath, original, 0600)
	_ = os.WriteFile(current.mihomoBinary, []byte("#!/bin/sh\nexit 1\n"), 0700)
	if _, err := migrateCallIngress(context.Background(), current); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	payload, _ := os.ReadFile(current.configPath)
	if string(payload) != string(original) {
		t.Fatal("working config overwritten")
	}
	backups, _ := filepath.Glob(current.configPath + ".before-call-ingress-*")
	candidates, _ := filepath.Glob(filepath.Join(dir, ".call-ingress-*"))
	if len(backups)+len(candidates) != 0 {
		t.Fatal("invalid candidate/backup left behind")
	}
}
