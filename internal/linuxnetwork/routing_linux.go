//go:build linux

package linuxnetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/gooog1111/orcheroute/internal/network"
)

func Discover(ctx context.Context) (network.Topology, error) {
	commandJSON := func(arguments ...string) ([]map[string]any, error) {
		command := exec.CommandContext(ctx, "ip", append([]string{"-j"}, arguments...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("ip %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
		}
		var value []map[string]any
		err = json.Unmarshal(output, &value)
		return value, err
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
		addressMap[text(item["ifname"])] = values
	}
	local := map[string]bool{}
	for _, value := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8"} {
		local[value] = true
	}
	topology := network.Topology{Interfaces: []network.Interface{}}
	for _, link := range links {
		name := text(link["ifname"])
		kind := "unknown"
		if name == "lo" {
			kind = "loopback"
		}
		if info, ok := link["linkinfo"].(map[string]any); ok && text(info["info_kind"]) != "" {
			kind = text(info["info_kind"])
		}
		item := network.Interface{Name: name, Kind: kind, State: strings.ToLower(text(link["operstate"])), MTU: link["mtu"], Loopback: name == "lo", Addresses: []network.Address{}, DefaultRoutes: []network.DefaultRoute{}}
		for _, raw := range addressMap[name] {
			address, _ := raw.(map[string]any)
			family := text(address["family"])
			if family != "inet" && family != "inet6" {
				continue
			}
			cidr := fmt.Sprintf("%s/%d", text(address["local"]), integer(address["prefixlen"]))
			item.Addresses = append(item.Addresses, network.Address{Family: family, CIDR: cidr, Scope: text(address["scope"])})
			if prefix, parseErr := netip.ParsePrefix(cidr); parseErr == nil {
				local[prefix.Masked().String()] = true
				local[netip.PrefixFrom(prefix.Addr(), prefix.Addr().BitLen()).String()] = true
			}
		}
		for _, route := range routes {
			if text(route["dst"]) != "default" || text(route["dev"]) != name {
				continue
			}
			var gateway, source *string
			if value := text(route["gateway"]); value != "" {
				gateway = &value
			}
			if value := text(route["prefsrc"]); value != "" {
				source = &value
			}
			item.DefaultRoutes = append(item.DefaultRoutes, network.DefaultRoute{Gateway: gateway, Source: source, Metric: integer(route["metric"]), Table: text(route["table"]), Protocol: text(route["protocol"])})
		}
		topology.Interfaces = append(topology.Interfaces, item)
	}
	for value := range local {
		topology.LocalCIDRs = append(topology.LocalCIDRs, value)
	}
	sort.Slice(topology.LocalCIDRs, func(i, j int) bool {
		left, _ := netip.ParsePrefix(topology.LocalCIDRs[i])
		right, _ := netip.ParsePrefix(topology.LocalCIDRs[j])
		if left.Addr().BitLen() != right.Addr().BitLen() {
			return left.Addr().BitLen() < right.Addr().BitLen()
		}
		if left.Addr() != right.Addr() {
			return left.Addr().Less(right.Addr())
		}
		return left.Bits() < right.Bits()
	})
	return topology, nil
}

func Apply(ctx context.Context, profile network.ProfileInput) (network.Preview, error) {
	topology, err := Discover(ctx)
	if err != nil {
		return network.Preview{}, err
	}
	preview, err := network.PreviewProfile(profile, topology)
	if err != nil {
		return network.Preview{}, err
	}
	if err := Remove(ctx); err != nil {
		return network.Preview{}, err
	}
	installed := false
	defer func() {
		if !installed {
			_ = Remove(context.Background())
		}
	}()
	for _, name := range []string{"direct", "vpn_underlay"} {
		if err := installRole(ctx, preview.ResolvedRoles[name], topology); err != nil {
			return network.Preview{}, err
		}
	}
	installed = true
	return preview, nil
}

func Remove(ctx context.Context) error {
	var first error
	for _, spec := range []struct{ priority, table int }{{90, 5351}, {91, 5352}} {
		for count := 0; count < 8; count++ {
			result := exec.CommandContext(ctx, "ip", "-4", "rule", "del", "priority", strconv.Itoa(spec.priority)).Run()
			if result != nil {
				break
			}
		}
		if output, err := exec.CommandContext(ctx, "ip", "-4", "route", "flush", "table", strconv.Itoa(spec.table)).CombinedOutput(); err != nil && !routingTableMissing(string(output)) && first == nil {
			first = fmt.Errorf("flush table %d: %w: %s", spec.table, err, strings.TrimSpace(string(output)))
		}
	}
	return first
}

func routingTableMissing(output string) bool {
	message := strings.ToLower(output)
	return strings.Contains(message, "fib table does not exist") ||
		strings.Contains(message, "routing table does not exist") ||
		strings.Contains(message, "table does not exist") ||
		strings.Contains(message, "no such file or directory")
}

func installRole(ctx context.Context, role network.ResolvedRole, topology network.Topology) error {
	table := strconv.Itoa(role.Table)
	if role.Source != nil {
		for _, item := range topology.Interfaces {
			if item.Name != role.Interface {
				continue
			}
			for _, address := range item.Addresses {
				prefix, err := netip.ParsePrefix(address.CIDR)
				if err != nil || prefix.Addr().String() != *role.Source || prefix.Bits() >= 32 {
					continue
				}
				if err := run(ctx, "ip", "-4", "route", "replace", "table", table, prefix.Masked().String(), "dev", role.Interface, "scope", "link", "src", *role.Source); err != nil {
					return err
				}
			}
		}
	}
	if role.Gateway != nil {
		if err := run(ctx, "ip", "-4", "route", "replace", "table", table, *role.Gateway+"/32", "dev", role.Interface, "scope", "link"); err != nil {
			return err
		}
	}
	arguments := []string{"ip", "-4", "route", "replace", "table", table, "default"}
	if role.Gateway != nil {
		arguments = append(arguments, "via", *role.Gateway)
	}
	arguments = append(arguments, "dev", role.Interface)
	if role.Source != nil {
		arguments = append(arguments, "src", *role.Source)
	}
	if err := run(ctx, arguments...); err != nil {
		return err
	}
	return run(ctx, "ip", "-4", "rule", "add", "priority", strconv.Itoa(role.Priority), "fwmark", fmt.Sprintf("0x%x/0xffff", role.Mark), "lookup", table)
}

func run(ctx context.Context, arguments ...string) error {
	output, err := exec.CommandContext(ctx, arguments[0], arguments[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
func text(value any) string {
	if current, ok := value.(string); ok {
		return current
	}
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
func integer(value any) int {
	switch current := value.(type) {
	case float64:
		return int(current)
	case int:
		return current
	case json.Number:
		result, _ := strconv.Atoi(current.String())
		return result
	}
	return 0
}
