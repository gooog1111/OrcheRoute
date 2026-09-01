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
	Version         = 1
	Transport       = "vk-call"
	LegacyTransport = "vk-call-xray"
	Provider        = "vk"
	URIScheme       = "orcheroute"
)

type Profile struct {
	Version           int           `json:"version"`
	Transport         string        `json:"transport"`
	Provider          string        `json:"provider"`
	Name              string        `json:"name,omitempty"`
	InvitationURL     string        `json:"invitation_url"`
	InvitationURLs    []string      `json:"invitation_urls,omitempty"`
	PeerAddress       string        `json:"peer_address"`
	PSK               string        `json:"psk"`
	VLESSUUID         string        `json:"vless_uuid"`
	ExpiresAt         int64         `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64        `json:"traffic_limit_bytes,omitempty"`
	PacketTunnel      *PacketTunnel `json:"packet_tunnel,omitempty"`
}

// PacketTunnel describes the optional packet-oriented carrier used instead of
// the legacy local VLESS/TCP hop. Keeping it nested makes the profile extensible
// to SFU carriers without coupling the common profile to one platform runtime.
type PacketTunnel struct {
	Carrier            string `json:"carrier"`
	Mode               string `json:"mode"`
	Config             string `json:"config"`
	ObfuscationProfile string `json:"obfuscation_profile,omitempty"`
	ObfuscationKey     string `json:"obfuscation_key,omitempty"`
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
	InvitationURLs    []string
	PeerAddress       string
	ExpiresAt         int64
	TrafficLimitBytes uint64
	Random            io.Reader
	Now               time.Time
}

// New generates independent subscription and VLESS credentials on the trusted server
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
		InvitationURL: input.InvitationURL, InvitationURLs: append([]string(nil), input.InvitationURLs...), PeerAddress: input.PeerAddress,
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
	invitationValues := append([]string{profile.InvitationURL}, profile.InvitationURLs...)
	canonicalInvitations := make([]string, 0, len(invitationValues))
	seenInvitations := make(map[string]struct{}, len(invitationValues))
	for _, value := range invitationValues {
		if strings.TrimSpace(value) == "" {
			continue
		}
		invitation, err := callvk.ParseInvitation(value)
		if err != nil {
			return err
		}
		if _, exists := seenInvitations[invitation.CanonicalURL]; exists {
			continue
		}
		seenInvitations[invitation.CanonicalURL] = struct{}{}
		canonicalInvitations = append(canonicalInvitations, invitation.CanonicalURL)
	}
	if len(canonicalInvitations) == 0 {
		return fmt.Errorf("call_transport_invitation_missing")
	}
	profile.InvitationURL = canonicalInvitations[0]
	profile.InvitationURLs = append([]string(nil), canonicalInvitations[1:]...)
	profile.PeerAddress = strings.TrimSpace(profile.PeerAddress)
	profile.PSK = strings.TrimSpace(profile.PSK)
	profile.VLESSUUID = strings.ToLower(strings.TrimSpace(profile.VLESSUUID))
	if profile.PacketTunnel != nil {
		profile.PacketTunnel.Carrier = strings.ToLower(strings.TrimSpace(profile.PacketTunnel.Carrier))
		profile.PacketTunnel.Mode = strings.ToLower(strings.TrimSpace(profile.PacketTunnel.Mode))
		profile.PacketTunnel.Config = strings.TrimSpace(profile.PacketTunnel.Config)
		profile.PacketTunnel.ObfuscationProfile = strings.ToLower(strings.TrimSpace(profile.PacketTunnel.ObfuscationProfile))
		profile.PacketTunnel.ObfuscationKey = strings.TrimSpace(profile.PacketTunnel.ObfuscationKey)
	}
	return nil
}

func (profile Profile) Validate() error {
	if profile.Version != Version {
		return fmt.Errorf("call_transport_profile_version_unsupported")
	}
	if (profile.Transport != Transport && profile.Transport != LegacyTransport) || profile.Provider != Provider {
		return fmt.Errorf("call_transport_profile_kind_unsupported")
	}
	if _, err := callvk.ParseInvitation(profile.InvitationURL); err != nil {
		return err
	}
	for _, value := range profile.InvitationURLs {
		if _, err := callvk.ParseInvitation(value); err != nil {
			return err
		}
	}
	if err := validatePeer(profile.PeerAddress); err != nil {
		return err
	}
	if _, err := profile.PSKBytes(); err != nil {
		return err
	}
	if _, err := uuid.Parse(profile.VLESSUUID); err != nil {
		return fmt.Errorf("call_transport_profile_invalid_vless_uuid")
	}
	if err := profile.PacketTunnel.validate(); err != nil {
		return err
	}
	return nil
}

func (tunnel *PacketTunnel) validate() error {
	if tunnel == nil {
		return nil
	}
	if tunnel.Carrier != "vk-turn" {
		return fmt.Errorf("call_transport_packet_carrier_unsupported")
	}
	if tunnel.Mode != "awg" || tunnel.Config == "" {
		return fmt.Errorf("call_transport_packet_tunnel_invalid")
	}
	if tunnel.ObfuscationProfile != "rtpopus3" {
		return fmt.Errorf("call_transport_obfuscation_unsupported")
	}
	key, err := base64.RawURLEncoding.DecodeString(tunnel.ObfuscationKey)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("call_transport_obfuscation_key_invalid")
	}
	return nil
}

func (profile Profile) UsesPacketTunnel() bool { return profile.PacketTunnel != nil }

// AllInvitationURLs returns the canonical primary and additional VK links.
// The primary field remains populated for the existing single-link code paths;
// clients must understand the optional additional-links field to use this profile.
func (profile Profile) AllInvitationURLs() []string {
	result := make([]string, 0, 1+len(profile.InvitationURLs))
	if profile.InvitationURL != "" {
		result = append(result, profile.InvitationURL)
	}
	return append(result, profile.InvitationURLs...)
}

func (profile Profile) ValidateAt(now time.Time) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	if profile.ExpiresAt != 0 && !now.Before(time.Unix(profile.ExpiresAt, 0)) {
		return fmt.Errorf("call_transport_profile_expired")
	}
	return nil
}

func (profile Profile) PSKBytes() ([]byte, error) { return decodePSK(profile.PSK) }

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
	if address, parseErr := netip.ParseAddr(host); parseErr == nil {
		if address.IsUnspecified() || address.IsMulticast() {
			return fmt.Errorf("call_transport_profile_invalid_peer")
		}
	} else if !validPeerDNSName(host) {
		return fmt.Errorf("call_transport_profile_invalid_peer")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("call_transport_profile_invalid_peer")
	}
	return nil
}

func validPeerDNSName(value string) bool {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" || len(value) > 253 || value == "localhost" {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return strings.Contains(value, ".")
}

func decodePSK(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(strings.TrimSpace(value)); err == nil && len(decoded) >= 16 {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("call_transport_profile_invalid_psk")
}
