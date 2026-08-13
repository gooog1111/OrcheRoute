//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/linuxnetwork"
	"github.com/gooog1111/orcheroute/internal/mihomo"
	"github.com/gooog1111/orcheroute/internal/network"
	"golang.org/x/net/proxy"
)

type options struct {
	action, profilePath, stateDirectory, configPath, runtimeEnv, mihomoBinary, coreService string
	confirm                                                                                bool
}

func main() {
	current := options{}
	flag.StringVar(&current.action, "action", "preview", "preview, apply, remove or apply-staged")
	flag.StringVar(&current.profilePath, "profile", "/var/lib/orcheroute/network-active.json", "network profile")
	flag.StringVar(&current.stateDirectory, "state-dir", "/var/lib/orcheroute", "state directory")
	flag.StringVar(&current.configPath, "config", "/etc/orcheroute/config.json", "Mihomo configuration")
	flag.StringVar(&current.runtimeEnv, "runtime-env", "/etc/orcheroute/runtime.env", "runtime secrets")
	flag.StringVar(&current.mihomoBinary, "mihomo", "/opt/orcheroute/bin/mihomo", "Mihomo binary")
	flag.StringVar(&current.coreService, "core-service", "orcheroute-core.service", "Mihomo systemd service")
	flag.BoolVar(&current.confirm, "confirm-system-capture", false, "confirm system traffic capture")
	flag.Parse()
	if err := run(context.Background(), current); err != nil {
		fatal(err)
	}
}
func run(ctx context.Context, current options) error {
	if current.action == "remove" {
		if err := linuxnetwork.Remove(ctx); err != nil {
			return err
		}
		write(map[string]any{"removed": true})
		return nil
	}
	if current.action == "apply-staged" {
		result, err := applyStaged(ctx, current)
		if err != nil {
			return err
		}
		write(result)
		return nil
	}
	payload, err := os.ReadFile(current.profilePath)
	if err != nil {
		return err
	}
	var profile network.ProfileInput
	if err := json.Unmarshal(payload, &profile); err != nil {
		return err
	}
	if current.action == "apply" {
		preview, err := linuxnetwork.Apply(ctx, profile)
		if err != nil {
			return err
		}
		write(preview)
		return nil
	}
	if current.action == "preview" {
		topology, err := linuxnetwork.Discover(ctx)
		if err != nil {
			return err
		}
		preview, err := network.PreviewProfile(profile, topology)
		if err != nil {
			return err
		}
		write(preview)
		return nil
	}
	return fmt.Errorf("unknown_action")
}
func applyStaged(ctx context.Context, current options) (result map[string]any, returnErr error) {
	desiredPath, activePath := filepath.Join(current.stateDirectory, "network-profile.json"), filepath.Join(current.stateDirectory, "network-active.json")
	requestPath, statusPath := filepath.Join(current.stateDirectory, "network-apply-request.json"), filepath.Join(current.stateDirectory, "network-apply-status.json")
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
	revision := number(request["revision"])
	if revision != desired.Revision {
		return nil, fmt.Errorf("network_revision_conflict")
	}
	if !current.confirm {
		return nil, fmt.Errorf("system_capture_confirmation_required")
	}
	if reason := foreignCapture(); reason != "" {
		_ = status(statusPath, "blocked", revision, map[string]any{"error": reason})
		return nil, errors.New(reason)
	}
	desiredTopology, err := linuxnetwork.Discover(ctx)
	if err != nil {
		return nil, err
	}
	desiredPreview, err := network.PreviewProfile(desired, desiredTopology)
	if err != nil {
		return nil, err
	}
	activePreview, err := network.PreviewProfile(active, desiredTopology)
	if err != nil {
		return nil, err
	}
	configBackup, configBackupErr := os.ReadFile(current.configPath)
	if configBackupErr != nil && !os.IsNotExist(configBackupErr) {
		return nil, configBackupErr
	}
	activeBackup, err := os.ReadFile(activePath)
	if err != nil {
		return nil, err
	}
	_ = status(statusPath, "applying", revision, map[string]any{"digest": desiredPreview.Digest})
	defer os.Remove(requestPath)
	rollback := func(cause error) error {
		if configBackupErr == nil {
			_ = os.WriteFile(current.configPath+".rollback", configBackup, 0o600)
			_ = os.Rename(current.configPath+".rollback", current.configPath)
		} else {
			_ = os.Remove(current.configPath)
		}
		_ = os.WriteFile(activePath+".rollback", activeBackup, 0o600)
		_ = os.Rename(activePath+".rollback", activePath)
		activeInput := active
		_, _ = linuxnetwork.Apply(context.Background(), activeInput)
		_ = exec.Command("systemctl", "restart", current.coreService).Run()
		_ = status(statusPath, "failed", revision, map[string]any{"error": cause.Error(), "rolled_back": true, "digest": activePreview.Digest})
		return cause
	}
	if _, err := linuxnetwork.Apply(ctx, desired); err != nil {
		return nil, rollback(err)
	}
	generated, err := generateConfig(desiredPreview, current)
	if err != nil {
		return nil, rollback(err)
	}
	candidate := current.configPath + ".new"
	if err := writeJSON(candidate, generated); err != nil {
		return nil, rollback(err)
	}
	if output, err := exec.CommandContext(ctx, current.mihomoBinary, "-t", "-d", current.stateDirectory, "-f", candidate).CombinedOutput(); err != nil {
		return nil, rollback(fmt.Errorf("mihomo_network_config_invalid: %s", tail(string(output), 1000)))
	}
	if err := os.Rename(candidate, current.configPath); err != nil {
		return nil, rollback(err)
	}
	if err := writeJSON(activePath, desiredPreview.Profile); err != nil {
		return nil, rollback(err)
	}
	_ = exec.CommandContext(ctx, "systemctl", "enable", "orcheroute-routing.service", current.coreService).Run()
	if output, err := exec.CommandContext(ctx, "systemctl", "restart", current.coreService).CombinedOutput(); err != nil {
		return nil, rollback(fmt.Errorf("core_restart_failed: %s", tail(string(output), 500)))
	}
	if !waitProxy(ctx) {
		return nil, rollback(fmt.Errorf("network_profile_healthcheck_failed"))
	}
	fields := map[string]any{"digest": desiredPreview.Digest, "direct_interface": desiredPreview.Profile.Roles["direct"].Interface, "vpn_underlay_interface": desiredPreview.Profile.Roles["vpn_underlay"].Interface, "capture_mode": desiredPreview.Profile.Capture.Mode}
	if err := status(statusPath, "applied", revision, fields); err != nil {
		return nil, err
	}
	return map[string]any{"applied": true, "revision": revision, "profile": desiredPreview.Profile}, nil
}
func generateConfig(preview network.Preview, current options) (map[string]any, error) {
	values, err := readEnv(current.runtimeEnv)
	if err != nil {
		return nil, err
	}
	routeDefault := "proxy"
	var routesState map[string]any
	if readJSON(filepath.Join(current.stateDirectory, "routes.json"), &routesState) == nil {
		if value := text(routesState["default"]); value != "" {
			routeDefault = value
		}
	}
	roles := map[string]mihomo.Role{}
	for name, value := range preview.ResolvedRoles {
		roles[name] = mihomo.Role{Interface: value.Interface, Mark: value.Mark}
	}
	input := mihomo.Input{StateDir: current.stateDirectory, TestURL: "https://www.gstatic.com/generate_204", Secret: values["controller_secret"], RouteDefault: routeDefault, Network: mihomo.Network{Profile: mihomo.Profile{Capture: mihomo.Capture{Mode: preview.Profile.Capture.Mode, Interfaces: preview.Profile.Capture.Interfaces, DNSHijack: preview.Profile.Capture.DNSHijack, StrictRoute: preview.Profile.Capture.StrictRoute}}, ResolvedRoles: roles, EffectiveBypassCIDRs: preview.EffectiveBypassCIDRs, DNS: mihomo.DNS{Config: mihomo.DNSConfig{IPv6: preview.DNS.Config.IPv6, CacheAlgorithm: preview.DNS.Config.CacheAlgorithm, PreferH3: preview.DNS.Config.PreferH3, UseHosts: preview.DNS.Config.UseHosts}, Effective: mihomo.DNSEffective{Bootstrap: preview.DNS.Effective.Bootstrap, Proxy: preview.DNS.Effective.Proxy, Direct: preview.DNS.Effective.Direct, VPNUnderlay: preview.DNS.Effective.VPNUnderlay}}}}
	return mihomo.Build(input)
}
func waitProxy(ctx context.Context) bool {
	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:21080", nil, proxy.Direct)
	if err != nil {
		return false
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}},
		Timeout: 15 * time.Second,
	}
	for count := 0; count < 12; count++ {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.gstatic.com/generate_204", nil)
		if requestErr == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusNoContent {
					return true
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
	return false
}
func foreignCapture() string {
	entries, _ := os.ReadDir("/sys/class/net")
	for _, entry := range entries {
		if entry.Name() == "orcheroute0" {
			continue
		}
		if _, err := os.Stat(filepath.Join("/sys/class/net", entry.Name(), "tun_flags")); err == nil {
			return "foreign_tun_active"
		}
	}
	output, _ := exec.Command("ip", "-4", "rule", "show").Output()
	rules := string(output)
	for _, marker := range []string{"lookup 2022", "iif singbox_tun", "goto 9010"} {
		if strings.Contains(rules, marker) {
			return "foreign_capture_stack_active"
		}
	}
	return ""
}
func status(path, value string, revision int64, fields map[string]any) error {
	payload := map[string]any{"status": value, "revision": revision, "updated_at": time.Now().Unix()}
	for key, item := range fields {
		payload[key] = item
	}
	return writeJSON(path, payload)
}
func readJSON(path string, value any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, value)
}
func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
func readEnv(path string) (map[string]string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, line := range strings.Split(string(payload), "\n") {
		if strings.Contains(line, "=") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			parts := strings.SplitN(line, "=", 2)
			result[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		}
	}
	return result, nil
}
func number(value any) int64 {
	switch current := value.(type) {
	case float64:
		return int64(current)
	case int64:
		return current
	case int:
		return int64(current)
	}
	return -1
}
func text(value any) string {
	if current, ok := value.(string); ok {
		return current
	}
	return fmt.Sprint(value)
}
func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
func write(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
