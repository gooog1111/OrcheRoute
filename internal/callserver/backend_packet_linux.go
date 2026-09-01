//go:build linux

package callserver

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	awgtun "github.com/amnezia-vpn/amneziawg-go/v3/tun"
)

const packetNATTable = "orcheroute_call"

// EmbeddedPacketBackend is the packet endpoint behind FreeTURN. It embeds the
// userspace AWG implementation; the machine does not need wg/awg executables.
// Ordinary VLESS, Trojan and Hysteria2 listeners remain an independent Mihomo
// sidecar and are deliberately not routed through this backend.
type EmbeddedPacketBackend struct {
	MihomoBinary   string
	StateDirectory string
}

type embeddedPacketRuntime struct {
	once              sync.Once
	device            *awgdevice.Device
	ordinary          io.Closer
	interfaceName     string
	forwardingChanged bool
	forwardRules      [][]string
	peerClients       map[string]string
	lastTraffic       map[string]Traffic
}

func (backend EmbeddedPacketBackend) Start(ctx context.Context, snapshot RuntimeSnapshot) (io.Closer, error) {
	uapi, err := snapshot.Packet.UAPIConfig()
	if err != nil {
		return nil, err
	}
	if len(snapshot.Packet.Peers) == 0 {
		return nil, fmt.Errorf("call_server_no_active_clients")
	}
	tunDevice, err := awgtun.CreateTUN(snapshot.Packet.InterfaceName, 1280)
	if err != nil {
		return nil, fmt.Errorf("call_server_packet_tun_create: %w", err)
	}
	interfaceName, err := tunDevice.Name()
	if err != nil {
		_ = tunDevice.Close()
		return nil, fmt.Errorf("call_server_packet_tun_name: %w", err)
	}
	device := awgdevice.NewDevice(tunDevice, awgconn.NewDefaultBind(), awgdevice.NewLogger(awgdevice.LogLevelError, "(OrcheRoute Call) "))
	running := &embeddedPacketRuntime{device: device, interfaceName: interfaceName,
		peerClients: make(map[string]string, len(snapshot.Packet.Peers)), lastTraffic: make(map[string]Traffic, len(snapshot.Packet.Peers))}
	for _, peer := range snapshot.Packet.Peers {
		running.peerClients[hex.EncodeToString(peer.PublicKey)] = peer.ClientID
	}
	if err := device.IpcSet(uapi); err != nil {
		running.Close()
		return nil, fmt.Errorf("call_server_packet_configure: %w", err)
	}
	if err := running.configureNetwork(snapshot.Packet.InterfaceAddress); err != nil {
		running.Close()
		return nil, err
	}
	if err := device.Up(); err != nil {
		running.Close()
		return nil, fmt.Errorf("call_server_packet_start: %w", err)
	}
	if snapshot.Ordinary.Enabled {
		if backend.MihomoBinary == "" || backend.StateDirectory == "" {
			running.Close()
			return nil, fmt.Errorf("call_server_mihomo_unavailable")
		}
		ordinary, err := (ordinaryMihomoBackend{Binary: backend.MihomoBinary, StateDirectory: backend.StateDirectory}).Start(ctx, snapshot.Ordinary)
		if err != nil {
			running.Close()
			return nil, err
		}
		running.ordinary = ordinary
	}
	return running, nil
}

