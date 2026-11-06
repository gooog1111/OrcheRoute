//go:build linux

package linuxqualify

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"syscall"
	"time"
)

func temporaryGeoError(err error) bool {
	var invalid x509.CertificateInvalidError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	if errors.As(err, &invalid) || errors.As(err, &authority) || errors.As(err, &hostname) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var operation *net.OpError
	if errors.As(err, &operation) && operation.Op == "remote error" {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && (network.Timeout() || network.Temporary())
}

func temporaryCurlGeoError(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	switch exit.ExitCode() {
	case 7, 28, 35, 52, 55, 56:
		return true
	}
	return false
}

func geoCountry(ctx context.Context, client *http.Client, proxyAddress, target string) (string, string) {
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return retryCountry(probe, func() (string, string, bool) {
		country, status, retry := traceCountry(probe, client, target)
		if status != "" && retry && proxyAddress != "" && probe.Err() == nil {
			return curlCountry(probe, proxyAddress, target)
		}
		return country, status, retry
	})
}

func retryCountry(ctx context.Context, attempt func() (string, string, bool)) (string, string) {
	if ctx.Err() != nil {
		return "", "geo_failed"
	}
	country, status, retry := attempt()
	if status == "" || !retry || ctx.Err() != nil {
		return country, status
	}
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", "geo_failed"
	case <-timer.C:
	}
	country, status, _ = attempt()
	return country, status
}
