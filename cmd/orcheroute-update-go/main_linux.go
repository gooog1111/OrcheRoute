//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gooog1111/orcheroute/internal/linuxnetwork"
	"github.com/gooog1111/orcheroute/internal/linuxqualify"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/serverstate"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"github.com/gooog1111/orcheroute/internal/updater"
	"golang.org/x/sys/unix"
)

type groupFlags []string
type stringFlags []string

func (values *groupFlags) String() string { return strings.Join(*values, ",") }
func (values *groupFlags) Set(value string) error {
	if value != "primary" && value != "emergency" {
		return fmt.Errorf("invalid group")
	}
	*values = append(*values, value)
	return nil
}
func (values *stringFlags) String() string { return strings.Join(*values, ",") }
func (values *stringFlags) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("empty value")
	}
	*values = append(*values, value)
	return nil
}

type updateRequest struct {
	Groups          []string `json:"groups"`
	SubscriptionIDs []string `json:"subscription_ids"`
	Force           bool     `json:"force"`
}
type unavailableFetcher struct{}

func (unavailableFetcher) Fetch(context.Context, subscriptions.Subscription) ([]string, error) {
	return nil, errors.New("cached_only")
}

func main() {
	var groups groupFlags
	var subscriptionIDs stringFlags
	stateDirectory := flag.String("state-dir", "/var/lib/orcheroute", "state directory")
	outputStateDirectory := flag.String("output-state-dir", "", "isolated output directory (defaults to state-dir)")
	operationPath := flag.String("operation-path", "", "operation status file override")
	activeProfile := flag.String("network-profile", "/var/lib/orcheroute/network-active.json", "active network profile")
	policyPath := flag.String("policy", "/var/lib/orcheroute/qualification-policy.json", "qualification policy")
	mihomo := flag.String("mihomo", "/opt/orcheroute/bin/mihomo", "Mihomo binary")
	force := flag.Bool("force", false, "force refresh and provider rebuild")
	cachedOnly := flag.Bool("cached-only", false, "use existing cache without network subscription refresh")
	fetchOnly := flag.Bool("fetch-only", false, "refresh subscription caches without rebuilding providers")
	flag.Var(&subscriptionIDs, "subscription-id", "subscription ID to refresh (repeatable)")
	flag.Var(&groups, "group", "pool to update (repeatable)")
	flag.Parse()
	outputDirectory := *outputStateDirectory
	if outputDirectory == "" {
		outputDirectory = *stateDirectory
	}
	if err := run(context.Background(), options{StateDirectory: *stateDirectory, OutputStateDirectory: outputDirectory, OperationPath: *operationPath, ActiveProfile: *activeProfile, PolicyPath: *policyPath, Mihomo: *mihomo, Force: *force, CachedOnly: *cachedOnly, FetchOnly: *fetchOnly, Groups: groups, SubscriptionIDs: subscriptionIDs}); err != nil {
		fmt.Fprintln(os.Stderr, "update error:", err)
		os.Exit(1)
	}
}

type options struct {
	StateDirectory, OutputStateDirectory, OperationPath, ActiveProfile, PolicyPath, Mihomo string
	Force, CachedOnly, FetchOnly                                                           bool
	Groups                                                                                 []string
	SubscriptionIDs                                                                        []string
}

