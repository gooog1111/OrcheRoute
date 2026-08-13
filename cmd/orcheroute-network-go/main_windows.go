//go:build windows

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/mihomo"
	"github.com/gooog1111/orcheroute/internal/network"
	"golang.org/x/net/proxy"
)

type options struct {
	action, profilePath, stateDirectory, configPath, runtimeEnv, mihomoBinary, coreService string
	confirm                                                                                bool
}

func main() {
	root := os.Getenv("ProgramData")
	if root == "" {
		root = `C:\ProgramData`
	}
	root = filepath.Join(root, "OrcheRoute")
	current := options{}
	flag.StringVar(&current.action, "action", "preview", "preview, apply, remove or apply-staged")
	flag.StringVar(&current.profilePath, "profile", filepath.Join(root, "state", "network-active.json"), "network profile")
	flag.StringVar(&current.stateDirectory, "state-dir", filepath.Join(root, "state"), "state directory")
	flag.StringVar(&current.configPath, "config", filepath.Join(root, "config.json"), "Mihomo configuration")
	flag.StringVar(&current.runtimeEnv, "runtime-env", filepath.Join(root, "runtime.env"), "runtime secrets")
	flag.StringVar(&current.mihomoBinary, "mihomo", filepath.Join(root, "bin", "mihomo.exe"), "Mihomo binary")
	flag.StringVar(&current.coreService, "core-service", "OrcheRouteMihomo", "Mihomo Windows service")
	flag.BoolVar(&current.confirm, "confirm-system-capture", false, "confirm system traffic capture")
	flag.Parse()
	if err := run(context.Background(), current); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, current options) error {
	switch current.action {
	case "remove":
		if err := stopService(ctx, current.coreService); err != nil {
			return err
		}
		return writeOutput(map[string]any{"removed": true})
	case "apply-staged":
		result, err := applyStaged(ctx, current)
		if err != nil {
			return err
		}
		return writeOutput(result)
	case "preview", "apply":
		var profile network.ProfileInput
		if err := readJSON(current.profilePath, &profile); err != nil {
			return err
		}
		preview, err := previewProfile(ctx, profile)
		if err != nil {
			return err
		}
		return writeOutput(preview)
	default:
		return fmt.Errorf("unknown_action")
	}
}

func applyStaged(ctx context.Context, current options) (map[string]any, error) {
	desiredPath := filepath.Join(current.stateDirectory, "network-profile.json")
	activePath := filepath.Join(current.stateDirectory, "network-active.json")
	requestPath := filepath.Join(current.stateDirectory, "network-apply-request.json")
	statusPath := filepath.Join(current.stateDirectory, "network-apply-status.json")
	var desired, active network.ProfileInput
	var request map[string]any
	if err := readJSON(desiredPath, &desired); err != nil {
		return nil, err
	}
	if err := readJSON(activePath, &active); err != nil {
		return nil, err
	}
	if err := readJSON(requestPath, &request); err != nil {
		return nil, err
	}
	revision := int64(number(request["revision"]))
	if revision != desired.Revision {
		return nil, fmt.Errorf("network_revision_conflict")
	}
	if !current.confirm {
		return nil, fmt.Errorf("system_capture_confirmation_required")
	}
	desiredPreview, err := previewProfile(ctx, desired)
	if err != nil {
		return nil, err
	}
	configBackup, configExists := snapshot(current.configPath)
	activeBackup, activeExists := snapshot(activePath)
	_ = writeStatus(statusPath, "applying", revision, map[string]any{"digest": desiredPreview.Digest})
	defer os.Remove(requestPath)
	rollback := func(cause error) (map[string]any, error) {
		restore(current.configPath, configBackup, configExists)
		restore(activePath, activeBackup, activeExists)
		_ = restartService(context.Background(), current.coreService)
		_ = writeStatus(statusPath, "failed", revision, map[string]any{"error": cause.Error(), "rolled_back": true})
		return nil, cause
	}
	generated, err := generateConfig(desiredPreview, current)
	if err != nil {
		return rollback(err)
	}
	candidate := current.configPath + ".new"
	if err := writeJSON(candidate, generated); err != nil {
		return rollback(err)
	}
	output, err := exec.CommandContext(ctx, current.mihomoBinary, "-t", "-d", current.stateDirectory, "-f", candidate).CombinedOutput()
	if err != nil {
		return rollback(fmt.Errorf("mihomo_network_config_invalid: %s", tail(string(output), 1000)))
	}
	if err := replace(candidate, current.configPath); err != nil {
		return rollback(err)
	}
	if err := writeJSON(activePath, desiredPreview.Profile); err != nil {
		return rollback(err)
	}
	if err := restartService(ctx, current.coreService); err != nil {
		return rollback(err)
	}
	if !waitProxy(ctx) {
		return rollback(fmt.Errorf("network_profile_healthcheck_failed"))
	}
	fields := map[string]any{"digest": desiredPreview.Digest, "capture_mode": desiredPreview.Profile.Capture.Mode,
		"direct_interface":       desiredPreview.Profile.Roles["direct"].Interface,
		"vpn_underlay_interface": desiredPreview.Profile.Roles["vpn_underlay"].Interface}
	if err := writeStatus(statusPath, "applied", revision, fields); err != nil {
		return nil, err
	}
	return map[string]any{"applied": true, "revision": revision, "profile": desiredPreview.Profile}, nil
}

