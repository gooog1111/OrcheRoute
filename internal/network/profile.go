package network

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var hostnameRE = regexp.MustCompile(`^[a-z0-9.-]+$`)

var roleOrder = []string{"direct", "vpn_underlay"}

var roleSpecs = map[string]struct{ Mark, Table, Priority int }{
	"direct":       {0x5351, 5351, 90},
	"vpn_underlay": {0x5352, 5352, 91},
}

var DefaultDNS = DNSConfig{
	Direct:         []string{"1.1.1.1", "8.8.8.8"},
	Proxy:          []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"},
	VPNUnderlay:    []string{"1.1.1.1", "8.8.8.8"},
	Bootstrap:      []string{"1.1.1.1", "8.8.8.8"},
	CacheAlgorithm: "arc", PreferH3: false, UseHosts: true, IPv6: false,
}

type ValidationError struct {
	Code  string `json:"error"`
	Field string `json:"field,omitempty"`
	Value any    `json:"value,omitempty"`
}

func (e *ValidationError) Error() string { return e.Code }

type RoleInput struct {
	Interface string  `json:"interface"`
	Gateway   *string `json:"gateway"`
	Source    *string `json:"source"`
}

type CaptureInput struct {
	Mode            string   `json:"mode"`
	Interfaces      []string `json:"interfaces"`
	BypassLocal     *bool    `json:"bypass_local"`
	BypassCIDRs     []string `json:"bypass_cidrs"`
	ManagementCIDRs []string `json:"management_cidrs"`
	DNSHijack       *bool    `json:"dns_hijack"`
	StrictRoute     *bool    `json:"strict_route"`
}

type DNSInput struct {
	Direct         []string `json:"direct"`
	Proxy          []string `json:"proxy"`
	VPNUnderlay    []string `json:"vpn_underlay"`
	Bootstrap      []string `json:"bootstrap"`
	CacheAlgorithm string   `json:"cache_algorithm"`
	PreferH3       *bool    `json:"prefer_h3"`
	UseHosts       *bool    `json:"use_hosts"`
	IPv6           *bool    `json:"ipv6"`
	UnknownFields  []string `json:"-"`
}

