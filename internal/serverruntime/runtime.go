package serverruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gooog1111/orcheroute/internal/callserver"
	"github.com/gooog1111/orcheroute/internal/controller"
	"github.com/gooog1111/orcheroute/internal/core/noderank"
	corevalidator "github.com/gooog1111/orcheroute/internal/core/validator"
	"github.com/gooog1111/orcheroute/internal/core/whitelist"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/serverstate"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"golang.org/x/net/proxy"
)

type Config struct {
	Listen              string
	WebListen           string
	WebTLSListen        string
	ProductionState     string
	StateDirectory      string
	WebRoot             string
	RuntimeEnv          string
	ConfigDirectory     string
	MihomoAPI           string
	MihomoBinary        string
	FreeTURNBinary      string
	UpdateBinary        string
	NetworkBinary       string
	ComponentBinary     string
	SelfUpdateBinary    string
	CoreService         string
	ControllerEvery     time.Duration
	ConnectivityEvery   time.Duration
	ConnectivityTimeout time.Duration
	// RequireAPIAuth protects the control API with the token from runtime.env.
	// It may only be disabled for an API bound exclusively to loopback.
	RequireAPIAuth bool
}

func DefaultConfig() Config {
	return platformDefaultConfig()
}

type Runtime struct {
	Config                   Config
	Store                    *serverstate.Store
	CallServer               *callserver.Manager
	callServerError          string
	CallTransport            *callserver.Runtime
	apiToken                 string
	controllerSecret         string
	client                   *http.Client
	mu                       sync.RWMutex
	lastDecision             controller.Decision
	lastObservation          controller.Observation
	connectivityProbeFactory connectivityProbeFactory
	startedAt                int64
}

func New(config Config) (*Runtime, error) {
	if filepath.Clean(config.StateDirectory) != filepath.Clean(config.ProductionState) {
		return nil, fmt.Errorf("state_directory_must_be_production_directory")
	}
	if config.ControllerEvery <= 0 {
		config.ControllerEvery = 10 * time.Second
	}
	if config.ConnectivityEvery <= 0 {
		config.ConnectivityEvery = 10 * time.Second
	}
	if config.ConnectivityTimeout <= 0 {
		config.ConnectivityTimeout = 6 * time.Second
	}
	_, stateErr := os.Stat(filepath.Join(config.StateDirectory, "state.db"))
	freshState := os.IsNotExist(stateErr)
	if stateErr != nil && !freshState {
		return nil, stateErr
	}
	if err := os.MkdirAll(config.StateDirectory, 0o700); err != nil {
		return nil, err
	}
	values, err := readEnv(config.RuntimeEnv)
	if err != nil {
		return nil, err
	}
	if (config.RequireAPIAuth && values["api_token"] == "") || values["controller_secret"] == "" {
		return nil, fmt.Errorf("runtime_secrets_missing")
	}
	store, err := serverstate.Open(filepath.Join(config.StateDirectory, "state.db"))
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{Config: config, Store: store, apiToken: values["api_token"], controllerSecret: values["controller_secret"], client: &http.Client{Timeout: 10 * time.Second}, startedAt: time.Now().Unix()}
	callManager, callErr := callserver.Open(filepath.Join(config.StateDirectory, "call-server.json"))
	if callErr != nil {
		runtime.callServerError = callErr.Error()
	} else {
		runtime.CallServer = callManager
		runtime.CallTransport = platformCallServerRuntime(config)
	}
	if err := runtime.bootstrap(context.Background(), freshState); err != nil {
		_ = store.Close()
		return nil, err
	}
	return runtime, nil
}