func previewProfile(ctx context.Context, profile network.ProfileInput) (network.Preview, error) {
	topology, err := discoverTopology(ctx)
	if err != nil {
		return network.Preview{}, err
	}
	return network.PreviewProfile(profile, topology)
}

type routeEntry struct {
	InterfaceAlias string `json:"InterfaceAlias"`
	NextHop        string `json:"NextHop"`
	RouteMetric    int    `json:"RouteMetric"`
}

func discoverTopology(ctx context.Context) (network.Topology, error) {
	routes := []routeEntry{}
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		`@(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Select-Object InterfaceAlias,NextHop,RouteMetric) | ConvertTo-Json -Compress`)
	if payload, err := command.Output(); err == nil && len(strings.TrimSpace(string(payload))) > 2 {
		if json.Unmarshal(payload, &routes) != nil {
			var single routeEntry
			if json.Unmarshal(payload, &single) == nil {
				routes = []routeEntry{single}
			}
		}
	}
	byInterface := map[string][]network.DefaultRoute{}
	for _, route := range routes {
		var gateway *string
		if value := strings.TrimSpace(route.NextHop); value != "" && value != "0.0.0.0" {
			gateway = &value
		}
		byInterface[route.InterfaceAlias] = append(byInterface[route.InterfaceAlias], network.DefaultRoute{Gateway: gateway, Metric: route.RouteMetric, Table: "main", Protocol: "windows"})
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return network.Topology{}, err
	}
	result := network.Topology{LocalCIDRs: []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8"}}
	seen := map[string]bool{}
	for _, cidr := range result.LocalCIDRs {
		seen[cidr] = true
	}
	for _, current := range interfaces {
		loopback := current.Flags&net.FlagLoopback != 0
		kind := "ethernet"
		lower := strings.ToLower(current.Name)
		if loopback {
			kind = "loopback"
		} else if strings.Contains(lower, "wi-fi") || strings.Contains(lower, "wireless") {
			kind = "wifi"
		} else if strings.Contains(lower, "tun") || strings.Contains(lower, "wintun") {
			kind = "tun"
		}
		state := "down"
		if current.Flags&net.FlagUp != 0 {
			state = "up"
		}
		entry := network.Interface{Name: current.Name, Kind: kind, State: state, MTU: current.MTU, Loopback: loopback, DefaultRoutes: byInterface[current.Name]}
		addresses, _ := current.Addrs()
		for _, raw := range addresses {
			ip, prefix, parseErr := net.ParseCIDR(raw.String())
			if parseErr != nil {
				continue
			}
			ones, _ := prefix.Mask.Size()
			family, scope := "inet6", "global"
			if ip.To4() != nil {
				family = "inet"
			}
			if ip.IsLoopback() {
				scope = "host"
			} else if ip.IsLinkLocalUnicast() {
				scope = "link"
			}
			cidr := fmt.Sprintf("%s/%d", ip.String(), ones)
			entry.Addresses = append(entry.Addresses, network.Address{Family: family, CIDR: cidr, Scope: scope})
			if !seen[cidr] {
				seen[cidr], result.LocalCIDRs = true, append(result.LocalCIDRs, cidr)
			}
		}
		result.Interfaces = append(result.Interfaces, entry)
	}
	sort.Strings(result.LocalCIDRs)
	return result, nil
}

