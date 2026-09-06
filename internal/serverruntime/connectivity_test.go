package serverruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mobileconnectivity "github.com/gooog1111/orcheroute/internal/core/connectivity"
	"github.com/gooog1111/orcheroute/internal/network"
)

func TestServerConnectivityMonitorUsesDirectInterfaceAndHysteresis(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("controller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	config.RequireAPIAuth = false
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	profile := network.DefaultProfile("direct-test0")
	if err := atomicJSON(filepath.Join(directory, "network-active.json"), profile); err != nil {
		t.Fatal(err)
	}
	available := map[string]bool{
		"allowlist": true, "open_internet": true, "open_anchor_github": true,
	}
	runtime.connectivityProbeFactory = func(interfaceName string, timeout time.Duration) mobileconnectivity.Probe {
		if interfaceName != "direct-test0" {
			t.Fatalf("probe interface=%q", interfaceName)
		}
		if timeout != 3*time.Second {
			t.Fatalf("probe timeout=%s", timeout)
		}
		return func(_ context.Context, target mobileconnectivity.Target) bool { return available[target.Name] }
	}

	runtime.connectivityCycle(context.Background())
	snapshot := runtime.connectivitySnapshot()
	if snapshot.State != mobileconnectivity.Normal || snapshot.ObservedState != mobileconnectivity.Normal {
		t.Fatalf("normal snapshot=%#v", snapshot)
	}

	available = map[string]bool{"allowlist": true}
	runtime.connectivityCycle(context.Background())
	first := runtime.connectivitySnapshot()
	if first.State != mobileconnectivity.Normal || first.CandidateState != mobileconnectivity.Allowlist || first.CandidateCount != 1 {
		t.Fatalf("first allowlist observation=%#v", first)
	}
	runtime.connectivityCycle(context.Background())
	second := runtime.connectivitySnapshot()
	if second.State != mobileconnectivity.Normal || second.CandidateCount != 2 {
		t.Fatalf("second allowlist observation=%#v", second)
	}
	runtime.connectivityCycle(context.Background())
	restricted := runtime.connectivitySnapshot()
	if restricted.State != mobileconnectivity.Normal {
		t.Fatal("three rapid observations must not bypass the minimum hold")
	}
	// Simulate the hold elapsing without sleeping or using the real network.
	restricted.CandidateSinceMS = time.Now().Add(-21 * time.Second).UnixMilli()
	if err := atomicJSON(runtime.connectivityPath(), restricted); err != nil {
		t.Fatal(err)
	}
	runtime.connectivityCycle(context.Background())
	restricted = runtime.connectivitySnapshot()
	if restricted.State != mobileconnectivity.Allowlist || !restricted.Changed {
		t.Fatalf("confirmed allowlist=%#v", restricted)
	}

	available = map[string]bool{}
	for index := 0; index < 2; index++ {
		runtime.connectivityCycle(context.Background())
		if got := runtime.connectivitySnapshot().State; got != mobileconnectivity.Allowlist {
			t.Fatalf("transient offline cycle %d changed state to %q", index+1, got)
		}
	}
	runtime.connectivityCycle(context.Background())
	if got := runtime.connectivitySnapshot().State; got != mobileconnectivity.Offline {
		t.Fatalf("third offline state=%q", got)
	}
}

func TestServerConnectivityMonitorResetsCandidateOnSameInterfaceNewNetwork(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("controller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	config.RequireAPIAuth = false
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	profile := network.DefaultProfile("direct-test0")
	if err := atomicJSON(filepath.Join(directory, "network-active.json"), profile); err != nil {
		t.Fatal(err)
	}
	runtime.connectivityProbeFactory = func(string, time.Duration) mobileconnectivity.Probe {
		return func(_ context.Context, target mobileconnectivity.Target) bool { return target.Name == "allowlist" }
	}
	gateway := "203.0.113.1"
	topology := network.Topology{Interfaces: []network.Interface{{
		Name:          "direct-test0",
		Addresses:     []network.Address{{Family: "inet", CIDR: "203.0.113.10/24"}},
		DefaultRoutes: []network.DefaultRoute{{Gateway: &gateway}},
	}}}
	runtime.connectivityTopologyLookup = func(context.Context) (network.Topology, error) { return topology, nil }

	// Two allowlist observations build a candidate, still short of the
	// three required to confirm — the interface and its attached network
	// have not changed yet.
	runtime.connectivityCycle(context.Background())
	runtime.connectivityCycle(context.Background())
	building := runtime.connectivitySnapshot()
	if building.CandidateState != mobileconnectivity.Allowlist || building.CandidateCount != 2 {
		t.Fatalf("candidate before network change=%#v", building)
	}

	// Same interface name, but DHCP handed out a different address and
	// gateway: this must be treated as a new network, not a continuation of
	// the allowlist candidate observed on the old one.
	otherGateway := "198.51.100.1"
	topology = network.Topology{Interfaces: []network.Interface{{
		Name:          "direct-test0",
		Addresses:     []network.Address{{Family: "inet", CIDR: "198.51.100.20/24"}},
		DefaultRoutes: []network.DefaultRoute{{Gateway: &otherGateway}},
	}}}
	runtime.connectivityCycle(context.Background())
	afterChange := runtime.connectivitySnapshot()
	if afterChange.CandidateCount != 1 {
		t.Fatalf("candidate must restart at 1 after same-name network change, got=%#v", afterChange)
	}
	if afterChange.DirectNetwork == building.DirectNetwork {
		t.Fatalf("direct network fingerprint must change with the gateway: before=%q after=%q", building.DirectNetwork, afterChange.DirectNetwork)
	}
}

func TestStatusUsesPhysicalConnectivitySnapshot(t *testing.T) {
	runtime := cleanTestRuntime(t)
	if err := atomicJSON(runtime.connectivityPath(), ConnectivitySnapshot{
		State: mobileconnectivity.Allowlist, UpdatedAt: 123, ConfirmedAt: 120, DirectInterface: "wan0",
	}); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(runtime.identityPath(), IdentitySnapshot{
		Direct: &mobileconnectivity.Identity{IP: "198.51.100.10", CountryCode: "US", Region: "USA", Flag: "🇺🇸"},
	}); err != nil {
		t.Fatal(err)
	}
	status, payload := runtime.getStatus(context.Background())
	if status != 200 {
		t.Fatalf("status=%d payload=%#v", status, payload)
	}
	wan := payload.(map[string]any)["wan"].(map[string]any)
	if wan["mode"] != mobileconnectivity.Allowlist || wan["available"] != true {
		t.Fatalf("wan=%#v", wan)
	}
	identity := wan["identity"].(*mobileconnectivity.Identity)
	if identity.IP != "198.51.100.10" || identity.CountryCode != "US" {
		t.Fatalf("identity=%#v", identity)
	}
	proxy := payload.(map[string]any)["proxy"].(map[string]any)
	if proxy["last_switch"] != int64(120) {
		t.Fatalf("proxy=%#v", proxy)
	}
}

func cleanTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("controller_secret=test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.StateDirectory, config.ProductionState = directory, directory
	config.ConfigDirectory, config.RuntimeEnv = directory, runtimeEnv
	config.RequireAPIAuth = false
	runtime, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	return runtime
}
