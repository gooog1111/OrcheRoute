//go:build linux

package linuxnetwork

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

type dnsCommand func(context.Context, string, ...string) ([]byte, error)

// PhysicalNameServers is refreshed from the selected link, not from the
// system-wide stub or a file that becomes stale after DHCP/profile changes.
func PhysicalNameServers(ctx context.Context, iface string, fallback []string) []string {
	return physicalNameServers(ctx, iface, fallback, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	})
}

func physicalNameServers(ctx context.Context, iface string, fallback []string, run dnsCommand) []string {
	if iface != "" {
		for _, command := range [][]string{
			{"nmcli", "--wait", "1", "--terse", "--escape", "no", "--get-values", "IP4.DNS,IP6.DNS", "device", "show", iface},
			{"resolvectl", "dns", iface},
		} {
			attempt, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
			payload, err := run(attempt, command[0], command[1:]...)
			cancel()
			if err == nil {
				if servers := usableNameServers(strings.Fields(string(payload))); len(servers) > 0 {
					return servers
				}
			}
			if ctx.Err() != nil {
				break
			}
		}
	}
	return usableNameServers(fallback)
}

func usableNameServers(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		// nmcli versions may separate multiple values with commas or pipes.
		for _, token := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '|' }) {
			address, err := netip.ParseAddr(strings.TrimSpace(token))
			if err != nil {
				continue
			}
			address = address.Unmap()
			if address.IsLoopback() || address.IsUnspecified() || address.IsMulticast() || netip.MustParsePrefix("198.18.0.0/15").Contains(address) {
				continue
			}
			if address.Is6() && address.IsLinkLocalUnicast() && address.Zone() == "" {
				continue
			}
			server := net.JoinHostPort(address.String(), "53")
			if !seen[server] {
				seen[server] = true
				result = append(result, server)
			}
		}
	}
	return result
}

// BoundResolver replaces resolver endpoints, never the socket's physical
// binding. A local stub cannot be reached on a non-loopback interface.
func BoundResolver(dial func(context.Context, string, string) (net.Conn, error), servers []string, bound bool) *net.Resolver {
	var next atomic.Uint64
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		if len(servers) == 0 {
			host, _, err := net.SplitHostPort(address)
			ip := net.ParseIP(host)
			if bound && (err != nil || ip == nil || ip.IsLoopback() || ip.IsUnspecified()) {
				return nil, fmt.Errorf("physical_dns_unavailable")
			}
			return dial(ctx, network, address)
		}
		start := int((next.Add(1) - 1) % uint64(len(servers)))
		var last error
		for offset := 0; offset < len(servers); offset++ {
			connection, err := dial(ctx, network, servers[(start+offset)%len(servers)])
			if err == nil {
				return connection, nil
			}
			last = err
			if ctx.Err() != nil {
				break
			}
		}
		return nil, last
	}}
}
