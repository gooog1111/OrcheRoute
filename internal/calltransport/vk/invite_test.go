package vk

import "testing"

func TestParseInvitationCanonicalizesSupportedVKLinks(t *testing.T) {
	tests := []string{
		"https://vk.com/call/join/abc_123",
		" https://vk.ru/call/join/abc_123?from=copy#ignored ",
		"https://www.vk.com/call/join/abc_123",
	}
	for _, raw := range tests {
		invitation, err := ParseInvitation(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if invitation.Token != "abc_123" || invitation.CanonicalURL != "https://vk.com/call/join/abc_123" {
			t.Fatalf("unexpected invitation: %+v", invitation)
		}
	}
}

func TestParseInvitationRejectsLookalikes(t *testing.T) {
	tests := []string{
		"http://vk.com/call/join/token",
		"https://vk.com.example/call/join/token",
		"https://vk.com/not-a-call/token",
		"https://vk.com/call/join/",
		"https://vk.com/call/join/a/b",
	}
	for _, raw := range tests {
		if _, err := ParseInvitation(raw); err == nil {
			t.Fatalf("accepted invalid invitation %q", raw)
		}
	}
}
