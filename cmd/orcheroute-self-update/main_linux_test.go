//go:build linux

package main

import "testing"

func TestRollbackAssetURL(t *testing.T) {
	tests := []struct {
		version string
		beta    bool
		want    string
	}{
		{"0.5.11-beta.7", true, "https://github.com/gooog1111/OrcheRoute/releases/download/server-beta/OrcheRoute-Linux-Server-0.5.11-beta.7-amd64.deb"},
		{"0.5.12", false, "https://github.com/gooog1111/OrcheRoute/releases/download/v0.5.12/OrcheRoute-Linux-Server-0.5.12-amd64.deb"},
	}
	for _, test := range tests {
		if got := rollbackAssetURL(test.version, test.beta); got != test.want {
			t.Fatalf("rollbackAssetURL(%q, %t)=%q want %q", test.version, test.beta, got, test.want)
		}
	}
}
