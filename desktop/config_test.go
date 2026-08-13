package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.env")
	if err := os.WriteFile(path, []byte("# private\napi_token='secret'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := readEnvironment(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["api_token"] != "secret" {
		t.Fatalf("unexpected values: %#v", values)
	}
}
