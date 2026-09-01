package callserver

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/netip"
	"strings"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
)

func (manager *Manager) ensureServerIdentityLocked(config *Config) (bool, error) {
	changed := false
	if config.RealityPrivateKey == "" || config.RealityPublicKey == "" {
		privateKey, err := ecdh.X25519().GenerateKey(manager.rand)
		if err != nil {
			return false, err
		}
		config.RealityPrivateKey = base64.RawURLEncoding.EncodeToString(privateKey.Bytes())
		config.RealityPublicKey = base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
		changed = true
	}
	if config.RealityShortID == "" {
		value := make([]byte, 8)
		if _, err := manager.rand.Read(value); err != nil {
			return false, err
		}
		config.RealityShortID = hex.EncodeToString(value)
		changed = true
	}
	if config.TLSCertificate == "" || config.TLSPrivateKey == "" {
		certificate, privateKey, err := selfSignedCertificate(config.FakeSNI, manager.now())
		if err != nil {
			return false, err
		}
		config.TLSCertificate, config.TLSPrivateKey = certificate, privateKey
		changed = true
	}
	if config.PacketPrivateKey == "" {
		privateKey, err := ecdh.X25519().GenerateKey(manager.rand)
		if err != nil {
			return false, err
		}
		config.PacketPrivateKey = base64.RawURLEncoding.EncodeToString(privateKey.Bytes())
		changed = true
	}
	if config.PacketObfuscationKey == "" {
		value := make([]byte, 32)
		if _, err := manager.rand.Read(value); err != nil {
			return false, err
		}
		config.PacketObfuscationKey = base64.RawURLEncoding.EncodeToString(value)
		changed = true
	}
	for index := range config.Clients {
		if config.Clients[index].Profile.PacketTunnel != nil {
			continue
		}
		if err := manager.attachPacketProfileLocked(config, &config.Clients[index].Profile); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func (manager *Manager) attachPacketProfileLocked(config *Config, profile *callprofile.Profile) error {
	if profile.PacketTunnel != nil {
		return nil
	}
	serverPrivate, err := decode32ByteKey(config.PacketPrivateKey)
	if err != nil {
		return err
	}
	serverKey, err := ecdh.X25519().NewPrivateKey(serverPrivate)
	if err != nil {
		return err
	}
	clientKey, err := ecdh.X25519().GenerateKey(manager.rand)
	if err != nil {
		return err
	}
	address, err := nextPacketClientAddress(config.Clients)
	if err != nil {
		return err
	}
	profile.PacketTunnel = &callprofile.PacketTunnel{
		Carrier: "vk-turn", Mode: "awg", ObfuscationProfile: "rtpopus3",
		ObfuscationKey: config.PacketObfuscationKey,
		Config: fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\nDNS = 1.1.1.1, 8.8.8.8\n\n[Peer]\nPublicKey = %s\nAllowedIPs = 0.0.0.0/0\nPersistentKeepalive = 25\n",
			base64.StdEncoding.EncodeToString(clientKey.Bytes()), address,
			base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes())),
	}
	return profile.Normalize()
}

func nextPacketClientAddress(clients []Client) (string, error) {
	used := map[string]bool{"10.77.0.1": true}
	for _, client := range clients {
		if client.Profile.PacketTunnel == nil {
			continue
		}
		for _, line := range strings.Split(client.Profile.PacketTunnel.Config, "\n") {
			key, value, found := strings.Cut(line, "=")
			if !found || !strings.EqualFold(strings.TrimSpace(key), "Address") {
				continue
			}
			for _, raw := range strings.Split(value, ",") {
				prefix, parseErr := netip.ParsePrefix(strings.TrimSpace(raw))
				if parseErr == nil && prefix.Addr().Is4() {
					used[prefix.Addr().String()] = true
				}
			}
		}
	}
	for third := 0; third < 256; third++ {
		for fourth := 2; fourth < 255; fourth++ {
			candidate := fmt.Sprintf("10.77.%d.%d", third, fourth)
			if !used[candidate] {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("call_server_packet_address_exhausted")
}

func decode32ByteKey(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		decoded, err := encoding.DecodeString(strings.TrimSpace(value))
		if err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid_32_byte_key")
}

func selfSignedCertificate(serverName string, now time.Time) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: serverName}, DNSNames: []string{serverName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), nil
}