func (input *DNSInput) UnmarshalJSON(data []byte) error {
	type plain DNSInput
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	known := map[string]bool{
		"direct": true, "proxy": true, "vpn_underlay": true, "bootstrap": true,
		"cache_algorithm": true, "prefer_h3": true, "use_hosts": true, "ipv6": true,
	}
	unknown := []string{}
	for name := range fields {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	*input = DNSInput(decoded)
	input.UnknownFields = unknown
	return nil
}

type ProfileInput struct {
	Version   int                  `json:"version"`
	Revision  int64                `json:"revision"`
	UpdatedAt int64                `json:"updated_at"`
	Roles     map[string]RoleInput `json:"roles"`
	Capture   *CaptureInput        `json:"capture"`
	DNS       *DNSInput            `json:"dns"`
}

type Role struct {
	Interface string  `json:"interface"`
	Gateway   *string `json:"gateway"`
	Source    *string `json:"source"`
}

type Capture struct {
	Mode            string   `json:"mode"`
	Interfaces      []string `json:"interfaces"`
	BypassLocal     bool     `json:"bypass_local"`
	BypassCIDRs     []string `json:"bypass_cidrs"`
	ManagementCIDRs []string `json:"management_cidrs"`
	DNSHijack       bool     `json:"dns_hijack"`
	StrictRoute     bool     `json:"strict_route"`
}

type DNSConfig struct {
	Direct         []string `json:"direct"`
	Proxy          []string `json:"proxy"`
	VPNUnderlay    []string `json:"vpn_underlay"`
	Bootstrap      []string `json:"bootstrap"`
	CacheAlgorithm string   `json:"cache_algorithm"`
	PreferH3       bool     `json:"prefer_h3"`
	UseHosts       bool     `json:"use_hosts"`
	IPv6           bool     `json:"ipv6"`
}

type Profile struct {
	Version   int             `json:"version"`
	Revision  int64           `json:"revision"`
	UpdatedAt int64           `json:"updated_at"`
	Roles     map[string]Role `json:"roles"`
	Capture   Capture         `json:"capture"`
	DNS       DNSConfig       `json:"dns"`
}

type Address struct {
	Family string `json:"family"`
	CIDR   string `json:"cidr"`
	Scope  string `json:"scope"`
}

type DefaultRoute struct {
	Gateway  *string `json:"gateway"`
	Source   *string `json:"source"`
	Metric   int     `json:"metric"`
	Table    string  `json:"table"`
	Protocol string  `json:"protocol"`
}

type Interface struct {
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	State         string         `json:"state"`
	MTU           any            `json:"mtu"`
	Loopback      bool           `json:"loopback"`
	Addresses     []Address      `json:"addresses"`
	DefaultRoutes []DefaultRoute `json:"default_routes"`
}

type Topology struct {
	Interfaces []Interface `json:"interfaces"`
	LocalCIDRs []string    `json:"local_cidrs"`
}

type ResolvedRole struct {
	Interface string  `json:"interface"`
	Gateway   *string `json:"gateway"`
	Source    *string `json:"source"`
	Mark      int     `json:"mark"`
	MarkHex   string  `json:"mark_hex"`
	Table     int     `json:"table"`
	Priority  int     `json:"priority"`
	State     string  `json:"state"`
	Kind      string  `json:"kind"`
}

type Warning struct {
	Code      string `json:"code"`
	Role      string `json:"role,omitempty"`
	Interface string `json:"interface"`
}

type DNSPreview struct {
	Config    DNSConfig    `json:"config"`
	Effective DNSEffective `json:"effective"`
}

type DNSEffective struct {
	Direct      []string `json:"direct"`
	Proxy       []string `json:"proxy"`
	VPNUnderlay []string `json:"vpn_underlay"`
	Bootstrap   []string `json:"bootstrap"`
}

type DNSProtection struct {
	Enabled     bool     `json:"enabled"`
	Scope       string   `json:"scope"`
	StrictRoute bool     `json:"strict_route"`
	Intercepts  []string `json:"intercepts"`
}

type Preview struct {
	Valid                      bool                    `json:"valid"`
	Profile                    Profile                 `json:"profile"`
	ResolvedRoles              map[string]ResolvedRole `json:"resolved_roles"`
	EffectiveBypassCIDRs       []string                `json:"effective_bypass_cidrs"`
	Warnings                   []Warning               `json:"warnings"`
	RequiresSystemConfirmation bool                    `json:"requires_system_confirmation"`
	DNS                        DNSPreview              `json:"dns"`
	DNSProtection              DNSProtection           `json:"dns_protection"`
	Digest                     string                  `json:"digest"`
}

func DefaultProfile(interfaceName string) ProfileInput {
	if interfaceName == "" {
		interfaceName = "ppp0"
	}
	trueValue, falseValue := true, false
	return ProfileInput{
		Version: 1,
		Roles: map[string]RoleInput{
			"direct": {Interface: interfaceName}, "vpn_underlay": {Interface: interfaceName},
		},
		Capture: &CaptureInput{
			Mode: "system", Interfaces: []string{}, BypassLocal: &trueValue,
			BypassCIDRs: []string{}, ManagementCIDRs: []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}, DNSHijack: &trueValue, StrictRoute: &trueValue,
		},
		DNS: &DNSInput{
			Direct: append([]string{}, DefaultDNS.Direct...), Proxy: append([]string{}, DefaultDNS.Proxy...),
			VPNUnderlay: append([]string{}, DefaultDNS.VPNUnderlay...), Bootstrap: append([]string{}, DefaultDNS.Bootstrap...),
			CacheAlgorithm: DefaultDNS.CacheAlgorithm, PreferH3: &falseValue, UseHosts: &trueValue, IPv6: &falseValue,
		},
	}
}

