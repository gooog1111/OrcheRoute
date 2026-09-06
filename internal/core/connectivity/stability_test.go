package connectivity

import (
	"testing"
	"time"
)

func TestStabilityRequiresDurationAndRepeatedTraffic(t *testing.T) {
	var window StabilityWindow
	start := time.Unix(100, 0)
	probe := func(second int, key string, ok bool) bool {
		return window.Observe(start.Add(time.Duration(second)*time.Second), key, ok, 30*time.Second, 3)
	}
	if probe(0, "node/network", true) || probe(1, "node/network", true) || probe(2, "node/network", true) {
		t.Fatal("rapid successes are not stable")
	}
	if !probe(30, "node/network", true) {
		t.Fatal("stable traffic rejected")
	}
	if probe(31, "other/network", true) || probe(32, "other/network", false) || probe(61, "other/network", true) {
		t.Fatal("node change/failure must reset stability")
	}
	if probe(62, "other/network", true) || !probe(91, "other/network", true) {
		t.Fatal("recovery window")
	}
}

func TestStabilityDoesNotCountRepeatedSnapshot(t *testing.T) {
	var window StabilityWindow
	now := time.Unix(100, 0)
	for i := 0; i < 5; i++ {
		if window.Observe(now, "node", true, 0, 3) {
			t.Fatal("duplicate sample counted")
		}
	}
}

func TestAllowlistHoldUsesTimeNotCallbackCount(t *testing.T) {
	input := ConfirmationInput{ConfirmedState: Normal, ObservedState: Allowlist}
	for _, now := range []int64{1000, 2000, 3000, 20000} {
		input.NowMS = now
		result, err := Confirm(input)
		if err != nil || result.State != Normal {
			t.Fatalf("early transition at %d: %#v %v", now, result, err)
		}
		input.CandidateState, input.CandidateCount, input.CandidateSinceMS = result.CandidateState, result.CandidateCount, result.CandidateSinceMS
	}
	input.NowMS = 21000
	result, err := Confirm(input)
	if err != nil || result.State != Allowlist {
		t.Fatalf("hold did not complete: %#v %v", result, err)
	}
	// A confirmed successful open-Internet observation must still recover now.
	result, err = Confirm(ConfirmationInput{ConfirmedState: Allowlist, ObservedState: Normal, NowMS: 22000})
	if err != nil || result.State != Normal {
		t.Fatal("normal recovery delayed")
	}
}
