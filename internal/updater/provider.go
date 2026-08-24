package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gooog1111/orcheroute/internal/noderank"
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
	nodes := providerMetadata(result, sources, nil)
	return store.writeRanked(pool, result.Proxies, nodes)
}

// MergeSource replaces only the checked subscription inside an active pool.
// Other subscriptions remain untouched, including their qualification data.
func (store FileProviderStore) MergeSource(pool, sourceID string, result qualification.Result, sources map[string]subscriptions.SourceIdentity) error {
	proxies := []map[string]any{}
	providerPath := filepath.Join(store.ProvidersDirectory, pool+".json")
	var provider struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := readProviderJSON(providerPath, &provider); err != nil && !os.IsNotExist(err) {
		return err
	}
	var metadataPayload struct {
		Nodes map[string]map[string]any `json:"nodes"`
	}
	metadataPath := filepath.Join(store.ProvidersDirectory, pool+".sources.json")
	if err := readProviderJSON(metadataPath, &metadataPayload); err != nil && !os.IsNotExist(err) {
		return err
	}
	if metadataPayload.Nodes == nil {
		metadataPayload.Nodes = map[string]map[string]any{}
	}
	prefix := strings.ToUpper(strings.TrimSpace(sourceID)) + "-"
	removed := map[string]bool{}
	for _, proxy := range provider.Proxies {
		name, _ := proxy["name"].(string)
		metadata := metadataPayload.Nodes[name]
		if stringMetadata(metadata, "id") == sourceID || (prefix != "-" && strings.HasPrefix(name, prefix)) {
			removed[name] = true
			continue
		}
		proxies = append(proxies, proxy)
	}
	proxies = append(proxies, result.Proxies...)
	metadataPayload.Nodes = providerMetadata(result, sources, metadataPayload.Nodes)
	for name := range removed {
		if _, retained := sources[name]; !retained {
			delete(metadataPayload.Nodes, name)
		}
	}
	return store.writeRanked(pool, proxies, metadataPayload.Nodes)
}

func providerMetadata(result qualification.Result, sources map[string]subscriptions.SourceIdentity, nodes map[string]map[string]any) map[string]map[string]any {
	if nodes == nil {
		nodes = map[string]map[string]any{}
	}
	for name, source := range sources {
		value := map[string]any{"id": source.ID, "name": source.Name}
		if previous := nodes[name]; previous != nil {
			for _, key := range []string{"health_successes", "health_failures"} {
				if current, ok := previous[key]; ok {
					value[key] = current
				}
			}
		}
		if metrics, ok := result.Metrics[name]; ok {
			value["delay_ms"] = metrics.DelayMS
			value["speed_mbps"] = metrics.SpeedMbps
			value["stability_ratio"] = metrics.StabilityRatio
			value["country"] = metrics.Country
		}
		nodes[name] = value
	}
	return nodes
}

func (store FileProviderStore) writeRanked(pool string, proxies []map[string]any, nodes map[string]map[string]any) error {
	rankProviderProxies(proxies, nodes)
	if err := atomicJSON(filepath.Join(store.ProvidersDirectory, pool+".json"), map[string]any{"proxies": proxies}); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(store.ProvidersDirectory, pool+".sources.json"), map[string]any{"nodes": nodes})
}

func rankProviderProxies(proxies []map[string]any, metadata map[string]map[string]any) {
	sort.SliceStable(proxies, func(i, j int) bool {
		leftName, _ := proxies[i]["name"].(string)
		rightName, _ := proxies[j]["name"].(string)
		left := rankedProviderNode(leftName, metadata[leftName])
		right := rankedProviderNode(rightName, metadata[rightName])
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		return leftName < rightName
	})
}

func rankedProviderNode(id string, metadata map[string]any) noderank.Node {
	node := noderank.Node{ID: id, Alive: true, DelayMS: intMetadata(metadata, "delay_ms"), SpeedMbps: floatMetadata(metadata, "speed_mbps"), StabilityRatio: floatMetadata(metadata, "stability_ratio"), HealthSuccesses: intMetadata(metadata, "health_successes"), HealthFailures: intMetadata(metadata, "health_failures")}
	node.Score = noderank.Score(node)
	return node
}

func readProviderJSON(path string, target any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}

func stringMetadata(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	result, _ := value[key].(string)
	return result
}

func floatMetadata(value map[string]any, key string) float64 {
	if value == nil {
		return 0
	}
	result, _ := value[key].(float64)
	return result
}

func intMetadata(value map[string]any, key string) int {
	return int(floatMetadata(value, key))
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
