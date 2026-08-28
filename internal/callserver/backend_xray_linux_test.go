//go:build linux

package callserver

import (
	"context"
	"net"
	"testing"
	"time"

	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
)

func TestEmbeddedXrayBackendStartsVLESSListener(t *testing.T) {
	address := freeTCPAddress(t)
	backend := EmbeddedXrayBackend{}
	running, err := backend.Start(context.Background(), address, []callxray.Client{{ID: "b831381d-6324-4d53-ad4f-8cda48b30811", Email: "phone"}})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("embedded Xray did not listen on %s: %v", address, err)
	}
	_ = connection.Close()
}