func (runtime *embeddedPacketRuntime) configureNetwork(address string) error {
	for _, binary := range []string{"ip", "nft", "iptables"} {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("call_server_dependency_missing:%s", binary)
		}
	}
	if err := runPacketCommand("ip", "address", "replace", address, "dev", runtime.interfaceName); err != nil {
		return err
	}
	if err := runPacketCommand("ip", "link", "set", "dev", runtime.interfaceName, "mtu", "1280", "up"); err != nil {
		return err
	}
	forwarding, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return fmt.Errorf("call_server_packet_forwarding_read: %w", err)
	}
	if strings.TrimSpace(string(forwarding)) != "1" {
		if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644); err != nil {
			return fmt.Errorf("call_server_packet_forwarding_enable: %w", err)
		}
		runtime.forwardingChanged = true
	}
	// NAT alone is insufficient on hosts where Docker, UFW or the system
	// firewall leaves FORWARD at DROP. Insert only the two interface-scoped
	// rules required by this tunnel. Rules that already existed remain owned by
	// the administrator and are not removed on shutdown.
	for _, rule := range packetForwardRules(runtime.interfaceName) {
		check := append([]string{"-w", "5", "-C", "FORWARD"}, rule...)
		if exec.Command("iptables", check...).Run() == nil {
			continue
		}
		insert := append([]string{"-w", "5", "-I", "FORWARD", "1"}, rule...)
		if err := runPacketCommand("iptables", insert...); err != nil {
			return err
		}
		runtime.forwardRules = append(runtime.forwardRules, append([]string(nil), rule...))
	}
	_ = exec.Command("nft", "delete", "table", "inet", packetNATTable).Run()
	commands := [][]string{
		{"add", "table", "inet", packetNATTable},
		{"add", "chain", "inet", packetNATTable, "postrouting", "{", "type", "nat", "hook", "postrouting", "priority", "srcnat", ";", "policy", "accept", ";", "}"},
		{"add", "rule", "inet", packetNATTable, "postrouting", "ip", "saddr", "10.77.0.0/16", "masquerade"},
	}
	for _, arguments := range commands {
		if err := runPacketCommand("nft", arguments...); err != nil {
			return err
		}
	}
	return nil
}

func packetForwardRules(interfaceName string) [][]string {
	return [][]string{
		{"-i", interfaceName, "-j", "ACCEPT"},
		{"-o", interfaceName, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
	}
}

func runPacketCommand(name string, arguments ...string) error {
	output, err := exec.Command(name, arguments...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("call_server_packet_network: %s %s: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (runtime *embeddedPacketRuntime) Alive() error {
	if runtime.device == nil {
		return fmt.Errorf("call_server_packet_stopped")
	}
	if _, err := runtime.device.IpcGet(); err != nil {
		return err
	}
	if runtime.ordinary != nil {
		if reporter, ok := runtime.ordinary.(healthReporter); ok {
			return reporter.Alive()
		}
	}
	return nil
}

func (runtime *embeddedPacketRuntime) DrainTraffic() map[string]Traffic {
	if runtime.device == nil {
		return nil
	}
	state, err := runtime.device.IpcGet()
	if err != nil {
		return nil
	}
	result := map[string]Traffic{}
	publicKey := ""
	traffic := Traffic{}
	flush := func() {
		if id := runtime.peerClients[publicKey]; id != "" {
			previous := runtime.lastTraffic[publicKey]
			delta := Traffic{RXBytes: counterDelta(traffic.RXBytes, previous.RXBytes), TXBytes: counterDelta(traffic.TXBytes, previous.TXBytes)}
			if delta.RXBytes != 0 || delta.TXBytes != 0 {
				result[id] = delta
			}
			runtime.lastTraffic[publicKey] = traffic
		}
		traffic = Traffic{}
	}
	for _, line := range strings.Split(state, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case "public_key":
			flush()
			publicKey = value
		case "rx_bytes":
			traffic.RXBytes, _ = strconv.ParseUint(value, 10, 64)
		case "tx_bytes":
			traffic.TXBytes, _ = strconv.ParseUint(value, 10, 64)
		}
	}
	flush()
	return result
}

func counterDelta(current, previous uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}

func (runtime *embeddedPacketRuntime) Close() error {
	runtime.once.Do(func() {
		if runtime.ordinary != nil {
			_ = runtime.ordinary.Close()
		}
		_ = exec.Command("nft", "delete", "table", "inet", packetNATTable).Run()
		for index := len(runtime.forwardRules) - 1; index >= 0; index-- {
			arguments := append([]string{"-w", "5", "-D", "FORWARD"}, runtime.forwardRules[index]...)
			_ = exec.Command("iptables", arguments...).Run()
		}
		runtime.forwardRules = nil
		if runtime.forwardingChanged {
			_ = os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("0\n"), 0o644)
		}
		if runtime.device != nil {
			runtime.device.Close()
			runtime.device = nil
		}
	})
	return nil
}
