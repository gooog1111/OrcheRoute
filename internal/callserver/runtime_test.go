package callserver

import (
	"context"
	"io"
	"net"
	"testing"
)

type testBackend struct {
	listener net.Listener
	run      *testBackendRun
	starts   int
}

type testBackendRun struct {
	net.Listener
	traffic map[string]Traffic
	health  error
}

type testRelay struct {
	starts int
	run    *testRelayRun
}

type testRelayRun struct {
	health error
	closed bool
}

func (relay *testRelay) Start(context.Context, RuntimeSnapshot) (io.Closer, error) {
	relay.starts++
	relay.run = &testRelayRun{}
	return relay.run, nil
}

func (running *testRelayRun) Alive() error { return running.health }
func (running *testRelayRun) Close() error { running.closed = true; return nil }

func (running *testBackendRun) Alive() error { return running.health }

func (running *testBackendRun) DrainTraffic() map[string]Traffic {
	result := running.traffic
	running.traffic = map[string]Traffic{}
	return result
}

func (backend *testBackend) Start(_ context.Context, snapshot RuntimeSnapshot) (io.Closer, error) {
	listener, err := net.Listen("tcp", snapshot.BackendAddress)
	if err != nil {
		return nil, err
	}
	backend.listener = listener
	backend.run = &testBackendRun{Listener: listener, traffic: map[string]Traffic{}}
	backend.starts++
	return backend.run, nil
}

func TestRuntimeAppliesAndStopsConfiguredRegistry(t *testing.T) {
	manager, _ := configuredManager(t)
	config := manager.data
	config.ListenAddress = freeUDPAddress(t)
	config.BackendAddress = freeTCPAddress(t)
	if _, err := manager.UpdateConfig(config); err != nil {
		t.Fatal(err)
	}
	client, err := manager.CreateClient(CreateClientInput{Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	backend, relay := &testBackend{}, &testRelay{}
	runtime := newRuntimeWithRelay(backend, relay)
	if err := runtime.Apply(manager); err != nil {
		t.Fatal(err)
	}
	status := runtime.Status()
	if !status.Active || status.Clients != 1 || backend.listener == nil {
		t.Fatalf("unexpected runtime status: %#v", status)
	}
	if err := runtime.Apply(manager); err != nil || backend.starts != 1 || relay.starts != 1 {
		t.Fatalf("unchanged runtime restarted: starts=%d err=%v", backend.starts, err)
	}
	backend.run.health = io.EOF
	if err := runtime.Apply(manager); err != nil || backend.starts != 2 || relay.starts != 2 {
		t.Fatalf("failed backend was not restarted: starts=%d err=%v", backend.starts, err)
	}
	backend.run.traffic[client.ID] = Traffic{RXBytes: 60, TXBytes: 40}
	if err := runtime.Apply(manager); err != nil || backend.starts != 2 {
		t.Fatalf("traffic sync restarted runtime: starts=%d err=%v", backend.starts, err)
	}
	public := manager.PublicConfig().Clients[0]
	if public.TrafficRXBytes != 60 || public.TrafficTXBytes != 40 || public.LastSeenAt == 0 {
		t.Fatalf("runtime traffic was not persisted: %#v", public)
	}
	if _, err := manager.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Apply(manager); err != nil {
		t.Fatal(err)
	}
	if runtime.Status().Active {
		t.Fatal("disabled runtime remained active")
	}
}

func TestFreeTURNRuntimeUsesExternalRelayAndRestartsAfterFailure(t *testing.T) {
	manager, _ := configuredManager(t)
	config := manager.data
	config.ListenAddress = freeUDPAddress(t)
	config.BackendAddress = freeTCPAddress(t)
	if _, err := manager.UpdateConfig(config); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateClient(CreateClientInput{Name: "Phone"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	backend, relay := &testBackend{}, &testRelay{}
	runtime := newRuntimeWithRelay(backend, relay)
	if err := runtime.Apply(manager); err != nil {
		t.Fatal(err)
	}
	if relay.starts != 1 || !runtime.Status().Active {
		t.Fatalf("relay starts=%d status=%#v", relay.starts, runtime.Status())
	}
	relay.run.health = io.EOF
	if err := runtime.Apply(manager); err != nil {
		t.Fatal(err)
	}
	if relay.starts != 2 {
		t.Fatalf("failed relay was not restarted: %d", relay.starts)
	}
	if err := runtime.Close(); err != nil || !relay.run.closed {
		t.Fatalf("relay was not closed: err=%v run=%#v", err, relay.run)
	}
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func freeUDPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.LocalAddr().String()
	_ = listener.Close()
	return address
}