func ValidateProfile(input ProfileInput, available map[string]bool) (Profile, error) {
	if input.Version == 0 {
		input.Version = 1
	}
	if input.Version != 1 {
		return Profile{}, validation("invalid_network_profile", "", nil)
	}
	if input.Roles == nil || input.Capture == nil {
		return Profile{}, validation("invalid_network_profile_shape", "", nil)
	}
	roles := map[string]Role{}
	for _, roleName := range roleOrder {
		role, exists := input.Roles[roleName]
		if !exists {
			return Profile{}, validation("missing_network_role", "roles."+roleName, nil)
		}
		field := "roles." + roleName + ".interface"
		if !validInterfaceName(role.Interface) || role.Interface == "lo" {
			return Profile{}, validation("invalid_egress_interface", field, role.Interface)
		}
		if available != nil && !available[role.Interface] {
			return Profile{}, validation("interface_not_found", field, role.Interface)
		}
		gateway, err := normalizeIP(role.Gateway, "roles."+roleName+".gateway")
		if err != nil {
			return Profile{}, err
		}
		source, err := normalizeIP(role.Source, "roles."+roleName+".source")
		if err != nil {
			return Profile{}, err
		}
		if (gateway != nil && strings.Contains(*gateway, ":")) || (source != nil && strings.Contains(*source, ":")) {
			return Profile{}, validation("ipv6_egress_not_enabled", "roles."+roleName, nil)
		}
		roles[roleName] = Role{Interface: role.Interface, Gateway: gateway, Source: source}
	}

	captureInput := input.Capture
	mode := strings.ToLower(strings.TrimSpace(captureInput.Mode))
	if mode == "" {
		mode = "system"
	}
	if mode != "interfaces" && mode != "system" {
		return Profile{}, validation("invalid_capture_mode", "capture.mode", mode)
	}
	if len(captureInput.Interfaces) > 64 {
		return Profile{}, validation("invalid_capture_interfaces", "capture.interfaces", nil)
	}
	ingress := []string{}
	for _, raw := range captureInput.Interfaces {
		current := strings.TrimSpace(raw)
		if !validInterfaceName(current) || current == "lo" {
			return Profile{}, validation("invalid_capture_interface", "capture.interfaces", current)
		}
		if available != nil && !available[current] {
			return Profile{}, validation("interface_not_found", "capture.interfaces", current)
		}
		if !contains(ingress, current) {
			ingress = append(ingress, current)
		}
	}
	if mode == "interfaces" && len(ingress) == 0 {
		return Profile{}, validation("capture_interfaces_required", "capture.interfaces", nil)
	}
	if mode != "interfaces" && len(ingress) > 0 {
		return Profile{}, validation("capture_interfaces_only_for_interface_mode", "capture.interfaces", nil)
	}
	management, err := normalizeCIDRs(captureInput.ManagementCIDRs, "capture.management_cidrs")
	if err != nil {
		return Profile{}, err
	}
	if mode == "system" && len(management) == 0 {
		return Profile{}, validation("management_cidr_required_for_system_mode", "capture.management_cidrs", nil)
	}
	if mode == "system" {
		nonLoopback := false
		for _, cidr := range management {
			prefix, _ := netip.ParsePrefix(cidr)
			if !prefix.Addr().IsLoopback() {
				nonLoopback = true
			}
		}
		if !nonLoopback {
			return Profile{}, validation("non_loopback_management_cidr_required", "capture.management_cidrs", nil)
		}
	}
	bypass, err := normalizeCIDRs(captureInput.BypassCIDRs, "capture.bypass_cidrs")
	if err != nil {
		return Profile{}, err
	}
	dns, err := ValidateDNS(input.DNS)
	if err != nil {
		return Profile{}, err
	}
	return Profile{
		Version: 1, Revision: input.Revision, UpdatedAt: input.UpdatedAt, Roles: roles,
		Capture: Capture{
			Mode: mode, Interfaces: ingress, BypassLocal: boolDefault(captureInput.BypassLocal, true),
			BypassCIDRs: bypass, ManagementCIDRs: management,
			DNSHijack: boolDefault(captureInput.DNSHijack, true), StrictRoute: boolDefault(captureInput.StrictRoute, true),
		},
		DNS: dns,
	}, nil
}