// bootstrap makes a clean installation immediately configurable. It only
// creates missing state and never overwrites a profile or subscription that
// the user has already changed.
func (runtime *Runtime) bootstrap(ctx context.Context, freshState bool) error {
	// The database schema historically defaults to enabled for migration
	// compatibility. A genuinely new installation must never capture traffic
	// before the user presses Enable.
	if freshState {
		if err := runtime.Store.SetEnabled(ctx, false); err != nil {
			return err
		}
	}
	items, err := runtime.Store.List(ctx, true)
	if err != nil {
		return err
	}
	for _, item := range subscriptions.MissingDefaults(items, true) {
		if _, err := runtime.Store.Create(ctx, item); err != nil {
			return err
		}
	}

	settingsPath := filepath.Join(runtime.Config.StateDirectory, "component-settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := atomicJSON(settingsPath, map[string]any{
			"geo_auto_update": true, "geo_interval_hours": 24,
			"geo_source": "metacubex", "geoip_url": "", "geosite_url": "",
		}); err != nil {
			return err
		}
	}

	// Mihomo validates file providers before it starts.  On a clean install the
	// updater may still be downloading subscriptions and the user may not have
	// opened the Routes page yet, so none of these files exists naturally.  A
	// missing provider makes the very first network apply fail before the core
	// can start.  Seed valid empty files, but never replace updater/user data.
	if err := runtime.bootstrapMihomoFiles(); err != nil {
		return err
	}

	desiredPath := filepath.Join(runtime.Config.StateDirectory, "network-profile.json")
	activePath := filepath.Join(runtime.Config.StateDirectory, "network-active.json")
	_, desiredErr := os.Stat(desiredPath)
	_, activeErr := os.Stat(activePath)
	if desiredErr == nil && activeErr == nil {
		return nil
	}
	if desiredErr != nil && !os.IsNotExist(desiredErr) {
		return desiredErr
	}
	if activeErr != nil && !os.IsNotExist(activeErr) {
		return activeErr
	}

	profile := network.Profile{}
	if desiredErr == nil {
		if err := readJSON(desiredPath, &profile); err != nil {
			return err
		}
	} else if activeErr == nil {
		if err := readJSON(activePath, &profile); err != nil {
			return err
		}
	} else {
		topology, err := discoverTopology(ctx)
		if err != nil {
			// Component and subscription data must remain usable even when the host
			// has no interface yet (for example during early boot).
			return nil
		}
		interfaceName := bootstrapInterface(topology)
		if interfaceName == "" {
			return nil
		}
		preview, err := corevalidator.NetworkProfile(network.DefaultProfile(interfaceName), topology)
		if err != nil {
			return nil
		}
		profile = preview.Profile
		profile.Revision = 1
		profile.UpdatedAt = time.Now().Unix()
	}
	if os.IsNotExist(desiredErr) {
		if err := atomicJSON(desiredPath, profile); err != nil {
			return err
		}
	}
	if os.IsNotExist(activeErr) {
		if err := atomicJSON(activePath, profile); err != nil {
			return err
		}
	}
	// A generated profile has not been applied to the operating system yet.
	// Keep the transport stopped until the user explicitly enables it.
	return nil
}

func (runtime *Runtime) bootstrapMihomoFiles() error {
	providersDirectory := filepath.Join(runtime.Config.StateDirectory, "providers")
	for _, pool := range []string{"primary", "emergency", "whitelist"} {
		path := filepath.Join(providersDirectory, pool+".json")
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := atomicJSON(path, map[string]any{"proxies": []any{}}); err != nil {
			return err
		}
	}

	rulesPath := filepath.Join(runtime.Config.StateDirectory, "routes.json")
	if _, err := os.Stat(rulesPath); os.IsNotExist(err) {
		state := map[string]any{
			"revision": 0, "updated_at": time.Now().Unix(), "default": "proxy",
			"lists": map[string]any{"direct": []string{}, "proxy": []string{}, "block": []string{}},
			"stats": map[string]any{}, "items": []any{},
		}
		if err := atomicJSON(rulesPath, state); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	rulesDirectory := filepath.Join(runtime.Config.StateDirectory, "rules")
	for _, name := range []string{"direct", "proxy", "block"} {
		path := filepath.Join(rulesDirectory, name+".txt")
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := atomicWrite(path, []byte{}, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapInterface(topology network.Topology) string {
	bestName, bestScore := "", int(^uint(0)>>1)
	for _, current := range topology.Interfaces {
		if current.Loopback || current.Name == "" {
			continue
		}
		virtual := strings.HasPrefix(current.Name, "tun") || strings.HasPrefix(current.Name, "tap") ||
			strings.HasPrefix(current.Name, "wg") || strings.Contains(current.Name, "orcheroute")
		for _, route := range current.DefaultRoutes {
			score := route.Metric
			if route.Table != "" && route.Table != "main" && route.Table != "254" {
				score += 100000
			}
			if virtual {
				score += 1000000
			}
			if score < bestScore {
				bestName, bestScore = current.Name, score
			}
		}
	}
	if bestName != "" {
		return bestName
	}
	for _, current := range topology.Interfaces {
		if !current.Loopback && current.Name != "" && (current.State == "up" || current.State == "unknown") {
			return current.Name
		}
	}
	return ""
}

func (runtime *Runtime) Close() error {
	if runtime.CallTransport != nil {
		_ = runtime.CallTransport.Close()
	}
	return runtime.Store.Close()
}

func (runtime *Runtime) ReconcileCallServer(ctx context.Context) {
	if runtime.CallServer == nil || runtime.CallTransport == nil {
		return
	}
	_ = runtime.CallTransport.Apply(runtime.CallServer)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = runtime.CallTransport.Close()
			return
		case <-ticker.C:
			_ = runtime.CallTransport.Apply(runtime.CallServer)
		}
	}
}

func readEnv(path string) (map[string]string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, raw := range strings.Split(string(payload), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		result[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.Trim(strings.TrimSpace(parts[1]), "\"'")
	}
	return result, nil
}

func readJSON(path string, target any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}
func atomicJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(payload, '\n'), 0o600)
}
func atomicWrite(path string, payload []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, payload, mode); err != nil {
		return err
	}
	if err := os.Chmod(temporary, mode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (runtime *Runtime) mihomo(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, runtime.Config.MihomoAPI+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+runtime.controllerSecret)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := runtime.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("mihomo_http_%d", response.StatusCode)
	}
	result := map[string]any{}
	if response.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil && err != io.EOF {
			return nil, err
		}
	}
	return result, nil
}

