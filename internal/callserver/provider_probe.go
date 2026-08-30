package callserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gooog1111/orcheroute/internal/calltransport"
	callvk "github.com/gooog1111/orcheroute/internal/calltransport/vk"
)

// ProviderProbeResult contains only non-secret evidence that the saved call
// invitation was accepted and temporary TURN credentials were issued.
type ProviderProbeResult struct {
	Provider     string `json:"provider"`
	Status       string `json:"status"`
	Reachable    bool   `json:"reachable"`
	TURNEndpoint string `json:"turn_endpoint"`
	Network      string `json:"network"`
	ExpiresAt    int64  `json:"expires_at"`
}

// ProbeProvider performs an explicit provider-side check. It never changes
// the active VPN runtime and never exposes the invitation or TURN password.
func ProbeProvider(ctx context.Context, manager *Manager, source calltransport.CredentialSource) (ProviderProbeResult, error) {
	if manager == nil || source == nil {
		return ProviderProbeResult{}, fmt.Errorf("call_server_provider_probe_unavailable")
	}
	manager.mu.Lock()
	invitation := strings.TrimSpace(manager.data.InvitationURL)
	manager.mu.Unlock()
	if invitation == "" {
		return ProviderProbeResult{}, fmt.Errorf("call_server_invitation_required")
	}
	credentials, err := source.Resolve(ctx, invitation)
	if err != nil {
		var challenge *callvk.CaptchaRequiredError
		if errors.As(err, &challenge) {
			return ProviderProbeResult{Provider: "vk", Status: "captcha_required", Reachable: true}, nil
		}
		return ProviderProbeResult{}, err
	}
	if credentials.TURN.ServerAddress == "" || credentials.TURN.Username == "" || credentials.TURN.Password == "" || credentials.ExpiresAt.IsZero() {
		return ProviderProbeResult{}, fmt.Errorf("call_server_provider_probe_incomplete")
	}
	network := strings.ToLower(strings.TrimSpace(credentials.TURN.Network))
	if network == "" {
		network = "udp"
	}
	return ProviderProbeResult{Provider: "vk", Status: "ready", Reachable: true, TURNEndpoint: credentials.TURN.ServerAddress, Network: network, ExpiresAt: credentials.ExpiresAt.Unix()}, nil
}
