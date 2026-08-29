package nodes

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
)

var controlRE = regexp.MustCompile(`[\x00-\x1f\x7f]`)
var spacesRE = regexp.MustCompile(`\s+`)

var shadowsocksCiphers = map[string]bool{
	"aes-128-gcm": true, "aes-192-gcm": true, "aes-256-gcm": true,
	"aes-128-ctr": true, "aes-192-ctr": true, "aes-256-ctr": true,
	"aes-128-cfb": true, "aes-192-cfb": true, "aes-256-cfb": true,
	"chacha20": true, "chacha20-ietf": true, "xchacha20": true,
	"chacha20-ietf-poly1305": true, "xchacha20-ietf-poly1305": true,
	"2022-blake3-aes-128-gcm": true, "2022-blake3-aes-256-gcm": true,
	"2022-blake3-chacha20-poly1305": true, "rc4-md5": true, "none": true,
}

type ConversionResult struct {
	Proxies []map[string]any `json:"proxies"`
	Errors  map[string]int   `json:"errors"`
}

func ConvertLinks(links []string, source string) ConversionResult {
	result := ConversionResult{Proxies: []map[string]any{}, Errors: map[string]int{}}
	seen := map[string]bool{}
	for index, link := range links {
		if seen[link] {
			continue
		}
		seen[link] = true
		proxy, err := ParseLink(link, source, index+1)
		if err != nil {
			message := strings.SplitN(err.Error(), ":", 2)[0]
			if len(message) > 80 {
				message = message[:80]
			}
			result.Errors["ValueError:"+message]++
			continue
		}
		oldName := proxy["name"].(string)
		label := ""
		if position := strings.Index(oldName, " "); position >= 0 {
			label = oldName[position+1:]
		}
		fingerprintSource := cloneMap(proxy)
		delete(fingerprintSource, "name")
		digest := sha256.Sum256(canonicalJSON(fingerprintSource))
		name := strings.ToUpper(source) + "-" + hex.EncodeToString(digest[:])[:12]
		if label != "" {
			name += " " + label
		}
		proxy["name"] = name
		result.Proxies = append(result.Proxies, proxy)
	}
	return result
}

func ParseLink(link, source string, index int) (map[string]any, error) {
	scheme, _, found := strings.Cut(link, ":")
	if !found {
		return nil, fmt.Errorf("unsupported protocol")
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVLESS(link, source, index)
	case "vmess":
		return parseVMess(link, source, index)
	case "trojan":
		return parseTrojan(link, source, index)
	case "ss":
		return parseShadowsocks(link, source, index)
	case "hysteria2", "hy2":
		return parseHysteria2(link, source, index)
	case "wireguard", "wg", "amneziawg", "awg":
		return parseWireGuard(link, source, index)
	case "orcheroute":
		return parseVKCall(link, source, index)
	default:
		return nil, fmt.Errorf("unsupported protocol")
	}
}

func parseVKCall(link, source string, index int) (map[string]any, error) {
	profile, err := callprofile.Decode(link, time.Now())
	if err != nil {
		return nil, err
	}
	host, portValue, err := net.SplitHostPort(profile.PeerAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid vkcall peer")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		return nil, fmt.Errorf("invalid vkcall peer")
	}
	label := profile.Name
	if label == "" {
		label = "VK Call"
	}
	return map[string]any{
		"name": name(source, index, label), "type": "vkcall",
		"server": host, "port": port, "udp": true, "profile": link,
	}, nil
}

