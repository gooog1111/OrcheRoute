//go:build linux

package linuxqualify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCoreConfigBindsCandidateAndDNS(t *testing.T) {
	config := DefaultConfig()
	config.Interface = "ppp0"
	config.Mark = 0x5352
	config.DNS = DNSConfig{Bootstrap: []string{"1.1.1.1"}, Proxy: []string{"https://1.1.1.1/dns-query"}, VPNUnderlay: []string{"8.8.8.8"}}
	result := CoreConfig(config, map[string]any{"name": "NODE", "type": "vless"}, 22000)
	proxies := result["proxies"].([]map[string]any)
	candidate := proxies[1]
	if candidate["interface-name"] != "ppp0" || candidate["routing-mark"] != 0x5352 {
		t.Fatalf("candidate is not bound: %#v", candidate)
	}
	dns := result["dns"].(map[string]any)
	if dns["nameserver"].([]string)[0] != "https://1.1.1.1/dns-query#NODE" || dns["proxy-server-nameserver"].([]string)[0] != "8.8.8.8#QUALIFY-UNDERLAY-DNS" {
		t.Fatalf("unexpected DNS: %#v", dns)
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
	latency, err := probeURLMajority(context.Background(), server.Client(), "", []URLTarget{
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

func TestDefaultQualificationConcurrencyAndURLs(t *testing.T) {
	config := DefaultConfig()
	if config.SpeedWorkers != 6 {
		t.Fatalf("speed workers = %d, want 6", config.SpeedWorkers)
	}
	if config.URLWorkers != 80 {
		t.Fatalf("URL workers = %d, want 80", config.URLWorkers)
	}
	if len(config.URLTests) != 3 {
		t.Fatalf("URL targets = %d, want 3", len(config.URLTests))
	}
	if len(speedTargets(config)) < 3 {
		t.Fatal("independent speed targets missing")
	}
}

func TestSpeedTargetFallbackAndBoundedSamples(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		switch request.URL.Path {
		case "/blocked":
			writer.WriteHeader(403)
		case "/html":
			writer.Header().Set("Content-Type", "text/html")
			writer.Write([]byte("not a speed sample"))
		case "/short":
			writer.Header().Set("Content-Type", "application/octet-stream")
			writer.Write([]byte("x"))
		default:
			if request.Header.Get("Range") != "bytes=0-63" || request.URL.Query().Get("bytes") != "64" {
				t.Error("sample request is not bounded")
			}
			writer.Header().Set("Content-Type", "application/octet-stream")
			writer.Write(make([]byte, 128))
		}
	}))
	defer server.Close()
	targets := []string{server.URL + "/blocked", server.URL + "/html", server.URL + "/short", server.URL + "/ok?bytes={bytes}"}
	sample := downloadSpeedTargets(context.Background(), server.Client(), "", targets, 64)
	if !usableSpeed(sample, 64) || sample.Bytes != 64 || requests != 4 {
		t.Fatalf("fallback failed: %+v requests=%d", sample, requests)
	}
	failed := downloadSpeedTargets(context.Background(), server.Client(), "", targets[:3], 64)
	if failed.OK {
		t.Fatal("unavailable providers were accepted as speed samples")
	}
}

func TestSpeedTargetCancellationAndMalformedURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { <-request.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if downloadSpeedTargets(ctx, server.Client(), "", []string{"%", server.URL}, 64).OK {
		t.Fatal("cancelled request succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("speed test ignored cancellation")
	}
}

func TestCustomURLAcceptsAnySuccessfulHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	if _, err := probeURLMajority(context.Background(), server.Client(), "", []URLTarget{{URL: server.URL}}); err != nil {
		t.Fatalf("custom URL-test rejected successful status: %v", err)
	}
}
