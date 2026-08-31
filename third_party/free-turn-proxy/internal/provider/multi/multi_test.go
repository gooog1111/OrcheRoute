package multi

import (
	"context"
	"errors"
	"testing"

	"github.com/samosvalishe/free-turn-proxy/internal/provider"
)

type stubProvider struct {
	credentials provider.Credentials
	err         error
}

func (p stubProvider) GetCredentials(context.Context, int) (provider.Credentials, error) {
	return p.credentials, p.err
}
func (stubProvider) IsAuthError(error) bool   { return false }
func (stubProvider) HandleAuthError(int) bool { return false }
func (stubProvider) ResetErrors(int)          {}
func (stubProvider) DropCredentials(int)      {}
func (stubProvider) BackoffUntilUnix() int64  { return 0 }
func (stubProvider) Name() string             { return "stub" }

func TestFatalFailureOfOneProviderDoesNotStopOtherProvider(t *testing.T) {
	good := provider.Credentials{User: "working"}
	m := New([]provider.Provider{
		stubProvider{err: errors.Join(provider.ErrFatalNoStreams, errors.New("captcha failed"))},
		stubProvider{credentials: good},
	})

	if _, err := m.GetCredentials(context.Background(), 1); err == nil {
		t.Fatal("failed provider returned no error")
	} else if errors.Is(err, provider.ErrFatalNoStreams) {
		t.Fatalf("provider-local failure retained fatal sentinel: %v", err)
	}
	credentials, err := m.GetCredentials(context.Background(), 2)
	if err != nil || credentials.User != good.User {
		t.Fatalf("working provider was not available: credentials=%#v err=%v", credentials, err)
	}
}

func TestSingleProviderPreservesFatalFailure(t *testing.T) {
	m := New([]provider.Provider{stubProvider{err: provider.ErrFatalNoStreams}})
	_, err := m.GetCredentials(context.Background(), 1)
	if !errors.Is(err, provider.ErrFatalNoStreams) {
		t.Fatalf("single provider fatal error was hidden: %v", err)
	}
}
