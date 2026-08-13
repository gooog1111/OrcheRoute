// Package mobilecore is the stable gomobile boundary shared by Android and
// Apple applications. It intentionally exposes JSON and primitive values only,
// which keeps generated Java/Objective-C bindings small and versionable.
package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/mihomo"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/nodes"
	"github.com/gooog1111/orcheroute/internal/orchestrator"
	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/routes"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"github.com/gooog1111/orcheroute/internal/whitelist"
)

func Capabilities() string {
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"runtime_engine":         "mihomo",
		"embedded_engine":        embeddedEngineAvailable(),
		"external_tunnel_fd":     true,
		"route_rules":            true,
		"mihomo_config":          true,
		"network_profile":        true,
		"subscription_registry":  true,
		"qualification_policy":   true,
		"whitelist_pool":         true,
		"connectivity_automaton": true,
		"share_protocols":        []string{"vless", "vmess", "trojan", "ss"},
	}})
}

func ValidateQualificationPolicy(policyJSON string) string {
	var policy map[string]any
	if json.Unmarshal([]byte(policyJSON), &policy) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	result, err := qualification.Validate(policy)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

func EffectiveQualificationPolicy(policyJSON, pool string) string {
	var policy map[string]any
	if json.Unmarshal([]byte(policyJSON), &policy) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	result, err := qualification.Effective(policy, pool)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

// DecodeSubscriptionBody accepts either plain share links or a base64 encoded
// subscription body. Fetching remains a platform adapter so mobile clients can
// use their native HTTP and credential storage stacks.
func DecodeSubscriptionBody(body string) string {
	return encode(map[string]any{"ok": true, "result": subscriptions.Decode([]byte(body))})
}

// FetchSubscription is the portable network adapter used by mobile and
// desktop clients. It supports ordinary HTTP/base64 subscriptions, raw share
// links and the authenticated BlackTemple protocol used by OrcheRoute.
func FetchSubscription(parser, secret, stateDir string) string {
	parser = strings.ToLower(strings.TrimSpace(parser))
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_secret"}})
	}
	if parser == string(subscriptions.Standard) && isShareLink(secret) {
		return encode(map[string]any{"ok": true, "result": map[string]any{"links": []string{secret}}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	subscription := subscriptions.Subscription{Parser: subscriptions.Parser(parser), Secret: secret}
	var fetcher subscriptions.Fetcher
	switch subscription.Parser {
	case subscriptions.Standard:
		if parsed, err := url.ParseRequestURI(secret); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_subscription_url"}})
		}
		fetcher = subscriptions.HTTPFetcher{UserAgent: "OrcheRoute Mobile/0.2"}
	case subscriptions.BlackTemple:
		fetcher = subscriptions.BlackTempleFetcher{CredentialsPath: filepath.Join(stateDir, "blacktemple_credentials.json")}
	case subscriptions.Inline:
		fetcher = subscriptions.InlineFetcher{}
	case subscriptions.WireGuard:
		fetcher = subscriptions.WireGuardFetcher{}
	default:
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "unsupported_parser"}})
	}
	links, err := fetcher.Fetch(ctx, subscription)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"links": links}})
}

