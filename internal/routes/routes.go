package routes

import (
	"fmt"
	"math/big"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

var (
	transportRE       = regexp.MustCompile(`(?i)^(tcp|udp|any)\s*:\s*(.+)$`)
	portOnlyRE        = regexp.MustCompile(`^:\s*(.+)$`)
	transportSuffixRE = regexp.MustCompile(`(?i)^(.+?)\s*/\s*(tcp|udp|any)$`)
	geoRE             = regexp.MustCompile(`(?i)^(geoip|geosite)\s*:\s*([a-z0-9_@.+-]+)$`)
	presetRE          = regexp.MustCompile(`(?i)^preset\s*:\s*([a-z0-9_-]+)$`)
	domainRE          = regexp.MustCompile(`^[a-z0-9*?._-]+$`)
)

var routeLists = []string{"direct", "proxy", "block"}

const MaxEntriesPerList = 10_000

var trafficPresets = map[string][][2]string{
	"http":      {{"tcp", "80"}, {"tcp", "8080"}, {"tcp", "8000-8008"}},
	"https":     {{"tcp", "443"}, {"tcp", "8443"}},
	"quic":      {{"udp", "443"}, {"udp", "8443"}},
	"dns":       {{"tcp", "53"}, {"udp", "53"}},
	"dot":       {{"tcp", "853"}},
	"doq":       {{"udp", "853"}},
	"ssh":       {{"tcp", "22"}},
	"ftp":       {{"tcp", "20-21"}},
	"mail":      {{"tcp", "25"}, {"tcp", "110"}, {"tcp", "143"}, {"tcp", "465"}, {"tcp", "587"}, {"tcp", "993"}, {"tcp", "995"}},
	"torrent":   {{"tcp", "6881-6889"}, {"udp", "6881-6889"}},
	"ntp":       {{"udp", "123"}},
	"stun":      {{"udp", "3478-3481"}},
	"rdp":       {{"tcp", "3389"}, {"udp", "3389"}},
	"openvpn":   {{"tcp", "1194"}, {"udp", "1194"}},
	"wireguard": {{"udp", "51820"}},
}

type ParsedEntry struct {
	Kind       string   `json:"kind"`
	Normalized string   `json:"normalized"`
	Rules      []string `json:"rules"`
}

type Item struct {
	Source     string   `json:"source"`
	Kind       string   `json:"kind"`
	Normalized string   `json:"normalized"`
	Rules      []string `json:"rules"`
}

type Stats struct {
	SourceEntries     int `json:"source_entries"`
	NormalizedEntries int `json:"normalized_entries"`
	CompiledRules     int `json:"compiled_rules"`
	DuplicatesRemoved int `json:"duplicates_removed"`
	Ranges            int `json:"ranges"`
}

type CompileResult struct {
	Normalized map[string][]string `json:"normalized"`
	Compiled   map[string][]string `json:"compiled"`
	Items      map[string][]Item   `json:"items"`
	Stats      map[string]Stats    `json:"stats"`
}

type ValidationError struct {
	Code  string `json:"error"`
	List  string `json:"list,omitempty"`
	Index *int   `json:"index,omitempty"`
	Entry string `json:"entry,omitempty"`
}

func (e *ValidationError) Error() string { return e.Code }

func ParseEntry(entry string) (ParsedEntry, error) {
	if result, matched, err := normalizePreset(entry); matched || err != nil {
		return result, err
	}
	if result, matched, err := normalizeTransport(entry); matched || err != nil {
		return result, err
	}
	if result, matched, err := normalizeGeo(entry); matched || err != nil {
		return result, err
	}

	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(entry))
		if err != nil {
			return ParsedEntry{}, fmt.Errorf("invalid_cidr")
		}
		canonical := prefix.Masked().String()
		return ParsedEntry{"cidr", canonical, []string{"IP-CIDR," + canonical}}, nil
	}

	if address, err := netip.ParseAddr(strings.TrimSpace(entry)); err == nil {
		canonical := address.String()
		prefix := netip.PrefixFrom(address, address.BitLen()).String()
		return ParsedEntry{"ip", canonical, []string{"IP-CIDR," + prefix}}, nil
	}

	if strings.Count(entry, "-") == 1 {
		parts := strings.SplitN(entry, "-", 2)
		left, leftErr := netip.ParseAddr(strings.TrimSpace(parts[0]))
		right, rightErr := netip.ParseAddr(strings.TrimSpace(parts[1]))
		if leftErr == nil || rightErr == nil {
			if leftErr != nil || rightErr != nil || left.BitLen() != right.BitLen() || left.Compare(right) > 0 {
				return ParsedEntry{}, fmt.Errorf("invalid_ip_range")
			}
			rules := make([]string, 0)
			for _, prefix := range summarizeRange(left, right) {
				rules = append(rules, "IP-CIDR,"+prefix.String())
			}
			return ParsedEntry{"ip_range", left.String() + "-" + right.String(), rules}, nil
		}
	}

	rule, normalized, err := normalizeDomain(entry)
	if err != nil {
		return ParsedEntry{}, err
	}
	return ParsedEntry{"domain", normalized, []string{rule}}, nil
}

