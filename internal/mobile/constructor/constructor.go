// Package constructor builds a complete Mihomo configuration from validated
// nodes, DNS and an already compiled routing plan. It performs no selection,
// qualification, persistence, or transport lifecycle operations.
package constructor

import (
	"fmt"
	"strings"

	"github.com/gooog1111/orcheroute/internal/mobile/routing"
)

const (
	GeoIPURL   = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat"
	GeoSiteURL = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
)

type DNSProfile struct {
	Direct      []string `json:"direct"`
	Proxy       []string `json:"proxy"`
	VPNUnderlay []string `json:"vpn_underlay"`
	Bootstrap   []string `json:"bootstrap"`
	Cache       string   `json:"cache_algorithm"`
	PreferH3    bool     `json:"prefer_h3"`
	UseHosts    bool     `json:"use_hosts"`
	IPv6        bool     `json:"ipv6"`
}

type Request struct {
	Proxy   map[string]any
	Routing routing.Plan
	DNS     DNSProfile
}

type Result struct {
	Config map[string]any `json:"config"`
	Node   string         `json:"node"`
}

func DefaultDNS() DNSProfile {
	return DNSProfile{
		Direct: []string{"1.1.1.1", "8.8.8.8"}, Proxy: []string{"https://1.1.1.1/dns-query", "https://dns.google/dns-query"},
		VPNUnderlay: []string{"1.1.1.1", "8.8.8.8"}, Bootstrap: []string{"1.1.1.1", "8.8.8.8"},
		Cache: "arc", UseHosts: true,
	}
}

func Build(request Request) (Result, error) {
	name, _ := request.Proxy["name"].(string)
	if strings.TrimSpace(name) == "" {
		return Result{}, fmt.Errorf("proxy_name_required")
	}
	if request.Routing.Default == "" || len(request.Routing.Rules) == 0 {
		return Result{}, fmt.Errorf("invalid_routes")
	}
	dns := request.DNS
	if len(dns.Direct) == 0 || len(dns.Proxy) == 0 || len(dns.VPNUnderlay) == 0 || len(dns.Bootstrap) == 0 {
		return Result{}, fmt.Errorf("invalid_dns_profile")
	}
	if dns.Cache != "arc" && dns.Cache != "lru" {
		dns.Cache = "arc"
	}
	config := map[string]any{
		"mode": "rule", "log-level": "info", "ipv6": dns.IPv6, "find-process-mode": "off", "unified-delay": true, "tcp-concurrent": true,
		"geodata-mode": true, "geodata-loader": "standard", "geo-auto-update": false,
		"geox-url": map[string]any{"geoip": GeoIPURL, "geosite": GeoSiteURL},
		"sniffer": map[string]any{
			"enable": true, "force-dns-mapping": true, "parse-pure-ip": true, "override-destination": true,
			"sniff": map[string]any{
				"HTTP": map[string]any{"ports": []any{80, "8080-8880"}, "override-destination": true},
				"TLS":  map[string]any{"ports": []any{443, 8443}}, "QUIC": map[string]any{"ports": []any{443, 8443}},
			},
		},
		"dns": map[string]any{
			"enable": true, "ipv6": dns.IPv6, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16", "respect-rules": true,
			"default-nameserver": dns.Bootstrap, "proxy-server-nameserver": dns.VPNUnderlay, "nameserver": dns.Proxy, "direct-nameserver": dns.Direct,
			"cache-algorithm": dns.Cache, "prefer-h3": dns.PreferH3, "use-hosts": dns.UseHosts,
		},
		"proxies": []any{request.Proxy}, "proxy-groups": []any{map[string]any{"name": "ACTIVE", "type": "select", "proxies": []string{name}}},
		"rules": request.Routing.Rules,
	}
	return Result{Config: config, Node: name}, nil
}
