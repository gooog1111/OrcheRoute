package mobilecore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gooog1111/orcheroute/internal/calltransport"
	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
	callvk "github.com/gooog1111/orcheroute/internal/calltransport/vk"
	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
)

const callChallengeLifetime = 5 * time.Minute

type vkPendingFlow struct {
	source    callvk.Source
	challenge *callvk.CaptchaRequiredError
	expiresAt time.Time
	profile   *callprofile.Profile
}

var vkCallFlows = struct {
	sync.Mutex
	pending  map[string]vkPendingFlow
	ready    map[string]calltransport.ProviderCredentials
	profiles map[string]callprofile.Profile
}{pending: map[string]vkPendingFlow{}, ready: map[string]calltransport.ProviderCredentials{}, profiles: map[string]callprofile.Profile{}}

var vkCarrier = struct {
	sync.Mutex
	cancel     context.CancelFunc
	status     string
	endpoint   string
	lastErr    string
	profile    *callprofile.Profile
	generation uint64
}{status: "idle"}

// BeginVKCallCredentials starts the provider signalling flow. TURN passwords
// never cross the gomobile/UI boundary: a ready result contains only an opaque
// credential ID consumed later by the native carrier.
func BeginVKCallCredentials(invitation string) string {
	return beginVKCall(invitation, nil)
}

// BeginVKCallProfile imports a complete OrcheRoute call subscription without
// exposing its invitation, PSK or VLESS identity to the Android WebView.
func BeginVKCallProfile(encodedProfile string) string {
	profile, err := callprofile.Decode(encodedProfile, time.Now())
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return beginVKCall(profile.InvitationURL, &profile)
}

func beginVKCall(invitation string, profile *callprofile.Profile) string {
	invitation = strings.TrimSpace(invitation)
	if invitation == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_vk_invitation_required"}})
	}
	source := callvk.Source{Client: platformCallHTTPClient(30 * time.Second), Identity: callvk.DefaultIdentity()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	credentials, err := source.Resolve(ctx, invitation)
	if err == nil {
		return storeReadyVK(credentials, profile)
	}
	var challenge *callvk.CaptchaRequiredError
	if !errors.As(err, &challenge) || challenge.RedirectURL == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	id := uuid.NewString()
	now := time.Now()
	vkCallFlows.Lock()
	cleanupVKFlowsLocked(now)
	vkCallFlows.pending[id] = vkPendingFlow{source: source, challenge: challenge, expiresAt: now.Add(callChallengeLifetime), profile: profile}
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
	vkCallFlows.Lock()
	delete(vkCallFlows.pending, challengeID)
	vkCallFlows.Unlock()
	return storeReadyVK(credentials, flow.profile)
}

func CancelVKCallCredentials(challengeID string) {
	vkCallFlows.Lock()
	delete(vkCallFlows.pending, strings.TrimSpace(challengeID))
	vkCallFlows.Unlock()
}

func storeReadyVK(credentials calltransport.ProviderCredentials, profile *callprofile.Profile) string {
	if credentials.ExpiresAt.IsZero() {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_provider_expiration_required"}})
	}
	id := uuid.NewString()
	vkCallFlows.Lock()
	cleanupVKFlowsLocked(time.Now())
	vkCallFlows.ready[id] = credentials
	if profile != nil {
		vkCallFlows.profiles[id] = *profile
	}
	vkCallFlows.Unlock()
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"status": "ready", "credential_id": id, "turn_endpoint": credentials.TURN.ServerAddress,
		"network": credentials.TURN.Network, "expires_at": credentials.ExpiresAt.Unix(),
	}})
}

