package mobilecore

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLiveBlackTempleAutoDetection(t *testing.T) {
	link := os.Getenv("ORCHEROUTE_TEST_BLACKTEMPLE_URL")
	if link == "" {
		t.Skip("live BlackTemple URL is not configured")
	}
	var payload struct {
		OK     bool `json:"ok"`
		Result struct {
			Parser string   `json:"parser"`
			Links  []string `json:"links"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(FetchSubscription("standard", link, t.TempDir())), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Result.Parser != "blacktemple" || len(payload.Result.Links) < 2 {
		t.Fatalf("auto detection failed: ok=%v parser=%q links=%d", payload.OK, payload.Result.Parser, len(payload.Result.Links))
	}
}

func TestCapabilitiesAreMobileSafe(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(Capabilities()), &payload); err != nil || payload["ok"] != true {
		t.Fatalf("invalid capabilities: %s (%v)", Capabilities(), err)
	}
	encoded := Capabilities()
	for _, expected := range []string{"mihomo", "external_tunnel_fd", "embedded_engine", "vless", "vmess", "vkcall"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("missing %q in %s", expected, encoded)
		}
	}
}

func TestNativeConnectivityAdapterContract(t *testing.T) {
	targets := ConnectivityTargets("https://allowed.example/", "https://open.example/")
	if !strings.Contains(targets, `"name":"allowlist"`) || !strings.Contains(targets, `"name":"open_anchor_github"`) {
		t.Fatalf("unexpected targets: %s", targets)
	}
	classified := ClassifyConnectivity(`{"allowlist_available":true}`)
	if !strings.Contains(classified, `"state":"allowlist"`) {
		t.Fatalf("unexpected classification: %s", classified)
	}
}

func TestWhitelistTransitionBindingPreventsParallelSelection(t *testing.T) {
	state := `{"nodes":[{"id":"one","source_id":"a","alive":true,"priority":1,"proxy":{"name":"one"}},{"id":"two","source_id":"a","alive":true,"priority":2,"proxy":{"name":"two"}}]}`
	first := WhitelistTransition(state, `{"operation":"request"}`)
	if !strings.Contains(first, `"pending_node":"one"`) || !strings.Contains(first, `"candidate":{"id":"one"`) {
		t.Fatalf("unexpected first transition: %s", first)
	}
	var envelope struct {
		Result struct {
			State json.RawMessage `json:"state"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(first), &envelope); err != nil {
		t.Fatal(err)
	}
	second := WhitelistTransition(string(envelope.Result.State), `{"operation":"request"}`)
	if !strings.Contains(second, `"pending_node":"one"`) || strings.Contains(second, `"candidate":{"id":"two"`) {
		t.Fatalf("parallel transition selected another node: %s", second)
	}
}

func TestFailoverStepBindingUsesSharedFailureThreshold(t *testing.T) {
	observation := `{"now":100,"wan_available":true,"active_ok":false,"active":"old","active_pool":"primary","nodes":[{"name":"old","pool":"primary","alive":true},{"name":"next","pool":"primary","alive":true}],"control":{"enabled":true,"mode":"auto"}}`
	var first map[string]any
	if err := json.Unmarshal([]byte(FailoverStep(`{}`, observation)), &first); err != nil || first["ok"] != true {
		t.Fatalf("first step failed: %v %#v", err, first)
	}
	result := first["result"].(map[string]any)
	decision := result["decision"].(map[string]any)
	if decision["action"] != "keep" {
		t.Fatalf("first failure switched node: %#v", decision)
	}
	state, _ := json.Marshal(result["state"])
	var second map[string]any
	if err := json.Unmarshal([]byte(FailoverStep(string(state), observation)), &second); err != nil || second["ok"] != true {
		t.Fatalf("second step failed: %v %#v", err, second)
	}
	decision = second["result"].(map[string]any)["decision"].(map[string]any)
	if decision["action"] != "select" || decision["target"] != "next" {
		t.Fatalf("unexpected second decision: %#v", decision)
	}
}

func TestParseLinkBinding(t *testing.T) {
	result := ParseLink(
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@example.com:443?security=tls&type=tcp&sni=example.com#Mobile",
		"mobile", 1,
	)
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, `"type":"vless"`) {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestGenerateMihomoConfigRejectsIncompleteInput(t *testing.T) {
	result := GenerateMihomoConfig(`{"route_default":"proxy"}`)
	if !strings.Contains(result, `"ok":false`) || !strings.Contains(result, `missing_runtime_settings`) {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestSubscriptionBindings(t *testing.T) {
	decoded := DecodeSubscriptionBody("vless://id@example.test:443")
	if !strings.Contains(decoded, `"ok":true`) || !strings.Contains(decoded, `vless://id@example.test:443`) {
		t.Fatalf("unexpected decoded subscription: %s", decoded)
	}
	validated := ValidateSubscription(`{"name":"Main","group":"primary","parser":"standard","secret":"https://example.test/sub"}`, false)
	if !strings.Contains(validated, `"ok":true`) || !strings.Contains(validated, `"group_name":"primary"`) {
		t.Fatalf("unexpected validation: %s", validated)
	}
}

func TestFetchSubscriptionAcceptsRawShareLink(t *testing.T) {
	link := "trojan://secret@example.test:443#Phone"
	result := FetchSubscription("standard", link, t.TempDir())
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, link) {
		t.Fatalf("unexpected fetch result: %s", result)
	}
}

func TestBuildMobileProxyConfig(t *testing.T) {
	result := BuildMobileProxyConfig(`{"name":"PHONE-NODE","type":"trojan","server":"example.test","port":443,"password":"secret"}`)
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, `MATCH,ACTIVE`) || !strings.Contains(result, `PHONE-NODE`) {
		t.Fatalf("unexpected mobile config: %s", result)
	}
}

func TestBuildMobileProxyConfigWithRoutes(t *testing.T) {
	result := BuildMobileProxyConfigWithRoutes(
		`{"name":"PHONE-NODE","type":"trojan","server":"example.test","port":443,"password":"secret"}`,
		`{"default":"proxy","lists":{"direct":["*.ru",":53"],"proxy":[],"block":["ads.example"]}}`,
	)
	for _, expected := range []string{`DOMAIN-SUFFIX,ads.example,REJECT`, `DOMAIN-SUFFIX,ru,DIRECT`, `DST-PORT,53,DIRECT`, `MATCH,ACTIVE`, `proxy-server-nameserver`} {
		if !strings.Contains(result, expected) {
			t.Fatalf("missing %q in %s", expected, result)
		}
	}
}

func TestEmbeddedComponentStatus(t *testing.T) {
	if strings.TrimSpace(EmbeddedMihomoVersion()) == "" {
		t.Fatal("embedded Mihomo version is empty")
	}
	status := GeoStatus(t.TempDir())
	for _, expected := range []string{`"ok":true`, `"mihomo_version"`, `"geoip":{"installed":false`, `"geosite":{"installed":false`} {
		if !strings.Contains(status, expected) {
			t.Fatalf("missing %q in %s", expected, status)
		}
	}
}

func TestEmbeddedGeoSourcesAndValidation(t *testing.T) {
	for _, expected := range []string{`"id":"metacubex"`, `"id":"runetfreedom"`, `"id":"loyalsoldier"`, `"id":"custom"`} {
		if sources := GeoSources(); !strings.Contains(sources, expected) {
			t.Fatalf("missing %q in %s", expected, sources)
		}
	}
	valid := ResolveGeoSource("custom", "https://example.test/geoip.dat", "https://example.test/geosite.dat")
	if !strings.Contains(valid, `"ok":true`) {
		t.Fatalf("custom source rejected: %s", valid)
	}
	invalid := ResolveGeoSource("custom", "http://example.test/geoip.dat", "https://example.test/geosite.dat")
	if !strings.Contains(invalid, `invalid_geoip_url`) {
		t.Fatalf("insecure source accepted: %s", invalid)
	}
	catalog := GeoCatalog(t.TempDir())
	if !strings.Contains(catalog, `"geoip":[]`) || !strings.Contains(catalog, `"geosite":[]`) {
		t.Fatalf("unexpected empty catalog: %s", catalog)
	}
}

func TestBuildMobileProxyConfigWithNetworkDNS(t *testing.T) {
	result := BuildMobileProxyConfigWithNetwork(
		`{"name":"PHONE-NODE","type":"trojan","server":"example.test","port":443,"password":"secret"}`,
		`{"default":"proxy","lists":{"direct":[],"proxy":[],"block":[]}}`,
		`{"direct":["9.9.9.9"],"proxy":["https://dns.quad9.net/dns-query"],"vpn_underlay":["1.0.0.1"],"bootstrap":["8.8.4.4"],"cache_algorithm":"lru","prefer_h3":true,"use_hosts":false,"ipv6":true}`,
	)
	for _, expected := range []string{"9.9.9.9", "dns.quad9.net", "1.0.0.1", "8.8.4.4", "cache-algorithm", "lru", "ipv6"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("missing %q in %s", expected, result)
		}
	}
}
