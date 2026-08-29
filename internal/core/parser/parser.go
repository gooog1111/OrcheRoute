// Package parser owns the pure subscription-to-node conversion used by mobile
// clients. It has no Android, storage, HTTP, routing, or transport concerns.
package parser

import (
	"fmt"
	"strings"

	"github.com/gooog1111/orcheroute/internal/nodes"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

// DecodeSubscriptionBody decodes plain or base64 subscription payloads into
// share links. Fetching the payload remains an adapter responsibility.
func DecodeSubscriptionBody(body string) []string {
	return subscriptions.Decode([]byte(body))
}

// NormalizeInline canonicalizes a pasted list and reports removed duplicates.
func NormalizeInline(value string) (string, int) {
	return subscriptions.NormalizeInline(value)
}

// ParseLink converts one share link to the canonical Mihomo node shape.
func ParseLink(link, source string, index int) (map[string]any, error) {
	if strings.TrimSpace(source) == "" || index < 0 {
		return nil, fmt.Errorf("invalid_request")
	}
	return nodes.ParseLink(strings.TrimSpace(link), source, index)
}

// ParseSubscription converts and deduplicates all links from one source.
func ParseSubscription(links []string, source string) (nodes.ConversionResult, error) {
	if strings.TrimSpace(source) == "" {
		return nodes.ConversionResult{}, fmt.Errorf("invalid_request")
	}
	return nodes.ConvertLinks(links, source), nil
}

// IsShareLink reports whether a value can be parsed without an HTTP fetch.
func IsShareLink(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{
		"vless://", "vmess://", "trojan://", "ss://", "hysteria2://", "hy2://",
		"wireguard://", "wg://", "amneziawg://", "awg://", "orcheroute://call/",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
