package callserver

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

func TestPacketForwardRulesAreScopedToTunnelInterface(t *testing.T) {
	rules := packetForwardRules("orchecall0")
	joined := fmt.Sprint(rules)
	for _, required := range []string{"-i orchecall0 -j ACCEPT", "-o orchecall0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("forward rules %q do not contain %q", joined, required)
		}
	}
	if strings.Contains(joined, "0.0.0.0/0") {
		t.Fatalf("forward rules must remain interface-scoped: %q", joined)
	}
}

func mustPacketAddress(t *testing.T, value string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func TestRuntimeSnapshotBuildsOnePacketPeerPerAvailableClient(t *testing.T) {
	manager, _ := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Packet.Peers) != 1 || snapshot.Packet.Peers[0].ClientID != client.ID || snapshot.Packet.Peers[0].Address.String() != "10.77.0.2" {
		t.Fatalf("unexpected packet peers: %#v", snapshot.Packet.Peers)
	}
	config, err := snapshot.Packet.UAPIConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"listen_port=18443", "private_key=" + hex.EncodeToString(snapshot.Packet.PrivateKey), "allowed_ip=10.77.0.2/32"} {
		if !strings.Contains(config, expected) {
			t.Fatalf("UAPI config missing %q: %s", expected, config)
		}
	}
}

func TestPacketAddressesRemainStableAfterDeletingAnotherClient(t *testing.T) {
	manager, _ := configuredManager(t)
	first, _ := manager.CreateClient(CreateClientInput{Name: "First"})
	second, _ := manager.CreateClient(CreateClientInput{Name: "Second"})
	before := manager.data.Clients[1].Profile.PacketTunnel.Config
	if err := manager.DeleteClient(first.ID); err != nil {
		t.Fatal(err)
	}
	if manager.data.Clients[0].ID != second.ID || manager.data.Clients[0].Profile.PacketTunnel.Config != before {
		t.Fatal("deleting a client changed another client's packet identity")
	}
}
