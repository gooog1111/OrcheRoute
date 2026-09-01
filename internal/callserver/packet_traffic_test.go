//go:build linux

package callserver

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestPacketTrafficUsesDeltasAndHandlesDeviceReset(t *testing.T) {
	if got := counterDelta(120, 100); got != 20 {
		t.Fatalf("delta = %d, want 20", got)
	}
	if got := counterDelta(7, 100); got != 7 {
		t.Fatalf("reset delta = %d, want 7", got)
	}
}

func TestEmbeddedPacketBackendLifecycle(t *testing.T) {
	if os.Getenv("ORCHEROUTE_PACKET_INTEGRATION") != "1" {
		t.Skip("set ORCHEROUTE_PACKET_INTEGRATION=1 inside an isolated network namespace")
	}
	privateKey := make([]byte, 32)
	privateKey[0] = 1
	publicKey := make([]byte, 32)
	publicKey[0] = 2
	snapshot := RuntimeSnapshot{Packet: PacketSnapshot{
		InterfaceName: "orchetest0", InterfaceAddress: "10.77.0.1/16", ListenAddress: "127.0.0.1:18443",
		PrivateKey: privateKey, ObfuscationKey: make([]byte, 32),
		Peers: []PacketPeer{{ClientID: "test", PublicKey: publicKey, Address: mustPacketAddress(t, "10.77.0.2")}},
	}}
	running, err := (EmbeddedPacketBackend{}).Start(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if reporter, ok := running.(healthReporter); !ok || reporter.Alive() != nil {
		t.Fatal("packet backend did not become healthy")
	}
	if err := exec.Command("ip", "link", "show", "dev", "orchetest0").Run(); err != nil {
		t.Fatalf("packet interface missing: %v", err)
	}
	if err := running.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("ip", "link", "show", "dev", "orchetest0").Run(); err == nil {
		t.Fatal("packet interface survived backend close")
	}
}
