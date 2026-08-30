package callserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
)

type CreateClientInput struct {
	Name              string `json:"name"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64 `json:"traffic_limit_bytes,omitempty"`
}

type UpdateClientInput struct {
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	TrafficLimitBytes uint64 `json:"traffic_limit_bytes,omitempty"`
	ResetTraffic      bool   `json:"reset_traffic,omitempty"`
	RotateToken       bool   `json:"rotate_token,omitempty"`
}

type Manager struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	rand interface{ Read([]byte) (int, error) }
	data Config
}

type RuntimeSnapshot struct {
	Enabled        bool
	ListenAddress  string
	BackendAddress string
	Keys           map[string][]byte
	Clients        []callxray.Client
	Ordinary       OrdinarySnapshot
}

func Open(path string) (*Manager, error) {
	manager := &Manager{path: path, now: time.Now, rand: rand.Reader, data: DefaultConfig()}
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return manager, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &manager.data); err != nil {
		return nil, fmt.Errorf("call_server_config_invalid: %w", err)
	}
	if err := manager.data.Normalize(); err != nil {
		return nil, fmt.Errorf("call_server_config_invalid: %w", err)
	}
	identityChanged, err := manager.ensureServerIdentityLocked(&manager.data)
	if err != nil {
		return nil, fmt.Errorf("call_server_identity_invalid: %w", err)
	}
	if err := manager.data.Validate(); err != nil {
		return nil, fmt.Errorf("call_server_config_invalid: %w", err)
	}
	if identityChanged {
		if err := manager.saveLocked(manager.data); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (manager *Manager) PublicConfig() PublicConfig {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.data.PublicAt(manager.now())
}

func (manager *Manager) UpdateConfig(input Config) (PublicConfig, error) {
	return manager.updateConfig(input, true)
}

// UpdatePublicConfig applies settings received from a secret-free API state.
// Omitting the invitation keeps the existing call credential; an explicitly
// provided empty value still clears it.
func (manager *Manager) UpdatePublicConfig(input Config, invitationProvided bool) (PublicConfig, error) {
	return manager.updateConfig(input, invitationProvided)
}

func (manager *Manager) updateConfig(input Config, invitationProvided bool) (PublicConfig, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	previousEndpoint := manager.data.PublicEndpoint
	previousInvitation := manager.data.InvitationURL
	input.Clients = append([]Client(nil), manager.data.Clients...)
	input.Enabled = manager.data.Enabled
	input.RealityPrivateKey, input.RealityPublicKey, input.RealityShortID = manager.data.RealityPrivateKey, manager.data.RealityPublicKey, manager.data.RealityShortID
	input.TLSCertificate, input.TLSPrivateKey = manager.data.TLSCertificate, manager.data.TLSPrivateKey
	if !invitationProvided {
		input.InvitationURL = manager.data.InvitationURL
	}
	if err := input.Normalize(); err != nil {
		return PublicConfig{}, err
	}
	endpointChanged := input.PublicEndpoint != previousEndpoint
	invitationChanged := invitationProvided && input.InvitationURL != previousInvitation
	if endpointChanged || invitationChanged {
		for index := range input.Clients {
			if endpointChanged {
				input.Clients[index].Profile.PeerAddress = input.PublicEndpoint
			}
			if invitationChanged {
				input.Clients[index].Profile.InvitationURL = input.InvitationURL
			}
			if err := input.Clients[index].Profile.Normalize(); err != nil {
				return PublicConfig{}, err
			}
		}
	}
	if _, err := manager.ensureServerIdentityLocked(&input); err != nil {
		return PublicConfig{}, err
	}
	if err := input.Validate(); err != nil {
		return PublicConfig{}, err
	}
	if err := manager.saveLocked(input); err != nil {
		return PublicConfig{}, err
	}
	manager.data = input
	return input.PublicAt(manager.now()), nil
}

func (manager *Manager) SetEnabled(enabled bool) (PublicConfig, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	candidate := manager.data
	candidate.Enabled = enabled
	if _, err := manager.ensureServerIdentityLocked(&candidate); err != nil {
		return PublicConfig{}, err
	}
	if err := candidate.Validate(); err != nil {
		return PublicConfig{}, err
	}
	if err := manager.saveLocked(candidate); err != nil {
		return PublicConfig{}, err
	}
	manager.data = candidate
	return candidate.PublicAt(manager.now()), nil
}

func (manager *Manager) CreateClient(input CreateClientInput) (Client, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.now()
	if input.ExpiresAt != 0 && input.ExpiresAt <= now.Unix() {
		return Client{}, fmt.Errorf("call_server_client_expiry_must_be_future")
	}
	if manager.data.InvitationURL == "" || manager.data.PublicEndpoint == "" {
		return Client{}, fmt.Errorf("call_server_not_configured")
	}
	if _, err := manager.ensureServerIdentityLocked(&manager.data); err != nil {
		return Client{}, err
	}
	id, err := manager.randomString(12)
	if err != nil {
		return Client{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = defaultClientName(id)
	}
	token, err := manager.randomString(32)
	if err != nil {
		return Client{}, err
	}
	profile, err := callprofile.New(callprofile.NewInput{Name: name, InvitationURL: manager.data.InvitationURL,
		PeerAddress: manager.data.PublicEndpoint, ExpiresAt: input.ExpiresAt, TrafficLimitBytes: input.TrafficLimitBytes,
		Random: manager.rand, Now: now})
	if err != nil {
		return Client{}, err
	}
	client := Client{ID: id, Name: name, Enabled: true, CreatedAt: now.Unix(), SubscriptionToken: token, Profile: profile}
	candidate := manager.data
	candidate.Clients = append(append([]Client(nil), manager.data.Clients...), client)
	if err := candidate.Validate(); err != nil {
		return Client{}, err
	}
	if err := manager.saveLocked(candidate); err != nil {
		return Client{}, err
	}
	manager.data = candidate
	return client, nil
}

func (manager *Manager) UpdateClient(id string, input UpdateClientInput) (Client, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = defaultClientName(id)
	}
	if input.Enabled && input.ExpiresAt != 0 && input.ExpiresAt <= manager.now().Unix() {
		return Client{}, fmt.Errorf("call_server_client_expiry_must_be_future")
	}
	candidate := manager.data
	candidate.Clients = append([]Client(nil), manager.data.Clients...)
	for index := range candidate.Clients {
		client := &candidate.Clients[index]
		if client.ID != id {
			continue
		}
		client.Name, client.Enabled = name, input.Enabled
		client.Profile.Name, client.Profile.ExpiresAt, client.Profile.TrafficLimitBytes = name, input.ExpiresAt, input.TrafficLimitBytes
		if input.ResetTraffic {
			client.TrafficRXBytes, client.TrafficTXBytes = 0, 0
		}
		if input.RotateToken {
			token, err := manager.randomString(32)
			if err != nil {
				return Client{}, err
			}
			client.SubscriptionToken = token
		}
		if err := candidate.Validate(); err != nil {
			return Client{}, err
		}
		if err := manager.saveLocked(candidate); err != nil {
			return Client{}, err
		}
		manager.data = candidate
		return *client, nil
	}
	return Client{}, fmt.Errorf("call_server_client_not_found")
}

func defaultClientName(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 6 {
		id = id[:6]
	}
	if id == "" {
		return "OrcheRoute"
	}
	return "OrcheRoute " + id
}

func (manager *Manager) DeleteClient(id string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	candidate := manager.data
	candidate.Clients = make([]Client, 0, len(manager.data.Clients))
	found := false
	for _, client := range manager.data.Clients {
		if client.ID == id {
			found = true
			continue
		}
		candidate.Clients = append(candidate.Clients, client)
	}
	if !found {
		return fmt.Errorf("call_server_client_not_found")
	}
	if err := manager.saveLocked(candidate); err != nil {
		return err
	}
	manager.data = candidate
	return nil
}

func (manager *Manager) ClientProfile(id string) (string, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, client := range manager.data.Clients {
		if client.ID == id {
			return manager.encodeClientLocked(client)
		}
	}
	return "", fmt.Errorf("call_server_client_not_found")
}

func (manager *Manager) SubscriptionSecret(id string) (string, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, client := range manager.data.Clients {
		if client.ID == id {
			return client.SubscriptionToken, nil
		}
	}
	return "", fmt.Errorf("call_server_client_not_found")
}

func (manager *Manager) SubscriptionProfile(token string) (string, PublicClient, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, client := range manager.data.Clients {
		if len(token) != len(client.SubscriptionToken) || subtle.ConstantTimeCompare([]byte(token), []byte(client.SubscriptionToken)) != 1 {
			continue
		}
		encoded, err := manager.encodeClientLocked(client)
		return encoded, client.PublicAt(manager.now()), err
	}
	return "", PublicClient{}, fmt.Errorf("call_server_subscription_not_found")
}

func (manager *Manager) SubscriptionURL(token string) string {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	path := "/subscription/call/" + token
	if manager.data.SubscriptionBaseURL == "" {
		return path
	}
	return manager.data.SubscriptionBaseURL + path
}

func (manager *Manager) TransportKeys() (map[string][]byte, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	keys := map[string][]byte{}
	for _, client := range manager.data.Clients {
		if !client.AvailableAt(manager.now()) {
			continue
		}
		psk, err := client.Profile.PSKBytes()
		if err != nil {
			return nil, err
		}
		keys[client.Profile.VLESSUUID] = psk
	}
	return keys, nil
}

func (manager *Manager) XrayClients() []callxray.Client {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	result := []callxray.Client{}
	for _, client := range manager.data.Clients {
		if client.AvailableAt(manager.now()) {
			result = append(result, callxray.Client{ID: client.Profile.VLESSUUID, Email: client.ID})
		}
	}
	return result
}

func (manager *Manager) RuntimeSnapshot() (RuntimeSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	snapshot := RuntimeSnapshot{Enabled: manager.data.Enabled, ListenAddress: manager.data.ListenAddress, BackendAddress: manager.data.BackendAddress, Keys: map[string][]byte{}, Clients: []callxray.Client{},
		Ordinary: OrdinarySnapshot{Enabled: manager.data.OrdinaryEnabled, VLESSListenAddress: manager.data.VLESSListenAddress,
			TrojanListenAddress: manager.data.TrojanListenAddress, HysteriaListenAddress: manager.data.HysteriaListenAddress,
			FakeSNI: manager.data.FakeSNI, RealityPrivateKey: manager.data.RealityPrivateKey, RealityShortID: manager.data.RealityShortID,
			TLSCertificate: manager.data.TLSCertificate, TLSPrivateKey: manager.data.TLSPrivateKey}}
	for _, client := range manager.data.Clients {
		if !client.AvailableAt(manager.now()) {
			continue
		}
		psk, err := client.Profile.PSKBytes()
		if err != nil {
			return RuntimeSnapshot{}, err
		}
		snapshot.Keys[client.Profile.VLESSUUID] = psk
		snapshot.Clients = append(snapshot.Clients, callxray.Client{ID: client.Profile.VLESSUUID, Email: client.ID})
		snapshot.Ordinary.Clients = append(snapshot.Ordinary.Clients, OrdinaryClient{ID: client.ID, Name: client.Name,
			VLESSUUID: client.Profile.VLESSUUID, TrojanPassword: protocolPassword(client.Profile.PSK, "trojan"),
			HysteriaPassword: protocolPassword(client.Profile.PSK, "hysteria2")})
	}
	return snapshot, nil
}

func (manager *Manager) RecordTraffic(_ context.Context, id string, rx, tx uint64, seenAt int64) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	candidate := manager.data
	candidate.Clients = append([]Client(nil), manager.data.Clients...)
	for index := range candidate.Clients {
		client := &candidate.Clients[index]
		if client.ID != id {
			continue
		}
		client.TrafficRXBytes += rx
		client.TrafficTXBytes += tx
		if seenAt > client.LastSeenAt {
			client.LastSeenAt = seenAt
		}
		if err := manager.saveLocked(candidate); err != nil {
			return err
		}
		manager.data = candidate
		return nil
	}
	return fmt.Errorf("call_server_client_not_found")
}

func (manager *Manager) encodeClientLocked(client Client) (string, error) {
	if !client.AvailableAt(manager.now()) {
		return "", fmt.Errorf("call_server_subscription_inactive")
	}
	return manager.encodeSubscriptionLocked(client)
}

func (manager *Manager) randomString(size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(manager.rand, value); err != nil {
		return "", err
	}
	if size == 12 {
		return hex.EncodeToString(value), nil
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (manager *Manager) saveLocked(config Config) error {
	directory := filepath.Dir(manager.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".call-server-*.json")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
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
	return os.Rename(name, manager.path)
}