func CompileLists(lists map[string][]string) (CompileResult, error) {
	unknown := []string{}
	for name := range lists {
		if name != "direct" && name != "proxy" && name != "block" {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return CompileResult{}, &ValidationError{Code: "unknown_route_list", List: unknown[0]}
	}
	missing := []string{}
	for _, listName := range routeLists {
		if _, exists := lists[listName]; !exists {
			missing = append(missing, listName)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return CompileResult{}, &ValidationError{Code: "missing_route_list", List: missing[0]}
	}

	result := CompileResult{
		Normalized: map[string][]string{},
		Compiled:   map[string][]string{},
		Items:      map[string][]Item{},
		Stats:      map[string]Stats{},
	}
	for _, listName := range routeLists {
		entries := lists[listName]
		if len(entries) > MaxEntriesPerList {
			return CompileResult{}, &ValidationError{Code: "route_list_too_large", List: listName}
		}
		result.Normalized[listName] = []string{}
		result.Compiled[listName] = []string{}
		result.Items[listName] = []Item{}
		seenSources := map[string]bool{}
		seenRules := map[string]bool{}
		duplicates, ranges := 0, 0
		for index, entry := range entries {
			parsed, err := ParseEntry(entry)
			if err != nil {
				position := index
				return CompileResult{}, &ValidationError{Code: err.Error(), List: listName, Index: &position, Entry: entry}
			}
			sourceKey := parsed.Kind + "\x00" + parsed.Normalized
			if seenSources[sourceKey] {
				duplicates++
				continue
			}
			seenSources[sourceKey] = true
			result.Normalized[listName] = append(result.Normalized[listName], parsed.Normalized)
			if parsed.Kind == "ip_range" {
				ranges++
			}
			emitted := []string{}
			for _, rule := range parsed.Rules {
				if !seenRules[rule] {
					seenRules[rule] = true
					result.Compiled[listName] = append(result.Compiled[listName], rule)
					emitted = append(emitted, rule)
				}
			}
			result.Items[listName] = append(result.Items[listName], Item{entry, parsed.Kind, parsed.Normalized, emitted})
		}
		result.Stats[listName] = Stats{len(entries), len(result.Normalized[listName]), len(result.Compiled[listName]), duplicates, ranges}
	}
	return result, nil
}

func normalizePreset(value string) (ParsedEntry, bool, error) {
	match := presetRE.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "preset:") {
			return ParsedEntry{}, true, fmt.Errorf("invalid_traffic_preset")
		}
		return ParsedEntry{}, false, nil
	}
	name := strings.ToLower(match[1])
	preset, ok := trafficPresets[name]
	if !ok {
		return ParsedEntry{}, true, fmt.Errorf("unknown_traffic_preset")
	}
	rules := make([]string, 0, len(preset))
	for _, value := range preset {
		rules = append(rules, fmt.Sprintf("AND,((NETWORK,%s),(DST-PORT,%s))", strings.ToUpper(value[0]), value[1]))
	}
	return ParsedEntry{"preset", "preset:" + name, rules}, true, nil
}

