package serverruntime

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSystemTimeCheckAndSyncDoesNotSetArbitraryDate(t *testing.T) {
	runtime := cleanTestRuntime(t)
	var syncCalls atomic.Int32
	runtime.timeCommand = func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "set-ntp true" {
			syncCalls.Add(1)
			return nil, nil
		}
		if len(args) == 0 || args[0] != "show" {
			t.Errorf("unexpected clock mutation: %v", args)
		}
		return []byte("Timezone=Europe/Moscow\nCanNTP=yes\nNTP=yes\nNTPSynchronized=no\n"), nil
	}
	runtime.checkSystemTime(context.Background())
	before := runtime.systemTimeSnapshot()
	if before.Synchronized || before.Status != "unsynchronized" || before.Timezone != "Europe/Moscow" || before.Unix < time.Now().Unix()-1 {
		t.Fatalf("incorrect time status: %+v", before)
	}
	response := callServerAPI(t, runtime, http.MethodPost, "/v1/system/time/sync", map[string]any{}, 202)
	if response["accepted"] != true {
		t.Fatal(response)
	}
	deadline := time.Now().Add(time.Second)
	for runtime.systemTimeSnapshot().Syncing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	after := runtime.systemTimeSnapshot()
	if syncCalls.Load() != 1 || after.Syncing || after.Synchronized {
		t.Fatalf("request must not fabricate synchronized state: %+v calls=%d", after, syncCalls.Load())
	}
	status := callServerAPI(t, runtime, http.MethodGet, "/v1/status", nil, 200)
	if status["system_time"] == nil {
		t.Fatal("dashboard lacks time")
	}
}

func TestSystemTimeFailureAndRepeatedSyncAreBounded(t *testing.T) {
	runtime := cleanTestRuntime(t)
	started, release := make(chan struct{}), make(chan struct{})
	runtime.timeCommand = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "set-ntp" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return nil, errors.New("unavailable")
	}
	runtime.syncSystemTime()
	<-started
	runtime.syncSystemTime() // Must join the already running operation.
	if !runtime.systemTimeSnapshot().Syncing {
		t.Fatal("pending synchronization is hidden")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for runtime.systemTimeSnapshot().Syncing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	value := runtime.systemTimeSnapshot()
	if value.Status != "unavailable" || value.Error == "" || value.Synchronized || value.Syncing {
		t.Fatalf("false synchronized status: %+v", value)
	}
}

func TestSystemTimeOnlyTrustsExplicitSynchronization(t *testing.T) {
	for _, item := range []struct {
		input, status string
		synchronized  bool
	}{
		{"Timezone=UTC\nCanNTP=yes\nNTP=yes\nNTPSynchronized=yes\n", "synchronized", true},
		{"Timezone=UTC\nCanNTP=yes\nNTP=yes\nNTPSynchronized=no\n", "unsynchronized", false},
		{"unexpected", "unavailable", false},
	} {
		runtime := &Runtime{timeCommand: func(context.Context, ...string) ([]byte, error) { return []byte(item.input), nil }}
		runtime.checkSystemTime(context.Background())
		value := runtime.systemTimeSnapshot()
		if value.Status != item.status || value.Synchronized != item.synchronized {
			t.Fatalf("invalid synchronization status: %+v", value)
		}
	}
}