func ValidateDNS(input *DNSInput) (DNSConfig, error) {
	if input == nil {
		copy := DefaultDNS
		copy.Direct = append([]string{}, DefaultDNS.Direct...)
		copy.Proxy = append([]string{}, DefaultDNS.Proxy...)
		copy.VPNUnderlay = append([]string{}, DefaultDNS.VPNUnderlay...)
		copy.Bootstrap = append([]string{}, DefaultDNS.Bootstrap...)
		return copy, nil
	}
	if len(input.UnknownFields) > 0 {
		return DNSConfig{}, validation("unknown_dns_field", "dns."+input.UnknownFields[0], nil)
	}
	channels := []struct {
		name            string
		given, fallback []string
	}{
		{"direct", input.Direct, DefaultDNS.Direct},
		{"proxy", input.Proxy, DefaultDNS.Proxy},
		{"vpn_underlay", input.VPNUnderlay, DefaultDNS.VPNUnderlay},
	}
	result := DNSConfig{}
	for _, channel := range channels {
		values := channel.given
		if values == nil {
			values = channel.fallback
		}
		if len(values) < 1 || len(values) > 8 {
			return DNSConfig{}, validation("invalid_dns_resolver_list", "dns."+channel.name, nil)
		}
		normalized := []string{}
		for index, value := range values {
			resolver, err := normalizeResolver(value, fmt.Sprintf("dns.%s[%d]", channel.name, index))
			if err != nil {
				return DNSConfig{}, err
			}
			if !contains(normalized, resolver) {
				normalized = append(normalized, resolver)
			}
		}
		switch channel.name {
		case "direct":
			result.Direct = normalized
		case "proxy":
			result.Proxy = normalized
		case "vpn_underlay":
			result.VPNUnderlay = normalized
		}
	}
	bootstrap := input.Bootstrap
	if bootstrap == nil {
		bootstrap = DefaultDNS.Bootstrap
	}
	if len(bootstrap) < 1 || len(bootstrap) > 8 {
		return DNSConfig{}, validation("invalid_dns_bootstrap", "dns.bootstrap", nil)
	}
	result.Bootstrap = []string{}
	for index, value := range bootstrap {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			return DNSConfig{}, validation("dns_bootstrap_requires_ip", fmt.Sprintf("dns.bootstrap[%d]", index), value)
		}
		if !address.Is4() {
			return DNSConfig{}, validation("ipv6_dns_not_enabled", fmt.Sprintf("dns.bootstrap[%d]", index), value)
		}
		if !contains(result.Bootstrap, address.String()) {
			result.Bootstrap = append(result.Bootstrap, address.String())
		}
	}
	result.CacheAlgorithm = strings.ToLower(input.CacheAlgorithm)
	if result.CacheAlgorithm == "" {
		result.CacheAlgorithm = DefaultDNS.CacheAlgorithm
	}
	if result.CacheAlgorithm != "lru" && result.CacheAlgorithm != "arc" {
		return DNSConfig{}, validation("invalid_dns_cache_algorithm", "dns.cache_algorithm", result.CacheAlgorithm)
	}
	result.PreferH3 = boolDefault(input.PreferH3, DefaultDNS.PreferH3)
	result.UseHosts = boolDefault(input.UseHosts, DefaultDNS.UseHosts)
	result.IPv6 = boolDefault(input.IPv6, DefaultDNS.IPv6)
	if result.IPv6 {
		return DNSConfig{}, validation("ipv6_dns_not_enabled", "dns.ipv6", true)
	}
	return result, nil
}