type PublicNode struct {
	ID              string  `json:"id"`
	DisplayName     string  `json:"display_name"`
	Pool            string  `json:"pool"`
	Priority        int     `json:"priority"`
	Alive           bool    `json:"alive"`
	Delay           *int    `json:"delay_ms"`
	SpeedMbps       float64 `json:"speed_mbps,omitempty"`
	StabilityRatio  float64 `json:"stability_ratio,omitempty"`
	Country         string  `json:"country,omitempty"`
	HealthSuccesses int     `json:"health_successes,omitempty"`
	HealthFailures  int     `json:"health_failures,omitempty"`
	LastTestedAt    int64   `json:"last_tested_at,omitempty"`
	Score           float64 `json:"score,omitempty"`
	Selected        bool    `json:"selected"`
	SourceID        any     `json:"source_id"`
	SourceName      any     `json:"source_name"`
	FullName        string  `json:"-"`
}

func safeName(value string) string {
	if index := strings.IndexByte(value, ' '); index >= 0 {
		return value[:index]
	}
	return value
}

func (runtime *Runtime) liveNodes(ctx context.Context) ([]PublicNode, map[string]string, error) {
	providersPayload, err := runtime.mihomo(ctx, http.MethodGet, "/providers/proxies", nil)
	transportErr := err
	providers, _ := providersPayload["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		for _, pool := range []string{"primary", "emergency"} {
			var provider map[string]any
			if readJSON(filepath.Join(runtime.Config.ProductionState, "providers", pool+".json"), &provider) == nil {
				providers[pool] = provider
			}
		}
	}
	selected := ""
	if transportErr == nil {
		activePayload, _ := runtime.mihomo(ctx, http.MethodGet, "/proxies/ACTIVE", nil)
		selected, _ = activePayload["now"].(string)
	}
	selectedHealthy := false
	if snapshot, snapshotErr := runtime.Store.Snapshot(ctx); snapshotErr == nil {
		status := stringValue(snapshot.State["status"])
		selectedHealthy = status == "proxy_ok" || status == "manual_proxy_ok" || status == "emergency_proxy_ok"
		if selected == "" {
			selected = stringValue(snapshot.State["active"])
		}
	}
	result := []PublicNode{}
	mapping := map[string]string{}
	for _, pool := range []string{"primary", "emergency"} {
		provider, _ := providers[pool].(map[string]any)
		rawProxies, _ := provider["proxies"].([]any)
		counts := map[string]int{}
		for _, raw := range rawProxies {
			proxy, _ := raw.(map[string]any)
			counts[safeName(stringValue(proxy["name"]))]++
		}
		metadata := map[string]any{}
		var sourcePayload map[string]any
		if readJSON(filepath.Join(runtime.Config.ProductionState, "providers", pool+".sources.json"), &sourcePayload) == nil {
			metadata, _ = sourcePayload["nodes"].(map[string]any)
		}
		for _, raw := range rawProxies {
			proxy, _ := raw.(map[string]any)
			full := stringValue(proxy["name"])
			base := safeName(full)
			if base == "" {
				continue
			}
			id := base
			if counts[base] > 1 {
				digest := sha256.Sum256([]byte(full))
				id += "-" + hex.EncodeToString(digest[:])[:6]
			}
			display := base
			if index := strings.IndexByte(full, ' '); index >= 0 && strings.TrimSpace(full[index+1:]) != "" {
				display = strings.TrimSpace(full[index+1:])
			}
			alive := transportErr != nil
			if value, exists := proxy["alive"]; exists {
				alive, _ = value.(bool)
			}
			if full == selected && selectedHealthy {
				alive = true
			}
			var delay *int
			if history, ok := proxy["history"].([]any); ok && len(history) > 0 {
				if latest, ok := history[len(history)-1].(map[string]any); ok {
					value := intValue(latest["delay"])
					if value > 0 {
						delay = &value
					}
				}
			}
			var sourceID, sourceName any
			speedMbps, stabilityRatio := float64(0), float64(0)
			country := ""
			healthSuccesses, healthFailures := 0, 0
			lastTestedAt := int64(0)
			if item, ok := metadata[full].(map[string]any); ok {
				sourceID, sourceName = item["id"], item["name"]
				if value := intValue(item["delay_ms"]); value > 0 {
					delay = &value
				}
				speedMbps = floatValue(item["speed_mbps"])
				stabilityRatio = floatValue(item["stability_ratio"])
				country = strings.ToUpper(strings.TrimSpace(stringValue(item["country"])))
				healthSuccesses = intValue(item["health_successes"])
				healthFailures = intValue(item["health_failures"])
				lastTestedAt = int64Value(item["last_tested_at"])
			}
			priority := 1
			if pool == "primary" {
				priority = 0
			}
			ranked := noderank.Node{ID: id, Pool: pool, Alive: alive, SpeedMbps: speedMbps, StabilityRatio: stabilityRatio, HealthSuccesses: healthSuccesses, HealthFailures: healthFailures}
			if delay != nil {
				ranked.DelayMS = *delay
			}
			result = append(result, PublicNode{ID: id, DisplayName: display, Pool: pool, Priority: priority, Alive: alive, Delay: delay, SpeedMbps: speedMbps, StabilityRatio: stabilityRatio, Country: country, HealthSuccesses: healthSuccesses, HealthFailures: healthFailures, LastTestedAt: lastTestedAt, Score: noderank.Score(ranked), Selected: full == selected, SourceID: sourceID, SourceName: sourceName, FullName: full})
			mapping[id] = full
		}
	}
	derived := runtime.whitelistState()
	for _, node := range derived.Nodes {
		if !node.Alive {
			continue
		}
		delay := node.DelayMS
		var delayValue *int
		if delay > 0 {
			delayValue = &delay
		}
		fullName := stringValue(node.Proxy["name"])
		if fullName == "" {
			fullName = node.DisplayName
		}
		result = append(result, PublicNode{ID: node.ID, DisplayName: node.DisplayName, Pool: whitelist.Pool,
			Priority: 2, Alive: true, Delay: delayValue, Selected: node.ID == derived.SelectedNode,
			SourceID: node.SourceID, SourceName: node.SourceName, FullName: fullName})
		mapping[node.ID] = fullName
	}
	return result, mapping, transportErr
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if current, ok := value.(string); ok {
		return current
	}
	return fmt.Sprint(value)
}
func intValue(value any) int {
	switch current := value.(type) {
	case float64:
		return int(current)
	case int:
		return current
	case int64:
		return int(current)
	case int32:
		return int(current)
	case json.Number:
		value, _ := strconv.Atoi(current.String())
		return value
	}
	return 0
}

