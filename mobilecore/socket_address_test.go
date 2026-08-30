package mobilecore

import "testing"

func TestLoopbackSocketAddress(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:38979":    true,
		"[::1]:38979":        true,
		"localhost:38979":    true,
		"185.20.134.206:443": false,
		"10.42.0.1:443":      false,
		"invalid":            false,
	}
	for value, expected := range tests {
		if actual := isLoopbackSocketAddress(value); actual != expected {
			t.Fatalf("isLoopbackSocketAddress(%q) = %v, want %v", value, actual, expected)
		}
	}
}
