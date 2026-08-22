package updater

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

type FileProviderStore struct {
	ProvidersDirectory string
	ReportsDirectory   string
}

func (store FileProviderStore) Exists(pool string) bool {
	_, err := os.Stat(filepath.Join(store.ProvidersDirectory, pool+".json"))
	return err == nil
}

func (store FileProviderStore) WriteReport(pool string, report qualification.Report) error {
	if store.ReportsDirectory == "" {
		return nil
	}
	return atomicJSON(filepath.Join(store.ReportsDirectory, pool+".json"), report)
}

func (store FileProviderStore) Write(pool string, result qualification.Result, sources map[string]subscriptions.SourceIdentity) error {
	if err := atomicJSON(filepath.Join(store.ProvidersDirectory, pool+".json"), map[string]any{"proxies": result.Proxies}); err != nil {
		return err
	}
	nodes := map[string]any{}
	for name, source := range sources {
		value := map[string]any{"id": source.ID, "name": source.Name}
		if metrics, ok := result.Metrics[name]; ok {
			value["delay_ms"] = metrics.DelayMS
			value["speed_mbps"] = metrics.SpeedMbps
			value["stability_ratio"] = metrics.StabilityRatio
		}
		nodes[name] = value
	}
	if err := atomicJSON(filepath.Join(store.ProvidersDirectory, pool+".sources.json"), map[string]any{"nodes": nodes}); err != nil {
		return err
	}
	return nil
}

func atomicJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atomic-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
