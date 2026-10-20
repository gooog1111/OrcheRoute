//go:build linux

package callserver

import (
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPacketPerClientLANKeepsIsolationFirst(t *testing.T) {
	allow := true
	rules := packetClientForwardRules("orchecall0", false, []PacketPeer{{Address: netip.MustParseAddr("10.77.0.2"), AllowLAN: &allow}})
	if strings.Join(rules[0], " ") != "-i orchecall0 -o orchecall0 -j DROP" {
		t.Fatal(rules)
	}
	if strings.Join(rules[1], " ") != "-i orchecall0 -s 10.77.0.2 -d 10.0.0.0/8 -j ACCEPT" {
		t.Fatal(rules)
	}
	if indexOfRule(rules, []string{"-i", "orchecall0", "-d", "10.0.0.0/8", "-j", "DROP"}) < 2 {
		t.Fatal("global block must follow client override")
	}
	input := packetClientInputRules("orchecall0", false, []PacketPeer{{Address: netip.MustParseAddr("10.77.0.2"), AllowLAN: &allow}})
	if strings.Join(input[0], " ") != "-i orchecall0 -s 10.77.0.2 -d 10.0.0.0/8 -j ACCEPT" {
		t.Fatal(input)
	}
	if indexOfRule(input, []string{"-i", "orchecall0", "-d", "10.0.0.0/8", "-j", "DROP"}) < 1 {
		t.Fatal("server-local address bypass")
	}
}

func TestPacketForwardRulesAreScopedToTunnelInterface(t *testing.T) {
	rules := packetForwardRules("orchecall0", false)
	joined := fmt.Sprint(rules)
	for _, required := range []string{
		"-i orchecall0 -j ACCEPT", "-o orchecall0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT",
		"-i orchecall0 -o orchecall0 -j DROP", "-i orchecall0 -d 10.0.0.0/8 -j DROP", "-i orchecall0 -d 192.168.0.0/16 -j DROP",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("forward rules %q do not contain %q", joined, required)
		}
	}
	if strings.Contains(joined, "0.0.0.0/0") {
		t.Fatalf("forward rules must remain interface-scoped: %q", joined)
	}
	acceptIndex := indexOfRule(rules, []string{"-i", "orchecall0", "-j", "ACCEPT"})
	peerIndex := indexOfRule(rules, []string{"-i", "orchecall0", "-o", "orchecall0", "-j", "DROP"})
	lanIndex := indexOfRule(rules, []string{"-i", "orchecall0", "-d", "10.0.0.0/8", "-j", "DROP"})
	if peerIndex < 0 || lanIndex < 0 || acceptIndex < 0 || peerIndex >= acceptIndex || lanIndex >= acceptIndex {
		t.Fatalf("peer isolation and LAN block must precede the generic accept: peer=%d lan=%d accept=%d", peerIndex, lanIndex, acceptIndex)
	}
}

func TestPacketForwardRulesAllowLANOmitsBlockButKeepsPeerIsolation(t *testing.T) {
	rules := packetForwardRules("orchecall0", true)
	joined := fmt.Sprint(rules)
	if !strings.Contains(joined, "-i orchecall0 -o orchecall0 -j DROP") {
		t.Fatalf("peer isolation must remain even when LAN access is allowed: %q", joined)
	}
	for _, cidr := range privateIPv4CIDRs {
		if strings.Contains(joined, cidr) {
			t.Fatalf("allowLAN must omit the LAN block, found %s in %q", cidr, joined)
		}
	}
}

func indexOfRule(rules [][]string, target []string) int {
	for index, rule := range rules {
		if fmt.Sprint(rule) == fmt.Sprint(target) {
			return index
		}
	}
	return -1
}

func TestPacketRouteConflictRejectsLegacyReverseVPN(t *testing.T) {
	routes := "default via 81.26.129.239 dev ppp0\n10.77.0.0/24 dev or-reverse src 10.77.0.1\n"
	conflict, found, err := packetRouteConflict(routes, "10.77.0.1/16", "orchecall0")
	if err != nil || !found || conflict != "10.77.0.0/24:or-reverse" {
		t.Fatalf("legacy route conflict was not detected: conflict=%q found=%v err=%v", conflict, found, err)
	}
}

func TestPacketRouteConflictAllowsOwnAndUnrelatedRoutes(t *testing.T) {
	routes := "default via 81.26.129.239 dev ppp0\n10.42.0.0/24 dev eth0\n10.77.0.0/16 dev orchecall0 src 10.77.0.1\n"
	conflict, found, err := packetRouteConflict(routes, "10.77.0.1/16", "orchecall0")
	if err != nil || found {
		t.Fatalf("valid route table was rejected: conflict=%q found=%v err=%v", conflict, found, err)
	}
}

// Run inside an isolated network namespace:
//
//	unshare -n env ORCHEROUTE_PACKET_INTEGRATION=1 go test ./internal/callserver -run TestPacketForwardingLifecycle -v
func TestPacketForwardingLifecycle(t *testing.T) {
	if os.Getenv("ORCHEROUTE_PACKET_INTEGRATION") != "1" {
		t.Skip("set ORCHEROUTE_PACKET_INTEGRATION=1 inside an isolated network namespace")
	}
	for _, binary := range []string{"ip", "nft", "iptables"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Fatalf("missing integration dependency %s: %v", binary, err)
		}
	}
	const interfaceName = "orchefwtest0"
	if output, err := exec.Command("ip", "link", "add", interfaceName, "type", "dummy").CombinedOutput(); err != nil {
		t.Fatalf("create dummy interface: %v: %s", err, output)
	}
	defer exec.Command("ip", "link", "delete", interfaceName).Run()
	if output, err := exec.Command("iptables", "-P", "FORWARD", "DROP").CombinedOutput(); err != nil {
		t.Fatalf("set isolated FORWARD policy: %v: %s", err, output)
	}

	runtime := &embeddedPacketRuntime{interfaceName: interfaceName}
	allow := true
	peers := []PacketPeer{{Address: netip.MustParseAddr("10.77.0.2"), AllowLAN: &allow}}
	if err := runtime.configureNetwork("10.77.0.1/16", false, peers); err != nil {
		t.Fatal(err)
	}
	for _, rule := range packetClientForwardRules(interfaceName, false, peers) {
		arguments := append([]string{"-C", "FORWARD"}, rule...)
		if output, err := exec.Command("iptables", arguments...).CombinedOutput(); err != nil {
			t.Fatalf("forward rule was not installed: %v: %s", err, output)
		}
	}
	for _, rule := range packetClientInputRules(interfaceName, false, peers) {
		if output, err := exec.Command("iptables", append([]string{"-C", "INPUT"}, rule...)...).CombinedOutput(); err != nil {
			t.Fatalf("input rule missing: %v %s", err, output)
		}
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	for _, rule := range packetClientForwardRules(interfaceName, false, peers) {
		arguments := append([]string{"-C", "FORWARD"}, rule...)
		if exec.Command("iptables", arguments...).Run() == nil {
			t.Fatalf("forward rule remained after shutdown: %v", rule)
		}
	}
	for _, rule := range packetClientInputRules(interfaceName, false, peers) {
		if exec.Command("iptables", append([]string{"-C", "INPUT"}, rule...)...).Run() == nil {
			t.Fatal("input rule remained after shutdown", rule)
		}
	}
}
