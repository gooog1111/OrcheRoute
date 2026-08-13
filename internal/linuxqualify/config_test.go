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
}
