package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebianPackageStartsControlPlaneWithoutOptionalNftables(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "packaging", "debian", "control"))
	if err != nil {
		t.Fatal(err)
	}
	control := string(payload)
	for _, line := range strings.Split(control, "\n") {
		if strings.HasPrefix(line, "Depends:") && strings.Contains(line, "nftables") {
			t.Fatalf("nftables must not prevent clean WebUI configuration: %s", line)
		}
	}
	if !strings.Contains(control, "Recommends: nftables") {
		t.Fatal("nftables must remain a recommended VPN routing dependency")
	}
	for _, obsolete := range []string{"wireguard-tools", "iptables"} {
		if strings.Contains(control, obsolete) {
			t.Fatalf("embedded Xray call server must not require legacy %s", obsolete)
		}
	}
}

func TestDebianRemovePreservesStateAndPurgeDeletesIt(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "packaging", "debian", "postrm"))
	if err != nil {
		t.Fatal(err)
	}
	postrm := string(payload)
	if !strings.Contains(postrm, `if [ "$1" = purge ]; then`) {
		t.Fatal("postrm does not distinguish purge from remove")
	}
	for _, path := range []string{"/etc/orcheroute", "/var/lib/orcheroute", "/var/backups/orcheroute"} {
		if !strings.Contains(postrm, path) {
			t.Fatalf("purge does not remove %s", path)
		}
	}
	if strings.Index(postrm, `if [ "$1" = purge ]; then`) > strings.Index(postrm, "rm -rf -- /etc/orcheroute") {
		t.Fatal("persistent state removal is not guarded by the purge branch")
	}
}

func TestDebianMaintainerScriptsUseUnixLineEndings(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"preinst", "postinst", "prerm", "postrm"} {
		payload, err := os.ReadFile(filepath.Join(root, "packaging", "debian", name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "\r") {
			t.Fatalf("Debian maintainer script %s contains CRLF line endings", name)
		}
	}
}

func TestDebianPackageContainsLicenseMetadata(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	copyright, err := os.ReadFile(filepath.Join(root, "packaging", "debian", "copyright"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(copyright), "License: GPL-3.0") {
		t.Fatal("Debian copyright does not declare GPLv3")
	}
	if !strings.Contains(string(copyright), "Xray-core contributors") || !strings.Contains(string(copyright), "MPL-2.0") {
		t.Fatal("Debian copyright does not declare embedded Xray Core and MPL-2.0")
	}
	script, err := os.ReadFile(filepath.Join(root, "scripts", "package-linux-server.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"copyright", "THIRD_PARTY_NOTICES.md"} {
		if !strings.Contains(string(script), name) {
			t.Fatalf("package script does not include %s", name)
		}
	}
}
