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
	"math/big"
	"time"
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
	return changed, nil
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