func isShareLink(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"vless://", "vmess://", "trojan://", "ss://", "hysteria2://", "hy2://"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// BuildMobileProxyConfig creates a self-contained Mihomo configuration for a
// selected parsed node. The result can be loaded directly by EngineLoadConfig.
func BuildMobileProxyConfig(proxyJSON string) string {
	return BuildMobileProxyConfigWithRoutes(proxyJSON, `{"default":"proxy","lists":{"direct":[],"proxy":[],"block":[]}}`)
}

// BuildMobileProxyConfigWithRoutes applies the shared route compiler and
// embeds Block -> Direct -> Proxy -> default rules in a mobile configuration.
func BuildMobileProxyConfigWithRoutes(proxyJSON, routesJSON string) string {
	return buildMobileProxyConfig(proxyJSON, routesJSON, `{"direct":["1.1.1.1","8.8.8.8"],"proxy":["https://1.1.1.1/dns-query","https://dns.google/dns-query"],"vpn_underlay":["1.1.1.1","8.8.8.8"],"bootstrap":["1.1.1.1","8.8.8.8"],"cache_algorithm":"arc","prefer_h3":false,"use_hosts":true,"ipv6":false}`)
}

// BuildMobileProxyConfigWithNetwork adds the platform-owned DNS profile to
// the portable proxy and route configuration.
func BuildMobileProxyConfigWithNetwork(proxyJSON, routesJSON, dnsJSON string) string {
	return buildMobileProxyConfig(proxyJSON, routesJSON, dnsJSON)
}

func buildMobileProxyConfig(proxyJSON, routesJSON, dnsJSON string) string {
	var proxy map[string]any
	if json.Unmarshal([]byte(proxyJSON), &proxy) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_proxy"}})
	}
	name, _ := proxy["name"].(string)
	if strings.TrimSpace(name) == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "proxy_name_required"}})
	}
	var routeInput struct {
		Default string              `json:"default"`
		Lists   map[string][]string `json:"lists"`
	}
	if json.Unmarshal([]byte(routesJSON), &routeInput) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_routes"}})
	}
	if routeInput.Default != "proxy" && routeInput.Default != "direct" && routeInput.Default != "block" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_route_default"}})
	}
	var dnsInput struct {
		Direct      []string `json:"direct"`
		Proxy       []string `json:"proxy"`
		VPNUnderlay []string `json:"vpn_underlay"`
		Bootstrap   []string `json:"bootstrap"`
		Cache       string   `json:"cache_algorithm"`
		PreferH3    bool     `json:"prefer_h3"`
		UseHosts    bool     `json:"use_hosts"`
		IPv6        bool     `json:"ipv6"`
	}
	if json.Unmarshal([]byte(dnsJSON), &dnsInput) != nil || len(dnsInput.Direct) == 0 || len(dnsInput.Proxy) == 0 || len(dnsInput.VPNUnderlay) == 0 || len(dnsInput.Bootstrap) == 0 {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_dns_profile"}})
	}
	if dnsInput.Cache != "arc" && dnsInput.Cache != "lru" {
		dnsInput.Cache = "arc"
	}
	compiled, err := routes.CompileLists(routeInput.Lists)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	actions := map[string]string{"proxy": "ACTIVE", "direct": "DIRECT", "block": "REJECT"}
	ruleList := []string{}
	for _, listName := range []string{"block", "direct", "proxy"} {
		for _, rule := range compiled.Compiled[listName] {
			ruleList = append(ruleList, rule+","+actions[listName])
		}
	}
	ruleList = append(ruleList, "MATCH,"+actions[routeInput.Default])
	config := map[string]any{
		"mode": "rule", "log-level": "info", "ipv6": dnsInput.IPv6, "find-process-mode": "off", "unified-delay": true, "tcp-concurrent": true,
		"geodata-mode": true, "geodata-loader": "standard", "geo-auto-update": false,
		"geox-url": map[string]any{"geoip": geoIPURL, "geosite": geoSiteURL},
		"sniffer": map[string]any{
			"enable": true, "force-dns-mapping": true, "parse-pure-ip": true, "override-destination": true,
			"sniff": map[string]any{
				"HTTP": map[string]any{"ports": []any{80, "8080-8880"}, "override-destination": true},
				"TLS":  map[string]any{"ports": []any{443, 8443}},
				"QUIC": map[string]any{"ports": []any{443, 8443}},
			},
		},
		"dns": map[string]any{
			"enable": true, "ipv6": dnsInput.IPv6, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16",
			"respect-rules":           true,
			"default-nameserver":      dnsInput.Bootstrap,
			"proxy-server-nameserver": dnsInput.VPNUnderlay,
			"nameserver":              dnsInput.Proxy,
			"direct-nameserver":       dnsInput.Direct,
			"cache-algorithm":         dnsInput.Cache,
			"prefer-h3":               dnsInput.PreferH3,
			"use-hosts":               dnsInput.UseHosts,
		},
		"proxies":      []any{proxy},
		"proxy-groups": []any{map[string]any{"name": "ACTIVE", "type": "select", "proxies": []string{name}}},
		"rules":        ruleList,
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "config_encoding_failed"}})
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"config": string(payload), "node": name}})
}

