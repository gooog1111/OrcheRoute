package reversevpn

import (
	"context"
	"path/filepath"
	"testing"
)

type fakeAdapter struct {
	active   bool
	applyErr error
	applies  int
	disables int
	counters map[string]PeerCounters
}

func (f *fakeAdapter) Apply(context.Context, Config) error {
	f.applies++
	if f.applyErr != nil {
		return f.applyErr
	}
	f.active = true
	return nil
}
func (f *fakeAdapter) Disable(context.Context, string) error {
	f.disables++
	f.active = false
	return nil
}
func (f *fakeAdapter) Active(context.Context, string) bool { return f.active }
func (f *fakeAdapter) Counters(context.Context, string) (map[string]PeerCounters, error) {
	return f.counters, nil
}

func TestManagerLifecycleAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reverse-vpn.json")
	adapter := &fakeAdapter{}
	manager, err := Open(path, adapter)
	if err != nil {
		t.Fatal(err)
	}
	public := manager.PublicConfig()
	public.PublicEndpoint = "vpn.example:51820"
	if _, err := manager.Update(public); err != nil {
		t.Fatal(err)
	}
	client, err := manager.CreateClient("Phone")
	if err != nil {
		t.Fatal(err)
	}
	if client.Address != "10.77.0.2/32" {
		t.Fatalf("unexpected address %s", client.Address)
	}
	if err := manager.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !manager.Status(context.Background()).Active {
		t.Fatal("adapter not active")
	}
	profile, err := manager.ClientProfile(client.ID)
	if err != nil || profile == "" {
		t.Fatalf("profile: %q %v", profile, err)
	}
	reopened, err := Open(path, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.PublicConfig().Enabled || len(reopened.PublicConfig().Clients) != 1 {
		t.Fatal("state not persisted")
	}
	if err := reopened.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAccountingDisablesPeerAtTrafficLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reverse-vpn.json")
	adapter := &fakeAdapter{counters: map[string]PeerCounters{}}
	manager, err := Open(path, adapter)
	if err != nil {
		t.Fatal(err)
	}
	public := manager.PublicConfig()
	public.PublicEndpoint = "vpn.example:51820"
	if _, err := manager.Update(public); err != nil {
		t.Fatal(err)
	}
	client, err := manager.CreateClientWithOptions(CreateClientOptions{Name: "Limited", TrafficLimitBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	adapter.counters[client.PublicKey] = PeerCounters{RXBytes: 70, TXBytes: 40, LastHandshake: 123}
	if err := manager.RefreshAccounting(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adapter.applies != 2 {
		t.Fatalf("quota crossing did not reapply peers: %d", adapter.applies)
	}
	if _, _, err := manager.SubscriptionProfile(client.SubscriptionToken); err == nil || err.Error() != "subscription_inactive" {
		t.Fatalf("unexpected subscription result: %v", err)
	}
	reopened, err := Open(path, adapter)
	if err != nil {
		t.Fatal(err)
	}
	clients := reopened.PublicConfig().Clients
	if len(clients) != 1 || clients[0].TrafficUsedBytes != 110 || clients[0].Available {
		t.Fatalf("unexpected accounting: %#v", clients)
	}
}

func TestApplyFailureDoesNotPersistEnabledState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reverse-vpn.json")
	adapter := &fakeAdapter{applyErr: context.DeadlineExceeded}
	manager, err := Open(path, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply(context.Background()); err == nil {
		t.Fatal("expected apply failure")
	}
	if manager.PublicConfig().Enabled {
		t.Fatal("failed apply persisted enabled state")
	}
	reopened, err := Open(path, &fakeAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.PublicConfig().Enabled {
		t.Fatal("failed state survived restart")
	}
}
