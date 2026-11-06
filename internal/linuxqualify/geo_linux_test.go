//go:build linux

package linuxqualify

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestGeoRetriesEarlyConnectionCloseOnSameEndpoint(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			connection, _, _ := w.(http.Hijacker).Hijack()
			connection.Close()
			return
		}
		w.Write([]byte("ip=192.0.2.1\nloc=NL\n"))
	}))
	defer server.Close()
	client := server.Client()
	client.Transport = &http.Transport{DisableKeepAlives: true}
	defer client.CloseIdleConnections()
	started := time.Now()
	country, status := geoCountry(context.Background(), client, "", server.URL)
	if country != "NL" || status != "" || calls.Load() != 2 {
		t.Fatalf("retry failed: %s %s calls=%d", country, status, calls.Load())
	}
	if time.Since(started) < 190*time.Millisecond {
		t.Fatal("retry did not wait for startup to settle")
	}
}

func TestGeoDoesNotRetryHTTPFailureOrUnknownCountry(t *testing.T) {
	for _, code := range []int{200, 403, 429} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(code)
			w.Write([]byte("no_country_here"))
		}))
		country, status := geoCountry(context.Background(), server.Client(), "", server.URL)
		server.Close()
		if country != "" || status == "" || calls.Load() != 1 {
			t.Fatalf("permanent error retried: %d %s %s %d", code, country, status, calls.Load())
		}
	}
}

func TestGeoRetryIsBoundedAndPreservesCountry(t *testing.T) {
	calls := 0
	country, status := retryCountry(context.Background(), func() (string, string, bool) { calls++; return "", "geo_failed", true })
	if calls != 2 || country != "" || status != "geo_failed" {
		t.Fatal(calls, country, status)
	}
	calls = 0
	country, status = retryCountry(context.Background(), func() (string, string, bool) { calls++; return "RU", "", false })
	if calls != 1 || country != "RU" || status != "" {
		t.Fatal("valid excluded country must reach the existing country filter unchanged")
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	_, status = retryCountry(ctx, func() (string, string, bool) { calls++; cancel(); return "", "geo_failed", true })
	if calls != 1 || status != "geo_failed" {
		t.Fatal("cancelled qualification retried")
	}
}

func TestGeoCertificateFailuresAreNotTransient(t *testing.T) {
	for _, err := range []error{x509.UnknownAuthorityError{}, x509.HostnameError{}, x509.CertificateInvalidError{}, errors.New("invalid TLS record")} {
		if temporaryGeoError(err) {
			t.Fatalf("permanent TLS error accepted as transient: %v", err)
		}
	}
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, syscall.ECONNRESET, &net.OpError{Op: "remote error", Net: "tcp", Err: errors.New("tls: handshake failure")}} {
		if !temporaryGeoError(err) {
			t.Fatalf("early connection close not retried: %v", err)
		}
	}
}