func parseWireGuard(link, source string, index int) (map[string]any, error) {
	_, encoded, found := strings.Cut(link, "://")
	if !found {
		return nil, fmt.Errorf("invalid wireguard payload")
	}
	encoded, _, _ = strings.Cut(encoded, "#")
	decoded, err := decodeBase64(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid wireguard payload")
	}
	sections, err := parseWireGuardINI(string(decoded))
	if err != nil {
		return nil, err
	}
	iface := sections[0]
	privateKey := iface["privatekey"]
	addresses := commaValues(iface["address"])
	if privateKey == "" || len(addresses) == 0 || len(sections) < 2 {
		return nil, fmt.Errorf("incomplete wireguard config")
	}
	proxy := map[string]any{
		"name": name(source, index, wireGuardLabel(iface)), "type": "wireguard",
		"private-key": privateKey, "udp": true,
	}
	for _, address := range addresses {
		host := strings.SplitN(address, "/", 2)[0]
		if ip := net.ParseIP(host); ip != nil {
			if ip.To4() != nil && proxy["ip"] == nil {
				proxy["ip"] = host
			} else if ip.To4() == nil && proxy["ipv6"] == nil {
				proxy["ipv6"] = host
			}
		}
	}
	if proxy["ip"] == nil && proxy["ipv6"] == nil {
		return nil, fmt.Errorf("invalid wireguard address")
	}
	if mtu, parseErr := optionalInteger(iface["mtu"]); parseErr != nil {
		return nil, fmt.Errorf("invalid wireguard mtu")
	} else if mtu > 0 {
		proxy["mtu"] = mtu
	}
	if dns := commaValues(iface["dns"]); len(dns) > 0 {
		proxy["remote-dns-resolve"] = true
		proxy["dns"] = dns
	}
	peers := []map[string]any{}
	for _, values := range sections[1:] {
		endpoint, publicKey := values["endpoint"], values["publickey"]
		server, port, endpointErr := splitEndpoint(endpoint)
		if endpointErr != nil || publicKey == "" {
			return nil, fmt.Errorf("incomplete wireguard peer")
		}
		peer := map[string]any{"server": server, "port": port, "public-key": publicKey}
		allowed := commaValues(values["allowedips"])
		if len(allowed) == 0 {
			allowed = []string{"0.0.0.0/0", "::/0"}
		}
		peer["allowed-ips"] = allowed
		copyIf(peer, "pre-shared-key", values["presharedkey"])
		if keepalive, parseErr := optionalInteger(values["persistentkeepalive"]); parseErr != nil {
			return nil, fmt.Errorf("invalid wireguard keepalive")
		} else if keepalive > 0 {
			peer["persistent-keepalive"] = keepalive
		}
		if reserved := parseReserved(values["reserved"]); reserved != nil {
			peer["reserved"] = reserved
		}
		peers = append(peers, peer)
	}
	proxy["peers"] = peers
	if options := amneziaOptions(iface); len(options) > 0 {
		proxy["amnezia-wg-option"] = options
	}
	return proxy, nil
}

func parseWireGuardINI(value string) ([]map[string]string, error) {
	sections := []map[string]string{}
	current := map[string]string(nil)
	currentKind := ""
	for _, raw := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			kind := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if kind != "interface" && kind != "peer" {
				current = nil
				currentKind = ""
				continue
			}
			if kind == "interface" && len(sections) != 0 {
				return nil, fmt.Errorf("multiple wireguard interfaces")
			}
			if kind == "peer" && len(sections) == 0 {
				return nil, fmt.Errorf("wireguard interface missing")
			}
			current, currentKind = map[string]string{}, kind
			sections = append(sections, current)
			continue
		}
		key, item, found := strings.Cut(line, "=")
		if !found || current == nil || currentKind == "" {
			continue
		}
		current[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(item)
	}
	if len(sections) < 2 {
		return nil, fmt.Errorf("incomplete wireguard config")
	}
	return sections, nil
}

func splitEndpoint(value string) (string, int, error) {
	host, portRaw, err := net.SplitHostPort(value)
	if err != nil {
		position := strings.LastIndex(value, ":")
		if position <= 0 {
			return "", 0, err
		}
		host, portRaw = value[:position], value[position+1:]
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 || strings.Trim(host, "[] ") == "" {
		return "", 0, fmt.Errorf("invalid endpoint")
	}
	return strings.Trim(host, "[] "), port, nil
}

func optionalInteger(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return strconv.Atoi(strings.TrimSpace(value))
}

func commaValues(value string) []string {
	return splitNonEmpty(value)
}

func parseReserved(value string) any {
	items := commaValues(strings.Trim(value, "[]"))
	if len(items) != 3 {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		return nil
	}
	result := make([]int, 0, 3)
	for _, item := range items {
		parsed, err := strconv.Atoi(item)
		if err != nil || parsed < 0 || parsed > 255 {
			return strings.TrimSpace(value)
		}
		result = append(result, parsed)
	}
	return result
}

func amneziaOptions(iface map[string]string) map[string]any {
	result := map[string]any{}
	for _, key := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "itime"} {
		if value := iface[key]; value != "" {
			if number, err := strconv.Atoi(value); err == nil {
				result[key] = number
			} else {
				result[key] = value
			}
		}
	}
	for _, key := range []string{"h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5", "j1", "j2", "j3"} {
		copyIf(result, key, iface[key])
	}
	return result
}

