package reversevpn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
)

type Adapter interface {
	Apply(context.Context, Config) error
	Disable(context.Context, string) error
	Active(context.Context, string) bool
}

type PeerCounters struct {
	RXBytes       uint64
	TXBytes       uint64
	LastHandshake int64
}

type AccountingAdapter interface {
	Counters(context.Context, string) (map[string]PeerCounters, error)
}

type CreateClientOptions struct {
	Name              string `json:"name"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64 `json:"traffic_limit_bytes,omitempty"`
}

type UpdateClientOptions struct {
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64 `json:"traffic_limit_bytes,omitempty"`
	ResetTraffic      bool   `json:"reset_traffic,omitempty"`
	RotateToken       bool   `json:"rotate_token,omitempty"`
}

type Manager struct {
	mu         sync.Mutex
	path       string
	adapter    Adapter
	config     Config
	status     Status
	now        func() time.Time
	needsApply bool
}

func Open(path string, adapter Adapter) (*Manager, error) {
	manager := &Manager{path: path, adapter: adapter, now: time.Now}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		manager.config = DefaultConfig()
		manager.updateStatusLocked()
		return manager, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &manager.config); err != nil {
		return nil, fmt.Errorf("reverse_vpn_config_invalid: %w", err)
	}
	manager.config.Normalize()
	if err := manager.config.Validate(); err != nil {
		return nil, fmt.Errorf("reverse_vpn_config_invalid: %w", err)
	}
	manager.updateStatusLocked()
	return manager, nil
}

func (m *Manager) PublicConfig() PublicConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config.Public()
}

func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.DesiredEnabled = m.config.Enabled
	m.status.Clients = len(m.config.Clients)
	if m.adapter != nil {
		m.status.Active = m.adapter.Active(ctx, m.config.InterfaceName)
	}
	return m.status
}

func (m *Manager) Update(input PublicConfig) (PublicConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if input.Enabled != m.config.Enabled {
		return PublicConfig{}, fmt.Errorf("enabled_is_changed_by_apply_or_disable")
	}
	if m.config.Enabled {
		return PublicConfig{}, fmt.Errorf("disable_before_changing_settings")
	}
	candidate := m.config
	candidate.Version = input.Version
	candidate.InterfaceName = input.InterfaceName
	candidate.ListenPort = input.ListenPort
	candidate.ServerCIDR = input.ServerCIDR
	candidate.MTU = input.MTU
	candidate.DNS = append([]string(nil), input.DNS...)
	candidate.OutboundInterface = input.OutboundInterface
	candidate.PublicEndpoint = input.PublicEndpoint
	candidate.SubscriptionBaseURL = input.SubscriptionBaseURL
	candidate.Transport = input.Transport
	candidate.Normalize()
	if err := candidate.Validate(); err != nil {
		return PublicConfig{}, err
	}
	if err := m.saveLocked(candidate); err != nil {
		return PublicConfig{}, err
	}
	m.config = candidate
	m.updateStatusLocked()
	return candidate.Public(), nil
}

func (m *Manager) CreateClient(name string) (Client, error) {
	return m.CreateClientWithOptions(CreateClientOptions{Name: name})
}

func (m *Manager) CreateClientWithOptions(options CreateClientOptions) (Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return Client{}, fmt.Errorf("client_name_required")
	}
	if options.ExpiresAt > 0 && options.ExpiresAt <= m.now().Unix() {
		return Client{}, fmt.Errorf("client_expiry_must_be_future")
	}
	privateKey, publicKey, err := keyPair()
	if err != nil {
		return Client{}, err
	}
	address, err := nextAddress(m.config)
	if err != nil {
		return Client{}, err
	}
	randomID := make([]byte, 8)
	if _, err := rand.Read(randomID); err != nil {
		return Client{}, err
	}
	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		return Client{}, err
	}
	client := Client{ID: hex.EncodeToString(randomID), Name: name, Address: address,
		PrivateKey: privateKey, PublicKey: publicKey, SubscriptionToken: base64.RawURLEncoding.EncodeToString(token),
		Enabled: true, CreatedAt: m.now().Unix(), ExpiresAt: options.ExpiresAt, TrafficLimitBytes: options.TrafficLimitBytes}
	candidate := m.config
	if candidate.PrivateKey == "" || candidate.PublicKey == "" {
		candidate.PrivateKey, candidate.PublicKey, err = keyPair()
		if err != nil {
			return Client{}, err
		}
	}
	candidate.Clients = append(append([]Client(nil), candidate.Clients...), client)
	if err := candidate.Validate(); err != nil {
		return Client{}, err
	}
	if err := m.saveLocked(candidate); err != nil {
		return Client{}, err
	}
	m.config = candidate
	if candidate.Enabled {
		m.needsApply = true
	}
	m.updateStatusLocked()
	return client, nil
}

func (m *Manager) DeleteClient(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate := m.config
	candidate.Clients = make([]Client, 0, len(m.config.Clients))
	found := false
	for _, client := range m.config.Clients {
		if client.ID == id {
			found = true
			continue
		}
		candidate.Clients = append(candidate.Clients, client)
	}
	if !found {
		return fmt.Errorf("client_not_found")
	}
	if err := m.saveLocked(candidate); err != nil {
		return err
	}
	m.config = candidate
	if candidate.Enabled {
		m.needsApply = true
	}
	m.updateStatusLocked()
	return nil
}

func (m *Manager) UpdateClient(id string, options UpdateClientOptions) (Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return Client{}, fmt.Errorf("client_name_required")
	}
	if options.Enabled && options.ExpiresAt > 0 && options.ExpiresAt <= m.now().Unix() {
		return Client{}, fmt.Errorf("client_expiry_must_be_future")
	}
	candidate := m.config
	candidate.Clients = append([]Client(nil), m.config.Clients...)
	for index := range candidate.Clients {
		client := &candidate.Clients[index]
		if client.ID != id {
			continue
		}
		client.Name, client.Enabled = name, options.Enabled
		client.ExpiresAt, client.TrafficLimitBytes = options.ExpiresAt, options.TrafficLimitBytes
		if options.ResetTraffic {
			client.TrafficRXBytes, client.TrafficTXBytes = 0, 0
		}
		if options.RotateToken {
			token := make([]byte, 24)
			if _, err := rand.Read(token); err != nil {
				return Client{}, err
			}
			client.SubscriptionToken = base64.RawURLEncoding.EncodeToString(token)
		}
		if err := candidate.Validate(); err != nil {
			return Client{}, err
		}
		if err := m.saveLocked(candidate); err != nil {
			return Client{}, err
		}
		m.config = candidate
		if candidate.Enabled {
			m.needsApply = true
		}
		m.updateStatusLocked()
		return *client, nil
	}
	return Client{}, fmt.Errorf("client_not_found")
}

func (m *Manager) SubscriptionSecret(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, client := range m.config.Clients {
		if client.ID == id {
			return client.SubscriptionToken, nil
		}
	}
	return "", fmt.Errorf("client_not_found")
}

func (m *Manager) SubscriptionURL(token string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := "/subscription/reverse/" + token
	if m.config.SubscriptionBaseURL == "" {
		return path
	}
	return m.config.SubscriptionBaseURL + path
}

func (m *Manager) ClientProfile(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.PublicEndpoint == "" {
		return "", fmt.Errorf("public_endpoint_required")
	}
	if m.config.PublicKey == "" {
		return "", fmt.Errorf("server_keys_not_generated")
	}
	for _, client := range m.config.Clients {
		if client.ID != id {
			continue
		}
		return m.clientProfileLocked(client)
	}
	return "", fmt.Errorf("client_not_found")
}

func (m *Manager) SubscriptionProfile(token string) (string, PublicClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, client := range m.config.Clients {
		if len(token) != len(client.SubscriptionToken) || subtle.ConstantTimeCompare([]byte(token), []byte(client.SubscriptionToken)) != 1 {
			continue
		}
		profile, err := m.clientProfileLocked(client)
		public := m.publicClientLocked(client)
		return profile, public, err
	}
	return "", PublicClient{}, fmt.Errorf("subscription_not_found")
}

func (m *Manager) clientProfileLocked(client Client) (string, error) {
	if m.config.PublicEndpoint == "" {
		return "", fmt.Errorf("public_endpoint_required")
	}
	if m.config.PublicKey == "" {
		return "", fmt.Errorf("server_keys_not_generated")
	}
	if !client.AvailableAt(m.now()) {
		return "", fmt.Errorf("subscription_inactive")
	}
	dns := strings.Join(m.config.DNS, ", ")
	if dns == "" {
		dns = "1.1.1.1"
	}
	return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s\nDNS = %s\nMTU = %d\n\n[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = 0.0.0.0/0\nPersistentKeepalive = 25\n",
		client.PrivateKey, client.Address, dns, m.config.MTU, m.config.PublicKey, m.config.PublicEndpoint), nil
}

func (m *Manager) publicClientLocked(client Client) PublicClient {
	return PublicClient{ID: client.ID, Name: client.Name, Address: client.Address, PublicKey: client.PublicKey,
		Enabled: client.Enabled, CreatedAt: client.CreatedAt, ExpiresAt: client.ExpiresAt,
		TrafficLimitBytes: client.TrafficLimitBytes, TrafficUsedBytes: client.TrafficUsed(),
		TrafficRXBytes: client.TrafficRXBytes, TrafficTXBytes: client.TrafficTXBytes,
		LastSeenAt: client.LastSeenAt, Available: client.AvailableAt(m.now())}
}

func (m *Manager) Apply(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.adapter == nil {
		return fmt.Errorf("reverse_vpn_not_supported")
	}
	if m.config.Enabled && m.adapter.Active(ctx, m.config.InterfaceName) {
		if _, err := m.captureCountersLocked(ctx); err != nil {
			m.status.LastError = err.Error()
			return fmt.Errorf("accounting_snapshot_failed: %w", err)
		}
	}
	previous := m.config
	candidate := m.config
	if candidate.PrivateKey == "" || candidate.PublicKey == "" {
		var err error
		candidate.PrivateKey, candidate.PublicKey, err = keyPair()
		if err != nil {
			return err
		}
	}
	candidate.Enabled = true
	if err := candidate.Validate(); err != nil {
		return err
	}
	if err := m.adapter.Apply(ctx, candidate); err != nil {
		m.status.LastError = err.Error()
		if previous.Enabled {
			_ = m.adapter.Apply(context.Background(), previous)
		}
		return err
	}
	if err := m.saveLocked(candidate); err != nil {
		if previous.Enabled {
			_ = m.adapter.Apply(context.Background(), previous)
		} else {
			_ = m.adapter.Disable(context.Background(), candidate.InterfaceName)
		}
		return err
	}
	m.config = candidate
	m.status.LastAppliedAt = m.now().Unix()
	m.status.LastError = ""
	m.needsApply = false
	m.updateStatusLocked()
	return nil
}

func (m *Manager) Disable(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.adapter != nil {
		if err := m.adapter.Disable(ctx, m.config.InterfaceName); err != nil {
			m.status.LastError = err.Error()
			return err
		}
	}
	candidate := m.config
	candidate.Enabled = false
	if err := m.saveLocked(candidate); err != nil {
		return err
	}
	m.config = candidate
	m.status.LastError = ""
	m.needsApply = false
	m.updateStatusLocked()
	return nil
}

func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	enabled := m.config.Enabled
	m.mu.Unlock()
	if !enabled {
		return nil
	}
	return m.Apply(ctx)
}

func (m *Manager) RefreshAccounting(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.config.Enabled {
		return nil
	}
	availabilityChanged, err := m.captureCountersLocked(ctx)
	if err != nil {
		m.status.LastError = err.Error()
		return err
	}
	if availabilityChanged {
		m.needsApply = true
	}
	if m.needsApply {
		if err := m.adapter.Apply(ctx, m.config); err != nil {
			m.status.LastError = err.Error()
			return err
		}
		m.needsApply = false
		m.status.LastAppliedAt = m.now().Unix()
		m.status.LastError = ""
	}
	return nil
}

func (m *Manager) captureCountersLocked(ctx context.Context) (bool, error) {
	accounting, ok := m.adapter.(AccountingAdapter)
	if !ok {
		return false, nil
	}
	counters, err := accounting.Counters(ctx, m.config.InterfaceName)
	if err != nil {
		return false, err
	}
	candidate := m.config
	changed := false
	availabilityChanged := false
	now := m.now()
	for index := range candidate.Clients {
		client := &candidate.Clients[index]
		wasAvailable := client.AvailableAt(now)
		counter, found := counters[client.PublicKey]
		if found {
			client.TrafficRXBytes += counterDelta(client.LastCounterRXBytes, counter.RXBytes)
			client.TrafficTXBytes += counterDelta(client.LastCounterTXBytes, counter.TXBytes)
			client.LastCounterRXBytes, client.LastCounterTXBytes = counter.RXBytes, counter.TXBytes
			if counter.LastHandshake > 0 {
				client.LastSeenAt = counter.LastHandshake
			}
			changed = true
			if !client.AvailableAt(now) {
				availabilityChanged = true
			}
		}
		if wasAvailable != client.AvailableAt(now) {
			availabilityChanged = true
		}
	}
	if changed {
		if err := m.saveLocked(candidate); err != nil {
			return false, err
		}
		m.config = candidate
	}
	return availabilityChanged, nil
}

func counterDelta(previous, current uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	return current
}

func (m *Manager) updateStatusLocked() {
	m.status.DesiredEnabled = m.config.Enabled
	m.status.InterfaceName = m.config.InterfaceName
	m.status.Transport = m.config.Transport
	m.status.Clients = len(m.config.Clients)
}

func (m *Manager) saveLocked(config Config) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(m.path), ".reverse-vpn-*.json")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.path)
}

func keyPair() (string, string, error) {
	private := make([]byte, curve25519.ScalarSize)
	if _, err := rand.Read(private); err != nil {
		return "", "", err
	}
	private[0] &= 248
	private[31] &= 127
	private[31] |= 64
	public, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(private), base64.StdEncoding.EncodeToString(public), nil
}

func nextAddress(config Config) (string, error) {
	prefix, err := netip.ParsePrefix(config.ServerCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid_server_cidr")
	}
	used := map[netip.Addr]bool{prefix.Addr(): true}
	for _, client := range config.Clients {
		if value, err := netip.ParsePrefix(client.Address); err == nil {
			used[value.Addr()] = true
		}
	}
	address := prefix.Masked().Addr().Next()
	for prefix.Contains(address) {
		if !used[address] && !address.IsUnspecified() {
			return address.String() + "/32", nil
		}
		address = address.Next()
	}
	return "", fmt.Errorf("client_address_space_exhausted")
}

func ConfigFingerprint(config Config) string {
	data, _ := json.Marshal(config.Public())
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}