func PreviewDNS(config DNSConfig) DNSPreview {
	return DNSPreview{
		Config: config,
		Effective: DNSEffective{
			Direct: bindResolvers(config.Direct, "DIRECT-EGRESS"), Proxy: bindResolvers(config.Proxy, "ACTIVE"),
			VPNUnderlay: bindResolvers(config.VPNUnderlay, "VPN-UNDERLAY-DNS"),
			Bootstrap:   append([]string{}, config.Bootstrap...),
		},
	}
}

func PreviewProfile(input ProfileInput, topology Topology) (Preview, error) {
	available := map[string]bool{}
	interfaces := map[string]Interface{}
	for _, current := range topology.Interfaces {
		available[current.Name] = true
		interfaces[current.Name] = current
	}
	profile, err := ValidateProfile(input, available)
	if err != nil {
		return Preview{}, err
	}
	resolved := map[string]ResolvedRole{}
	warnings := []Warning{}
	for _, roleName := range roleOrder {
		role, err := resolveRole(roleName, profile.Roles[roleName], interfaces)
		if err != nil {
			return Preview{}, err
		}
		resolved[roleName] = role
		if role.State != "up" && role.State != "unknown" {
			warnings = append(warnings, Warning{Code: "interface_not_up", Role: roleName, Interface: role.Interface})
		}
	}
	if profile.Roles["direct"].Interface == profile.Roles["vpn_underlay"].Interface {
		warnings = append(warnings, Warning{Code: "shared_egress_interface", Interface: profile.Roles["direct"].Interface})
	}
	bypass := []string{}
	if profile.Capture.BypassLocal {
		bypass = append(bypass, topology.LocalCIDRs...)
	}
	for _, values := range [][]string{profile.Capture.BypassCIDRs, profile.Capture.ManagementCIDRs} {
		for _, value := range values {
			if !contains(bypass, value) {
				bypass = append(bypass, value)
			}
		}
	}
	dnsProtection := DNSProtection{
		Enabled: profile.Capture.DNSHijack,
		Scope:   "proxy_only", StrictRoute: profile.Capture.StrictRoute, Intercepts: []string{},
	}
	if profile.Capture.Mode == "interfaces" {
		dnsProtection.Scope = "lan"
	}
	if profile.Capture.Mode == "system" {
		dnsProtection.Scope = "system"
	}
	if dnsProtection.Enabled {
		dnsProtection.Intercepts = []string{"udp:53", "tcp:53"}
	}
	return Preview{
		Valid: true, Profile: profile, ResolvedRoles: resolved, EffectiveBypassCIDRs: bypass,
		Warnings: warnings, RequiresSystemConfirmation: true,
		DNS: PreviewDNS(profile.DNS), DNSProtection: dnsProtection, Digest: ProfileDigest(profile),
	}, nil
}

func ProfileDigest(profile Profile) string {
	material := map[string]any{}
	payload, _ := json.Marshal(profile)
	_ = json.Unmarshal(payload, &material)
	delete(material, "revision")
	delete(material, "updated_at")
	canonical, _ := json.Marshal(material)
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])[:16]
}