func wireGuardLabel(iface map[string]string) string {
	if len(amneziaOptions(iface)) > 0 {
		return "AmneziaWG"
	}
	return "WireGuard"
}

func parseHysteria2(link, source string, index int) (map[string]any, error) {
	parsed, query, err := parseURL(link)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" || parsed.User == nil || parsed.User.Username() == "" {
		return nil, fmt.Errorf("incomplete hysteria2 link")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("incomplete hysteria2 link")
	}
	password := parsed.User.Username()
	if suffix, ok := parsed.User.Password(); ok {
		password += ":" + suffix
	}
	proxy := map[string]any{
		"name": name(source, index, parsed.Fragment), "type": "hysteria2", "server": parsed.Hostname(),
		"port": port, "password": password, "udp": true,
	}
	copyIf(proxy, "sni", firstNonEmpty(query.Get("sni"), query.Get("peer")))
	copyIf(proxy, "fingerprint", firstNonEmpty(query.Get("fingerprint"), query.Get("fp")))
	copyIf(proxy, "obfs", query.Get("obfs"))
	copyIf(proxy, "obfs-password", firstNonEmpty(query.Get("obfs-password"), query.Get("obfsParam")))
	copyIf(proxy, "ports", firstNonEmpty(query.Get("ports"), query.Get("mport")))
	copyIf(proxy, "hop-interval", query.Get("hop-interval"))
	copyIf(proxy, "up", query.Get("up"))
	copyIf(proxy, "down", query.Get("down"))
	if alpn := splitNonEmpty(query.Get("alpn")); len(alpn) > 0 {
		proxy["alpn"] = alpn
	}
	if truthy(firstNonEmpty(query.Get("allowInsecure"), query.Get("allowinsecure"), query.Get("insecure"))) {
		proxy["skip-cert-verify"] = true
	}
	return proxy, nil
}

func parseVLESS(link, source string, index int) (map[string]any, error) {
	parsed, query, err := parseURL(link)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" || parsed.User == nil || parsed.User.Username() == "" {
		return nil, fmt.Errorf("incomplete vless link")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return nil, fmt.Errorf("incomplete vless link")
	}
	proxy := map[string]any{
		"name": name(source, index, parsed.Fragment), "type": "vless", "server": parsed.Hostname(),
		"port": port, "uuid": parsed.User.Username(), "udp": true,
	}
	security := strings.ToLower(first(query, "security", "none"))
	if security == "tls" || security == "reality" {
		proxy["tls"] = true
		copyIf(proxy, "servername", query.Get("sni"))
		copyIf(proxy, "client-fingerprint", query.Get("fp"))
		if truthy(firstNonEmpty(query.Get("allowInsecure"), query.Get("allowinsecure"), query.Get("insecure"))) {
			proxy["skip-cert-verify"] = true
		}
	}
	if security == "reality" {
		reality := map[string]any{}
		copyIf(reality, "public-key", query.Get("pbk"))
		copyIf(reality, "short-id", query.Get("sid"))
		if _, ok := reality["public-key"]; !ok {
			return nil, fmt.Errorf("reality public key missing")
		}
		proxy["reality-opts"] = reality
	}
	copyIf(proxy, "flow", query.Get("flow"))
	if err := applyTransport(proxy, query); err != nil {
		return nil, err
	}
	return proxy, nil
}

