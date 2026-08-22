//go:build windows

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

	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/operationcancel"
	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/serverstate"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"github.com/gooog1111/orcheroute/internal/updater"
	"github.com/gooog1111/orcheroute/internal/windowsqualify"
	"golang.org/x/sys/windows"
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

type options struct {
	StateDirectory, OutputStateDirectory, OperationPath, ActiveProfile, PolicyPath, Mihomo string
	Force, CachedOnly, FetchOnly                                                           bool
	Groups                                                                                 []string
	SubscriptionIDs                                                                        []string
}

func main() {
	root := os.Getenv("ProgramData")
	if root == "" {
		root = `C:\ProgramData`
	}
	root = filepath.Join(root, "OrcheRoute")
	var groups groupFlags
	var subscriptionIDs stringFlags
	stateDirectory := flag.String("state-dir", filepath.Join(root, "state"), "state directory")
	outputStateDirectory := flag.String("output-state-dir", "", "isolated output directory")
	operationPath := flag.String("operation-path", "", "operation status file override")
	cancelPath := flag.String("cancel-path", "", "cooperative cancellation request file")
	activeProfile := flag.String("network-profile", filepath.Join(root, "state", "network-active.json"), "active network profile")
	policyPath := flag.String("policy", filepath.Join(root, "state", "qualification-policy.json"), "qualification policy")
	mihomo := flag.String("mihomo", filepath.Join(root, "bin", "mihomo.exe"), "Mihomo binary")
	force := flag.Bool("force", false, "force refresh and provider rebuild")
	cachedOnly := flag.Bool("cached-only", false, "use existing cache without network refresh")
	fetchOnly := flag.Bool("fetch-only", false, "refresh caches without rebuilding providers")
	flag.Var(&subscriptionIDs, "subscription-id", "subscription ID to refresh")
	flag.Var(&groups, "group", "pool to update")
	flag.Parse()
	output := *outputStateDirectory
	if output == "" {
		output = *stateDirectory
	}
	current := options{StateDirectory: *stateDirectory, OutputStateDirectory: output, OperationPath: *operationPath, ActiveProfile: *activeProfile,
		PolicyPath: *policyPath, Mihomo: *mihomo, Force: *force, CachedOnly: *cachedOnly, FetchOnly: *fetchOnly, Groups: groups, SubscriptionIDs: subscriptionIDs}
	ctx, stop := operationcancel.Watch(context.Background(), *cancelPath, 100*time.Millisecond)
	defer stop()
	if err := run(ctx, current); err != nil {
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintln(os.Stderr, "update error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, options options) error {
	if err := os.MkdirAll(options.StateDirectory, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(options.OutputStateDirectory, 0o700); err != nil {
		return err
	}
	release, acquired, err := lockFile(filepath.Join(options.OutputStateDirectory, "update-go.lock"))
	if err != nil {
		return err
	}
	if !acquired {
		fmt.Println("update=skipped reason=already_running")
		return nil
	}
	defer release()
	profile, err := loadJSON[network.Profile](options.ActiveProfile)
	if err != nil {
		return fmt.Errorf("network profile: %w", err)
	}
	vpnRole, ok := profile.Roles["vpn_underlay"]
	if !ok || strings.TrimSpace(vpnRole.Interface) == "" {
		return fmt.Errorf("vpn_underlay interface is missing")
	}
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
		policies[group], err = qualification.Effective(validatedPolicy, string(group))
		if err != nil {
			return err
		}
	}
	database, err := serverstate.Open(filepath.Join(options.StateDirectory, "state.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	operationPath := filepath.Join(options.OutputStateDirectory, "update-operation-go.json")
	if options.OperationPath != "" {
		operationPath = options.OperationPath
	}
	cache := subscriptions.FileCache{Directory: filepath.Join(options.StateDirectory, "subscription-cache")}
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
	for _, id := range append(request.SubscriptionIDs, options.SubscriptionIDs...) {
		if id != "" {
			requestedIDs[id] = true
		}
	}
	fetcher := subscriptions.FetcherMap{
		subscriptions.Standard:    subscriptions.HTTPFetcher{},
		subscriptions.BlackTemple: subscriptions.BlackTempleFetcher{CredentialsPath: filepath.Join(options.OutputStateDirectory, "blacktemple_credentials.json")},
		subscriptions.Inline:      subscriptions.InlineFetcher{}, subscriptions.WireGuard: subscriptions.WireGuardFetcher{},
	}
	backendConfig := windowsqualify.DefaultConfig()
	testURLs, err := qualification.URLTestURLs(validatedPolicy)
	if err != nil {
		return err
	}
	backendConfig.URLTests = make([]windowsqualify.URLTarget, 0, len(testURLs))
	for _, target := range testURLs {
		backendConfig.URLTests = append(backendConfig.URLTests, windowsqualify.URLTarget{URL: target})
	}
	backendConfig.MihomoBinary, backendConfig.Interface = options.Mihomo, vpnRole.Interface
	backendConfig.DNS = windowsqualify.DNSConfig{CacheAlgorithm: profile.DNS.CacheAlgorithm, Bootstrap: profile.DNS.Bootstrap, Proxy: profile.DNS.Proxy, VPNUnderlay: profile.DNS.VPNUnderlay}
	probeBackend := &windowsqualify.Backend{Config: backendConfig}
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
		_ = writeAtomicJSON(operationPath, map[string]any{"kind": "subscription_update", "status": "running", "updated_at": now.Unix(), "phase": progress.Phase,
			"message": progress.Message, "current": progress.Current, "total": progress.Total, "pool": progress.Pool})
	}
	qualifier := updater.PipelineQualifier{Backend: probeBackend, Progress: func(pool, stage string, current, total int) {
		labels := map[string]string{"baseline": "Скорость прямого соединения", "tcp": "TCP-проверка", "url_test": "URL-test", "speed_test": "Speed-test"}
		label := labels[stage]
		if label == "" {
			label = stage
		}
		reportProgress(updater.Progress{Phase: stage, Message: fmt.Sprintf("%s %d/%d", label, current, total), Current: current, Total: total, Pool: pool})
	}}
	dependencies := updater.Dependencies{Repository: database, Cache: cache, Fetcher: fetcher, Qualifier: qualifier,
		Providers: updater.FileProviderStore{ProvidersDirectory: filepath.Join(options.OutputStateDirectory, "providers"), ReportsDirectory: filepath.Join(options.OutputStateDirectory, "qualification")}, Progress: reportProgress}
	dependencies.UpdateStatus = database.UpdateSubscriptionStatus
	dependencies.RecordEvent = func(ctx context.Context, eventType, severity, pool, reason string, details map[string]any) error {
		return database.AddEvent(ctx, serverstate.EventInput{EventType: eventType, Severity: severity, Pool: stringPointer(pool), Reason: stringPointer(reason), Details: details})
	}
	result, err := updater.Run(ctx, dependencies, updater.Request{Groups: selected, RequestedGroups: requestedGroups, SubscriptionIDs: requestedIDs,
		Force: options.Force || request.Force, FetchOnly: options.FetchOnly, SkipFetch: options.CachedOnly, Policies: policies})
	status, message := "success", "Обновление завершено"
	if err != nil {
		status, message = "error", "Обновление прервано"
	} else if len(result.Failures) > 0 {
		status, message = "warning", "Обновление завершено с предупреждениями"
	}
	_ = writeAtomicJSON(operationPath, map[string]any{"kind": "subscription_update", "status": status, "phase": "complete", "updated_at": time.Now().Unix(), "message": message, "failures": result.Failures, "pools": result.Pools})
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(result)
	fmt.Println(string(payload))
	return nil
}

func lockFile(path string) (func(), bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false, err
	}
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	if err != nil {
		file.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return func() {}, false, nil
		}
		return func() {}, false, err
	}
	return func() { _ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped); _ = file.Close() }, true, nil
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
	temporary := path + ".new"
	if err := os.WriteFile(temporary, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(temporary, path)
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
