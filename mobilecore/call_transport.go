package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gooog1111/orcheroute/internal/calltransport"
	callvk "github.com/gooog1111/orcheroute/internal/calltransport/vk"
)

const callChallengeLifetime = 5 * time.Minute

type vkPendingFlow struct {
	source    callvk.Source
	challenge *callvk.CaptchaRequiredError
	expiresAt time.Time
}

var vkCallFlows = struct {
	sync.Mutex
	pending map[string]vkPendingFlow
	ready   map[string]calltransport.ProviderCredentials
}{pending: map[string]vkPendingFlow{}, ready: map[string]calltransport.ProviderCredentials{}}

// BeginVKCallCredentials starts the provider signalling flow. TURN passwords
// never cross the gomobile/UI boundary: a ready result contains only an opaque
// credential ID consumed later by the native carrier.
func BeginVKCallCredentials(invitation string) string {
	invitation = strings.TrimSpace(invitation)
	if invitation == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_vk_invitation_required"}})
	}
	source := callvk.Source{Client: platformCallHTTPClient(30 * time.Second), Identity: callvk.DefaultIdentity()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	credentials, err := source.Resolve(ctx, invitation)
	if err == nil {
		return storeReadyVK(credentials)
	}
	var challenge *callvk.CaptchaRequiredError
	if !errors.As(err, &challenge) || challenge.RedirectURL == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	id := uuid.NewString()
	now := time.Now()
	vkCallFlows.Lock()
	cleanupVKFlowsLocked(now)
	vkCallFlows.pending[id] = vkPendingFlow{source: source, challenge: challenge, expiresAt: now.Add(callChallengeLifetime)}
	vkCallFlows.Unlock()
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"status": "captcha_required", "challenge_id": id, "redirect_url": challenge.RedirectURL,
		"expires_at": now.Add(callChallengeLifetime).Unix(),
	}})
}

func ContinueVKCallCredentials(challengeID, successToken string) string {
	challengeID, successToken = strings.TrimSpace(challengeID), strings.TrimSpace(successToken)
	if challengeID == "" || successToken == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_vk_invalid_captcha_continuation"}})
	}
	now := time.Now()
	vkCallFlows.Lock()
	cleanupVKFlowsLocked(now)
	flow, ok := vkCallFlows.pending[challengeID]
	delete(vkCallFlows.pending, challengeID)
	vkCallFlows.Unlock()
	if !ok || !now.Before(flow.expiresAt) {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_vk_challenge_expired"}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	credentials, err := flow.source.Continue(ctx, flow.challenge, successToken)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return storeReadyVK(credentials)
}

func CancelVKCallCredentials(challengeID string) {
	vkCallFlows.Lock()
	delete(vkCallFlows.pending, strings.TrimSpace(challengeID))
	vkCallFlows.Unlock()
}

func storeReadyVK(credentials calltransport.ProviderCredentials) string {
	if credentials.ExpiresAt.IsZero() {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_provider_expiration_required"}})
	}
	id := uuid.NewString()
	vkCallFlows.Lock()
	cleanupVKFlowsLocked(time.Now())
	vkCallFlows.ready[id] = credentials
	vkCallFlows.Unlock()
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"status": "ready", "credential_id": id, "turn_endpoint": credentials.TURN.ServerAddress,
		"network": credentials.TURN.Network, "expires_at": credentials.ExpiresAt.Unix(),
	}})
}

func takeVKCallCredentials(id string) (calltransport.ProviderCredentials, error) {
	now := time.Now()
	vkCallFlows.Lock()
	defer vkCallFlows.Unlock()
	cleanupVKFlowsLocked(now)
	credentials, ok := vkCallFlows.ready[strings.TrimSpace(id)]
	delete(vkCallFlows.ready, strings.TrimSpace(id))
	if !ok || !now.Before(credentials.ExpiresAt) {
		return calltransport.ProviderCredentials{}, fmt.Errorf("call_transport_vk_credentials_expired")
	}
	return credentials, nil
}

func cleanupVKFlowsLocked(now time.Time) {
	for id, flow := range vkCallFlows.pending {
		if !now.Before(flow.expiresAt) {
			delete(vkCallFlows.pending, id)
		}
	}
	for id, credentials := range vkCallFlows.ready {
		if !now.Before(credentials.ExpiresAt) {
			delete(vkCallFlows.ready, id)
		}
	}
}