func parseTrojan(link, source string, index int) (map[string]any, error) {
	parsed, query, err := parseURL(link)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" || parsed.User == nil || parsed.User.Username() == "" {
		return nil, fmt.Errorf("incomplete trojan link")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return nil, fmt.Errorf("incomplete trojan link")
	}
	proxy := map[string]any{
		"name": name(source, index, parsed.Fragment), "type": "trojan", "server": parsed.Hostname(),
		"port": port, "password": parsed.User.Username(), "udp": true,
	}
	copyIf(proxy, "sni", firstNonEmpty(query.Get("sni"), query.Get("peer")))
	copyIf(proxy, "client-fingerprint", query.Get("fp"))
	if truthy(firstNonEmpty(query.Get("allowInsecure"), query.Get("allowinsecure"), query.Get("insecure"))) {
		proxy["skip-cert-verify"] = true
	}
	if err := applyTransport(proxy, query); err != nil {
		return nil, err
	}
	return proxy, nil
}

func parseVMess(link, source string, index int) (map[string]any, error) {
	_, encoded, found := strings.Cut(link, "://")
	if !found {
		return nil, fmt.Errorf("incomplete vmess link")
	}
	decoded, err := decodeBase64(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid vmess payload")
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, fmt.Errorf("invalid vmess payload")
	}
	server, uuid := stringValue(payload["add"]), stringValue(payload["id"])
	port, portErr := integerValue(payload["port"])
	if server == "" || uuid == "" || portErr != nil {
		return nil, fmt.Errorf("incomplete vmess link")
	}
	alterID, _ := integerValueDefault(payload["aid"], 0)
	proxy := map[string]any{
		"name": name(source, index, stringValue(payload["ps"])), "type": "vmess", "server": server,
		"port": port, "uuid": uuid, "alterId": alterID, "cipher": firstNonEmpty(stringValue(payload["scy"]), "auto"), "udp": true,
	}
	query := url.Values{
		"type":        {firstNonEmpty(stringValue(payload["net"]), "tcp")},
		"path":        {stringValue(payload["path"])},
		"host":        {stringValue(payload["host"])},
		"serviceName": {stringValue(payload["path"])},
		"sni":         {firstNonEmpty(stringValue(payload["sni"]), stringValue(payload["host"]))},
		"fp":          {stringValue(payload["fp"])},
	}
	tls := strings.ToLower(stringValue(payload["tls"]))
	if tls != "" && tls != "none" && tls != "false" && tls != "0" {
		proxy["tls"] = true
		copyIf(proxy, "servername", query.Get("sni"))
		copyIf(proxy, "client-fingerprint", query.Get("fp"))
		if truthy(stringValue(payload["allowInsecure"])) {
			proxy["skip-cert-verify"] = true
		}
	}
	if err := applyTransport(proxy, query); err != nil {
		return nil, err
	}
	return proxy, nil
}

func parseShadowsocks(link, source string, index int) (map[string]any, error) {
	raw := strings.TrimPrefix(link, "ss://")
	body, fragment, _ := strings.Cut(raw, "#")
	body, queryString, _ := strings.Cut(body, "?")
	if !strings.Contains(body, "@") {
		decoded, err := decodeBase64(body)
		if err != nil {
			return nil, fmt.Errorf("invalid shadowsocks link")
		}
		body = string(decoded)
	}
	position := strings.LastIndex(body, "@")
	if position < 0 {
		return nil, fmt.Errorf("incomplete shadowsocks link")
	}
	credentials, address := body[:position], body[position+1:]
	if !strings.Contains(credentials, ":") {
		decoded, err := decodeBase64(credentials)
		if err != nil {
			return nil, fmt.Errorf("invalid shadowsocks credentials")
		}
		credentials = string(decoded)
	}
	method, password, found := strings.Cut(credentials, ":")
	if !found {
		return nil, fmt.Errorf("incomplete shadowsocks link")
	}
	method, _ = url.QueryUnescape(method)
	method = strings.ToLower(method)
	if method == "chacha20-poly1305" {
		method = "chacha20-ietf-poly1305"
	} else if method == "xchacha20-poly1305" {
		method = "xchacha20-ietf-poly1305"
	}
	if !shadowsocksCiphers[method] {
		return nil, fmt.Errorf("unsupported shadowsocks cipher")
	}
	if strings.Contains(strings.ToLower(queryString), "plugin=") {
		return nil, fmt.Errorf("shadowsocks plugin is not supported")
	}
	host, portRaw, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("incomplete shadowsocks link")
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		return nil, fmt.Errorf("incomplete shadowsocks link")
	}
	password, _ = url.QueryUnescape(password)
	return map[string]any{
		"name": name(source, index, fragment), "type": "ss", "server": host, "port": port,
		"cipher": method, "password": password, "udp": true,
	}, nil
}

