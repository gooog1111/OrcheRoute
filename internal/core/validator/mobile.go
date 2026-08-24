package validator

import (
	"fmt"
	"strings"
)

var mobileTransports = map[string]bool{
	"auto": true, "wifi": true, "cellular": true, "ethernet": true,
}

// MobileNetworkProfile validates the platform-neutral part of an Android/iOS
// VpnService profile. The native adapter owns interface discovery, but it must
// not maintain a second copy of profile or DNS validation rules.
func MobileNetworkProfile(profile map[string]any) error {
	roles, ok := object(profile["roles"])
	if !ok {
		return fmt.Errorf("invalid_network_profile")
	}
	capture, ok := object(profile["capture"])
	if !ok {
		return fmt.Errorf("invalid_network_profile")
	}
	dns, ok := object(profile["dns"])
	if !ok {
		return fmt.Errorf("invalid_network_profile")
	}
	for _, name := range []string{"direct", "vpn_underlay"} {
		role, exists := object(roles[name])
		if !exists || !mobileTransports[text(role["interface"], "auto")] {
			return fmt.Errorf("invalid_android_transport")
		}
	}
	if text(capture["mode"], "") != "system" {
		return fmt.Errorf("android_capture_must_be_system")
	}
	return MobileDNSProfile(dns)
}

func MobileDNSProfile(dns map[string]any) error {
	for _, name := range []string{"direct", "proxy", "vpn_underlay", "bootstrap"} {
		values, ok := list(dns[name])
		if !ok || len(values) == 0 {
			return fmt.Errorf("dns_%s_required", name)
		}
		for _, value := range values {
			if strings.TrimSpace(fmt.Sprint(value)) == "" {
				return fmt.Errorf("invalid_dns_value")
			}
		}
	}
	cache := text(dns["cache_algorithm"], "arc")
	if cache != "arc" && cache != "lru" {
		return fmt.Errorf("invalid_dns_cache")
	}
	return nil
}

func object(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func list(value any) ([]any, bool) {
	result, ok := value.([]any)
	return result, ok
}

func text(value any, fallback string) string {
	result, ok := value.(string)
	if !ok || strings.TrimSpace(result) == "" {
		return fallback
	}
	return strings.ToLower(strings.TrimSpace(result))
}