func int64Value(value any) int64 {
	switch current := value.(type) {
	case int64:
		return current
	case int:
		return int64(current)
	case int32:
		return int64(current)
	case float64:
		return int64(current)
	case json.Number:
		result, _ := current.Int64()
		return result
	}
	return 0
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func floatValue(value any) float64 {
	switch current := value.(type) {
	case float64:
		return current
	case float32:
		return float64(current)
	case int:
		return float64(current)
	case json.Number:
		result, _ := current.Float64()
		return result
	}
	return 0
}

func (runtime *Runtime) directAvailable(ctx context.Context, interfaceName string, mark int) bool {
	targets := []string{"1.1.1.1:443", "8.8.8.8:53", "9.9.9.9:443"}
	for _, target := range targets {
		dialer := platformDialer(interfaceName, mark)
		dialer.Timeout = 2500 * time.Millisecond
		connection, err := dialer.DialContext(ctx, "tcp", target)
		if err == nil {
			connection.Close()
			return true
		}
	}
	return false
}

func (runtime *Runtime) activeAvailable(ctx context.Context) bool {
	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:21080", nil, proxy.Direct)
	if err != nil {
		return false
	}
	transport := &http.Transport{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
		return dialer.Dial(network, address)
	}}
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
	for _, target := range []string{"https://www.gstatic.com/generate_204", "https://www.cloudflare.com/cdn-cgi/trace"} {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 400 {
				return true
			}
		}
	}
	return false
}