func run(ctx context.Context, options options) error {
	if err := os.MkdirAll(options.StateDirectory, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(options.OutputStateDirectory, 0o700); err != nil {
		return err
	}
	lockPath := filepath.Join(options.OutputStateDirectory, "update-go.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		fmt.Println("update=skipped reason=already_running")
		return nil
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	profile, err := qualificationNetworkProfile(ctx, options.ActiveProfile)
	if err != nil {
		return err
	}
	vpnRole := profile.Roles["vpn_underlay"]
	rawPolicy, err := loadMap(options.PolicyPath)
	if os.IsNotExist(err) {
		rawPolicy = qualification.DefaultPolicy()
	} else if err != nil {
		return err
	}
	rawPolicy = qualification.MigrateLegacyPools(rawPolicy)
	validatedPolicy, err := qualification.Validate(rawPolicy)
	if err != nil {
		return err
	}
	selected := []subscriptions.Group{}
	for _, group := range options.Groups {
		selected = append(selected, subscriptions.Group(group))
	}
	if len(selected) == 0 {
		selected = []subscriptions.Group{subscriptions.Primary, subscriptions.Emergency}
	}
	policies := map[subscriptions.Group]map[string]any{}
	for _, group := range selected {
		effective, effectiveErr := qualification.Effective(validatedPolicy, string(group))
		if effectiveErr != nil {
			return effectiveErr
		}
		policies[group] = effective
	}
	database, err := serverstate.Open(filepath.Join(options.StateDirectory, "state.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	operationPath := filepath.Join(options.OutputStateDirectory, "update-operation-go.json")
	providersDirectory := filepath.Join(options.OutputStateDirectory, "providers")
	reportsDirectory := filepath.Join(options.OutputStateDirectory, "qualification")
	cacheDirectory := filepath.Join(options.StateDirectory, "subscription-cache")
	var cache subscriptions.CacheRepository = subscriptions.FileCache{Directory: cacheDirectory}
	if options.OperationPath != "" {
		operationPath = options.OperationPath
	}
	request := updateRequest{}
	requestPath := filepath.Join(options.StateDirectory, "update-request.json")
	if current, readErr := loadJSON[updateRequest](requestPath); readErr == nil {
		request = current
		_ = os.Remove(requestPath)
	}
	requestedGroups := map[subscriptions.Group]bool{}
	for _, group := range request.Groups {
		if group == "primary" || group == "emergency" {
			requestedGroups[subscriptions.Group(group)] = true
		}
	}
	requestedIDs := map[string]bool{}
	for _, id := range request.SubscriptionIDs {
		if id != "" {
			requestedIDs[id] = true
		}
	}
	for _, id := range options.SubscriptionIDs {
		requestedIDs[id] = true
	}
	standard := subscriptions.HTTPFetcher{}
	credentialsPath := filepath.Join(options.OutputStateDirectory, "blacktemple_credentials.json")
	blacktemple := subscriptions.BlackTempleFetcher{CredentialsPath: credentialsPath}
	var fetcher subscriptions.Fetcher = subscriptions.FetcherMap{subscriptions.Standard: standard, subscriptions.BlackTemple: blacktemple, subscriptions.Inline: subscriptions.InlineFetcher{}, subscriptions.WireGuard: subscriptions.WireGuardFetcher{}}
	backendConfig := linuxqualify.DefaultConfig()
	testURLs, err := qualification.URLTestURLs(validatedPolicy)
	if err != nil {
		return err
	}
	backendConfig.URLTests = make([]linuxqualify.URLTarget, 0, len(testURLs))
	for _, target := range testURLs {
		backendConfig.URLTests = append(backendConfig.URLTests, linuxqualify.URLTarget{URL: target})
	}
	backendConfig.MihomoBinary = options.Mihomo
	backendConfig.Interface = vpnRole.Interface
	backendConfig.Mark = 0x5352
	backendConfig.DNS = linuxqualify.DNSConfig{CacheAlgorithm: profile.DNS.CacheAlgorithm, Bootstrap: profile.DNS.Bootstrap, Proxy: profile.DNS.Proxy, VPNUnderlay: profile.DNS.VPNUnderlay}
	probeBackend := &linuxqualify.Backend{Config: backendConfig}
	var progressMu sync.Mutex
	var lastProgress time.Time
	reportProgress := func(progress updater.Progress) {
		progressMu.Lock()
		defer progressMu.Unlock()
		now := time.Now()
		probeStage := progress.Phase == "tcp" || progress.Phase == "url_test" || progress.Phase == "speed_test"
		if probeStage && progress.Current > 0 && progress.Current < progress.Total && now.Sub(lastProgress) < 250*time.Millisecond {
			return
		}
		lastProgress = now
		_ = writeAtomicJSON(operationPath, map[string]any{"kind": "subscription_update", "status": "running", "updated_at": now.Unix(), "phase": progress.Phase, "message": progress.Message, "current": progress.Current, "total": progress.Total, "pool": progress.Pool})
	}
	qualifier := updater.PipelineQualifier{Backend: probeBackend, Progress: func(pool, stage string, current, total int) {
		labels := map[string]string{"baseline": "Скорость прямого соединения", "tcp": "TCP-проверка", "url_test": "URL-test", "speed_test": "Speed-test"}
		label := labels[stage]
		if label == "" {
			label = stage
		}
		reportProgress(updater.Progress{Phase: stage, Message: fmt.Sprintf("%s %d/%d", label, current, total), Current: current, Total: total, Pool: pool})
	}}
	dependencies := updater.Dependencies{Repository: database, Cache: cache, Fetcher: fetcher, Qualifier: qualifier, Providers: updater.FileProviderStore{ProvidersDirectory: providersDirectory, ReportsDirectory: reportsDirectory}, Progress: reportProgress}
	dependencies.UpdateStatus = database.UpdateSubscriptionStatus
	dependencies.RecordEvent = func(ctx context.Context, eventType, severity, pool, reason string, details map[string]any) error {
		return database.AddEvent(ctx, serverstate.EventInput{EventType: eventType, Severity: severity, Pool: stringPointer(pool), Reason: stringPointer(reason), Details: details})
	}
	result, err := updater.Run(ctx, dependencies, updater.Request{Groups: selected, RequestedGroups: requestedGroups, SubscriptionIDs: requestedIDs, Force: options.Force || request.Force, FetchOnly: options.FetchOnly, SkipFetch: options.CachedOnly, Policies: policies})
	status := "success"
	message := "Обновление завершено"
	if err != nil {
		status = "error"
		message = "Обновление прервано"
	} else if len(result.Failures) > 0 {
		status = "warning"
		message = qualificationFailureMessage(result)
	}
	_ = writeAtomicJSON(operationPath, map[string]any{"kind": "subscription_update", "status": status, "phase": "complete", "updated_at": time.Now().Unix(), "message": message, "failures": result.Failures, "pools": result.Pools})
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(result)
	fmt.Println(string(payload))
	return nil
}

// Qualification must remain usable before the user has applied a capture
// profile. A clean desktop installation may only have the host topology at
// this point, so derive a non-mutating underlay profile from the best default
// route instead of aborting before the first TCP probe.
func qualificationNetworkProfile(ctx context.Context, path string) (network.Profile, error) {
	profile, err := loadJSON[network.Profile](path)
	if err == nil {
		if role, ok := profile.Roles["vpn_underlay"]; ok && strings.TrimSpace(role.Interface) != "" {
			return profile, nil
		}
	} else if !os.IsNotExist(err) {
		return network.Profile{}, fmt.Errorf("network profile: %w", err)
	}
	topology, discoverErr := linuxnetwork.Discover(ctx)
	if discoverErr != nil {
		return network.Profile{}, fmt.Errorf("network profile unavailable: %w", discoverErr)
	}
	interfaceName := bestUnderlayInterface(topology)
	if interfaceName == "" {
		return network.Profile{}, fmt.Errorf("network profile unavailable: no default-route interface")
	}
	preview, previewErr := network.PreviewProfile(network.DefaultProfile(interfaceName), topology)
	if previewErr != nil {
		return network.Profile{}, fmt.Errorf("network profile unavailable: %w", previewErr)
	}
	return preview.Profile, nil
}

func bestUnderlayInterface(topology network.Topology) string {
	name, score := "", int(^uint(0)>>1)
	for _, current := range topology.Interfaces {
		if current.Loopback || current.Name == "" {
			continue
		}
		virtual := strings.HasPrefix(current.Name, "tun") || strings.HasPrefix(current.Name, "tap") ||
			strings.HasPrefix(current.Name, "wg") || strings.Contains(current.Name, "orcheroute")
		for _, route := range current.DefaultRoutes {
			candidate := route.Metric
			if route.Table != "" && route.Table != "main" && route.Table != "254" {
				candidate += 100000
			}
			if virtual {
				candidate += 1000000
			}
			if candidate < score {
				name, score = current.Name, candidate
			}
		}
	}
	return name
}

func qualificationFailureMessage(result updater.Result) string {
	for _, group := range []subscriptions.Group{subscriptions.Primary, subscriptions.Emergency} {
		switch result.Pools[group].Reason {
		case "all_servers_unavailable_tcp":
			return "Все серверы недоступны после TCP-проверки"
		case "all_servers_unavailable_url_test":
			return "Все серверы недоступны после URL-test"
		case "all_servers_failed_speed_or_stability":
			return "Ни один сервер не прошёл проверку скорости и стабильности"
		case "group_has_no_cached_links":
			return "В пуле нет сохранённых серверов для проверки"
		}
	}
	return "Обновление завершено с предупреждениями"
}

func loadMap(path string) (map[string]any, error) { return loadJSON[map[string]any](path) }
func loadJSON[T any](path string) (T, error) {
	var result T
	payload, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(payload, &result)
	return result, err
}
func writeAtomicJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".operation-*.tmp")
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
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
