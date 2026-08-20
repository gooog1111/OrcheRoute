// Package validator contains pure validation of platform-neutral mobile input.
// It does not parse share links, access storage, perform network I/O, or start
// the VPN transport.
package validator

import (
	"errors"

	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

func QualificationPolicy(policy map[string]any) (map[string]any, error) {
	return qualification.Validate(policy)
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

func DNS(input network.DNSInput) (network.DNSPreview, error) {
	config, err := network.ValidateDNS(&input)
	if err != nil {
		return network.DNSPreview{}, err
	}
	return network.PreviewDNS(config), nil
}

func NetworkError(err error) any {
	var validation *network.ValidationError
	if errors.As(err, &validation) {
		return validation
	}
	return map[string]string{"error": err.Error()}
}
