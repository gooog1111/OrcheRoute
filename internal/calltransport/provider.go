package calltransport

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type ProviderCredentials struct {
	TURN      TURNConfig
	ExpiresAt time.Time
}

type CredentialSource interface {
	Resolve(context.Context, string) (ProviderCredentials, error)
}

type CredentialSourceFunc func(context.Context, string) (ProviderCredentials, error)

func (function CredentialSourceFunc) Resolve(ctx context.Context, invitation string) (ProviderCredentials, error) {
	return function(ctx, invitation)
}

// CachedCredentialSource keeps short-lived call credentials away from the
// hot reconnect path and coalesces simultaneous refreshes for one invitation.
// Invitations are represented in memory by a hash so call links do not appear
// in diagnostic cache keys.
type CachedCredentialSource struct {
	Source      CredentialSource
	RefreshSkew time.Duration
	Now         func() time.Time

	mu      sync.RWMutex
	entries map[[sha256.Size]byte]ProviderCredentials
	group   singleflight.Group
}

func (source *CachedCredentialSource) Resolve(ctx context.Context, invitation string) (ProviderCredentials, error) {
	if source == nil || source.Source == nil {
		return ProviderCredentials{}, fmt.Errorf("call_transport_credential_source_required")
	}
	key := sha256.Sum256([]byte(invitation))
	if credentials, ok := source.cached(key); ok {
		return credentials, nil
	}
	value, err, _ := source.group.Do(fmt.Sprintf("%x", key), func() (any, error) {
		if credentials, ok := source.cached(key); ok {
			return credentials, nil
		}
		credentials, resolveErr := source.Source.Resolve(ctx, invitation)
		if resolveErr != nil {
			return ProviderCredentials{}, resolveErr
		}
		if credentials.TURN.ServerAddress == "" || credentials.TURN.Username == "" || credentials.TURN.Password == "" {
			return ProviderCredentials{}, fmt.Errorf("call_transport_incomplete_provider_credentials")
		}
		if credentials.ExpiresAt.IsZero() {
			return ProviderCredentials{}, fmt.Errorf("call_transport_provider_expiration_required")
		}
		source.mu.Lock()
		if source.entries == nil {
			source.entries = make(map[[sha256.Size]byte]ProviderCredentials)
		}
		source.entries[key] = credentials
		source.mu.Unlock()
		return credentials, nil
	})
	if err != nil {
		return ProviderCredentials{}, err
	}
	return value.(ProviderCredentials), nil
}

func (source *CachedCredentialSource) cached(key [sha256.Size]byte) (ProviderCredentials, bool) {
	now := time.Now
	if source.Now != nil {
		now = source.Now
	}
	skew := source.RefreshSkew
	if skew <= 0 {
		skew = time.Minute
	}
	source.mu.RLock()
	credentials, ok := source.entries[key]
	source.mu.RUnlock()
	return credentials, ok && now().Add(skew).Before(credentials.ExpiresAt)
}

func (source *CachedCredentialSource) Invalidate(invitation string) {
	if source == nil {
		return
	}
	key := sha256.Sum256([]byte(invitation))
	source.mu.Lock()
	delete(source.entries, key)
	source.mu.Unlock()
}
