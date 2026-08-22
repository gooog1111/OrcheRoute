package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEnvNormalizesExistingUppercaseCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.env")
	payload := "WEBUI_USERNAME=ivan\nWEBUI_PASSWORD_HASH=existing-hash\nAPI_TOKEN=token\n"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	values := readEnv(path)
	if values["webui_username"] != "ivan" || values["webui_password_hash"] != "existing-hash" || values["api_token"] != "token" {
		t.Fatalf("uppercase values were not preserved: %#v", values)
	}
}
