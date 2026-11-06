package serverruntime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func TestBootstrapRetiresUnavailableSourceWithoutDeletingServers(t *testing.T) {
	runtime := cleanTestRuntime(t)
	ctx := context.Background()
	_, err := runtime.Store.Create(ctx, subscriptions.Subscription{ID: "ebrasha-public", Name: "EbraSha", GroupName: subscriptions.Emergency, Parser: subscriptions.Standard, Secret: "https://raw.githubusercontent.com/ebrasha/free-v2ray-public-list/refs/heads/main/V2Ray-Config-By-EbraSha.txt", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runtime.Config.StateDirectory, "providers", "emergency.json")
	payload := []byte(`{"proxies":[{"name":"saved","type":"socks5","server":"192.0.2.1","port":443}]}`)
	if err = os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err = runtime.bootstrap(ctx, false); err != nil {
		t.Fatal(err)
	}
	item, err := runtime.Store.Get(ctx, "ebrasha-public", true)
	if err != nil || item == nil || item.Enabled {
		t.Fatalf("retired source still active: %+v %v", item, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, payload) {
		t.Fatal("cached server list was modified")
	}
}

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
