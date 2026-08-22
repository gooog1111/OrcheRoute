//go:build windows

package windowsqualify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultURLConcurrency(t *testing.T) {
	if workers := DefaultConfig().URLWorkers; workers != 80 {
		t.Fatalf("URL workers = %d, want 80", workers)
	}
}

func TestURLMajorityDoesNotWaitForSlowThirdProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/slow" {
			<-request.Context().Done()
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	started := time.Now()
	latency, err := probeURLMajority(context.Background(), server.Client(), []URLTarget{
		{URL: server.URL + "/one", StatusCode: http.StatusNoContent},
		{URL: server.URL + "/two", StatusCode: http.StatusNoContent},
		{URL: server.URL + "/slow", StatusCode: http.StatusNoContent},
	})
	if err != nil || latency <= 0 {
		t.Fatalf("majority probe failed: latency=%f err=%v", latency, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("majority waited for slow third URL: %v", elapsed)
	}
}

func TestCustomURLAcceptsAnySuccessfulHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	if _, err := probeURLMajority(context.Background(), server.Client(), []URLTarget{{URL: server.URL}}); err != nil {
		t.Fatalf("custom URL-test rejected successful status: %v", err)
	}
}
