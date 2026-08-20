package connectivity

import (
	"context"
	"sync"
	"testing"
)

func TestClassifyThreeStates(t *testing.T) {
	tests := []struct {
		name        string
		observation Observation
		want        State
	}{
		{"normal", Observation{AllowlistAvailable: true, ConfiguredOpenAvailable: true, OpenAnchorGitHubAvailable: true, OpenAnchorMozillaAvailable: true}, Normal},
		{"allowlist", Observation{AllowlistAvailable: true, ConfiguredOpenAvailable: false, OpenAnchorGitHubAvailable: false, OpenAnchorMozillaAvailable: false}, Allowlist},
		{"offline", Observation{}, Offline},
		{"partial open is restricted, not offline", Observation{ConfiguredOpenAvailable: true, OpenAnchorGitHubAvailable: true}, Allowlist},
		{"single reachable anchor proves a restricted network", Observation{OpenAnchorMozillaAvailable: true}, Allowlist},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.observation).State; got != test.want {
				t.Fatalf("state=%q want %q", got, test.want)
			}
		})
	}
}

func TestDiagnoseRunsEveryTargetAndClassifies(t *testing.T) {
	available := map[string]bool{"allowlist": true}
	seen := map[string]bool{}
	var lock sync.Mutex
	result, err := Diagnose(context.Background(), Config{
		AllowlistURL:    "https://allowed.example/",
		OpenInternetURL: "https://open.example/",
	}, func(_ context.Context, target Target) bool {
		lock.Lock()
		seen[target.Name] = true
		lock.Unlock()
		return available[target.Name]
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != Allowlist {
		t.Fatalf("state=%q", result.State)
	}
	if len(seen) != 4 {
		t.Fatalf("probed %d targets, want 4: %#v", len(seen), seen)
	}
}

func TestConnectivityCheckEndpointIsNotOpenInternetAnchor(t *testing.T) {
	targets, err := Targets(Config{AllowlistURL: "https://allowed.example/", OpenInternetURL: "https://www.gstatic.com/generate_204"})
	if err != nil {
		t.Fatal(err)
	}
	if targets[1].URL != "https://www.cloudflare.com/cdn-cgi/trace" {
		t.Fatalf("open target=%q", targets[1].URL)
	}
}
