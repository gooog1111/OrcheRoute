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
		{"normal tolerates one unavailable anchor", Observation{AllowlistAvailable: true, ConfiguredOpenAvailable: true, OpenAnchorGitHubAvailable: true}, Normal},
		{"allowlist", Observation{AllowlistAvailable: true, ConfiguredOpenAvailable: false, OpenAnchorGitHubAvailable: false, OpenAnchorMozillaAvailable: false}, Allowlist},
		{"offline", Observation{}, Offline},
		{"configured open target proves normal Internet", Observation{ConfiguredOpenAvailable: true}, Normal},
		{"physical network without reachable HTTP anchors is restricted", Observation{PhysicalNetworkAvailable: true}, Allowlist},
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

func TestConfirmRejectsTransientOffline(t *testing.T) {
	result, err := Confirm(ConfirmationInput{ConfirmedState: Allowlist, ObservedState: Offline})
	if err != nil || result.State != Allowlist || result.CandidateCount != 1 {
		t.Fatalf("first=%#v err=%v", result, err)
	}
	result, err = Confirm(ConfirmationInput{ConfirmedState: result.State, CandidateState: result.CandidateState, CandidateCount: result.CandidateCount, ObservedState: Offline})
	if err != nil || result.State != Allowlist || result.CandidateCount != 2 {
		t.Fatalf("second=%#v err=%v", result, err)
	}
	result, err = Confirm(ConfirmationInput{ConfirmedState: result.State, CandidateState: result.CandidateState, CandidateCount: result.CandidateCount, ObservedState: Offline})
	if err != nil || result.State != Offline || !result.Changed {
		t.Fatalf("third=%#v err=%v", result, err)
	}
}

func TestConfirmRestrictionNeedsTwoSamplesAndNormalRecoversImmediately(t *testing.T) {
	first, err := Confirm(ConfirmationInput{ConfirmedState: Normal, ObservedState: Allowlist})
	if err != nil || first.State != Normal || first.CandidateCount != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := Confirm(ConfirmationInput{ConfirmedState: first.State, CandidateState: first.CandidateState, CandidateCount: first.CandidateCount, ObservedState: Allowlist})
	if err != nil || second.State != Allowlist || !second.Changed {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	recovered, err := Confirm(ConfirmationInput{ConfirmedState: Offline, ObservedState: Normal})
	if err != nil || recovered.State != Normal || !recovered.Changed {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	recovered, err = Confirm(ConfirmationInput{ConfirmedState: Allowlist, ObservedState: Normal})
	if err != nil || recovered.State != Normal || !recovered.Changed {
		t.Fatalf("allowlist recovery=%#v err=%v", recovered, err)
	}
}

func TestParseTraceIdentity(t *testing.T) {
	identity, err := ParseTraceIdentity("fl=1\nip=203.0.113.8\nloc=US\ntls=TLSv1.3\n")
	if err != nil || identity.IP != "203.0.113.8" || identity.CountryCode != "US" || identity.Region != "USA" || identity.Flag != "🇺🇸" {
		t.Fatalf("identity=%#v err=%v", identity, err)
	}
}

func TestParseTraceIdentityRequiresAnIPAddress(t *testing.T) {
	if _, err := ParseTraceIdentity("loc=DE\n"); err == nil {
		t.Fatal("expected missing IP error")
	}
}
