//go:build linux

package reversevpn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type LinuxAdapter struct{ Directory string }

func NewLinuxAdapter(directory string) *LinuxAdapter { return &LinuxAdapter{Directory: directory} }

func (a *LinuxAdapter) Apply(ctx context.Context, config Config) error {
	for _, dependency := range []string{"ip", "iptables", "wg", "wg-quick", "sysctl"} {
		if _, err := exec.LookPath(dependency); err != nil {
			return fmt.Errorf("dependency_missing:%s", dependency)
		}
	}
	outbound := config.OutboundInterface
	if outbound == "" {
		value, err := defaultOutboundInterface(ctx)
		if err != nil {
			return err
		}
		outbound = value
	}
	text, err := WireGuardServerConfig(config, outbound)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.Directory, 0o700); err != nil {
		return err
	}
	path := filepath.Join(a.Directory, config.InterfaceName+".conf")
	if err := writeSecretAtomic(path, []byte(text)); err != nil {
		return err
	}
	// wg-quick down is idempotent only when wrapped this way; a missing link is
	// expected on the first apply and must not turn a valid setup into a failure.
	_ = runCommand(ctx, "wg-quick", "down", path)
	if err := runCommand(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return err
	}
	if err := runCommand(ctx, "wg-quick", "up", path); err != nil {
		_ = runCommand(context.Background(), "wg-quick", "down", path)
		return err
	}
	return nil
}

func (a *LinuxAdapter) Disable(ctx context.Context, interfaceName string) error {
	path := filepath.Join(a.Directory, interfaceName+".conf")
	if _, err := os.Stat(path); err == nil {
		if err := runCommand(ctx, "wg-quick", "down", path); err == nil {
			return nil
		}
	}
	if !a.Active(ctx, interfaceName) {
		return nil
	}
	return runCommand(ctx, "ip", "link", "delete", "dev", interfaceName)
}

func (a *LinuxAdapter) Active(ctx context.Context, interfaceName string) bool {
	return exec.CommandContext(ctx, "ip", "link", "show", "dev", interfaceName).Run() == nil
}

func (a *LinuxAdapter) Counters(ctx context.Context, interfaceName string) (map[string]PeerCounters, error) {
	output, err := exec.CommandContext(ctx, "wg", "show", interfaceName, "dump").Output()
	if err != nil {
		return nil, fmt.Errorf("wireguard_counters_failed: %w", err)
	}
	result := map[string]PeerCounters{}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for index, line := range lines {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		handshake, _ := strconv.ParseInt(fields[4], 10, 64)
		rx, _ := strconv.ParseUint(fields[5], 10, 64)
		tx, _ := strconv.ParseUint(fields[6], 10, 64)
		result[fields[0]] = PeerCounters{RXBytes: rx, TXBytes: tx, LastHandshake: handshake}
	}
	return result, nil
}

func defaultOutboundInterface(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return "", fmt.Errorf("default_route_unavailable: %w", err)
	}
	fields := strings.Fields(string(output))
	for index, field := range fields {
		if field == "dev" && index+1 < len(fields) && interfaceNamePattern.MatchString(fields[index+1]) {
			return fields[index+1], nil
		}
	}
	return "", fmt.Errorf("default_interface_unavailable")
}

func runCommand(ctx context.Context, name string, arguments ...string) error {
	output, err := exec.CommandContext(ctx, name, arguments...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s_failed: %w: %s", strings.ReplaceAll(name, "-", "_"), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeSecretAtomic(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".wireguard-*.conf")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
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
