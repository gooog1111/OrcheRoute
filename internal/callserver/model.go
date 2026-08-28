package callserver

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
	callvk "github.com/gooog1111/orcheroute/internal/calltransport/vk"
)

const CurrentVersion = 1

type Config struct {
	Version             int      `json:"version"`
	Enabled             bool     `json:"enabled"`
	ListenAddress       string   `json:"listen_address"`
	PublicEndpoint      string   `json:"public_endpoint,omitempty"`
	BackendAddress      string   `json:"backend_address"`
	InvitationURL       string   `json:"invitation_url,omitempty"`
	SubscriptionBaseURL string   `json:"subscription_base_url,omitempty"`
	Clients             []Client `json:"clients"`
}

type Client struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Enabled           bool                `json:"enabled"`
	CreatedAt         int64               `json:"created_at"`
	SubscriptionToken string              `json:"subscription_token"`
	Profile           callprofile.Profile `json:"profile"`
	TrafficRXBytes    uint64              `json:"traffic_rx_bytes"`
	TrafficTXBytes    uint64              `json:"traffic_tx_bytes"`
	LastSeenAt        int64               `json:"last_seen_at,omitempty"`
}

type PublicConfig struct {
	Version              int            `json:"version"`
	Enabled              bool           `json:"enabled"`
	ListenAddress        string         `json:"listen_address"`
	PublicEndpoint       string         `json:"public_endpoint,omitempty"`
	BackendAddress       string         `json:"backend_address"`
	InvitationConfigured bool           `json:"invitation_configured"`
	SubscriptionBaseURL  string         `json:"subscription_base_url,omitempty"`
	Clients              []PublicClient `json:"clients"`
}

type PublicClient struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	CreatedAt         int64  `json:"created_at"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64 `json:"traffic_limit_bytes,omitempty"`
	TrafficUsedBytes  uint64 `json:"traffic_used_bytes"`
	TrafficRXBytes    uint64 `json:"traffic_rx_bytes"`
	TrafficTXBytes    uint64 `json:"traffic_tx_bytes"`
	LastSeenAt        int64  `json:"last_seen_at,omitempty"`
	Available         bool   `json:"available"`
}

func DefaultConfig() Config {
	return Config{Version: CurrentVersion, ListenAddress: "0.0.0.0:4443", BackendAddress: "127.0.0.1:18443", Clients: []Client{}}
}

func (config *Config) Normalize() error {
	if config.Version == 0 {
		config.Version = CurrentVersion
	}
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	config.PublicEndpoint = strings.TrimSpace(config.PublicEndpoint)
	config.BackendAddress = strings.TrimSpace(config.BackendAddress)
	config.SubscriptionBaseURL = strings.TrimRight(strings.TrimSpace(config.SubscriptionBaseURL), "/")
	if config.InvitationURL != "" {
		invitation, err := callvk.ParseInvitation(config.InvitationURL)
		if err != nil {
			return err
		}
		config.InvitationURL = invitation.CanonicalURL
	}
	for index := range config.Clients {
		config.Clients[index].ID = strings.TrimSpace(config.Clients[index].ID)
		config.Clients[index].Name = strings.TrimSpace(config.Clients[index].Name)
		config.Clients[index].SubscriptionToken = strings.TrimSpace(config.Clients[index].SubscriptionToken)
		if err := config.Clients[index].Profile.Normalize(); err != nil {
			return err
		}
	}
	return nil
}

func (config Config) Validate() error {
	if config.Version != CurrentVersion {
		return fmt.Errorf("call_server_unsupported_version")
	}
	if err := validEndpoint(config.ListenAddress, false); err != nil {
		return fmt.Errorf("call_server_invalid_listen_address")
	}
	if err := validEndpoint(config.BackendAddress, true); err != nil {
		return fmt.Errorf("call_server_invalid_backend_address")
	}
	if config.PublicEndpoint != "" {
		if err := validEndpoint(config.PublicEndpoint, false); err != nil {
			return fmt.Errorf("call_server_invalid_public_endpoint")
		}
		host, _, _ := net.SplitHostPort(config.PublicEndpoint)
		if _, err := netip.ParseAddr(host); err != nil {
			return fmt.Errorf("call_server_public_endpoint_must_be_ip")
		}
	}
	if config.InvitationURL != "" {
		if _, err := callvk.ParseInvitation(config.InvitationURL); err != nil {
			return err
		}
	}
	if config.SubscriptionBaseURL != "" {
		parsed, err := url.Parse(config.SubscriptionBaseURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("call_server_invalid_subscription_base_url")
		}
	}
	ids, tokens, identities := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, client := range config.Clients {
		if client.ID == "" || client.Name == "" || client.SubscriptionToken == "" {
			return fmt.Errorf("call_server_invalid_client_identity")
		}
		if ids[client.ID] || tokens[client.SubscriptionToken] || identities[client.Profile.VLESSUUID] {
			return fmt.Errorf("call_server_duplicate_client_identity")
		}
		ids[client.ID], tokens[client.SubscriptionToken], identities[client.Profile.VLESSUUID] = true, true, true
		if err := client.Profile.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validEndpoint(value string, loopback bool) error {
	host, port, err := net.SplitHostPort(value)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid_endpoint")
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if loopback && !address.IsLoopback() {
		return fmt.Errorf("endpoint_must_be_loopback")
	}
	return nil
}

func (client Client) TrafficUsed() uint64 { return client.TrafficRXBytes + client.TrafficTXBytes }

func (client Client) AvailableAt(now time.Time) bool {
	if !client.Enabled || (client.Profile.ExpiresAt > 0 && now.Unix() >= client.Profile.ExpiresAt) {
		return false
	}
	return client.Profile.TrafficLimitBytes == 0 || client.TrafficUsed() < client.Profile.TrafficLimitBytes
}

func (client Client) PublicAt(now time.Time) PublicClient {
	return PublicClient{ID: client.ID, Name: client.Name, Enabled: client.Enabled, CreatedAt: client.CreatedAt,
		ExpiresAt: client.Profile.ExpiresAt, TrafficLimitBytes: client.Profile.TrafficLimitBytes,
		TrafficUsedBytes: client.TrafficUsed(), TrafficRXBytes: client.TrafficRXBytes, TrafficTXBytes: client.TrafficTXBytes,
		LastSeenAt: client.LastSeenAt, Available: client.AvailableAt(now)}
}

func (config Config) PublicAt(now time.Time) PublicConfig {
	clients := make([]PublicClient, 0, len(config.Clients))
	for _, client := range config.Clients {
		clients = append(clients, client.PublicAt(now))
	}
	return PublicConfig{Version: config.Version, Enabled: config.Enabled, ListenAddress: config.ListenAddress,
		PublicEndpoint: config.PublicEndpoint, BackendAddress: config.BackendAddress,
		InvitationConfigured: config.InvitationURL != "", SubscriptionBaseURL: config.SubscriptionBaseURL, Clients: clients}
}
