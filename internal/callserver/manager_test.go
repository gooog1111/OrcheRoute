package callserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
	"github.com/gooog1111/orcheroute/internal/nodes"
)

func configuredManager(t *testing.T) (*Manager, time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	manager, err := Open(filepath.Join(t.TempDir(), "call-server.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now }
	config := DefaultConfig()
	config.PublicEndpoint = "203.0.113.20:4443"
	config.InvitationURL = "https://vk.com/call/join/test-invite"
	config.SubscriptionBaseURL = "https://vpn.example"
	if _, err := manager.UpdateConfig(config); err != nil {
		t.Fatal(err)
	}
	return manager, now
}

func TestManagerPersistsClientsWithoutLeakingSecrets(t *testing.T) {
	manager, now := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{Name: "Phone", ExpiresAt: now.Add(time.Hour).Unix(), TrafficLimitBytes: 1000})
	if err != nil {
		t.Fatal(err)
	}
	public := manager.PublicConfig()
	if len(public.Clients) != 1 || !public.Clients[0].Available || public.Clients[0].Name != "Phone" {
		t.Fatalf("unexpected public config: %#v", public)
	}
	text := strings.Join([]string{client.Profile.PSK, client.Profile.InvitationURL, client.SubscriptionToken}, " ")
	if strings.Contains(mustJSON(t, public), client.Profile.PSK) || strings.Contains(mustJSON(t, public), client.Profile.InvitationURL) || strings.Contains(mustJSON(t, public), client.SubscriptionToken) {
		t.Fatalf("public config leaked secrets from %s", text)
	}
	reopened, err := Open(manager.path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = manager.now
	profileURI, exposed, err := reopened.SubscriptionProfile(client.SubscriptionToken)
	if err != nil || exposed.ID != client.ID {
		t.Fatalf("persisted subscription missing: %v, %#v", err, exposed)
	}
	profiles := strings.Split(profileURI, "\n")
	if len(profiles) != 4 || !strings.HasPrefix(profiles[1], "vless://") || !strings.HasPrefix(profiles[2], "trojan://") || !strings.HasPrefix(profiles[3], "hysteria2://") {
		t.Fatalf("ordinary protocols missing from subscription: %q", profileURI)
	}
	converted := nodes.ConvertLinks(profiles, "personal")
	if len(converted.Proxies) != 4 || len(converted.Errors) != 0 {
		t.Fatalf("generated subscription is not accepted by the shared parser: proxies=%d errors=%#v", len(converted.Proxies), converted.Errors)
	}
	for _, proxy := range converted.Proxies[1:] {
		if proxy["server"] != "vpn.example" {
			t.Fatalf("ordinary protocol ignored public DNS name: %#v", proxy)
		}
	}
	decoded, err := callprofile.Decode(profiles[0], now)
	if err != nil || decoded.VLESSUUID != client.Profile.VLESSUUID {
		t.Fatalf("unexpected persisted profile: %#v, %v", decoded.Public(), err)
	}
	if !decoded.UsesPacketTunnel() || decoded.PacketTunnel.Carrier != "vk-turn" || decoded.PacketTunnel.Mode != "awg" {
		t.Fatalf("packet tunnel missing from generated subscription: %#v", decoded.PacketTunnel)
	}
	if !strings.HasSuffix(reopened.SubscriptionURL(client.SubscriptionToken), "/subscription/"+client.SubscriptionToken) {
		t.Fatal("unexpected subscription URL")
	}
}

func TestOpenMigratesVersionTwoWithoutChangingClientToken(t *testing.T) {
	manager, _ := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{Name: "Existing"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := manager.data
	legacy.Version = 2
	legacy.PacketPrivateKey = ""
	legacy.PacketObfuscationKey = ""
	legacy.Clients[0].Profile.PacketTunnel = nil
	if err := manager.saveLocked(legacy); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(manager.path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.data.Clients[0]
	if got.SubscriptionToken != client.SubscriptionToken || got.Profile.VLESSUUID != client.Profile.VLESSUUID {
		t.Fatalf("migration replaced client identity: before=%#v after=%#v", client.PublicAt(manager.now()), got.PublicAt(manager.now()))
	}
	if reopened.data.Version != CurrentVersion || got.Profile.PacketTunnel == nil {
		t.Fatalf("packet migration missing: version=%d profile=%#v", reopened.data.Version, got.Profile.PacketTunnel)
	}
}

func TestManagerAcceptsHostnameAndGeneratesOptionalClientName(t *testing.T) {
	manager, _ := configuredManager(t)
	config := manager.data
	config.PublicEndpoint = "vpn.example:4443"
	if _, err := manager.UpdateConfig(config); err != nil {
		t.Fatal(err)
	}
	client, err := manager.CreateClient(CreateClientInput{})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name != "OrcheRoute" || client.Profile.PeerAddress != "vpn.example:4443" {
		t.Fatalf("unexpected generated client: %#v", client.PublicAt(manager.now()))
	}
	profiles, err := manager.ClientProfile(client.ID)
	if err != nil || !strings.Contains(profiles, "vpn.example") {
		t.Fatalf("hostname missing from subscription: %q, %v", profiles, err)
	}
}

func TestManagerMigratesLegacyGeneratedClientName(t *testing.T) {
	manager, _ := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{})
	if err != nil {
		t.Fatal(err)
	}
	manager.data.Clients[0].Name = "OrcheRoute " + client.ID[:6]
	manager.data.Clients[0].Profile.Name = manager.data.Clients[0].Name
	if err := manager.saveLocked(manager.data); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(manager.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.data.Clients[0].Name != "OrcheRoute" || reopened.data.Clients[0].Profile.Name != "OrcheRoute" {
		t.Fatalf("legacy technical name survived migration: %#v", reopened.data.Clients[0])
	}
}

func TestManagerEnforcesExpiryAndTrafficLimit(t *testing.T) {
	manager, now := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{Name: "Limited", ExpiresAt: now.Add(time.Hour).Unix(), TrafficLimitBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RecordTraffic(context.Background(), client.ID, 60, 41, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.SubscriptionProfile(client.SubscriptionToken); err == nil {
		t.Fatal("traffic-limited subscription remained active")
	}
	keys, err := manager.TransportKeys()
	if err != nil || len(keys) != 0 || len(manager.XrayClients()) != 0 {
		t.Fatalf("inactive client reached runtime: keys=%d xray=%d err=%v", len(keys), len(manager.XrayClients()), err)
	}
	manager.now = func() time.Time { return now.Add(2 * time.Hour) }
	_, err = Open(manager.path)
	if err != nil {
		t.Fatalf("expired client made persisted registry unloadable: %v", err)
	}
}

func TestManagerUpdatesAndDeletesOneClient(t *testing.T) {
	manager, now := configuredManager(t)
	first, err := manager.CreateClient(CreateClientInput{Name: "First", ExpiresAt: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.CreateClient(CreateClientInput{Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.UpdateClient(first.ID, UpdateClientInput{Name: "Renamed", Enabled: false, RotateToken: true})
	if err != nil || updated.SubscriptionToken == first.SubscriptionToken || updated.Enabled {
		t.Fatalf("unexpected update: %#v, %v", updated.PublicAt(now), err)
	}
	if err := manager.DeleteClient(first.ID); err != nil {
		t.Fatal(err)
	}
	public := manager.PublicConfig()
	if len(public.Clients) != 1 || public.Clients[0].ID != second.ID {
		t.Fatalf("delete affected the wrong client: %#v", public.Clients)
	}
}

func TestPublicConfigUpdatePreservesOmittedInvitation(t *testing.T) {
	manager, _ := configuredManager(t)
	public := manager.PublicConfig()
	config := Config{Version: public.Version, ListenAddress: public.ListenAddress, PublicEndpoint: public.PublicEndpoint,
		BackendAddress: public.BackendAddress, SubscriptionBaseURL: public.SubscriptionBaseURL}
	if _, err := manager.UpdatePublicConfig(config, false, false); err != nil {
		t.Fatal(err)
	}
	if !manager.PublicConfig().InvitationConfigured {
		t.Fatal("secret invitation was cleared by a public settings update")
	}
	if _, err := manager.CreateClient(CreateClientInput{Name: "Still configured"}); err != nil {
		t.Fatalf("preserved invitation could not issue a client: %v", err)
	}
}

func TestPublicConfigUpdateMovesExistingProfilesToHostnameAndNewInvitation(t *testing.T) {
	manager, _ := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	packetPrivateKey, packetObfuscationKey := manager.data.PacketPrivateKey, manager.data.PacketObfuscationKey
	packetConfig := client.Profile.PacketTunnel.Config
	config := manager.data
	config.PublicEndpoint = "vpn.example:4443"
	config.InvitationURL = "https://vk.ru/call/join/new-invite"
	if _, err := manager.UpdatePublicConfig(config, true, true); err != nil {
		t.Fatal(err)
	}
	updated := manager.data.Clients[0]
	if updated.ID != client.ID || updated.Profile.PeerAddress != "vpn.example:4443" || updated.Profile.InvitationURL != "https://vk.com/call/join/new-invite" {
		t.Fatalf("existing profile kept stale route: %#v", updated.Profile.Public())
	}
	if manager.data.PacketPrivateKey != packetPrivateKey || manager.data.PacketObfuscationKey != packetObfuscationKey || updated.Profile.PacketTunnel.Config != packetConfig {
		t.Fatal("public settings update rotated the packet tunnel identity")
	}
}

func TestPublicConfigAddsInvitationWithoutReplacingPrimary(t *testing.T) {
	manager, now := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	config := manager.data
	config.InvitationURL = ""
	config.InvitationURLs = []string{"https://vk.ru/call/join/second-invite"}
	if _, err := manager.UpdatePublicConfig(config, false, true); err != nil {
		t.Fatal(err)
	}
	updated := manager.data.Clients[0]
	links := updated.Profile.AllInvitationURLs()
	if updated.ID != client.ID || len(links) != 2 || links[0] != "https://vk.com/call/join/test-invite" || links[1] != "https://vk.com/call/join/second-invite" {
		t.Fatalf("existing profile did not receive the additional invitation: %#v", links)
	}
	encoded, _, err := manager.SubscriptionProfile(updated.SubscriptionToken)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := callprofile.Decode(strings.Split(encoded, "\n")[0], now)
	if err != nil || len(decoded.AllInvitationURLs()) != 2 {
		t.Fatalf("subscription lost additional invitation: %#v, %v", decoded.AllInvitationURLs(), err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