// StartVKCallProfileCarrier consumes the opaque credential ID and starts the
// carrier using secrets retained in native Go memory.
func StartVKCallProfileCarrier(credentialID, listenAddress string) string {
	credentialID = strings.TrimSpace(credentialID)
	vkCallFlows.Lock()
	cleanupVKFlowsLocked(time.Now())
	profile, ok := vkCallFlows.profiles[credentialID]
	vkCallFlows.Unlock()
	if !ok {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_profile_not_ready"}})
	}
	result := StartVKCallCarrierForProfile(credentialID, profile.PeerAddress, profile.VLESSUUID, profile.PSK, listenAddress)
	var envelope struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal([]byte(result), &envelope) == nil && envelope.OK {
		vkCarrier.Lock()
		activeProfile := profile
		vkCarrier.profile = &activeProfile
		vkCarrier.Unlock()
		vkCallFlows.Lock()
		delete(vkCallFlows.profiles, credentialID)
		vkCallFlows.Unlock()
	}
	return result
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

func restoreVKCallCredentials(id string, credentials calltransport.ProviderCredentials) {
	id = strings.TrimSpace(id)
	if id == "" || !time.Now().Before(credentials.ExpiresAt) {
		return
	}
	vkCallFlows.Lock()
	vkCallFlows.ready[id] = credentials
	vkCallFlows.Unlock()
}

func StartVKCallCarrier(credentialID, peerAddress, pskBase64, listenAddress string) string {
	return StartVKCallCarrierForProfile(credentialID, peerAddress, "orcheroute-call", pskBase64, listenAddress)
}

func StartVKCallCarrierForProfile(credentialID, peerAddress, profileIdentity, pskBase64, listenAddress string) string {
	peer, err := parseCallPeer(peerAddress)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_invalid_peer"}})
	}
	profileIdentity = strings.TrimSpace(profileIdentity)
	if profileIdentity == "" || len(profileIdentity) > 128 {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_invalid_identity"}})
	}
	psk, err := decodeCallPSK(pskBase64)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	if strings.TrimSpace(listenAddress) == "" {
		listenAddress = "127.0.0.1:0"
	}
	if host, _, splitErr := net.SplitHostPort(listenAddress); splitErr != nil || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_invalid_local_listener"}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	vkCarrier.Lock()
	if vkCarrier.cancel != nil {
		vkCarrier.Unlock()
		cancel()
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_already_running"}})
	}
	vkCarrier.cancel, vkCarrier.status, vkCarrier.lastErr = cancel, "connecting", ""
	vkCarrier.generation++
	generation := vkCarrier.generation
	vkCarrier.Unlock()
	credentials, err := takeVKCallCredentials(credentialID)
	if err != nil {
		cancel()
		setVKCarrierStopped(generation, err.Error())
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	connectCtx, connectCancel := context.WithTimeout(ctx, 20*time.Second)
	carrier, err := calltransport.DialTURNDTLSWithIdentity(connectCtx, credentials.TURN, peer, profileIdentity, psk, platformCallUnderlay())
	connectCancel()
	if err != nil {
		cancel()
		restoreVKCallCredentials(credentialID, credentials)
		setVKCarrierStopped(generation, err.Error())
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	reliable, err := calltransport.NewReliableClient(carrier)
	if err != nil {
		cancel()
		_ = carrier.Close()
		restoreVKCallCredentials(credentialID, credentials)
		setVKCarrierStopped(generation, err.Error())
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		cancel()
		_ = reliable.Close()
		restoreVKCallCredentials(credentialID, credentials)
		setVKCarrierStopped(generation, err.Error())
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_local_listen"}})
	}
	vkCarrier.Lock()
	vkCarrier.cancel, vkCarrier.status, vkCarrier.endpoint = cancel, "ready", listener.Addr().String()
	endpoint := vkCarrier.endpoint
	vkCarrier.Unlock()
	go func() {
		err := calltransport.ServeClient(ctx, reliable, listener)
		if err != nil {
			setVKCarrierStopped(generation, err.Error())
		} else {
			setVKCarrierStopped(generation, "")
		}
	}()
	return encode(map[string]any{"ok": true, "result": map[string]any{"status": "ready", "local_endpoint": endpoint}})
}

// parseCallPeer deliberately accepts only literal IP addresses. Resolving a
// hostname here would send DNS traffic before the protected Android underlay
// is established and could leak or follow the currently active VPN route.
func parseCallPeer(value string) (*net.UDPAddr, error) {
	host, portValue, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("call_transport_invalid_peer")
	}
	address, err := netip.ParseAddr(host)
	if err != nil || address.IsUnspecified() || address.IsMulticast() {
		return nil, fmt.Errorf("call_transport_invalid_peer")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("call_transport_invalid_peer")
	}
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(address, uint16(port))), nil
}

func StopVKCallCarrier() string {
	vkCarrier.Lock()
	cancel := vkCarrier.cancel
	vkCarrier.cancel, vkCarrier.status, vkCarrier.endpoint, vkCarrier.profile = nil, "idle", "", nil
	vkCarrier.generation++
	vkCarrier.Unlock()
	if cancel != nil {
		cancel()
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"status": "idle"}})
}

func VKCallCarrierStatus() string {
	vkCarrier.Lock()
	defer vkCarrier.Unlock()
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"status": vkCarrier.status, "local_endpoint": vkCarrier.endpoint, "error": vkCarrier.lastErr,
	}})
}

// BuildVKCallCarrierConfig creates the complete mobile Mihomo configuration
// inside the native layer. The WebView never receives the active VLESS UUID.
func BuildVKCallCarrierConfig(routesJSON, dnsJSON string) string {
	vkCarrier.Lock()
	endpoint := vkCarrier.endpoint
	var activeProfile *callprofile.Profile
	if vkCarrier.status == "ready" && vkCarrier.profile != nil {
		copy := *vkCarrier.profile
		activeProfile = &copy
	}
	vkCarrier.Unlock()
	if activeProfile == nil || endpoint == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_profile_not_active"}})
	}
	host, portValue, err := net.SplitHostPort(endpoint)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_invalid_local_listener"}})
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_invalid_local_listener"}})
	}
	proxy, err := callxray.MihomoProxy(callxray.MihomoInput{Name: activeProfile.Name, LocalAddress: host, LocalPort: port, ClientID: activeProfile.VLESSUUID})
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	proxyJSON, err := json.Marshal(proxy)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "call_transport_proxy_encode"}})
	}
	return buildMobileProxyConfig(string(proxyJSON), routesJSON, dnsJSON)
}

func setVKCarrierStopped(generation uint64, message string) {
	vkCarrier.Lock()
	if vkCarrier.generation != generation {
		vkCarrier.Unlock()
		return
	}
	vkCarrier.cancel, vkCarrier.endpoint, vkCarrier.lastErr, vkCarrier.profile = nil, "", message, nil
	if message == "" {
		vkCarrier.status = "idle"
	} else {
		vkCarrier.status = "error"
	}
	vkCarrier.Unlock()
}

func decodeCallPSK(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil && len(decoded) >= 16 {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("call_transport_invalid_psk")
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
			delete(vkCallFlows.profiles, id)
		}
	}
	for id := range vkCallFlows.profiles {
		if _, ok := vkCallFlows.ready[id]; !ok {
			delete(vkCallFlows.profiles, id)
		}
	}
}
