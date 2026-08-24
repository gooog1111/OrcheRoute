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
	mobileconnectivity "github.com/gooog1111/orcheroute/internal/mobile/connectivity"
	mobileconstructor "github.com/gooog1111/orcheroute/internal/mobile/constructor"
	mobilemapper "github.com/gooog1111/orcheroute/internal/mobile/mapper"
	mobileparser "github.com/gooog1111/orcheroute/internal/mobile/parser"
	mobilerouting "github.com/gooog1111/orcheroute/internal/mobile/routing"
	mobilevalidator "github.com/gooog1111/orcheroute/internal/mobile/validator"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/noderank"
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

// ConnectivityTargets exposes the portable probe policy to native adapters.
// Android performs DNS and HTTP through Network.openConnection so neither
// operation can re-enter the app-owned VPN tunnel.
func ConnectivityTargets(allowlistURL, openInternetURL string) string {
	targets, err := mobileconnectivity.Targets(mobileconnectivity.Config{
		AllowlistURL: allowlistURL, OpenInternetURL: openInternetURL,
	})
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	result := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		result = append(result, map[string]any{
			"name": target.Name, "url": target.URL, "open_internet": target.OpenInternet,
			"expect_no_content": target.ExpectNoContent,
		})
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"targets": result}})
}

// ClassifyConnectivity keeps policy in Go while the native platform owns
// physical-network I/O and returns only observations.
func ClassifyConnectivity(observationJSON string) string {
	var observation mobileconnectivity.Observation
	if json.Unmarshal([]byte(observationJSON), &observation) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_connectivity_observation"}})
	}
	return encode(map[string]any{"ok": true, "result": mobileconnectivity.Classify(observation)})
}

// ConfirmConnectivity stabilizes raw physical-network observations before a
// platform controller is allowed to pause or restart the VPN.
func ConfirmConnectivity(inputJSON string) string {
	var input mobileconnectivity.ConfirmationInput
	if json.Unmarshal([]byte(inputJSON), &input) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_connectivity_confirmation"}})
	}
	result, err := mobileconnectivity.Confirm(input)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

func ParseConnectionIdentity(traceBody string) string {
	result, err := mobileconnectivity.ParseTraceIdentity(traceBody)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
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
		standard := subscriptions.HTTPFetcher{UserAgent: "OrcheRoute Mobile/0.2"}
		blackTemple := subscriptions.BlackTempleFetcher{CredentialsPath: filepath.Join(stateDir, "blacktemple_credentials.json")}
		result, err := subscriptions.DetectAndFetch(ctx, subscription, standard, blackTemple)
		if err != nil {
			return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
		}
		return encode(map[string]any{"ok": true, "result": map[string]any{"links": result.Links, "parser": result.Parser}})
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
	return encode(map[string]any{"ok": true, "result": map[string]any{"links": links, "parser": subscription.Parser}})
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
// a single platform action. Android executes that action without duplicating
// failover policy.
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

// NetworkDecision maps an independently confirmed physical-network state to
// one platform action.
func NetworkDecision(inputJSON string) string {
	var input orchestrator.DecisionInput
	if json.Unmarshal([]byte(inputJSON), &input) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_network_decision"}})
	}
	result, err := orchestrator.DecideNetwork(input)
	if err != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
	}
	return encode(map[string]any{"ok": true, "result": result})
}

// RankNodes orders available nodes by shared latency, throughput and
// historical health evidence.
func RankNodes(nodesJSON string) string {
	var nodes []noderank.Node
	if json.Unmarshal([]byte(nodesJSON), &nodes) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_nodes"}})
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"nodes": noderank.Rank(nodes)}})
}

// SelectNode applies the shared strict pool policy to ranked nodes.
func SelectNode(nodesJSON, mode string) string {
	var nodes []noderank.Node
	if json.Unmarshal([]byte(nodesJSON), &nodes) != nil {
		return encode(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_nodes"}})
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"node": noderank.Select(nodes, mode)}})
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
// The same function is available to Linux Server and Android adapters.
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