func resolveRole(name string, role Role, interfaces map[string]Interface) (ResolvedRole, error) {
	current, exists := interfaces[role.Interface]
	if !exists {
		return ResolvedRole{}, validation("interface_not_found", "roles."+name+".interface", role.Interface)
	}
	source := role.Source
	if source == nil {
		ipv4 := []Address{}
		global := []Address{}
		for _, address := range current.Addresses {
			if address.Family == "inet" {
				ipv4 = append(ipv4, address)
				if address.Scope == "global" {
					global = append(global, address)
				}
			}
		}
		candidates := global
		if len(candidates) == 0 {
			candidates = ipv4
		}
		if len(candidates) > 0 {
			prefix, err := netip.ParsePrefix(candidates[0].CIDR)
			if err == nil {
				value := prefix.Addr().String()
				source = &value
			}
		}
	}
	routes := append([]DefaultRoute{}, current.DefaultRoutes...)
	sort.SliceStable(routes, func(i, j int) bool {
		firstMain, secondMain := routes[i].Table == "main", routes[j].Table == "main"
		if firstMain != secondMain {
			return firstMain
		}
		return routes[i].Metric < routes[j].Metric
	})
	gateway := role.Gateway
	if gateway == nil && len(routes) > 0 {
		gateway = routes[0].Gateway
	}
	if gateway == nil && len(routes) == 0 && current.Kind != "wireguard" && current.Kind != "tun" {
		return ResolvedRole{}, validation("egress_default_route_not_found", "roles."+name+".gateway", role.Interface)
	}
	spec := roleSpecs[name]
	return ResolvedRole{
		Interface: role.Interface, Gateway: gateway, Source: source, Mark: spec.Mark,
		MarkHex: fmt.Sprintf("0x%x", spec.Mark), Table: spec.Table, Priority: spec.Priority,
		State: current.State, Kind: current.Kind,
	}, nil
}

func normalizeIP(value *string, field string) (*string, error) {
	if value == nil || *value == "" || *value == "auto" {
		return nil, nil
	}
	address, err := netip.ParseAddr(*value)
	if err != nil {
		return nil, validation("invalid_ip_address", field, *value)
	}
	normalized := address.String()
	return &normalized, nil
}

func normalizeCIDRs(values []string, field string) ([]string, error) {
	if len(values) > 1024 {
		return nil, validation("invalid_cidr_list", field, nil)
	}
	result := []string{}
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, validation("invalid_cidr", field, value)
		}
		normalized := prefix.Masked().String()
		if !contains(result, normalized) {
			result = append(result, normalized)
		}
	}
	return result, nil
}

func normalizeResolver(value, field string) (string, error) {
	resolver := strings.TrimSpace(value)
	if resolver == "" || len(resolver) > 512 || strings.Contains(resolver, "#") || hasWhitespace(resolver) {
		return "", validation("invalid_dns_resolver", field, truncate(resolver, 200))
	}
	if address, err := netip.ParseAddr(resolver); err == nil {
		if !address.Is4() {
			return "", validation("ipv6_dns_not_enabled", field, resolver)
		}
		return address.String(), nil
	}
	parsed, err := url.Parse(resolver)
	if err != nil || !contains([]string{"udp", "tcp", "tls", "https", "quic"}, strings.ToLower(parsed.Scheme)) ||
		parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", validation("invalid_dns_resolver", field, truncate(resolver, 200))
	}
	port := parsed.Port()
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", validation("invalid_dns_resolver", field, truncate(resolver, 200))
		}
	}
	hostname := strings.ToLower(parsed.Hostname())
	if address, err := netip.ParseAddr(hostname); err == nil && !address.Is4() {
		return "", validation("ipv6_dns_not_enabled", field, resolver)
	}
	if !hostnameRE.MatchString(hostname) || strings.Contains(hostname, "..") {
		return "", validation("invalid_dns_resolver", field, truncate(resolver, 200))
	}
	host := hostname
	if port != "" {
		host += ":" + port
	}
	return strings.ToLower(parsed.Scheme) + "://" + host + parsed.EscapedPath() + querySuffix(parsed.RawQuery), nil
}

func bindResolvers(values []string, outbound string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value + "#" + outbound
	}
	return result
}

func validation(code, field string, value any) error {
	return &ValidationError{Code: code, Field: field, Value: value}
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func validInterfaceName(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.RuneCountInString(value) <= 128 &&
		!strings.ContainsAny(value, "\x00\r\n\t/\\")
}
func hasWhitespace(value string) bool {
	for _, character := range value {
		if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			return true
		}
	}
	return false
}
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
func querySuffix(value string) string {
	if value == "" {
		return ""
	}
	return "?" + value
}
