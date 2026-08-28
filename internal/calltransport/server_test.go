package calltransport

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestTURNCarrierReachesXrayBackend(t *testing.T) {
	turnListener, turnServer := newTestTURNServer(t)
	defer turnListener.Close()
	defer turnServer.Close()

	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		connection, acceptErr := backend.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	psk := []byte("0123456789abcdef0123456789abcdef")
	server, err := ListenDTLS("127.0.0.1:0", psk)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- ServeDTLS(ctx, server, backend.Addr().String(), nil) }()

	peer := server.Addr().(*net.UDPAddr)
	carrier, err := DialTURNDTLS(ctx, testTURNConfig(turnListener.LocalAddr().String()), peer, psk, nil)
	if err != nil {
		t.Fatal(err)
	}
	reliable, err := NewReliableClient(carrier)
	if err != nil {
		t.Fatal(err)
	}

	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- ServeClient(ctx, reliable, local) }()

	connection, err := net.DialTimeout("tcp", local.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	payload := []byte("vless-through-turn-dtls-kcp-smux")
	if _, err = connection.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	if _, err = io.ReadFull(connection, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != string(payload) {
		t.Fatalf("unexpected payload: %q", response)
	}

	cancel()
	select {
	case err = <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
	select {
	case err = <-clientDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not stop after cancellation")
	}
}
