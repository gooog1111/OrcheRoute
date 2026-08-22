package mihomo

import (
	"fmt"
	"path"
)

type Role struct {
	Interface string `json:"interface"`
	Mark      int    `json:"mark"`
}

type Capture struct {
	Mode        string   `json:"mode"`
	Interfaces  []string `json:"interfaces"`
	DNSHijack   bool     `json:"dns_hijack"`
	StrictRoute bool     `json:"strict_route"`
}

type Profile struct {
	Capture Capture `json:"capture"`
}

type DNSConfig struct {
	IPv6           bool   `json:"ipv6"`
	CacheAlgorithm string `json:"cache_algorithm"`
	PreferH3       bool   `json:"prefer_h3"`
	UseHosts       bool   `json:"use_hosts"`
}

type DNSEffective struct {
	Bootstrap   []string `json:"bootstrap"`
	Proxy       []string `json:"proxy"`
	Direct      []string `json:"direct"`
	VPNUnderlay []string `json:"vpn_underlay"`
}

type DNS struct {
	Config    DNSConfig    `json:"config"`
	Effective DNSEffective `json:"effective"`
}

type Network struct {
	Profile              Profile         `json:"profile"`
	ResolvedRoles        map[string]Role `json:"resolved_roles"`
	EffectiveBypassCIDRs []string        `json:"effective_bypass_cidrs"`
	DNS                  DNS             `json:"dns"`
}

type Input struct {
	StateDir     string  `json:"state_dir"`
	Platform     string  `json:"platform,omitempty"`
	TestURL      string  `json:"test_url"`
	Secret       string  `json:"secret"`
	RouteDefault string  `json:"route_default"`
	Network      Network `json:"network"`
}

