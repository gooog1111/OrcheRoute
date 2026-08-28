package calltransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/dtls/v3"
)

const dtlsIdentity = "orcheroute-call"

// ListenDTLS creates the public UDP endpoint used by the server side of a
// call carrier. The subscription PSK authenticates OrcheRoute endpoints; it
// is independent from short-lived credentials issued by a TURN provider.
func ListenDTLS(address string, psk []byte) (net.Listener, error) {
	return ListenDTLSProfiles(address, map[string][]byte{dtlsIdentity: psk})
}

// ListenDTLSProfiles serves multiple independent client profiles on one UDP
// endpoint. The client profile ID is sent as the DTLS PSK identity and selects
// only that client's PSK; no profile shares a transport key with another.
func ListenDTLSProfiles(address string, profiles map[string][]byte) (net.Listener, error) {
	lookup, err := profilePSKLookup(profiles)
	if err != nil {
		return nil, err
	}
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("call_transport_invalid_listen_address: %w", err)
	}
	listener, err := dtls.ListenWithOptions(
		"udp",
		udpAddress,
		dtls.WithPSK(lookup),
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

func profilePSKLookup(profiles map[string][]byte) (func([]byte) ([]byte, error), error) {
	if len(profiles) == 0 {
		return nil, fmt.Errorf("call_transport_dtls_profiles_required")
	}
	keys := make(map[string][]byte, len(profiles))
	for rawIdentity, rawPSK := range profiles {
		identity := strings.TrimSpace(rawIdentity)
		if identity == "" || len(identity) > 128 || len(rawPSK) < 16 {
			return nil, fmt.Errorf("call_transport_invalid_dtls_profile")
		}
		keys[identity] = append([]byte(nil), rawPSK...)
	}
	return func(identity []byte) ([]byte, error) {
		psk, ok := keys[string(identity)]
		if !ok {
			return nil, fmt.Errorf("call_transport_unknown_dtls_identity")
		}
		return append([]byte(nil), psk...), nil
	}, nil
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
