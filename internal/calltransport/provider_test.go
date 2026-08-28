package calltransport

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedCredentialSourceCoalescesAndRefreshes(t *testing.T) {
	now := time.Unix(1000, 0)
	var calls atomic.Int32
	upstream := CredentialSourceFunc(func(context.Context, string) (ProviderCredentials, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return ProviderCredentials{
			TURN:      TURNConfig{ServerAddress: "turn.example:3478", Username: "user", Password: "pass"},
			ExpiresAt: now.Add(10 * time.Minute),
		}, nil
	})
	source := &CachedCredentialSource{Source: upstream, Now: func() time.Time { return now }}

	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := source.Resolve(context.Background(), "https://vk.com/call/join/secret"); err != nil {
				t.Errorf("resolve: %v", err)
			}
		}()
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("simultaneous resolves called upstream %d times", calls.Load())
	}

	now = now.Add(10 * time.Minute)
	if _, err := source.Resolve(context.Background(), "https://vk.com/call/join/secret"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expired credentials did not refresh: calls=%d", calls.Load())
	}
}

func TestCachedCredentialSourceDoesNotCacheInvalidCredentials(t *testing.T) {
	var calls atomic.Int32
	source := &CachedCredentialSource{Source: CredentialSourceFunc(func(context.Context, string) (ProviderCredentials, error) {
		calls.Add(1)
		return ProviderCredentials{}, nil
	})}
	for range 2 {
		if _, err := source.Resolve(context.Background(), "invite"); err == nil {
			t.Fatal("expected invalid credentials error")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("invalid credentials were cached: calls=%d", calls.Load())
	}
}
