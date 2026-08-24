// Package validator contains pure validation of platform-neutral mobile input.
// It does not parse share links, access storage, perform network I/O, or start
// the VPN transport.
package validator

import (
	"errors"

	"github.com/gooog1111/orcheroute/internal/core/qualification"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func QualificationPolicy(policy map[string]any) (map[string]any, error) {
	return qualification.Validate(policy)
}

func DefaultQualificationPolicy() map[string]any {
	return qualification.DefaultPolicy()
}

func MigrateQualificationPolicy(policy map[string]any) map[string]any {
	return qualification.MigrateLegacyPools(policy)
}

func UpdateQualificationPolicy(policy, changes map[string]any) (map[string]any, error) {
	return qualification.Update(policy, changes)
}

func QualificationURLTestURLs(policy map[string]any) ([]string, error) {
	return qualification.URLTestURLs(policy)
}

func EffectiveQualificationPolicy(policy map[string]any, pool string) (map[string]any, error) {
	return qualification.Effective(policy, pool)
}

func Subscription(payload map[string]any, partial bool) (map[string]any, error) {
	return subscriptions.ValidateFields(payload, partial)
}

func NetworkProfile(profile network.ProfileInput, topology network.Topology) (network.Preview, error) {
	return network.PreviewProfile(profile, topology)
}

func ValidateNetworkProfile(profile network.ProfileInput) (network.Profile, error) {
	return network.ValidateProfile(profile, nil)
}

func DNS(input network.DNSInput) (network.DNSPreview, error) {
	_, preview, err := DNSConfig(&input)
	return preview, err
}

func DNSConfig(input *network.DNSInput) (network.DNSConfig, network.DNSPreview, error) {
	config, err := network.ValidateDNS(input)
	if err != nil {
		return network.DNSConfig{}, network.DNSPreview{}, err
	}
	return config, network.PreviewDNS(config), nil
}

func PreviewDNSConfig(config network.DNSConfig) network.DNSPreview {
	return network.PreviewDNS(config)
}

func NetworkError(err error) any {
	var validation *network.ValidationError
	if errors.As(err, &validation) {
		return validation
	}
	return map[string]string{"error": err.Error()}
}
