// Package profile defines the versioned client profile shared by OrcheRoute
// Server and native clients. Profiles contain connection secrets and must not
// be written to diagnostics or returned by public status APIs.
package profile

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	callvk "github.com/gooog1111/orcheroute/internal/calltransport/vk"
)

const (
	Version   = 1
	Transport = "vk-call-xray"
	Provider  = "vk"
	URIScheme = "orcheroute"
)

type Profile struct {
	Version           int    `json:"version"`
	Transport         string `json:"transport"`
	Provider          string `json:"provider"`
	Name              string `json:"name,omitempty"`
	InvitationURL     string `json:"invitation_url"`
	PeerAddress       string `json:"peer_address"`
	PSK               string `json:"psk"`
	VLESSUUID         string `json:"vless_uuid"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64 `json:"traffic_limit_bytes,omitempty"`
}

type PublicProfile struct {
	Version           int    `json:"version"`
	Transport         string `json:"transport"`
	Provider          string `json:"provider"`
	Name              string `json:"name,omitempty"`
	PeerAddress       string `json:"peer_address"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64 `json:"traffic_limit_bytes,omitempty"`
}

type NewInput struct {
	Name              string
	InvitationURL     string
	PeerAddress       string
	ExpiresAt         int64
	TrafficLimitBytes uint64
	Random            io.Reader
	Now               time.Time
}

// New generates independent DTLS and VLESS credentials on the trusted server
// side. Random is injectable only for deterministic tests.
func New(input NewInput) (Profile, error) {
	random := input.Random
	if random == nil {
		random = rand.Reader
	}
	psk := make([]byte, 32)
	if _, err := io.ReadFull(random, psk); err != nil {
		return Profile{}, fmt.Errorf("call_transport_profile_random: %w", err)
	}
	clientID, err := uuid.NewRandomFromReader(random)
	if err != nil {
		return Profile{}, fmt.Errorf("call_transport_profile_random: %w", err)
	}
	profile := Profile{
		Version: Version, Transport: Transport, Provider: Provider, Name: input.Name,
		InvitationURL: input.InvitationURL, PeerAddress: input.PeerAddress,
		PSK: base64.RawURLEncoding.EncodeToString(psk), VLESSUUID: clientID.String(),
		ExpiresAt: input.ExpiresAt, TrafficLimitBytes: input.TrafficLimitBytes,
	}
	if err := profile.Normalize(); err != nil {
		return Profile{}, err
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := profile.ValidateAt(now); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (profile Profile) Public() PublicProfile {
	return PublicProfile{
		Version: profile.Version, Transport: profile.Transport, Provider: profile.Provider,
		Name: profile.Name, PeerAddress: profile.PeerAddress, ExpiresAt: profile.ExpiresAt,
		TrafficLimitBytes: profile.TrafficLimitBytes,
	}
}

func (profile *Profile) Normalize() error {
	if profile.Version == 0 {
		profile.Version = Version
	}
	if profile.Transport == "" {
		profile.Transport = Transport
	}
	if profile.Provider == "" {
		profile.Provider = Provider
	}
	profile.Name = strings.TrimSpace(profile.Name)
	invitation, err := callvk.ParseInvitation(profile.InvitationURL)
	if err != nil {
		return err
	}
	profile.InvitationURL = invitation.CanonicalURL
	profile.PeerAddress = strings.TrimSpace(profile.PeerAddress)
	profile.PSK = strings.TrimSpace(profile.PSK)
	profile.VLESSUUID = strings.ToLower(strings.TrimSpace(profile.VLESSUUID))
	return nil
}

func (profile Profile) ValidateAt(now time.Time) error {
	if profile.Version != Version {
		return fmt.Errorf("call_transport_profile_version_unsupported")
	}
	if profile.Transport != Transport || profile.Provider != Provider {
		return fmt.Errorf("call_transport_profile_kind_unsupported")
	}
	if _, err := callvk.ParseInvitation(profile.InvitationURL); err != nil {
		return err
	}
	if err := validatePeer(profile.PeerAddress); err != nil {
		return err
	}
	if _, err := decodePSK(profile.PSK); err != nil {
		return err
	}
	if _, err := uuid.Parse(profile.VLESSUUID); err != nil {
		return fmt.Errorf("call_transport_profile_invalid_vless_uuid")
	}
	if profile.ExpiresAt != 0 && !now.Before(time.Unix(profile.ExpiresAt, 0)) {
		return fmt.Errorf("call_transport_profile_expired")
	}
	return nil
}

func Encode(profile Profile, now time.Time) (string, error) {
	if err := profile.Normalize(); err != nil {
		return "", err
	}
	if err := profile.ValidateAt(now); err != nil {
		return "", err
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		return "", fmt.Errorf("call_transport_profile_encode: %w", err)
	}
	return URIScheme + "://call/" + base64.RawURLEncoding.EncodeToString(payload), nil
}

func Decode(value string, now time.Time) (Profile, error) {
	value = strings.TrimSpace(value)
	payload := []byte(value)
	if !strings.HasPrefix(value, "{") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != URIScheme || parsed.Host != "call" {
			return Profile{}, fmt.Errorf("call_transport_profile_invalid_uri")
		}
		encoded := strings.TrimPrefix(parsed.EscapedPath(), "/")
		payload, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return Profile{}, fmt.Errorf("call_transport_profile_invalid_payload")
		}
	}
	var profile Profile
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("call_transport_profile_invalid_json")
	}
	if profile.Version == 0 || profile.Transport == "" || profile.Provider == "" {
		return Profile{}, fmt.Errorf("call_transport_profile_missing_kind")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Profile{}, fmt.Errorf("call_transport_profile_invalid_json")
	}
	if err := profile.Normalize(); err != nil {
		return Profile{}, err
	}
	if err := profile.ValidateAt(now); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func validatePeer(value string) error {
	host, portValue, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("call_transport_profile_invalid_peer")
	}
	address, err := netip.ParseAddr(host)
	if err != nil || address.IsUnspecified() || address.IsMulticast() {
		return fmt.Errorf("call_transport_profile_invalid_peer")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("call_transport_profile_invalid_peer")
	}
	return nil
}

func decodePSK(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(strings.TrimSpace(value)); err == nil && len(decoded) >= 16 {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("call_transport_profile_invalid_psk")
}
