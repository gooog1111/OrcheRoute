package serverruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mobileconnectivity "github.com/gooog1111/orcheroute/internal/mobile/connectivity"
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
	restricted := runtime.connectivitySnapshot()
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

func TestStatusUsesPhysicalConnectivitySnapshot(t *testing.T) {
	runtime := cleanTestRuntime(t)
	if err := atomicJSON(runtime.connectivityPath(), ConnectivitySnapshot{
		State: mobileconnectivity.Allowlist, UpdatedAt: 123, DirectInterface: "wan0",
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
