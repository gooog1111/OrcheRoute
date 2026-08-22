//go:build windows

package serverruntime

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/network"
	"golang.org/x/sys/windows"
)

func platformDefaultConfig() Config {
	root := os.Getenv("ProgramData")
	if root == "" {
		root = `C:\ProgramData`
	}
	root = filepath.Join(root, "OrcheRoute")
	bin := filepath.Join(root, "bin")
	return Config{Listen: "127.0.0.1:19100", WebListen: ":19110", WebTLSListen: ":19111",
		ProductionState: filepath.Join(root, "state"), StateDirectory: filepath.Join(root, "state"),
		WebRoot: filepath.Join(root, "webui"), RuntimeEnv: filepath.Join(root, "runtime.env"), ConfigDirectory: root,
		MihomoAPI: "http://127.0.0.1:19090", MihomoBinary: filepath.Join(bin, "mihomo.exe"),
		UpdateBinary: filepath.Join(bin, "orcheroute-update-go.exe"), NetworkBinary: filepath.Join(bin, "orcheroute-network-go.exe"),
		ComponentBinary: filepath.Join(bin, "orcheroute-components-go.exe"), SelfUpdateBinary: filepath.Join(bin, "orcheroute-self-update.exe"), CoreService: "OrcheRouteMihomo", ControllerEvery: 10 * time.Second,
		ConnectivityEvery: 10 * time.Second, ConnectivityTimeout: 6 * time.Second,
		RequireAPIAuth: true}
}

func discoverTopology(_ context.Context) (network.Topology, error) {
	result := baseTopology()
	seenCIDR := map[string]bool{}
	for _, value := range result.LocalCIDRs {
		seenCIDR[value] = true
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return network.Topology{}, err
	}
	for _, current := range interfaces {
		loopback := current.Flags&net.FlagLoopback != 0
		state := "down"
		if current.Flags&net.FlagUp != 0 {
			state = "up"
		}
		kind := "ethernet"
		if loopback {
			kind = "loopback"
		} else if strings.Contains(strings.ToLower(current.Name), "wi-fi") || strings.Contains(strings.ToLower(current.Name), "wireless") {
			kind = "wifi"
		}
		entry := network.Interface{Name: current.Name, Kind: kind, State: state, MTU: current.MTU, Loopback: loopback, Addresses: []network.Address{}, DefaultRoutes: []network.DefaultRoute{}}
		addresses, _ := current.Addrs()
		for _, raw := range addresses {
			ip, prefix, err := net.ParseCIDR(raw.String())
			if err != nil {
				continue
			}
			ones, _ := prefix.Mask.Size()
			family := "inet6"
			if ip.To4() != nil {
				family = "inet"
			}
			cidr := ip.String() + "/" + itoa(ones)
			scope := "global"
			if ip.IsLoopback() {
				scope = "host"
			} else if ip.IsLinkLocalUnicast() {
				scope = "link"
			}
			entry.Addresses = append(entry.Addresses, network.Address{Family: family, CIDR: cidr, Scope: scope})
			if !seenCIDR[cidr] {
				seenCIDR[cidr] = true
				result.LocalCIDRs = append(result.LocalCIDRs, cidr)
			}
		}
		result.Interfaces = append(result.Interfaces, entry)
	}
	sort.Strings(result.LocalCIDRs)
	return result, nil
}

func baseTopology() network.Topology {
	return network.Topology{Interfaces: []network.Interface{}, LocalCIDRs: []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8"}}
}

func platformDialer(interfaceName string, _ int) net.Dialer {
	dialer := net.Dialer{}
	if interfaceName == "" {
		return dialer
	}
	current, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return dialer
	}
	addresses, _ := current.Addrs()
	for _, raw := range addresses {
		ip, _, err := net.ParseCIDR(raw.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
			break
		}
	}
	return dialer
}

func platformSetTransportEnabled(_ context.Context, _ string, _ bool) error { return nil }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	buffer := [3]byte{}
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[position:])
}

func tryOperationLock(path string) (bool, func(), error) {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, func() {}, err
	}
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	if err != nil {
		_ = lock.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return false, func() {}, nil
		}
		return false, func() {}, err
	}
	return true, func() {
		_ = windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, overlapped)
		_ = lock.Close()
	}, nil
}
