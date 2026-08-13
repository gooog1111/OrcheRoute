package network

import "testing"

func boolPointer(value bool) *bool { return &value }

func testTopology() Topology {
	gateway := "192.0.2.1"
	return Topology{
		Interfaces: []Interface{
			{Name: "wan0", Kind: "ether", State: "up", Addresses: []Address{{Family: "inet", CIDR: "192.0.2.2/24", Scope: "global"}}, DefaultRoutes: []DefaultRoute{{Gateway: &gateway, Metric: 100, Table: "main"}}},
			{Name: "lan0", Kind: "ether", State: "up", Addresses: []Address{{Family: "inet", CIDR: "10.42.0.1/24", Scope: "global"}}, DefaultRoutes: []DefaultRoute{}},
		},
		LocalCIDRs: []string{"10.42.0.0/24"},
	}
}

func TestPreviewResolvesRoleAndDNS(t *testing.T) {
	profile := DefaultProfile("wan0")
	preview, err := PreviewProfile(profile, testTopology())
	if err != nil {
		t.Fatal(err)
	}
	if preview.ResolvedRoles["direct"].Gateway == nil || *preview.ResolvedRoles["direct"].Gateway != "192.0.2.1" {
		t.Fatalf("unexpected direct role: %#v", preview.ResolvedRoles["direct"])
	}
	if preview.DNS.Effective.Direct[0] != "1.1.1.1#DIRECT-EGRESS" {
		t.Fatalf("unexpected DNS: %#v", preview.DNS)
	}
}

func TestSystemModeRequiresManagementCIDR(t *testing.T) {
	profile := DefaultProfile("wan0")
	profile.Capture.Mode = "system"
	profile.Capture.ManagementCIDRs = nil
	_, err := ValidateProfile(profile, nil)
	validation, ok := err.(*ValidationError)
	if !ok || validation.Code != "management_cidr_required_for_system_mode" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestExplicitFalseValuesArePreserved(t *testing.T) {
	profile := DefaultProfile("wan0")
	profile.Capture.DNSHijack = boolPointer(false)
	profile.DNS.UseHosts = boolPointer(false)
	normalized, err := ValidateProfile(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Capture.DNSHijack || normalized.DNS.UseHosts {
		t.Fatalf("explicit false values were lost: %#v", normalized)
	}
}

func TestWindowsInterfaceNamesAreValid(t *testing.T) {
	for _, name := range []string{"Ethernet 2", "Беспроводная сеть"} {
		if !validInterfaceName(name) {
			t.Fatalf("Windows interface name rejected: %q", name)
		}
	}
	for _, name := range []string{"", " bad", "bad\\name", "bad\nname"} {
		if validInterfaceName(name) {
			t.Fatalf("unsafe interface name accepted: %q", name)
		}
	}
}
