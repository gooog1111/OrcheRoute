package callserver

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/nodes"
	"github.com/metacubex/mihomo/component/ca"
	mtls "github.com/metacubex/tls"
)

func TestGeneratedOrdinaryProfilesPinCertificateWithoutInsecureFlag(t *testing.T) {
	manager, _ := configuredManager(t)
	client, err := manager.CreateClient(CreateClientInput{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	profile, _, err := manager.SubscriptionProfile(client.SubscriptionToken)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := certificateFingerprint(manager.data.TLSCertificate)
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range strings.Split(profile, "\n") {
		if !strings.HasPrefix(link, "trojan://") && !strings.HasPrefix(link, "hysteria2://") {
			continue
		}
		proxy, err := nodes.ParseLink(link, "test", 1)
		if err != nil || proxy["fingerprint"] != expected || proxy["skip-cert-verify"] == true {
			t.Fatalf("pin mapping failed: %v", err)
		}
	}
}

func TestMihomoTLSRejectsSubstitutedCertificate(t *testing.T) {
	certPEM, keyPEM, err := selfSignedCertificate("m.vk.ru", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{identity}}
	server.StartTLS()
	defer server.Close()
	correct, _ := certificateFingerprint(certPEM)
	otherPEM, _, err := selfSignedCertificate("m.vk.ru", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wrong, _ := certificateFingerprint(otherPEM)
	for _, tc := range []struct {
		pin     string
		allowed bool
	}{{correct, true}, {wrong, false}} {
		config, err := ca.GetTLSConfig(ca.Option{TLSConfig: &mtls.Config{ServerName: "m.vk.ru"}, Fingerprint: tc.pin})
		if err != nil {
			t.Fatal(err)
		}
		connection, err := mtls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", server.Listener.Addr().String(), config)
		if connection != nil {
			connection.Close()
		}
		if (err == nil) != tc.allowed {
			t.Fatalf("allowed=%t error=%v", tc.allowed, err)
		}
	}
}
