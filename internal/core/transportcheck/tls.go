// Package transportcheck contains protocol-level availability checks shared by
// platform adapters. It does not decide when a VPN should start or fail over.
package transportcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type DialContext func(context.Context, string, string) (net.Conn, error)

// VerifiedTLS succeeds after an authenticated TLS handshake through dial. It
// deliberately does not send HTTP: restricted networks can deliver a valid
// tunnel while page responses take many seconds.
func VerifiedTLS(ctx context.Context, targets []string, roots *x509.CertPool, attemptTimeout time.Duration, dial DialContext) (time.Duration, error) {
	if dial == nil || attemptTimeout <= 0 {
		return 0, fmt.Errorf("invalid_tls_transport_check")
	}
	var lastErr error
	for _, target := range targets {
		serverName, address, err := tlsTarget(target)
		if err != nil {
			lastErr = err
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		started := time.Now()
		connection, err := dial(attemptCtx, "tcp", address)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		deadline := time.Now().Add(attemptTimeout)
		if parentDeadline, ok := attemptCtx.Deadline(); ok && parentDeadline.Before(deadline) {
			deadline = parentDeadline
		}
		_ = connection.SetDeadline(deadline)
		secure := tls.Client(connection, &tls.Config{ServerName: serverName, RootCAs: roots, MinVersion: tls.VersionTLS12})
		err = secure.HandshakeContext(attemptCtx)
		_ = secure.Close()
		cancel()
		if err == nil {
			return time.Since(started), nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("missing_tls_targets")
	}
	return 0, fmt.Errorf("tls_transport_unavailable: %w", lastErr)
}

func tlsTarget(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
			return "", "", fmt.Errorf("invalid_tls_target")
		}
		port := parsed.Port()
		if port == "" {
			port = "443"
		}
		return parsed.Hostname(), net.JoinHostPort(parsed.Hostname(), port), nil
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" || port == "" {
		return "", "", fmt.Errorf("invalid_tls_target")
	}
	return host, value, nil
}
