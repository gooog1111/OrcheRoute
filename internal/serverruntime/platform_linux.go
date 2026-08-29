//go:build linux

package serverruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gooog1111/orcheroute/internal/callserver"
	"github.com/gooog1111/orcheroute/internal/network"
	"golang.org/x/sys/unix"
)

func platformDefaultConfig() Config {
	return Config{Listen: "127.0.0.1:19100", WebListen: ":19110", WebTLSListen: ":19111",
		ProductionState: "/var/lib/orcheroute", StateDirectory: "/var/lib/orcheroute",
		WebRoot: "/opt/orcheroute/webui", RuntimeEnv: "/etc/orcheroute/runtime.env", ConfigDirectory: "/etc/orcheroute",
		MihomoAPI: "http://127.0.0.1:19090", MihomoBinary: "/opt/orcheroute/bin/mihomo",
		UpdateBinary: "/opt/orcheroute/bin/orcheroute-update-go", NetworkBinary: "/opt/orcheroute/bin/orcheroute-network-go",
		ComponentBinary: "/opt/orcheroute/bin/orcheroute-components-go", SelfUpdateBinary: "/opt/orcheroute/bin/orcheroute-self-update", CoreService: "orcheroute-core.service", ControllerEvery: 10 * time.Second,
		ConnectivityEvery: 10 * time.Second, ConnectivityTimeout: 6 * time.Second,
		RequireAPIAuth: true}
}

func platformCallServerRuntime() *callserver.Runtime {
	return callserver.NewRuntime(callserver.EmbeddedXrayBackend{})
}

func discoverTopology(ctx context.Context) (network.Topology, error) {
	commandJSON := func(arguments ...string) ([]map[string]any, error) {
		output, err := exec.CommandContext(ctx, "ip", append([]string{"-j"}, arguments...)...).Output()
		if err != nil {
			return nil, err
		}
		var result []map[string]any
		err = json.Unmarshal(output, &result)
		return result, err
	}
	links, err := commandJSON("link", "show")
	if err != nil {
		return network.Topology{}, err
	}
	addresses, err := commandJSON("address", "show")
	if err != nil {
		return network.Topology{}, err
	}
	routes, err := commandJSON("-4", "route", "show", "table", "all")
	if err != nil {
		return network.Topology{}, err
	}
	addressMap := map[string][]any{}
	for _, item := range addresses {
		values, _ := item["addr_info"].([]any)
		addressMap[stringValue(item["ifname"])] = values
	}
	result := baseTopology()
	seenCIDR := map[string]bool{}
	for _, value := range result.LocalCIDRs {
		seenCIDR[value] = true
	}
	for _, link := range links {
		name := stringValue(link["ifname"])
		kind := "unknown"
		if name == "lo" {
			kind = "loopback"
		}
		if info, ok := link["linkinfo"].(map[string]any); ok && stringValue(info["info_kind"]) != "" {
			kind = stringValue(info["info_kind"])
		}
		entry := network.Interface{Name: name, Kind: kind, State: strings.ToLower(stringValue(link["operstate"])), MTU: link["mtu"], Loopback: name == "lo", Addresses: []network.Address{}, DefaultRoutes: []network.DefaultRoute{}}
		for _, raw := range addressMap[name] {
			address, _ := raw.(map[string]any)
			family := stringValue(address["family"])
			if family != "inet" && family != "inet6" {
				continue
			}
			cidr := fmt.Sprintf("%s/%d", stringValue(address["local"]), intValue(address["prefixlen"]))
			entry.Addresses = append(entry.Addresses, network.Address{Family: family, CIDR: cidr, Scope: stringValue(address["scope"])})
			if !seenCIDR[cidr] {
				seenCIDR[cidr] = true
				result.LocalCIDRs = append(result.LocalCIDRs, cidr)
			}
		}
		for _, route := range routes {
			if stringValue(route["dst"]) != "default" || stringValue(route["dev"]) != name {
				continue
			}
			var gateway, source *string
			if value := stringValue(route["gateway"]); value != "" {
				gateway = &value
			}
			if value := stringValue(route["prefsrc"]); value != "" {
				source = &value
			}
			entry.DefaultRoutes = append(entry.DefaultRoutes, network.DefaultRoute{Gateway: gateway, Source: source, Metric: intValue(route["metric"]), Table: stringValue(route["table"]), Protocol: stringValue(route["protocol"])})
		}
		result.Interfaces = append(result.Interfaces, entry)
	}
	sort.Strings(result.LocalCIDRs)
	return result, nil
}

func baseTopology() network.Topology {
	return network.Topology{Interfaces: []network.Interface{}, LocalCIDRs: []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8"}}
}

func platformDialer(interfaceName string, mark int) net.Dialer {
	return net.Dialer{Control: socketControl(interfaceName, mark)}
}

func platformSetTransportEnabled(ctx context.Context, coreService string, enabled bool) error {
	arguments := transportSystemctlArguments(coreService, enabled)
	action := arguments[0]
	if output, err := exec.CommandContext(ctx, "systemctl", arguments...).CombinedOutput(); err != nil {
		return fmt.Errorf("transport_%s_failed: %w: %s", action, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func transportSystemctlArguments(coreService string, enabled bool) []string {
	if enabled {
		return []string{"enable", "--now", "orcheroute-routing.service", coreService}
	}
	return []string{"disable", "--now", coreService, "orcheroute-routing.service"}
}

func socketControl(interfaceName string, mark int) func(string, string, syscall.RawConn) error {
	return func(_, _ string, raw syscall.RawConn) error {
		var controlErr error
		err := raw.Control(func(fd uintptr) {
			if mark > 0 {
				if value := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark); value != nil {
					controlErr = value
					return
				}
			}
			if interfaceName != "" {
				controlErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, interfaceName)
			}
		})
		if err != nil {
			return err
		}
		return controlErr
	}
}

func tryOperationLock(path string) (bool, func(), error) {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, func() {}, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		return false, func() {}, nil
	}
	return true, func() {
		_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		_ = lock.Close()
	}, nil
}
