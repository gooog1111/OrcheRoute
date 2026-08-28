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
	const identity = "client-phone"
	server, err := ListenDTLSProfiles("127.0.0.1:0", map[string][]byte{identity: psk})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- ServeDTLS(ctx, server, backend.Addr().String(), nil) }()

	peer := server.Addr().(*net.UDPAddr)
	carrier, err := DialTURNDTLSWithIdentity(ctx, testTURNConfig(turnListener.LocalAddr().String()), peer, identity, psk, nil)
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

func TestDTLSProfileLookupSeparatesClientKeys(t *testing.T) {
	phone := []byte("phone-0123456789abcdef0123456789")
	laptop := []byte("laptop-0123456789abcdef01234567")
	lookup, err := profilePSKLookup(map[string][]byte{"phone": phone, "laptop": laptop})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := lookup([]byte("phone"))
	if err != nil || string(resolved) != string(phone) {
		t.Fatalf("wrong client key: %q, %v", resolved, err)
	}
	resolved[0] ^= 0xff
	again, err := lookup([]byte("phone"))
	if err != nil || string(again) != string(phone) {
		t.Fatal("returned key mutated the stored profile")
	}
	if _, err := lookup([]byte("unknown")); err == nil {
		t.Fatal("unknown client identity was accepted")
	}
	if _, err := profilePSKLookup(map[string][]byte{"bad": []byte("short")}); err == nil {
		t.Fatal("short client PSK was accepted")
	}
}
