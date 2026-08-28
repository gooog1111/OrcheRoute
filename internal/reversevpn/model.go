package reversevpn

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	CurrentVersion  = 1
	TransportDirect = "direct"
)

var interfaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

// Config is the persisted desired state of the inbound VPN server.  It is
// intentionally independent from the outbound OrcheRoute/Mihomo profile.
type Config struct {
	Version             int      `json:"version"`
	Enabled             bool     `json:"enabled"`
	InterfaceName       string   `json:"interface_name"`
	ListenPort          int      `json:"listen_port"`
	ServerCIDR          string   `json:"server_cidr"`
	MTU                 int      `json:"mtu"`
	DNS                 []string `json:"dns"`
	OutboundInterface   string   `json:"outbound_interface,omitempty"`
	PublicEndpoint      string   `json:"public_endpoint,omitempty"`
	SubscriptionBaseURL string   `json:"subscription_base_url,omitempty"`
	Transport           string   `json:"transport"`
	PrivateKey          string   `json:"private_key,omitempty"`
	PublicKey           string   `json:"public_key,omitempty"`
	Clients             []Client `json:"clients"`
}

type Client struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Address            string `json:"address"`
	PrivateKey         string `json:"private_key,omitempty"`
	PublicKey          string `json:"public_key"`
	SubscriptionToken  string `json:"subscription_token,omitempty"`
	Enabled            bool   `json:"enabled"`
	CreatedAt          int64  `json:"created_at"`
	ExpiresAt          int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes  uint64 `json:"traffic_limit_bytes,omitempty"`
	TrafficRXBytes     uint64 `json:"traffic_rx_bytes"`
	TrafficTXBytes     uint64 `json:"traffic_tx_bytes"`
	LastCounterRXBytes uint64 `json:"last_counter_rx_bytes,omitempty"`
	LastCounterTXBytes uint64 `json:"last_counter_tx_bytes,omitempty"`
	LastSeenAt         int64  `json:"last_seen_at,omitempty"`
}

type PublicConfig struct {
	Version             int            `json:"version"`
	Enabled             bool           `json:"enabled"`
	InterfaceName       string         `json:"interface_name"`
	ListenPort          int            `json:"listen_port"`
	ServerCIDR          string         `json:"server_cidr"`
	MTU                 int            `json:"mtu"`
	DNS                 []string       `json:"dns"`
	OutboundInterface   string         `json:"outbound_interface,omitempty"`
	PublicEndpoint      string         `json:"public_endpoint,omitempty"`
	SubscriptionBaseURL string         `json:"subscription_base_url,omitempty"`
	Transport           string         `json:"transport"`
	PublicKey           string         `json:"public_key,omitempty"`
	Clients             []PublicClient `json:"clients"`
}

type PublicClient struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Address           string `json:"address"`
	PublicKey         string `json:"public_key"`
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

