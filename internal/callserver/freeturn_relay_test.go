package callserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
)

func TestWriteFreeTURNClientsUsesVLESSIdentityAndNoSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	snapshot := RuntimeSnapshot{Clients: []callxray.Client{{ID: "client-uuid", Email: "phone"}}, Keys: map[string][]byte{"client-uuid": []byte("secret-psk")}}
	if err := writeFreeTURNClients(path, snapshot); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) == "" || json.Valid(payload) == false {
		t.Fatalf("invalid clients file: %q", payload)
	}
	if strings.Contains(string(payload), "secret-psk") || !containsAll(string(payload), "client-uuid", "phone") {
		t.Fatalf("unexpected clients file: %s", payload)
	}
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("clients permissions = %v, %v", info, err)
	}
}

func containsAll(value string, values ...string) bool {
	for _, item := range values {
		if !strings.Contains(value, item) {
			return false
		}
	}
	return true
}