func Build(input Input) (map[string]any, error) {
	if input.StateDir == "" || input.Secret == "" {
		return nil, fmt.Errorf("missing_runtime_settings")
	}
	if input.TestURL == "" {
		input.TestURL = "https://www.gstatic.com/generate_204"
	}
	directRole, ok := input.Network.ResolvedRoles["direct"]
	if !ok || directRole.Interface == "" {
		return nil, fmt.Errorf("missing_direct_role")
	}
	vpnRole, ok := input.Network.ResolvedRoles["vpn_underlay"]
	if !ok || vpnRole.Interface == "" {
		return nil, fmt.Errorf("missing_vpn_underlay_role")
	}
	defaultProxy := map[string]string{"proxy": "ACTIVE", "direct": "DIRECT-EGRESS", "block": "REJECT"}[input.RouteDefault]
	if defaultProxy == "" {
		return nil, fmt.Errorf("invalid_default_action")
	}

	provider := func(name string, interval int) map[string]any {
		override := map[string]any{"interface-name": vpnRole.Interface}
		if input.Platform != "windows" {
			override["routing-mark"] = vpnRole.Mark
		}
		return map[string]any{
			"type":     "file",
			"path":     configPath(input.StateDir, "providers", name+".json"),
			"interval": 60,
			"override": override,
			"health-check": map[string]any{
				"enable": true, "url": input.TestURL, "interval": interval, "timeout": 5000,
				"lazy": false, "expected-status": 204,
			},
		}
	}

	defaultOrder := []any{defaultProxy}
	for _, value := range []string{"ACTIVE", "DIRECT-EGRESS", "REJECT"} {
		if value != defaultProxy {
			defaultOrder = append(defaultOrder, value)
		}
	}

	directEgress := map[string]any{
		"name": "DIRECT-EGRESS", "type": "direct", "udp": true, "ip-version": "ipv4",
		"interface-name": directRole.Interface,
	}
	underlayDNS := map[string]any{
		"name": "VPN-UNDERLAY-DNS", "type": "direct", "udp": true, "ip-version": "ipv4",
		"interface-name": vpnRole.Interface,
	}
	if input.Platform != "windows" {
		directEgress["routing-mark"] = directRole.Mark
		underlayDNS["routing-mark"] = vpnRole.Mark
	}
	config := map[string]any{
		"mixed-port": 21080, "bind-address": "127.0.0.1", "allow-lan": false,
		"mode": "rule", "log-level": "info", "ipv6": input.Network.DNS.Config.IPv6,
		"unified-delay": true, "tcp-concurrent": true, "geodata-mode": true,
		"geodata-loader": "memconservative", "geo-auto-update": true, "geo-update-interval": 24,
		"geox-url": map[string]any{
			"geoip":   "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat",
			"geosite": "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat",
			"mmdb":    "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country.mmdb",
		},
		"etag-support": true, "external-controller": "127.0.0.1:19090", "secret": input.Secret,
		"profile": map[string]any{"store-selected": true, "store-fake-ip": false},
		"sniffer": map[string]any{
			"enable": true, "force-dns-mapping": true, "parse-pure-ip": true, "override-destination": true,
			"sniff": map[string]any{
				"HTTP": map[string]any{"ports": []any{80, "8080-8880"}, "override-destination": true},
				"TLS":  map[string]any{"ports": []any{443, 8443}},
				"QUIC": map[string]any{"ports": []any{443, 8443}},
			},
			"skip-dst-address": []string{
				"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
				"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4",
			},
		},
		"dns": map[string]any{
			"enable": true, "listen": "127.0.0.1:21053", "ipv6": input.Network.DNS.Config.IPv6,
			"enhanced-mode": "normal", "cache-algorithm": input.Network.DNS.Config.CacheAlgorithm,
			"prefer-h3": input.Network.DNS.Config.PreferH3, "use-hosts": input.Network.DNS.Config.UseHosts,
			"respect-rules": true, "default-nameserver": input.Network.DNS.Effective.Bootstrap,
			"nameserver": input.Network.DNS.Effective.Proxy, "direct-nameserver": input.Network.DNS.Effective.Direct,
			"direct-nameserver-follow-policy": false,
			"proxy-server-nameserver":         input.Network.DNS.Effective.VPNUnderlay,
			"nameserver-policy": map[string]any{
				"rule-set:routes-direct": input.Network.DNS.Effective.Direct,
				"rule-set:routes-proxy":  input.Network.DNS.Effective.Proxy,
			},
		},
		"proxies": []any{directEgress, underlayDNS},
		"proxy-providers": map[string]any{
			"primary": provider("primary", 300), "emergency": provider("emergency", 60),
			"whitelist": provider("whitelist", 60),
		},
		"rule-providers": map[string]any{
			"routes-block":  ruleProvider(input.StateDir, "block"),
			"routes-direct": ruleProvider(input.StateDir, "direct"),
			"routes-proxy":  ruleProvider(input.StateDir, "proxy"),
		},
		"proxy-groups": []any{
			map[string]any{
				"name": "ACTIVE", "type": "select", "proxies": []string{"DIRECT-EGRESS"},
				"use": []string{"primary", "emergency", "whitelist"},
			},
			map[string]any{"name": "DEFAULT", "type": "select", "proxies": defaultOrder, "hidden": true},
			map[string]any{
				"name": "PROBE-PRIMARY", "type": "url-test", "use": []string{"primary"},
				"url": input.TestURL, "interval": 300, "timeout": 5000, "tolerance": 50,
				"lazy": false, "expected-status": 204,
			},
			map[string]any{
				"name": "PROBE-EMERGENCY", "type": "url-test", "use": []string{"emergency"},
				"url": input.TestURL, "interval": 60, "timeout": 5000, "tolerance": 100,
				"lazy": false, "expected-status": 204,
			},
			map[string]any{
				"name": "PROBE-WHITELIST", "type": "url-test", "use": []string{"whitelist"},
				"url": input.TestURL, "interval": 60, "timeout": 5000, "tolerance": 100,
				"lazy": false, "expected-status": 204,
			},
		},
		"rules": []string{
			"RULE-SET,routes-block,REJECT", "RULE-SET,routes-direct,DIRECT-EGRESS",
			"RULE-SET,routes-proxy,ACTIVE", "MATCH,DEFAULT",
		},
	}

	capture := input.Network.Profile.Capture
	tun := map[string]any{
		"enable": true, "device": "orcheroute0", "stack": "mixed", "auto-route": true,
		"auto-detect-interface": false, "strict-route": capture.StrictRoute,
		"route-exclude-address": filterByIPv6(input.Network.EffectiveBypassCIDRs, input.Network.DNS.Config.IPv6),
	}
	if input.Platform != "windows" {
		tun["auto-redirect"] = true
		tun["iproute2-table-index"] = 5353
		tun["iproute2-rule-index"] = 9200
	}
	if capture.DNSHijack {
		tun["dns-hijack"] = []string{"any:53", "tcp://any:53"}
	}
	if capture.Mode == "interfaces" {
		tun["include-interface"] = capture.Interfaces
	}
	config["tun"] = tun
	return config, nil
}

func ruleProvider(stateDir, name string) map[string]any {
	return map[string]any{
		"type": "file", "behavior": "classical", "format": "text",
		"path": configPath(stateDir, "rules", name+".txt"),
	}
}

func configPath(base string, parts ...string) string {
	all := append([]string{base}, parts...)
	return path.Join(all...)
}

func filterByIPv6(values []string, enabled bool) []string {
	result := []string{}
	for _, value := range values {
		if enabled || !containsColon(value) {
			result = append(result, value)
		}
	}
	return result
}

func containsColon(value string) bool {
	for _, character := range value {
		if character == ':' {
			return true
		}
	}
	return false
}