type Status struct {
	DesiredEnabled bool   `json:"desired_enabled"`
	Active         bool   `json:"active"`
	InterfaceName  string `json:"interface_name"`
	Transport      string `json:"transport"`
	Clients        int    `json:"clients"`
	LastAppliedAt  int64  `json:"last_applied_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

func DefaultConfig() Config {
	return Config{Version: CurrentVersion, InterfaceName: "or-reverse", ListenPort: 51820,
		ServerCIDR: "10.77.0.1/24", MTU: 1280, DNS: []string{"1.1.1.1"},
		Transport: TransportDirect, Clients: []Client{}}
}

func (c Config) Public() PublicConfig {
	clients := make([]PublicClient, 0, len(c.Clients))
	for _, client := range c.Clients {
		clients = append(clients, PublicClient{ID: client.ID, Name: client.Name, Address: client.Address,
			PublicKey: client.PublicKey, Enabled: client.Enabled, CreatedAt: client.CreatedAt,
			ExpiresAt: client.ExpiresAt, TrafficLimitBytes: client.TrafficLimitBytes,
			TrafficUsedBytes: client.TrafficUsed(), TrafficRXBytes: client.TrafficRXBytes,
			TrafficTXBytes: client.TrafficTXBytes, LastSeenAt: client.LastSeenAt, Available: client.AvailableAt(time.Now())})
	}
	return PublicConfig{Version: c.Version, Enabled: c.Enabled, InterfaceName: c.InterfaceName,
		ListenPort: c.ListenPort, ServerCIDR: c.ServerCIDR, MTU: c.MTU, DNS: append([]string(nil), c.DNS...),
		OutboundInterface: c.OutboundInterface, PublicEndpoint: c.PublicEndpoint, SubscriptionBaseURL: c.SubscriptionBaseURL, Transport: c.Transport, PublicKey: c.PublicKey, Clients: clients}
}

func (c Client) TrafficUsed() uint64 { return c.TrafficRXBytes + c.TrafficTXBytes }

func (c Client) AvailableAt(now time.Time) bool {
	if !c.Enabled || (c.ExpiresAt > 0 && now.Unix() >= c.ExpiresAt) {
		return false
	}
	return c.TrafficLimitBytes == 0 || c.TrafficUsed() < c.TrafficLimitBytes
}

func (c *Config) Normalize() {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	c.InterfaceName = strings.TrimSpace(c.InterfaceName)
	c.ServerCIDR = strings.TrimSpace(c.ServerCIDR)
	c.OutboundInterface = strings.TrimSpace(c.OutboundInterface)
	c.PublicEndpoint = strings.TrimSpace(c.PublicEndpoint)
	c.SubscriptionBaseURL = strings.TrimRight(strings.TrimSpace(c.SubscriptionBaseURL), "/")
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	if c.Transport == "" {
		c.Transport = TransportDirect
	}
	for index := range c.DNS {
		c.DNS[index] = strings.TrimSpace(c.DNS[index])
	}
	for index := range c.Clients {
		c.Clients[index].ID = strings.TrimSpace(c.Clients[index].ID)
		c.Clients[index].Name = strings.TrimSpace(c.Clients[index].Name)
		c.Clients[index].Address = strings.TrimSpace(c.Clients[index].Address)
	}
}

func (c Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported_version")
	}
	if !interfaceNamePattern.MatchString(c.InterfaceName) {
		return fmt.Errorf("invalid_interface_name")
	}
	if c.ListenPort < 1 || c.ListenPort > 65535 {
		return fmt.Errorf("invalid_listen_port")
	}
	prefix, err := netip.ParsePrefix(c.ServerCIDR)
	if err != nil || !prefix.Addr().Is4() || !prefix.IsValid() {
		return fmt.Errorf("invalid_server_cidr")
	}
	if c.MTU < 576 || c.MTU > 9000 {
		return fmt.Errorf("invalid_mtu")
	}
	if c.Transport != TransportDirect {
		return fmt.Errorf("unsupported_transport")
	}
	if c.OutboundInterface != "" && !interfaceNamePattern.MatchString(c.OutboundInterface) {
		return fmt.Errorf("invalid_outbound_interface")
	}
	if c.PublicEndpoint != "" {
		if _, _, err := net.SplitHostPort(c.PublicEndpoint); err != nil {
			return fmt.Errorf("invalid_public_endpoint")
		}
	}
	if c.SubscriptionBaseURL != "" {
		parsed, err := url.Parse(c.SubscriptionBaseURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("invalid_subscription_base_url")
		}
	}
	for _, value := range c.DNS {
		if _, err := netip.ParseAddr(value); err != nil {
			return fmt.Errorf("invalid_dns")
		}
	}
	ids, addresses, keys := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, client := range c.Clients {
		if client.ID == "" || client.Name == "" {
			return fmt.Errorf("invalid_client_identity")
		}
		if ids[client.ID] {
			return fmt.Errorf("duplicate_client_id")
		}
		ids[client.ID] = true
		address, err := netip.ParsePrefix(client.Address)
		if err != nil || !address.Addr().Is4() || address.Bits() != 32 || !prefix.Contains(address.Addr()) || address.Addr() == prefix.Addr() {
			return fmt.Errorf("invalid_client_address")
		}
		if addresses[address.String()] {
			return fmt.Errorf("duplicate_client_address")
		}
		addresses[address.String()] = true
		if client.PublicKey == "" || keys[client.PublicKey] {
			return fmt.Errorf("invalid_client_public_key")
		}
		keys[client.PublicKey] = true
	}
	return nil
}

func (c Config) SortedClients() []Client {
	result := append([]Client(nil), c.Clients...)
	sort.Slice(result, func(i, j int) bool { return result[i].Address < result[j].Address })
	return result
}
