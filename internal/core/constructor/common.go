package constructor

// CommonConfig contains only Mihomo options whose behavior is identical on
// Android VpnService and Linux Server. Platform constructors extend this map
// with their own TUN, controller, provider and interface-binding sections.
func CommonConfig(ipv6 bool, geodataLoader string, geoAutoUpdate bool) map[string]any {
	return map[string]any{
		"mode": "rule", "log-level": "info", "ipv6": ipv6,
		"unified-delay": true, "tcp-concurrent": true, "geodata-mode": true,
		"geodata-loader": geodataLoader, "geo-auto-update": geoAutoUpdate,
	}
}

func GeoURLs(includeMMDB bool) map[string]any {
	result := map[string]any{"geoip": GeoIPURL, "geosite": GeoSiteURL}
	if includeMMDB {
		result["mmdb"] = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country.mmdb"
	}
	return result
}

func Sniffer(skipPrivateDestinations bool) map[string]any {
	result := map[string]any{
		"enable": true, "force-dns-mapping": true, "parse-pure-ip": true, "override-destination": true,
		"sniff": map[string]any{
			"HTTP": map[string]any{"ports": []any{80, "8080-8880"}, "override-destination": true},
			"TLS":  map[string]any{"ports": []any{443, 8443}},
			"QUIC": map[string]any{"ports": []any{443, 8443}},
		},
	}
	if skipPrivateDestinations {
		result["skip-dst-address"] = []string{
			"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
			"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4",
		}
	}
	return result
}
