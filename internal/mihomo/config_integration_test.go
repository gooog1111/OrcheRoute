package mihomo

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestGeneratedCleanInstallConfigWithMihomo is opt-in because CI does not
// bundle Mihomo. Release builds set ORCHEROUTE_MIHOMO_BINARY so the exact core
// shipped in a package validates the clean-install scaffold.
func TestGeneratedCleanInstallConfigWithMihomo(t *testing.T) {
	binary := os.Getenv("ORCHEROUTE_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("ORCHEROUTE_MIHOMO_BINARY is not set")
	}
	directory := t.TempDir()
	for _, child := range []string{"providers", "rules"} {
		if err := os.MkdirAll(filepath.Join(directory, child), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, pool := range []string{"primary", "emergency"} {
		writeIntegrationJSON(t, filepath.Join(directory, "providers", pool+".json"), map[string]any{"proxies": []any{}})
	}
	for _, name := range []string{"direct", "proxy", "block"} {
		if err := os.WriteFile(filepath.Join(directory, "rules", name+".txt"), []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	platform := ""
	if runtime.GOOS == "windows" {
		platform = "windows"
	}
	config, err := Build(Input{
		StateDir: directory, Platform: platform, TestURL: "https://www.gstatic.com/generate_204",
		Secret: "integration-secret", RouteDefault: "proxy",
		Network: Network{
			Profile:              Profile{Capture: Capture{Mode: "system", DNSHijack: true, StrictRoute: true}},
			ResolvedRoles:        map[string]Role{"direct": {Interface: "Ethernet", Mark: 110}, "vpn_underlay": {Interface: "Ethernet", Mark: 120}},
			EffectiveBypassCIDRs: []string{"127.0.0.0/8", "192.168.0.0/16"},
			DNS: DNS{Config: DNSConfig{CacheAlgorithm: "arc"}, Effective: DNSEffective{
				Bootstrap: []string{"1.1.1.1"}, Proxy: []string{"https://1.1.1.1/dns-query"},
				Direct: []string{"1.1.1.1"}, VPNUnderlay: []string{"1.1.1.1"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.json")
	writeIntegrationJSON(t, configPath, config)
	if output, err := exec.Command(binary, "-t", "-d", directory, "-f", configPath).CombinedOutput(); err != nil {
		t.Fatalf("mihomo rejected clean-install config: %v\n%s", err, output)
	}
}

func writeIntegrationJSON(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
