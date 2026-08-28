package callserver

import (
	"context"
	"io"
	"net"
	"testing"

	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
)

type testBackend struct {
	listener net.Listener
	starts   int
}

func (backend *testBackend) Start(_ context.Context, address string, _ []callxray.Client) (io.Closer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	backend.listener = listener
	backend.starts++
	return listener, nil
}

func TestRuntimeAppliesAndStopsConfiguredRegistry(t *testing.T) {
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
	backend := &testBackend{}
	runtime := NewRuntime(backend)
	if err := runtime.Apply(manager); err != nil {
		t.Fatal(err)
	}
	status := runtime.Status()
	if !status.Active || status.Clients != 1 || backend.listener == nil {
		t.Fatalf("unexpected runtime status: %#v", status)
	}
	if err := runtime.Apply(manager); err != nil || backend.starts != 1 {
		t.Fatalf("unchanged runtime restarted: starts=%d err=%v", backend.starts, err)
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
