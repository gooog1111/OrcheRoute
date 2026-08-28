package calltransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/dtls/v3"
)

const dtlsIdentity = "orcheroute-call"

// ListenDTLS creates the public UDP endpoint used by the server side of a
// call carrier. The subscription PSK authenticates OrcheRoute endpoints; it
// is independent from short-lived credentials issued by a TURN provider.
func ListenDTLS(address string, psk []byte) (net.Listener, error) {
	if len(psk) < 16 {
		return nil, fmt.Errorf("call_transport_invalid_dtls_psk")
	}
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("call_transport_invalid_listen_address: %w", err)
	}
	listener, err := dtls.ListenWithOptions(
		"udp",
		udpAddress,
		dtls.WithPSK(func(identity []byte) ([]byte, error) {
			if string(identity) != dtlsIdentity {
				return nil, fmt.Errorf("call_transport_unknown_dtls_identity")
			}
			return psk, nil
		}),
		dtls.WithPSKIdentityHint([]byte(dtlsIdentity)),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
		dtls.WithConnectionIDGenerator(dtls.RandomCIDGenerator(8)),
	)
	if err != nil {
		return nil, fmt.Errorf("call_transport_dtls_listen: %w", err)
	}
	return listener, nil
}

// ServeDTLS accepts authenticated carrier sessions and forwards their smux
// streams to the local Xray VLESS inbound. A failed client session is isolated
// and does not terminate the public listener.
func ServeDTLS(ctx context.Context, listener net.Listener, backendAddress string, dial DialContextFunc) error {
	if listener == nil {
		return fmt.Errorf("call_transport_listener_required")
	}
	if _, _, err := net.SplitHostPort(backendAddress); err != nil {
		return fmt.Errorf("call_transport_invalid_backend: %w", err)
	}

	var sessions sync.WaitGroup
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-done:
		}
	}()
	defer close(done)
	defer sessions.Wait()

	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("call_transport_dtls_accept: %w", err)
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			defer connection.Close()

			dtlsConnection, ok := connection.(*dtls.Conn)
			if !ok {
				return
			}
			handshake, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := dtlsConnection.HandshakeContext(handshake)
			cancel()
			if err != nil {
				return
			}
			reliable, err := AcceptReliableServer(dtlsConnection, 30*time.Second)
			if err != nil {
				return
			}
			defer reliable.Close()
			_ = ServeServer(ctx, reliable, backendAddress, dial)
		}()
	}
}
