package calltransport

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/turn/v5"
)

type TURNConfig struct {
	ServerAddress string
	Username      string
	Password      string
	Realm         string
	Network       string
}

type Underlay interface {
	ListenPacket(context.Context, string, string) (net.PacketConn, error)
	DialContext(context.Context, string, string) (net.Conn, error)
}

type systemUnderlay struct{}

func (systemUnderlay) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return (&net.ListenConfig{}).ListenPacket(ctx, network, address)
}

func (systemUnderlay) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
}

type TURNAllocation struct {
	net.PacketConn
	client    *turn.Client
	underlay  net.PacketConn
	transport net.Conn
}

func (allocation *TURNAllocation) Close() error {
	err := allocation.PacketConn.Close()
	allocation.client.Close()
	if allocation.underlay != nil {
		_ = allocation.underlay.Close()
	}
	if allocation.transport != nil {
		_ = allocation.transport.Close()
	}
	return err
}

func AllocateTURN(ctx context.Context, config TURNConfig, underlay Underlay) (*TURNAllocation, error) {
	config.ServerAddress = strings.TrimSpace(config.ServerAddress)
	config.Network = strings.ToLower(strings.TrimSpace(config.Network))
	if config.Network == "" {
		config.Network = "udp"
	}
	if _, _, err := net.SplitHostPort(config.ServerAddress); err != nil {
		return nil, fmt.Errorf("call_transport_invalid_turn_address: %w", err)
	}
	if config.Username == "" || config.Password == "" {
		return nil, fmt.Errorf("call_transport_turn_credentials_required")
	}
	if config.Network != "udp" && config.Network != "tcp" {
		return nil, fmt.Errorf("call_transport_turn_network_unsupported")
	}
	if underlay == nil {
		underlay = systemUnderlay{}
	}

	var packet net.PacketConn
	var packetUnderlay net.PacketConn
	var streamUnderlay net.Conn
	var err error
	if config.Network == "udp" {
		packetUnderlay, err = underlay.ListenPacket(ctx, "udp", "0.0.0.0:0")
		packet = packetUnderlay
	} else {
		streamUnderlay, err = underlay.DialContext(ctx, "tcp", config.ServerAddress)
		if err == nil {
			packet = turn.NewSTUNConn(streamUnderlay)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("call_transport_turn_underlay: %w", err)
	}

	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr:         config.ServerAddress,
		TURNServerAddr:         config.ServerAddress,
		Username:               config.Username,
		Password:               config.Password,
		Realm:                  config.Realm,
		Software:               "OrcheRoute",
		Conn:                   packet,
		RTO:                    time.Second,
		RequestedAddressFamily: turn.RequestedAddressFamilyIPv4,
	})
	if err != nil {
		_ = packet.Close()
		return nil, fmt.Errorf("call_transport_turn_client: %w", err)
	}
	if err := client.Listen(); err != nil {
		client.Close()
		_ = packet.Close()
		return nil, fmt.Errorf("call_transport_turn_listen: %w", err)
	}
	relay, err := client.Allocate()
	if err != nil {
		client.Close()
		_ = packet.Close()
		return nil, fmt.Errorf("call_transport_turn_allocate: %w", err)
	}
	return &TURNAllocation{PacketConn: relay, client: client, underlay: packetUnderlay, transport: streamUnderlay}, nil
}

type directedPacketConn struct {
	net.PacketConn
	peer net.Addr
}

func (connection *directedPacketConn) WriteTo(buffer []byte, _ net.Addr) (int, error) {
	return connection.PacketConn.WriteTo(buffer, connection.peer)
}

// DialTURNDTLS establishes the authenticated datagram carrier used by the VK
// adapter. TURN credentials come from the call provider; the PSK belongs to
// the OrcheRoute subscription and authenticates the two OrcheRoute endpoints.
func DialTURNDTLS(ctx context.Context, config TURNConfig, peer *net.UDPAddr, psk []byte, underlay Underlay) (net.Conn, error) {
	return DialTURNDTLSWithIdentity(ctx, config, peer, dtlsIdentity, psk, underlay)
}

// DialTURNDTLSWithIdentity selects the client's independent server-side PSK.
func DialTURNDTLSWithIdentity(ctx context.Context, config TURNConfig, peer *net.UDPAddr, identity string, psk []byte, underlay Underlay) (net.Conn, error) {
	identity = strings.TrimSpace(identity)
	if peer == nil || identity == "" || len(identity) > 128 || len(psk) < 16 {
		return nil, fmt.Errorf("call_transport_invalid_dtls_parameters")
	}
	allocation, err := AllocateTURN(ctx, config, underlay)
	if err != nil {
		return nil, err
	}
	carrier, err := dtls.ClientWithOptions(&directedPacketConn{PacketConn: allocation, peer: peer}, peer,
		dtls.WithPSK(func([]byte) ([]byte, error) { return psk, nil }),
		dtls.WithPSKIdentityHint([]byte(identity)),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	if err != nil {
		_ = allocation.Close()
		return nil, fmt.Errorf("call_transport_dtls_client: %w", err)
	}
	handshake, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := carrier.HandshakeContext(handshake); err != nil {
		_ = carrier.Close()
		_ = allocation.Close()
		return nil, fmt.Errorf("call_transport_dtls_handshake: %w", err)
	}
	return &carrierWithAllocation{Conn: carrier, allocation: allocation}, nil
}

type carrierWithAllocation struct {
	net.Conn
	allocation *TURNAllocation
}

func (carrier *carrierWithAllocation) Close() error {
	err := carrier.Conn.Close()
	_ = carrier.allocation.Close()
	return err
}
