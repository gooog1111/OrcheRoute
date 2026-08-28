package vk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSourceResolvesTURNCredentialsFromInvitation(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		calls = append(calls, request.URL.Path+":"+request.Form.Get("method"))
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/login":
			if request.Form.Get("client_id") != "client" || request.Form.Get("client_secret") != "secret" {
				t.Error("missing VK client identity")
			}
			_, _ = writer.Write([]byte(`{"data":{"access_token":"access"}}`))
		case request.URL.Path == "/api/calls.getAnonymousToken":
			if request.Form.Get("vk_join_link") != "https://vk.com/call/join/invite" {
				t.Errorf("unexpected invitation: %q", request.Form.Get("vk_join_link"))
			}
			_, _ = writer.Write([]byte(`{"response":{"token":"call-token"}}`))
		case request.Form.Get("method") == "auth.anonymLogin":
			_, _ = writer.Write([]byte(`{"session_key":"session"}`))
		case request.Form.Get("method") == "vchat.joinConversationByLink":
			if request.Form.Get("joinLink") != "invite" || request.Form.Get("anonymToken") != "call-token" {
				t.Error("join request did not carry call tokens")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"turn_server": map[string]any{
				"username": "turn-user", "credential": "turn-pass",
				"urls": []string{"turn:turn.example:3478?transport=udp", "turn:turn.example:3478?transport=tcp"},
			}})
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	now := time.Unix(1000, 0)
	source := Source{
		Client: server.Client(), Identity: ClientIdentity{ID: "client", Secret: "secret"},
		Endpoints: Endpoints{Login: server.URL + "/login", API: server.URL + "/api", Calls: server.URL + "/calls"},
		Now:       func() time.Time { return now },
	}
	credentials, err := source.Resolve(context.Background(), "https://vk.ru/call/join/invite?copy=1")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.TURN.ServerAddress != "turn.example:3478" || credentials.TURN.Network != "udp" || credentials.TURN.Username != "turn-user" || credentials.TURN.Password != "turn-pass" {
		t.Fatalf("unexpected TURN credentials: %+v", credentials.TURN)
	}
	if !credentials.ExpiresAt.Equal(now.Add(8 * time.Minute)) {
		t.Fatalf("unexpected expiration: %s", credentials.ExpiresAt)
	}
	if len(calls) != 4 {
		t.Fatalf("expected four provider calls, got %v", calls)
	}
}

func TestSourceReportsCaptchaWithoutCachingSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/login" {
			_, _ = writer.Write([]byte(`{"data":{"access_token":"access"}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"error":{"error_code":14,"captcha_sid":"captcha"}}`))
	}))
	defer server.Close()
	source := Source{Client: server.Client(), Identity: ClientIdentity{ID: "client", Secret: "secret"}, Endpoints: Endpoints{Login: server.URL + "/login", API: server.URL + "/api", Calls: server.URL + "/calls"}}
	if _, err := source.Resolve(context.Background(), "https://vk.com/call/join/invite"); err == nil || err.Error() != "call_transport_vk_captcha_required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSelectTURNURLRejectsTURNSTLSOnly(t *testing.T) {
	if _, _, err := selectTURNURL([]string{"turns:turn.example:5349?transport=tcp"}); err == nil {
		t.Fatal("accepted unsupported TURN over TLS URL")
	}
}
