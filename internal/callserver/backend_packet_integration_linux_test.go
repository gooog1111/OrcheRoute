//go:build linux

package callserver

import (
	"os"
	"os/exec"
	"testing"
)

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
	if err := runtime.configureNetwork("10.77.0.1/16"); err != nil {
		t.Fatal(err)
	}
	for _, rule := range packetForwardRules(interfaceName) {
		arguments := append([]string{"-C", "FORWARD"}, rule...)
		if output, err := exec.Command("iptables", arguments...).CombinedOutput(); err != nil {
			t.Fatalf("forward rule was not installed: %v: %s", err, output)
		}
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	for _, rule := range packetForwardRules(interfaceName) {
		arguments := append([]string{"-C", "FORWARD"}, rule...)
		if exec.Command("iptables", arguments...).Run() == nil {
			t.Fatalf("forward rule remained after shutdown: %v", rule)
		}
	}
}