func generateConfig(preview network.Preview, current options) (map[string]any, error) {
	values, err := readEnv(current.runtimeEnv)
	if err != nil {
		return nil, err
	}
	routeDefault := "proxy"
	var routeState map[string]any
	if readJSON(filepath.Join(current.stateDirectory, "routes.json"), &routeState) == nil {
		if value := fmt.Sprint(routeState["default"]); value == "direct" || value == "proxy" || value == "block" {
			routeDefault = value
		}
	}
	roles := map[string]mihomo.Role{}
	for name, value := range preview.ResolvedRoles {
		roles[name] = mihomo.Role{Interface: value.Interface, Mark: value.Mark}
	}
	input := mihomo.Input{StateDir: current.stateDirectory, Platform: "windows", TestURL: "https://www.gstatic.com/generate_204", Secret: values["controller_secret"], RouteDefault: routeDefault,
		Network: mihomo.Network{Profile: mihomo.Profile{Capture: mihomo.Capture{Mode: preview.Profile.Capture.Mode, Interfaces: preview.Profile.Capture.Interfaces, DNSHijack: preview.Profile.Capture.DNSHijack, StrictRoute: preview.Profile.Capture.StrictRoute}},
			ResolvedRoles: roles, EffectiveBypassCIDRs: preview.EffectiveBypassCIDRs,
			DNS: mihomo.DNS{Config: mihomo.DNSConfig{IPv6: preview.DNS.Config.IPv6, CacheAlgorithm: preview.DNS.Config.CacheAlgorithm, PreferH3: preview.DNS.Config.PreferH3, UseHosts: preview.DNS.Config.UseHosts},
				Effective: mihomo.DNSEffective{Bootstrap: preview.DNS.Effective.Bootstrap, Proxy: preview.DNS.Effective.Proxy, Direct: preview.DNS.Effective.Direct, VPNUnderlay: preview.DNS.Effective.VPNUnderlay}}}}
	return mihomo.Build(input)
}

func waitProxy(ctx context.Context) bool {
	for count := 0; count < 12; count++ {
		dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:21080", nil, &net.Dialer{Timeout: 5 * time.Second})
		if err == nil {
			transport := &http.Transport{DialContext: func(_ context.Context, networkName, address string) (net.Conn, error) {
				return dialer.Dial(networkName, address)
			}, TLSHandshakeTimeout: 8 * time.Second}
			client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.gstatic.com/generate_204", nil)
			response, requestErr := client.Do(request)
			if requestErr == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode == http.StatusNoContent {
					return true
				}
			}
			transport.CloseIdleConnections()
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
	return false
}

func restartService(ctx context.Context, name string) error {
	_ = stopService(ctx, name)
	output, err := exec.CommandContext(ctx, "sc.exe", "start", name).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "already running") {
		return fmt.Errorf("core_start_failed: %s", tail(string(output), 500))
	}
	return nil
}

func stopService(ctx context.Context, name string) error {
	output, err := exec.CommandContext(ctx, "sc.exe", "stop", name).CombinedOutput()
	if err != nil {
		text := strings.ToLower(string(output))
		if strings.Contains(text, "not been started") || strings.Contains(text, "1062") {
			return nil
		}
		return fmt.Errorf("core_stop_failed: %s", tail(string(output), 500))
	}
	for count := 0; count < 30; count++ {
		query, _ := exec.CommandContext(ctx, "sc.exe", "query", name).CombinedOutput()
		if strings.Contains(string(query), "STOPPED") {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("core_stop_timeout")
}

func readEnv(path string) (map[string]string, error) {
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
		if found {
			result[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
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

func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	return replace(temporary, path)
}

func replace(source, target string) error {
	_ = os.Remove(target)
	return os.Rename(source, target)
}

func snapshot(path string) ([]byte, bool) {
	payload, err := os.ReadFile(path)
	return payload, err == nil
}

func restore(path string, payload []byte, exists bool) {
	if !exists {
		_ = os.Remove(path)
		return
	}
	_ = os.WriteFile(path, payload, 0o600)
}

func writeStatus(path, status string, revision int64, fields map[string]any) error {
	payload := map[string]any{"status": status, "revision": revision, "updated_at": time.Now().Unix()}
	for key, value := range fields {
		payload[key] = value
	}
	return writeJSON(path, payload)
}

func writeOutput(value any) error {
	return json.NewEncoder(os.Stdout).Encode(value)
}

func number(value any) int {
	switch current := value.(type) {
	case float64:
		return int(current)
	case int:
		return current
	default:
		return 0
	}
}

func tail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
