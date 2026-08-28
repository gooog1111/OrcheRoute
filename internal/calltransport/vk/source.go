package vk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/calltransport"
)

type ClientIdentity struct {
	ID     string
	Secret string
}

// DefaultIdentity is a public installed-application identity used by the VK
// anonymous calls flow. It originates from the GPLv3 vk-turn-proxy client and
// is not a user secret. Keeping the override on Source allows replacement if
// VK retires this application identity.
func DefaultIdentity() ClientIdentity {
	return ClientIdentity{ID: "6287487", Secret: "QbYic1K3lEV5kTGiqlq2"}
}

type Endpoints struct {
	Login string
	API   string
	Calls string
}

type Source struct {
	Client    *http.Client
	Identity  ClientIdentity
	Endpoints Endpoints
	Name      string
	Now       func() time.Time
}

type CaptchaRequiredError struct {
	RedirectURL string
	invitation  Invitation
	accessToken string
}

func (*CaptchaRequiredError) Error() string { return "call_transport_vk_captcha_required" }

func (source Source) Resolve(ctx context.Context, rawInvitation string) (calltransport.ProviderCredentials, error) {
	invitation, err := ParseInvitation(rawInvitation)
	if err != nil {
		return calltransport.ProviderCredentials{}, err
	}
	if source.Identity.ID == "" || source.Identity.Secret == "" {
		return calltransport.ProviderCredentials{}, fmt.Errorf("call_transport_vk_client_identity_required")
	}
	client := source.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	endpoints := source.Endpoints
	if endpoints.Login == "" {
		endpoints.Login = "https://login.vk.ru/?act=get_anonym_token"
	}
	if endpoints.API == "" {
		endpoints.API = "https://api.vk.ru/method/"
	}
	if endpoints.Calls == "" {
		endpoints.Calls = "https://calls.okcdn.ru/fb.do"
	}

	accessToken, err := source.anonymousAccessToken(ctx, client, endpoints.Login)
	if err != nil {
		return calltransport.ProviderCredentials{}, err
	}
	callToken, err := source.callToken(ctx, client, endpoints.API, invitation.CanonicalURL, accessToken, "")
	if err != nil {
		var challenge *CaptchaRequiredError
		if errors.As(err, &challenge) {
			challenge.invitation = invitation
			challenge.accessToken = accessToken
		}
		return calltransport.ProviderCredentials{}, err
	}
	return source.finish(ctx, client, endpoints, invitation, callToken)
}

// Continue resumes the exact anonymous VK session that produced challenge.
// The success token must be obtained by the user in the provider's own CAPTCHA
// page; starting a new access-token chain would invalidate that relationship.
func (source Source) Continue(ctx context.Context, challenge *CaptchaRequiredError, successToken string) (calltransport.ProviderCredentials, error) {
	if challenge == nil || challenge.invitation.Token == "" || challenge.accessToken == "" || strings.TrimSpace(successToken) == "" {
		return calltransport.ProviderCredentials{}, fmt.Errorf("call_transport_vk_invalid_captcha_continuation")
	}
	client := source.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	endpoints := source.Endpoints
	if endpoints.API == "" {
		endpoints.API = "https://api.vk.ru/method/"
	}
	if endpoints.Calls == "" {
		endpoints.Calls = "https://calls.okcdn.ru/fb.do"
	}
	callToken, err := source.callToken(ctx, client, endpoints.API, challenge.invitation.CanonicalURL, challenge.accessToken, strings.TrimSpace(successToken))
	if err != nil {
		return calltransport.ProviderCredentials{}, err
	}
	return source.finish(ctx, client, endpoints, challenge.invitation, callToken)
}

func (source Source) finish(ctx context.Context, client *http.Client, endpoints Endpoints, invitation Invitation, callToken string) (calltransport.ProviderCredentials, error) {
	sessionKey, err := source.callSession(ctx, client, endpoints.Calls)
	if err != nil {
		return calltransport.ProviderCredentials{}, err
	}
	turnConfig, err := source.joinCall(ctx, client, endpoints.Calls, invitation.Token, callToken, sessionKey)
	if err != nil {
		return calltransport.ProviderCredentials{}, err
	}
	now := time.Now
	if source.Now != nil {
		now = source.Now
	}
	return calltransport.ProviderCredentials{TURN: turnConfig, ExpiresAt: now().Add(8 * time.Minute)}, nil
}