func ValidateSubscription(payloadJSON string, partial bool) string {
	var payload map[string]any
	if json.Unmarshal([]byte(payloadJSON), &payload) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	result, err := subscriptions.ValidateFields(payload, partial)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

func AggregateSubscriptions(sourcesJSON string) string {
	var sources []subscriptions.SourceLinks
	if json.Unmarshal([]byte(sourcesJSON), &sources) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	return encode(map[string]any{"ok": true, "result": subscriptions.Aggregate(sources)})
}

// WhitelistTransition applies the shared derived-pool state machine. Native
// adapters only test nodes and execute the returned candidate.
func WhitelistTransition(stateJSON, commandJSON string) string {
	var state whitelist.State
	var command whitelist.Command
	if json.Unmarshal([]byte(stateJSON), &state) != nil || json.Unmarshal([]byte(commandJSON), &command) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_whitelist_transition"}})
	}
	result, err := whitelist.Transition(state, command)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

// OrchestratorTransition owns normal/allowlist/offline transitions and emits
// a single platform action. Android, Apple and desktop adapters execute that
// action without duplicating failover policy.
func OrchestratorTransition(stateJSON, eventJSON string) string {
	var state orchestrator.State
	var event orchestrator.Event
	if json.Unmarshal([]byte(stateJSON), &state) != nil || json.Unmarshal([]byte(eventJSON), &event) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_orchestrator_transition"}})
	}
	result, err := orchestrator.Transition(state, event)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

func PreviewNetworkProfile(profileJSON, topologyJSON string) string {
	var profile network.ProfileInput
	var topology network.Topology
	if json.Unmarshal([]byte(profileJSON), &profile) != nil || json.Unmarshal([]byte(topologyJSON), &topology) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	preview, err := network.PreviewProfile(profile, topology)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": networkError(err)})
	}
	return encode(map[string]any{"ok": true, "result": preview})
}

func PreviewDNS(dnsJSON string) string {
	var input network.DNSInput
	if json.Unmarshal([]byte(dnsJSON), &input) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	config, err := network.ValidateDNS(&input)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": networkError(err)})
	}
	return encode(map[string]any{"ok": true, "result": network.PreviewDNS(config)})
}

func networkError(err error) any {
	var validation *network.ValidationError
	if errors.As(err, &validation) {
		return validation
	}
	return map[string]string{"error": err.Error()}
}

// GenerateMihomoConfig accepts a resolved, platform-neutral network profile.
// The same function is used by server, desktop and mobile frontends.
func GenerateMihomoConfig(inputJSON string) string {
	var input mihomo.Input
	if json.Unmarshal([]byte(inputJSON), &input) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	config, err := mihomo.Build(input)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": config})
}

func ParseLink(link, source string, index int) string {
	result, err := nodes.ParseLink(link, source, index)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

// ParseSubscription accepts a JSON array of share links and returns normalized
// nodes compatible with the existing OrcheRoute/Mihomo state format.
func ParseSubscription(linksJSON, source string) string {
	var links []string
	if json.Unmarshal([]byte(linksJSON), &links) != nil || source == "" {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	return encode(map[string]any{"ok": true, "result": nodes.ConvertLinks(links, source)})
}

// CompileRoutes accepts direct/proxy/block arrays in the same shape used by
// the WebUI and REST API.
func CompileRoutes(listsJSON string) string {
	var lists map[string][]string
	if json.Unmarshal([]byte(listsJSON), &lists) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	result, err := routes.CompileLists(lists)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

func encode(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return `{"ok":false,"error":{"error":"encoding_failed"}}`
	}
	return string(payload)
}
