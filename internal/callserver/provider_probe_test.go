package callserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/calltransport"
	callvk "github.com/gooog1111/orcheroute/internal/calltransport/vk"
)

func TestProbeProviderReturnsOnlyNonSecretConnectionEvidence(t *testing.T) {
	manager, now := configuredManager(t)
	seenInvitation := ""
	result, err := ProbeProvider(context.Background(), manager, calltransport.CredentialSourceFunc(func(_ context.Context, invitation string) (calltransport.ProviderCredentials, error) {
		seenInvitation = invitation
		return calltransport.ProviderCredentials{TURN: calltransport.TURNConfig{ServerAddress: "turn.example:3478", Username: "secret-user", Password: "secret-password", Network: "udp"}, ExpiresAt: now.Add(8 * time.Minute)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if seenInvitation != "https://vk.com/call/join/test-invite" || result.Provider != "vk" || result.Status != "ready" || !result.Reachable || result.TURNEndpoint != "turn.example:3478" || result.ExpiresAt == 0 {
		t.Fatalf("unexpected provider probe: %#v invitation=%q", result, seenInvitation)
	}
	if text := mustJSON(t, result); strings.Contains(text, "secret-user") || strings.Contains(text, "secret-password") || strings.Contains(text, "test-invite") {
		t.Fatalf("provider probe leaked secrets: %s", text)
	}
}

func TestProbeProviderReportsCaptchaAsReachableWithoutClaimingTURNReady(t *testing.T) {
	manager, _ := configuredManager(t)
	result, err := ProbeProvider(context.Background(), manager, calltransport.CredentialSourceFunc(func(context.Context, string) (calltransport.ProviderCredentials, error) {
		return calltransport.ProviderCredentials{}, &callvk.CaptchaRequiredError{RedirectURL: "https://id.vk.ru/not_robot_captcha"}
	}))
	if err != nil || !result.Reachable || result.Status != "captcha_required" || result.TURNEndpoint != "" {
		t.Fatalf("captcha reachability was misreported: %#v, %v", result, err)
	}
}