func (source Source) anonymousAccessToken(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	form := url.Values{"client_id": {source.Identity.ID}, "token_type": {"messages"}, "client_secret": {source.Identity.Secret}, "version": {"1"}, "app_id": {source.Identity.ID}}
	var response struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := postForm(ctx, client, endpoint, form, &response); err != nil {
		return "", fmt.Errorf("call_transport_vk_anonymous_access: %w", err)
	}
	if response.Data.AccessToken == "" {
		return "", fmt.Errorf("call_transport_vk_anonymous_access_missing_token")
	}
	return response.Data.AccessToken, nil
}

func (source Source) callToken(ctx context.Context, client *http.Client, api, invitation, accessToken, successToken string) (string, error) {
	name := strings.TrimSpace(source.Name)
	if name == "" {
		name = "OrcheRoute"
	}
	form := url.Values{"vk_join_link": {invitation}, "name": {name}, "access_token": {accessToken}}
	if successToken != "" {
		form.Set("success_token", successToken)
	}
	var response struct {
		Response struct {
			Token string `json:"token"`
		} `json:"response"`
		Error *struct {
			Code       int    `json:"error_code"`
			Message    string `json:"error_msg"`
			CaptchaSID string `json:"captcha_sid"`
			Redirect   string `json:"redirect_uri"`
		} `json:"error"`
	}
	endpoint := strings.TrimRight(api, "/") + "/calls.getAnonymousToken?v=5.275&client_id=" + url.QueryEscape(source.Identity.ID)
	if err := postForm(ctx, client, endpoint, form, &response); err != nil {
		return "", fmt.Errorf("call_transport_vk_call_token: %w", err)
	}
	if response.Error != nil {
		if response.Error.Code == 14 || response.Error.CaptchaSID != "" {
			return "", &CaptchaRequiredError{RedirectURL: response.Error.Redirect}
		}
		return "", fmt.Errorf("call_transport_vk_api_error_%d", response.Error.Code)
	}
	if response.Response.Token == "" {
		return "", fmt.Errorf("call_transport_vk_call_token_missing")
	}
	return response.Response.Token, nil
}

func (source Source) callSession(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	deviceID := make([]byte, 16)
	if _, err := rand.Read(deviceID); err != nil {
		return "", fmt.Errorf("call_transport_vk_device_id: %w", err)
	}
	sessionData, _ := json.Marshal(map[string]any{"version": 2, "device_id": hex.EncodeToString(deviceID), "client_version": 1.1, "client_type": "SDK_JS"})
	form := url.Values{"session_data": {string(sessionData)}, "method": {"auth.anonymLogin"}, "format": {"JSON"}, "application_key": {"CGMMEJLGDIHBABABA"}}
	var response struct {
		SessionKey string `json:"session_key"`
	}
	if err := postForm(ctx, client, endpoint, form, &response); err != nil {
		return "", fmt.Errorf("call_transport_vk_session: %w", err)
	}
	if response.SessionKey == "" {
		return "", fmt.Errorf("call_transport_vk_session_missing")
	}
	return response.SessionKey, nil
}

func (source Source) joinCall(ctx context.Context, client *http.Client, endpoint, token, callToken, sessionKey string) (calltransport.TURNConfig, error) {
	form := url.Values{
		"joinLink": {token}, "isVideo": {"false"}, "protocolVersion": {"5"}, "capabilities": {"2F7F"},
		"anonymToken": {callToken}, "method": {"vchat.joinConversationByLink"}, "format": {"JSON"},
		"application_key": {"CGMMEJLGDIHBABABA"}, "session_key": {sessionKey},
	}
	var response struct {
		TURN struct {
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
			URLs       []string `json:"urls"`
		} `json:"turn_server"`
	}
	if err := postForm(ctx, client, endpoint, form, &response); err != nil {
		return calltransport.TURNConfig{}, fmt.Errorf("call_transport_vk_join: %w", err)
	}
	address, network, err := selectTURNURL(response.TURN.URLs)
	if err != nil || response.TURN.Username == "" || response.TURN.Credential == "" {
		return calltransport.TURNConfig{}, fmt.Errorf("call_transport_vk_turn_credentials_missing")
	}
	return calltransport.TURNConfig{ServerAddress: address, Username: response.TURN.Username, Password: response.TURN.Credential, Network: network}, nil
}

func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Mozilla/5.0 OrcheRoute")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("http_%d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func selectTURNURL(values []string) (string, string, error) {
	for _, raw := range values {
		lower := strings.ToLower(raw)
		if !strings.HasPrefix(lower, "turn:") {
			continue
		}
		address := strings.SplitN(raw[len("turn:"):], "?", 2)[0]
		if _, _, err := net.SplitHostPort(address); err != nil {
			continue
		}
		network := "udp"
		if strings.Contains(lower, "transport=tcp") {
			network = "tcp"
		}
		return address, network, nil
	}
	return "", "", fmt.Errorf("call_transport_vk_turn_url_missing")
}
