package serverruntime

import (
	"testing"
	"time"
)

func TestNextAppUpdateCheckSurvivesServiceRestart(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if got := nextAppUpdateCheck(int(now.Add(-time.Hour).Unix()), now); got != 5*time.Hour {
		t.Fatalf("got %s, want 5h", got)
	}
	if got := nextAppUpdateCheck(0, now); got != 20*time.Second {
		t.Fatalf("initial delay %s", got)
	}
}
