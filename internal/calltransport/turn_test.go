package calltransport

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

const testTURNUsername, testTURNPassword, testTURNRealm = "orcheroute", "test-password", "orcheroute.test"

func newTestTURNServer(t *testing.T) (net.PacketConn, *turn.Server) {
	t.Helper()
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := turn.NewServer(turn.ServerConfig{
		Realm:         testTURNRealm,
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		AuthHandler: func(attributes *turn.RequestAttributes) (string, []byte, bool) {
			if attributes.Username != testTURNUsername {
				return "", nil, false
			}
			return attributes.Username, turn.GenerateAuthKey(attributes.Username, attributes.Realm, testTURNPassword), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: listener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"),
				Address:      "0.0.0.0",
			},
		}},
	})
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	return listener, server
}

func testTURNConfig(address string) TURNConfig {
	return TURNConfig{ServerAddress: address, Username: testTURNUsername, Password: testTURNPassword, Realm: testTURNRealm, Network: "udp"}
}

func TestTURNAllocationCarriesDatagrams(t *testing.T) {
	listener, server := newTestTURNServer(t)
	defer listener.Close()
	defer server.Close()

	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	go func() {
		buffer := make([]byte, 1500)
		count, address, readErr := peer.ReadFrom(buffer)
		if readErr == nil {
			_, _ = peer.WriteTo(buffer[:count], address)
		}
	}()

	allocation, err := AllocateTURN(context.Background(), testTURNConfig(listener.LocalAddr().String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer allocation.Close()
	if err := allocation.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("orcheroute-turn-call")
	if _, err := allocation.WriteTo(payload, peer.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	count, _, err := allocation.ReadFrom(response)
	if err != nil {
		t.Fatal(err)
	}
	if string(response[:count]) != string(payload) {
		t.Fatalf("unexpected TURN response %q", response[:count])
	}
}

func TestTURNDTLSAuthenticatesOrcheRouteEndpoints(t *testing.T) {
	turnListener, turnServer := newTestTURNServer(t)
	defer turnListener.Close()
	defer turnServer.Close()

	secret := bytes.Repeat([]byte{0x72}, 32)
	psk := func([]byte) ([]byte, error) { return secret, nil }
	dtlsListener, err := dtls.ListenWithOptions("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
		dtls.WithPSK(psk),
		dtls.WithPSKIdentityHint([]byte("orcheroute-call")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
		dtls.WithConnectionIDGenerator(dtls.RandomCIDGenerator(8)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer dtlsListener.Close()
	go func() {
		connection, acceptErr := dtlsListener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	carrier, err := DialTURNDTLS(context.Background(), testTURNConfig(turnListener.LocalAddr().String()), dtlsListener.Addr().(*net.UDPAddr), secret, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer carrier.Close()
	if err := carrier.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("orcheroute-vless-over-turn-dtls")
	if _, err := carrier.Write(payload); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(carrier, response); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response, payload) {
		t.Fatalf("unexpected DTLS response %q", response)
	}
}
