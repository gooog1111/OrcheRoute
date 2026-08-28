package mobilecore

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/calltransport"
)

func TestVKReadyCredentialsStayOutsideUIResponse(t *testing.T) {
	credentials := calltransport.ProviderCredentials{
		TURN:      calltransport.TURNConfig{ServerAddress: "turn.example:3478", Username: "private-user", Password: "private-password", Network: "udp"},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	encoded := storeReadyVK(credentials)
	if strings.Contains(encoded, credentials.TURN.Username) || strings.Contains(encoded, credentials.TURN.Password) {
		t.Fatalf("TURN credentials leaked to UI response: %s", encoded)
	}
	var response struct {
		OK     bool `json:"ok"`
		Result struct {
			CredentialID string `json:"credential_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(encoded), &response); err != nil || !response.OK || response.Result.CredentialID == "" {
		t.Fatalf("unexpected response: %s (%v)", encoded, err)
	}
	stored, err := takeVKCallCredentials(response.Result.CredentialID)
	if err != nil || stored.TURN.Password != credentials.TURN.Password {
		t.Fatalf("credentials not retained for carrier: %+v, %v", stored, err)
	}
	if _, err := takeVKCallCredentials(response.Result.CredentialID); err == nil {
		t.Fatal("credential ID was reusable")
	}
}

func TestVKCredentialBoundaryRejectsIncompleteRequests(t *testing.T) {
	if result := BeginVKCallCredentials(" "); !strings.Contains(result, "call_transport_vk_invitation_required") {
		t.Fatalf("unexpected begin result: %s", result)
	}
	if result := ContinueVKCallCredentials("", ""); !strings.Contains(result, "call_transport_vk_invalid_captcha_continuation") {
		t.Fatalf("unexpected continuation result: %s", result)
	}
}

func TestVKCarrierRejectsInvalidEndpointsBeforeConsumingCredentials(t *testing.T) {
	psk := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	if result := StartVKCallCarrier("missing", "host-without-port", psk, "127.0.0.1:0"); !strings.Contains(result, "call_transport_invalid_peer") {
		t.Fatalf("unexpected peer validation: %s", result)
	}
	if result := StartVKCallCarrier("missing", "turn.example:56000", psk, "127.0.0.1:0"); !strings.Contains(result, "call_transport_invalid_peer") {
		t.Fatalf("hostname must not be resolved before protected transport: %s", result)
	}
	if result := StartVKCallCarrier("missing", "127.0.0.1:56000", "bad", "127.0.0.1:0"); !strings.Contains(result, "call_transport_invalid_psk") {
		t.Fatalf("unexpected PSK validation: %s", result)
	}
	if result := StartVKCallCarrier("missing", "127.0.0.1:56000", psk, "0.0.0.0:9000"); !strings.Contains(result, "call_transport_invalid_local_listener") {
		t.Fatalf("unexpected listener validation: %s", result)
	}
	if result := StopVKCallCarrier(); !strings.Contains(result, `"status":"idle"`) {
		t.Fatalf("unexpected stop result: %s", result)
	}
}
