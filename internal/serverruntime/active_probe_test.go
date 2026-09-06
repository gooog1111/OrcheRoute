package serverruntime

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type redirectedDialer struct{ address string }

func (dialer redirectedDialer) Dial(network, _ string) (net.Conn, error) {
	return net.Dial(network, dialer.address)
}

func TestTLSTargetAvailableDoesNotWaitForSlowHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(2 * time.Second)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	started := time.Now()
	ok := tlsTargetsAvailable(context.Background(), redirectedDialer{server.Listener.Addr().String()}, []string{"example.com:443"}, roots, time.Second)
	if !ok {
		t.Fatal("verified TLS transport was reported unavailable")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("probe waited for HTTP response: %s", elapsed)
	}
	if requests.Load() != 0 {
		t.Fatal("transport probe sent an HTTP request")
	}
}

func TestTLSTargetRejectsWrongCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	if tlsTargetsAvailable(context.Background(), redirectedDialer{server.Listener.Addr().String()}, []string{"wrong.example:443"}, roots, time.Second) {
		t.Fatal("TLS identity mismatch was accepted")
	}
}
