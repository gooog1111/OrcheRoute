package callserver

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
)

func (manager *Manager) encodeSubscriptionLocked(client Client) (string, error) {
	callURI, err := callprofileEncode(client, manager)
	if err != nil {
		return "", err
	}
	profiles := []string{callURI}
	if !manager.data.OrdinaryEnabled {
		return callURI, nil
	}
	host, _, err := net.SplitHostPort(manager.data.PublicEndpoint)
	if err != nil || host == "" {
		return "", fmt.Errorf("call_server_invalid_public_endpoint")
	}
	// A configured HTTPS subscription address is also the user's public DNS
	// identity. Prefer its hostname for ordinary protocols while keeping the
	// literal Call endpoint intact for allowlist reliability.
	if manager.data.SubscriptionBaseURL != "" {
		if publicURL, parseErr := url.Parse(manager.data.SubscriptionBaseURL); parseErr == nil && publicURL.Hostname() != "" {
			host = publicURL.Hostname()
		}
	}
	name := client.Name
	vlessPort := endpointPort(manager.data.VLESSListenAddress)
	trojanPort := endpointPort(manager.data.TrojanListenAddress)
	hy2Port := endpointPort(manager.data.HysteriaListenAddress)
	if vlessPort == "" || trojanPort == "" || hy2Port == "" {
		return "", fmt.Errorf("call_server_invalid_protocol_address")
	}

	vlessQuery := url.Values{"encryption": {"none"}, "security": {"reality"}, "type": {"tcp"},
		"sni": {manager.data.FakeSNI}, "fp": {"chrome"}, "pbk": {manager.data.RealityPublicKey}, "sid": {manager.data.RealityShortID}}
	profiles = append(profiles, (&url.URL{Scheme: "vless", User: url.User(client.Profile.VLESSUUID), Host: net.JoinHostPort(host, vlessPort), RawQuery: vlessQuery.Encode(), Fragment: name + " · VLESS"}).String())

	trojanQuery := url.Values{"security": {"tls"}, "type": {"tcp"}, "sni": {manager.data.FakeSNI}, "allowInsecure": {"true"}}
	profiles = append(profiles, (&url.URL{Scheme: "trojan", User: url.User(protocolPassword(client.Profile.PSK, "trojan")), Host: net.JoinHostPort(host, trojanPort), RawQuery: trojanQuery.Encode(), Fragment: name + " · Trojan"}).String())

	hy2Query := url.Values{"sni": {manager.data.FakeSNI}, "insecure": {"1"}}
	profiles = append(profiles, (&url.URL{Scheme: "hysteria2", User: url.User(protocolPassword(client.Profile.PSK, "hysteria2")), Host: net.JoinHostPort(host, hy2Port), RawQuery: hy2Query.Encode(), Fragment: name + " · Hysteria2"}).String())
	return strings.Join(profiles, "\n"), nil
}

func callprofileEncode(client Client, manager *Manager) (string, error) {
	return callprofile.Encode(client.Profile, manager.now())
}

func protocolPassword(secret, protocol string) string {
	digest := sha256.Sum256([]byte("orcheroute:" + protocol + ":" + secret))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func endpointPort(endpoint string) string {
	_, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return ""
	}
	return port
}