func applyTransport(proxy map[string]any, query url.Values) error {
	network := strings.ToLower(first(query, "type", first(query, "network", "tcp")))
	switch network {
	case "raw", "tcp":
		proxy["network"] = "tcp"
	case "ws":
		proxy["network"] = "ws"
		opts := map[string]any{"path": unescapeDefault(query.Get("path"), "/")}
		if host := query.Get("host"); host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		proxy["ws-opts"] = opts
	case "grpc", "gun":
		proxy["network"] = "grpc"
		// Mihomo treats a leading slash as an explicit custom gRPC path. It
		// must not be normalized away: "service" maps to /service/Tun while
		// "/api/v1/stream" must remain exactly /api/v1/stream.
		service := unescapeDefault(firstNonEmpty(query.Get("serviceName"), query.Get("service_name"), query.Get("path")), "")
		opts := map[string]any{"grpc-service-name": service}
		copyIf(opts, "grpc-authority", query.Get("authority"))
		proxy["grpc-opts"] = opts
	case "http", "h2":
		proxy["network"] = "h2"
		opts := map[string]any{"path": unescapeDefault(query.Get("path"), "/")}
		if host := query.Get("host"); host != "" {
			hosts := []string{}
			for _, item := range strings.Split(host, ",") {
				if item = strings.TrimSpace(item); item != "" {
					hosts = append(hosts, item)
				}
			}
			opts["host"] = hosts
		}
		proxy["h2-opts"] = opts
	case "httpupgrade", "http-upgrade":
		proxy["network"] = "httpupgrade"
		opts := map[string]any{"path": unescapeDefault(query.Get("path"), "/")}
		if host := query.Get("host"); host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		proxy["http-upgrade-opts"] = opts
	case "xhttp", "splithttp":
		proxy["network"] = "xhttp"
		opts := map[string]any{"path": unescapeDefault(query.Get("path"), "/")}
		copyIf(opts, "host", query.Get("host"))
		copyIf(opts, "mode", query.Get("mode"))
		if extra := query.Get("extra"); extra != "" {
			decoded, _ := url.QueryUnescape(extra)
			var values map[string]any
			if json.Unmarshal([]byte(decoded), &values) == nil {
				for key, value := range values {
					opts[key] = value
				}
			}
		}
		proxy["xhttp-opts"] = opts
	default:
		return fmt.Errorf("unsupported network")
	}
	return nil
}

func parseURL(link string) (*url.URL, url.Values, error) {
	parsed, err := url.Parse(strings.ReplaceAll(link, "&amp;", "&"))
	if err != nil {
		return nil, nil, err
	}
	return parsed, parsed.Query(), nil
}

func name(source string, index int, fragment string) string {
	label, _ := url.QueryUnescape(fragment)
	label = controlRE.ReplaceAllString(strings.TrimSpace(label), "")
	label = spacesRE.ReplaceAllString(label, " ")
	if runes := []rune(label); len(runes) > 72 {
		label = string(runes[:72])
	}
	result := fmt.Sprintf("%s-%04d", strings.ToUpper(source), index)
	if label != "" {
		result += " " + label
	}
	return result
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), "")
	if result, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "=")); err == nil {
		return result, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(value, "="))
}

func canonicalJSON(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func copyIf(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func truthy(value string) bool {
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func first(values url.Values, key, fallback string) string {
	if value := values.Get(key); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func splitNonEmpty(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func unescapeDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	if decoded, err := url.QueryUnescape(value); err == nil {
		return decoded
	}
	return value
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func integerValue(value any) (int, error) {
	switch current := value.(type) {
	case float64:
		return int(current), nil
	case string:
		return strconv.Atoi(current)
	case int:
		return current, nil
	default:
		return 0, fmt.Errorf("invalid integer")
	}
}

func integerValueDefault(value any, fallback int) (int, error) {
	if value == nil || stringValue(value) == "" {
		return fallback, nil
	}
	return integerValue(value)
}
