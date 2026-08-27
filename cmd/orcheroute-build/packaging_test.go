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
