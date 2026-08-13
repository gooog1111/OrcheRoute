package serverruntime

import (
	"testing"

	"github.com/gooog1111/orcheroute/internal/network"
)

func TestBootstrapInterfacePrefersPhysicalMainDefaultRoute(t *testing.T) {
	topology := network.Topology{Interfaces: []network.Interface{
		{Name: "tun0", State: "up", DefaultRoutes: []network.DefaultRoute{{Metric: 0, Table: "main"}}},
		{Name: "ppp0", State: "unknown", DefaultRoutes: []network.DefaultRoute{{Metric: 10, Table: "main"}}},
	}}
	if got := bootstrapInterface(topology); got != "ppp0" {
		t.Fatalf("bootstrapInterface() = %q, want ppp0", got)
	}
}

func TestBootstrapInterfaceFallsBackToUsableInterface(t *testing.T) {
	topology := network.Topology{Interfaces: []network.Interface{
		{Name: "lo", Loopback: true, State: "up"},
		{Name: "enp1s0", State: "up"},
	}}
	if got := bootstrapInterface(topology); got != "enp1s0" {
		t.Fatalf("bootstrapInterface() = %q, want enp1s0", got)
	}
}
