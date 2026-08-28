package vk

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSourceLive is intentionally skipped during normal builds. It provides a
// reproducible provider check without storing a private call invitation or VK
// application identity in the repository.
func TestSourceLive(t *testing.T) {
	invitation := os.Getenv("ORCHEROUTE_VK_INVITATION")
	clientID := os.Getenv("ORCHEROUTE_VK_CLIENT_ID")
	clientSecret := os.Getenv("ORCHEROUTE_VK_CLIENT_SECRET")
	if invitation == "" || clientID == "" || clientSecret == "" {
		t.Skip("live VK credentials are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	credentials, err := (Source{Identity: ClientIdentity{ID: clientID, Secret: clientSecret}}).Resolve(ctx, invitation)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.TURN.ServerAddress == "" || credentials.TURN.Username == "" || credentials.TURN.Password == "" {
		t.Fatal("provider returned incomplete TURN credentials")
	}
	t.Logf("TURN endpoint=%s network=%s expires_in=%s", credentials.TURN.ServerAddress, credentials.TURN.Network, time.Until(credentials.ExpiresAt).Round(time.Second))
}