func normalizeTransport(value string) (ParsedEntry, bool, error) {
	trimmed := strings.TrimSpace(value)
	protocol, rawPorts := "", ""
	if match := portOnlyRE.FindStringSubmatch(trimmed); match != nil {
		protocol, rawPorts = "any", match[1]
	} else if match := transportRE.FindStringSubmatch(trimmed); match != nil {
		protocol, rawPorts = strings.ToLower(match[1]), match[2]
	} else if match := transportSuffixRE.FindStringSubmatch(trimmed); match != nil {
		protocol, rawPorts = strings.ToLower(match[2]), match[1]
	} else {
		return ParsedEntry{}, false, nil
	}

	parts := strings.Split(rawPorts, ",")
	if len(parts) == 0 {
		return ParsedEntry{}, true, fmt.Errorf("invalid_port_rule")
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
		if parts[index] == "" {
			return ParsedEntry{}, true, fmt.Errorf("invalid_port_rule")
		}
	}
	if contains(parts, "*") {
		if len(parts) != 1 || protocol == "any" {
			return ParsedEntry{}, true, fmt.Errorf("invalid_port_rule")
		}
		return ParsedEntry{"protocol", protocol + ":*", []string{"NETWORK," + strings.ToUpper(protocol)}}, true, nil
	}

	normalizedPorts := []string{}
	for _, part := range parts {
		normalized := ""
		if strings.Contains(part, "-") {
			if strings.Count(part, "-") != 1 {
				return ParsedEntry{}, true, fmt.Errorf("invalid_port_range")
			}
			bounds := strings.SplitN(part, "-", 2)
			start, startErr := strconv.Atoi(bounds[0])
			end, endErr := strconv.Atoi(bounds[1])
			if startErr != nil || endErr != nil || start < 1 || start > end || end > 65535 {
				return ParsedEntry{}, true, fmt.Errorf("invalid_port_range")
			}
			normalized = strconv.Itoa(start) + "-" + strconv.Itoa(end)
		} else {
			port, err := strconv.Atoi(part)
			if err != nil || port < 1 || port > 65535 {
				return ParsedEntry{}, true, fmt.Errorf("invalid_port")
			}
			normalized = strconv.Itoa(port)
		}
		if !contains(normalizedPorts, normalized) {
			normalizedPorts = append(normalizedPorts, normalized)
		}
	}
	rules := make([]string, 0, len(normalizedPorts))
	for _, port := range normalizedPorts {
		portRule := "DST-PORT," + port
		if protocol == "any" {
			rules = append(rules, portRule)
		} else {
			rules = append(rules, fmt.Sprintf("AND,((NETWORK,%s),(%s))", strings.ToUpper(protocol), portRule))
		}
	}
	prefix, kind := protocol+":", "transport"
	if protocol == "any" {
		prefix, kind = ":", "port"
	}
	return ParsedEntry{kind, prefix + strings.Join(normalizedPorts, ","), rules}, true, nil
}

func normalizeGeo(value string) (ParsedEntry, bool, error) {
	trimmed := strings.TrimSpace(value)
	match := geoRE.FindStringSubmatch(trimmed)
	if match == nil {
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "geoip:") || strings.HasPrefix(lower, "geosite:") {
			return ParsedEntry{}, true, fmt.Errorf("invalid_geo_rule")
		}
		return ParsedEntry{}, false, nil
	}
	kind, code := strings.ToLower(match[1]), strings.ToLower(match[2])
	if kind == "geoip" {
		code = strings.ToUpper(code)
	}
	return ParsedEntry{kind, kind + ":" + code, []string{strings.ToUpper(kind) + "," + code}}, true, nil
}

