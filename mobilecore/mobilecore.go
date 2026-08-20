// Package mobilecore is the stable gomobile boundary shared by Android and
// Apple applications. It intentionally exposes JSON and primitive values only,
// which keeps generated Java/Objective-C bindings small and versionable.
package mobilecore

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/mihomo"
	mobileconstructor "github.com/gooog1111/orcheroute/internal/mobile/constructor"
	mobilemapper "github.com/gooog1111/orcheroute/internal/mobile/mapper"
	mobileparser "github.com/gooog1111/orcheroute/internal/mobile/parser"
	mobilerouting "github.com/gooog1111/orcheroute/internal/mobile/routing"
	mobilevalidator "github.com/gooog1111/orcheroute/internal/mobile/validator"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/orchestrator"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"github.com/gooog1111/orcheroute/internal/whitelist"
)

func Capabilities() string {
	return encode(map[string]any{"ok": true, "result": map[string]any{
		"runtime_engine":         "mihomo",
		"embedded_engine":        activeTransport.Available(),
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
	result, err := mobilevalidator.QualificationPolicy(policy)
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
	result, err := mobilevalidator.EffectiveQualificationPolicy(policy, pool)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

// DecodeSubscriptionBody accepts either plain share links or a base64 encoded
// subscription body. Fetching remains a platform adapter so mobile clients can
// use their native HTTP and credential storage stacks.
func DecodeSubscriptionBody(body string) string {
	return encode(map[string]any{"ok": true, "result": mobileparser.DecodeSubscriptionBody(body)})
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
	if parser == string(subscriptions.Standard) && mobileparser.IsShareLink(secret) {
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
	var routeInput mobilerouting.Input
	if json.Unmarshal([]byte(routesJSON), &routeInput) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_routes"}})
	}
	plan, err := mobilerouting.Compile(routeInput)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	var dnsInput mobileconstructor.DNSProfile
	if json.Unmarshal([]byte(dnsJSON), &dnsInput) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_dns_profile"}})
	}
	result, err := mobileconstructor.Build(mobileconstructor.Request{Proxy: proxy, Routing: plan, DNS: dnsInput})
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	payload, err := json.Marshal(result.Config)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "config_encoding_failed"}})
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"config": string(payload), "node": result.Node}})
}

func ValidateSubscription(payloadJSON string, partial bool) string {
	var payload map[string]any
	if json.Unmarshal([]byte(payloadJSON), &payload) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	result, err := mobilevalidator.Subscription(payload, partial)
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
	return encode(map[string]any{"ok": true, "result": mobilemapper.Subscriptions(sources)})
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
	preview, err := mobilevalidator.NetworkProfile(profile, topology)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": mobilevalidator.NetworkError(err)})
	}
	return encode(map[string]any{"ok": true, "result": preview})
}

func PreviewDNS(dnsJSON string) string {
	var input network.DNSInput
	if json.Unmarshal([]byte(dnsJSON), &input) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	preview, err := mobilevalidator.DNS(input)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": mobilevalidator.NetworkError(err)})
	}
	return encode(map[string]any{"ok": true, "result": preview})
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
	result, err := mobileparser.ParseLink(link, source, index)
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
	result, err := mobileparser.ParseSubscription(links, source)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

// CompileRoutes accepts direct/proxy/block arrays in the same shape used by
// the WebUI and REST API.
func CompileRoutes(listsJSON string) string {
	var lists map[string][]string
	if json.Unmarshal([]byte(listsJSON), &lists) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
	}
	result, err := mobilerouting.CompileLists(lists)
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
