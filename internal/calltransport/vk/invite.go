package vk

import (
	"fmt"
	"net/url"
	"strings"
)

type Invitation struct {
	Token        string
	CanonicalURL string
}

func ParseInvitation(raw string) (Invitation, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" {
		return Invitation{}, fmt.Errorf("call_transport_vk_invalid_invitation")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "vk.com" && host != "www.vk.com" && host != "vk.ru" && host != "www.vk.ru" {
		return Invitation{}, fmt.Errorf("call_transport_vk_invalid_invitation_host")
	}
	const prefix = "/call/join/"
	if !strings.HasPrefix(parsed.EscapedPath(), prefix) {
		return Invitation{}, fmt.Errorf("call_transport_vk_invalid_invitation_path")
	}
	token, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), prefix))
	if err != nil || token == "" || strings.ContainsAny(token, "/\\?#") {
		return Invitation{}, fmt.Errorf("call_transport_vk_invalid_invitation_token")
	}
	return Invitation{Token: token, CanonicalURL: "https://vk.com/call/join/" + url.PathEscape(token)}, nil
}
