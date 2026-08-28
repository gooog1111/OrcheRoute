package profile

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewGeneratesDeterministicIndependentCredentials(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	random := bytes.NewReader(bytes.Repeat([]byte{0x42}, 64))
	generated, err := New(NewInput{
		Name: " Phone ", InvitationURL: "https://vk.ru/call/join/token", PeerAddress: "203.0.113.2:9000",
		ExpiresAt: now.Add(time.Hour).Unix(), TrafficLimitBytes: 2048, Random: random, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	decodedPSK, err := base64.RawURLEncoding.DecodeString(generated.PSK)
	if err != nil || len(decodedPSK) != 32 {
		t.Fatalf("unexpected PSK: %q (%v)", generated.PSK, err)
	}
	if generated.VLESSUUID == "" || generated.PSK == generated.VLESSUUID || generated.Name != "Phone" {
		t.Fatalf("unexpected generated profile: %#v", generated.Public())
	}
	if generated.InvitationURL != "https://vk.com/call/join/token" {
		t.Fatalf("invitation was not canonicalized: %s", generated.InvitationURL)
	}
}

func validProfile(now time.Time) Profile {
	return Profile{
		Name: "Phone", InvitationURL: "https://www.vk.com/call/join/invite-token",
		PeerAddress: "203.0.113.10:443", PSK: base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
		VLESSUUID: "b831381d-6324-4d53-ad4f-8cda48b30811", ExpiresAt: now.Add(time.Hour).Unix(),
		TrafficLimitBytes: 1024,
	}
}

func TestProfileRoundTripAndCanonicalization(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	source := validProfile(now)
	encoded, err := Encode(source, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "orcheroute://call/") {
		t.Fatalf("unexpected URI: %s", encoded)
	}
	decoded, err := Decode(encoded, now)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.InvitationURL != "https://vk.com/call/join/invite-token" || decoded.Version != Version || decoded.Transport != Transport || decoded.Provider != Provider {
		t.Fatalf("unexpected normalized profile: %#v", decoded)
	}
}

func TestProfilePublicViewDoesNotLeakSecrets(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	profile := validProfile(now)
	if err := profile.Normalize(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(profile.Public())
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, profile.InvitationURL) || strings.Contains(text, profile.PSK) || strings.Contains(text, profile.VLESSUUID) {
		t.Fatalf("public profile leaked connection secrets: %s", text)
	}
}

func TestProfileRejectsUnsafeOrExpiredValues(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for name, mutate := range map[string]func(*Profile){
		"hostname peer": func(value *Profile) { value.PeerAddress = "turn.example:443" },
		"invalid psk":   func(value *Profile) { value.PSK = "short" },
		"invalid uuid":  func(value *Profile) { value.VLESSUUID = "not-a-uuid" },
		"expired":       func(value *Profile) { value.ExpiresAt = now.Add(-time.Second).Unix() },
	} {
		t.Run(name, func(t *testing.T) {
			profile := validProfile(now)
			mutate(&profile)
			if _, err := Encode(profile, now); err == nil {
				t.Fatal("invalid profile was accepted")
			}
		})
	}
}

func TestExpiredProfileRemainsStructurallyValid(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	profile := validProfile(now)
	profile.ExpiresAt = now.Add(-time.Second).Unix()
	if err := profile.Normalize(); err != nil {
		t.Fatal(err)
	}
	if err := profile.Validate(); err != nil {
		t.Fatalf("expired profile must remain loadable: %v", err)
	}
	if err := profile.ValidateAt(now); err == nil {
		t.Fatal("expired profile was active")
	}
}

func TestProfileDecoderRejectsUnknownFields(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	value := validProfile(now)
	if err := value.Normalize(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	payload = append(payload[:len(payload)-1], []byte(`,"unexpected":true}`)...)
	if _, err := Decode(string(payload), now); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestProfileDecoderRequiresExplicitKindAndSingleDocument(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	profile := validProfile(now)
	profile.Version, profile.Transport, profile.Provider = Version, Transport, Provider
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(string(payload)+` {}`, now); err == nil {
		t.Fatal("trailing JSON document was accepted")
	}
	profile.Version = 0
	payload, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(string(payload), now); err == nil {
		t.Fatal("profile without explicit version was accepted")
	}
}
