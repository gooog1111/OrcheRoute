package transportcheck

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

func TestVerifiedTLSDoesNotWaitForSlowHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(2 * time.Second)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	started := time.Now()
	if _, err := VerifiedTLS(context.Background(), []string{"https://example.com/delayed"}, roots, time.Second, dial); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("probe waited for HTTP response: %s", elapsed)
	}
	if requests.Load() != 0 {
		t.Fatal("transport probe sent an HTTP request")
	}
}

func TestVerifiedTLSRejectsWrongCertificateAndUnsafeTarget(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	if _, err := VerifiedTLS(context.Background(), []string{"https://wrong.example/"}, roots, time.Second, dial); err == nil {
		t.Fatal("TLS identity mismatch was accepted")
	}
	if _, err := VerifiedTLS(context.Background(), []string{"http://example.com/"}, roots, time.Second, dial); err == nil {
		t.Fatal("plain HTTP target was accepted")
	}
}
