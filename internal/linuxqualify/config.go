package linuxqualify

import "strings"

type Config struct {
	MihomoBinary string
	Interface    string
	Mark         int
	DNS          DNSConfig
	URLTests     []URLTarget
	TraceURL     string
	SpeedURL     string
	TCPWorkers   int
	URLWorkers   int
	SpeedWorkers int
}

type URLTarget struct {
	URL        string
	StatusCode int
}

type DNSConfig struct {
	CacheAlgorithm string
	Bootstrap      []string
	Proxy          []string
	VPNUnderlay    []string
}

func DefaultConfig() Config {
	return Config{
		MihomoBinary: "/opt/orcheroute/bin/mihomo",
		URLTests: []URLTarget{
			{URL: "https://www.gstatic.com/generate_204", StatusCode: 204},
			{URL: "https://cp.cloudflare.com/generate_204", StatusCode: 204},
			{URL: "https://www.msftconnecttest.com/connecttest.txt", StatusCode: 200},
		},
		TraceURL: "https://www.cloudflare.com/cdn-cgi/trace",
		SpeedURL: "https://cachefly.cachefly.net/10mb.test", TCPWorkers: 128, URLWorkers: 80, SpeedWorkers: 6,
	}
}

func CoreConfig(config Config, proxy map[string]any, port int) map[string]any {
	candidate := cloneMap(proxy)
	candidate["interface-name"] = config.Interface
	candidate["routing-mark"] = config.Mark
	underlayName := "QUALIFY-UNDERLAY-DNS"
	return map[string]any{
		"mixed-port": port, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "silent", "ipv6": false,
		"dns":     map[string]any{"enable": true, "ipv6": false, "cache-algorithm": cacheAlgorithm(config.DNS.CacheAlgorithm), "default-nameserver": config.DNS.Bootstrap, "nameserver": bindResolvers(config.DNS.Proxy, candidate["name"].(string)), "proxy-server-nameserver": bindResolvers(config.DNS.VPNUnderlay, underlayName)},
		"proxies": []map[string]any{{"name": underlayName, "type": "direct", "udp": true, "ip-version": "ipv4", "interface-name": config.Interface, "routing-mark": config.Mark}, candidate},
		"rules":   []string{"MATCH," + candidate["name"].(string)},
	}
}

func bindResolvers(values []string, outbound string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value+"#"+outbound)
		}
	}
	return result
}
func cacheAlgorithm(value string) string {
	if value == "" {
		return "arc"
	}
	return value
}
func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
