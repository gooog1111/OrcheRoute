package calltransport

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
)

func TestDTLSKCPMuxCarriesVLESSStream(t *testing.T) {
	secret := bytes.Repeat([]byte{0x6f}, 32)
	psk := func([]byte) ([]byte, error) { return secret, nil }
	listener, err := dtls.ListenWithOptions("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
		dtls.WithPSK(psk),
		dtls.WithPSKIdentityHint([]byte("orcheroute")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
		dtls.WithConnectionIDGenerator(dtls.RandomCIDGenerator(8)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

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

	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverError := make(chan error, 1)
	go func() {
		carrier, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverError <- acceptErr
			return
		}
		reliable, reliableErr := AcceptReliableServer(carrier, 3*time.Second)
		if reliableErr != nil {
			serverError <- reliableErr
			return
		}
		serverError <- ServeServer(ctx, reliable, backend.Addr().String(), nil)
	}()

	remote := listener.Addr().(*net.UDPAddr)
	carrier, err := dtls.DialWithOptions("udp", remote,
		dtls.WithPSK(psk),
		dtls.WithPSKIdentityHint([]byte("orcheroute")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	if err != nil {
		t.Fatal(err)
	}
	reliable, err := NewReliableClient(carrier)
	if err != nil {
		t.Fatal(err)
	}
	clientError := make(chan error, 1)
	go func() { clientError <- ServeClient(ctx, reliable, local) }()

	connection, err := net.DialTimeout("tcp", local.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("vless-through-call-carrier")
	if _, err := connection.Write(payload); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, response); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if !bytes.Equal(response, payload) {
		t.Fatalf("unexpected response %q", response)
	}

	cancel()
	for _, result := range []<-chan error{clientError, serverError} {
		select {
		case serveErr := <-result:
			if serveErr != nil {
				t.Errorf("serve: %v", serveErr)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("DTLS/KCP pipeline did not stop")
		}
	}
}
