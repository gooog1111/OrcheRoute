//go:build linux

package reversevpn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLinuxAdapterWireGuardLifecycle(t *testing.T) {
	if os.Getenv("ORCHEROUTE_INTEGRATION_WG") != "1" {
		t.Skip("set ORCHEROUTE_INTEGRATION_WG=1 as root to run")
	}
	if os.Geteuid() != 0 {
		t.Skip("WireGuard integration test requires root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	adapter := NewLinuxAdapter(t.TempDir())
	config := DefaultConfig()
	config.InterfaceName = "or-rvtest"
	config.ServerCIDR = "10.253.77.1/30"
	config.ListenPort = 51987
	config.OutboundInterface = "eth0"
	config.PrivateKey, config.PublicKey, _ = keyPair()
	peerPrivate, peerPublic, _ := keyPair()
	config.Clients = []Client{{ID: "integration", Name: "Integration", Address: "10.253.77.2/32", PrivateKey: peerPrivate, PublicKey: peerPublic, Enabled: true}}
	_ = adapter.Disable(context.Background(), config.InterfaceName)
	t.Cleanup(func() { _ = adapter.Disable(context.Background(), config.InterfaceName) })
	if err := adapter.Apply(ctx, config); err != nil {
		t.Fatalf("apply real WireGuard interface: %v", err)
	}
	if !adapter.Active(ctx, config.InterfaceName) {
		t.Fatal("WireGuard interface is not active")
	}
	counters, err := adapter.Counters(ctx, config.InterfaceName)
	if err != nil {
		t.Fatalf("read peer counters: %v", err)
	}
	if _, ok := counters[peerPublic]; !ok {
		t.Fatalf("configured peer is missing from wg dump: %#v", counters)
	}
	const namespace = "or-rvns"
	_ = exec.Command("ip", "netns", "del", namespace).Run()
	_ = exec.Command("ip", "link", "del", "or-rvh").Run()
	t.Cleanup(func() {
		_ = exec.Command("ip", "netns", "del", namespace).Run()
		_ = exec.Command("ip", "link", "del", "or-rvh").Run()
	})
	peerKeyPath := filepath.Join(t.TempDir(), "peer.key")
	if err := os.WriteFile(peerKeyPath, []byte(peerPrivate+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"ip", "netns", "add", namespace},
		{"ip", "link", "add", "or-rvh", "type", "veth", "peer", "name", "or-rvc"},
		{"ip", "addr", "add", "192.0.2.1/30", "dev", "or-rvh"},
		{"ip", "link", "set", "or-rvh", "up"},
		{"ip", "link", "set", "or-rvc", "netns", namespace},
		{"ip", "-n", namespace, "addr", "add", "192.0.2.2/30", "dev", "or-rvc"},
		{"ip", "-n", namespace, "link", "set", "lo", "up"},
		{"ip", "-n", namespace, "link", "set", "or-rvc", "up"},
		{"ip", "-n", namespace, "link", "add", "or-rvcli", "type", "wireguard"},
		{"ip", "-n", namespace, "addr", "add", "10.253.77.2/32", "dev", "or-rvcli"},
		{"ip", "netns", "exec", namespace, "wg", "set", "or-rvcli", "private-key", peerKeyPath, "peer", config.PublicKey, "endpoint", "192.0.2.1:51987", "allowed-ips", "10.253.77.0/30", "persistent-keepalive", "2"},
		{"ip", "-n", namespace, "link", "set", "or-rvcli", "up"},
		{"ip", "-n", namespace, "route", "add", "10.253.77.0/30", "dev", "or-rvcli"},
		{"ip", "netns", "exec", namespace, "ping", "-c", "2", "-W", "3", "10.253.77.1"},
	}
	for _, command := range commands {
		if output, err := exec.CommandContext(ctx, command[0], command[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", fmt.Sprint(command), err, output)
		}
	}
	counters, err = adapter.Counters(ctx, config.InterfaceName)
	if err != nil || counters[peerPublic].LastHandshake == 0 || counters[peerPublic].RXBytes == 0 {
		t.Fatalf("real client traffic was not accounted: %#v, %v", counters, err)
	}
	if err := adapter.Disable(ctx, config.InterfaceName); err != nil {
		t.Fatalf("disable real WireGuard interface: %v", err)
	}
	if adapter.Active(ctx, config.InterfaceName) {
		t.Fatal("WireGuard interface remained active after disable")
	}
}

func TestLinuxAdapterDoesNotDeleteUnmanagedInterface(t *testing.T) {
	if os.Getenv("ORCHEROUTE_INTEGRATION_WG") != "1" {
		t.Skip("set ORCHEROUTE_INTEGRATION_WG=1 as root to run")
	}
	if os.Geteuid() != 0 {
		t.Skip("WireGuard integration test requires root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const interfaceName = "or-rvcoll"
	_ = exec.Command("ip", "link", "del", "dev", interfaceName).Run()
	if output, err := exec.Command("ip", "link", "add", interfaceName, "type", "dummy").CombinedOutput(); err != nil {
		t.Fatalf("create unmanaged interface: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", "dev", interfaceName).Run() })

	adapter := NewLinuxAdapter(t.TempDir())
	config := DefaultConfig()
	config.InterfaceName = interfaceName
	config.PrivateKey, config.PublicKey, _ = keyPair()
	if err := adapter.Apply(ctx, config); err == nil || err.Error() != "interface_name_in_use" {
		t.Fatalf("apply should reject unmanaged interface, got %v", err)
	}
	if err := adapter.Disable(ctx, interfaceName); err == nil || err.Error() != "managed_interface_config_missing" {
		t.Fatalf("disable should reject unmanaged interface, got %v", err)
	}
	if !adapter.Active(ctx, interfaceName) {
		t.Fatal("unmanaged interface was deleted")
	}
}
