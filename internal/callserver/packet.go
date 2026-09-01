package callserver

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

type PacketSnapshot struct {
	InterfaceName    string
	InterfaceAddress string
	ListenAddress    string
	PrivateKey       []byte
	ObfuscationKey   []byte
	Peers            []PacketPeer
}

type PacketPeer struct {
	ClientID  string
	PublicKey []byte
	Address   netip.Addr
}

func (snapshot PacketSnapshot) ListenPort() (int, error) {
	_, raw, err := net.SplitHostPort(snapshot.ListenAddress)
	if err != nil {
		return 0, fmt.Errorf("call_server_invalid_backend_address")
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("call_server_invalid_backend_address")
	}
	return port, nil
}

func (snapshot PacketSnapshot) UAPIConfig() (string, error) {
	port, err := snapshot.ListenPort()
	if err != nil {
		return "", err
	}
	if len(snapshot.PrivateKey) != 32 {
		return "", fmt.Errorf("call_server_packet_identity_missing")
	}
	var output strings.Builder
	fmt.Fprintf(&output, "private_key=%s\nlisten_port=%d\nreplace_peers=true\n", hex.EncodeToString(snapshot.PrivateKey), port)
	for _, peer := range snapshot.Peers {
		if len(peer.PublicKey) != 32 || !peer.Address.Is4() {
			return "", fmt.Errorf("call_server_packet_peer_invalid")
		}
		fmt.Fprintf(&output, "public_key=%s\nreplace_allowed_ips=true\nallowed_ip=%s/32\n", hex.EncodeToString(peer.PublicKey), peer.Address)
	}
	return output.String(), nil
}

func packetPeer(client Client) (PacketPeer, error) {
	if client.Profile.PacketTunnel == nil {
		return PacketPeer{}, fmt.Errorf("call_server_packet_profile_missing")
	}
	privateValue, addressValue := "", ""
	for _, line := range strings.Split(client.Profile.PacketTunnel.Config, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "privatekey":
			if privateValue == "" {
				privateValue = strings.TrimSpace(value)
			}
		case "address":
			if addressValue == "" {
				addressValue = strings.TrimSpace(strings.Split(value, ",")[0])
			}
		}
	}
	privateKey, err := base64.StdEncoding.DecodeString(privateValue)
	if err != nil || len(privateKey) != 32 {
		return PacketPeer{}, fmt.Errorf("call_server_packet_client_key_invalid")
	}
	key, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return PacketPeer{}, fmt.Errorf("call_server_packet_client_key_invalid")
	}
	prefix, err := netip.ParsePrefix(addressValue)
	if err != nil || !prefix.Addr().Is4() {
		return PacketPeer{}, fmt.Errorf("call_server_packet_client_address_invalid")
	}
	return PacketPeer{ClientID: client.ID, PublicKey: key.PublicKey().Bytes(), Address: prefix.Addr()}, nil
}