func normalizeDomain(value string) (string, string, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	exact := strings.HasPrefix(domain, "=")
	if exact {
		domain = strings.TrimPrefix(domain, "=")
	}
	explicitSuffix := strings.HasPrefix(domain, ".")
	suffix := explicitSuffix || !exact
	if explicitSuffix {
		domain = strings.TrimPrefix(domain, ".")
	}
	leadingWildcard := false
	if strings.HasPrefix(domain, "*.") {
		leadingWildcard, suffix, domain = true, true, strings.TrimPrefix(domain, "*.")
	} else if strings.HasPrefix(domain, "*") && strings.Count(domain, "*") == 1 && !strings.Contains(domain, "?") {
		leadingWildcard, suffix, domain = true, true, strings.TrimPrefix(domain, "*")
	}
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, " \t\r\n/,:@#") {
		return "", "", fmt.Errorf("invalid_domain")
	}
	wildcard := strings.ContainsAny(domain, "*?")
	if wildcard && !leadingWildcard {
		suffix = false
	}
	if wildcard && !isASCII(domain) {
		return "", "", fmt.Errorf("unicode_wildcard_requires_punycode")
	}
	if !wildcard {
		ascii, err := idna.Lookup.ToASCII(domain)
		if err != nil {
			return "", "", fmt.Errorf("invalid_domain")
		}
		domain = ascii
	}
	if !domainRE.MatchString(domain) {
		return "", "", fmt.Errorf("invalid_domain")
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 {
			return "", "", fmt.Errorf("invalid_domain")
		}
		sample := strings.NewReplacer("*", "a", "?", "a").Replace(label)
		if strings.HasPrefix(sample, "-") || strings.HasSuffix(sample, "-") {
			return "", "", fmt.Errorf("invalid_domain")
		}
	}
	if exact && wildcard {
		return "", "", fmt.Errorf("exact_domain_cannot_use_wildcard")
	}
	if suffix {
		return "DOMAIN-SUFFIX," + domain, "." + domain, nil
	}
	if wildcard {
		return "DOMAIN-WILDCARD," + domain, domain, nil
	}
	return "DOMAIN," + domain, "=" + domain, nil
}

func summarizeRange(first, last netip.Addr) []netip.Prefix {
	bits := first.BitLen()
	current := addrToBig(first)
	end := addrToBig(last)
	one := big.NewInt(1)
	result := []netip.Prefix{}
	for current.Cmp(end) <= 0 {
		trailing := bits
		for index := 0; index < bits; index++ {
			if current.Bit(index) != 0 {
				trailing = index
				break
			}
		}
		remaining := new(big.Int).Sub(end, current)
		remaining.Add(remaining, one)
		maxByRemaining := remaining.BitLen() - 1
		hostBits := trailing
		if maxByRemaining < hostBits {
			hostBits = maxByRemaining
		}
		address := bigToAddr(current, bits)
		result = append(result, netip.PrefixFrom(address, bits-hostBits))
		current = new(big.Int).Add(current, new(big.Int).Lsh(big.NewInt(1), uint(hostBits)))
	}
	return result
}

func addrToBig(address netip.Addr) *big.Int {
	bytes := address.AsSlice()
	return new(big.Int).SetBytes(bytes)
}

func bigToAddr(value *big.Int, bits int) netip.Addr {
	size := bits / 8
	buffer := make([]byte, size)
	value.FillBytes(buffer)
	if bits == 32 {
		var bytes [4]byte
		copy(bytes[:], buffer)
		return netip.AddrFrom4(bytes)
	}
	var bytes [16]byte
	copy(bytes[:], buffer)
	return netip.AddrFrom16(bytes)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isASCII(value string) bool {
	for _, character := range value {
		if character > 127 {
			return false
		}
	}
	return true
}
