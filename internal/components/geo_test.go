package components

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func encodedField(field int, value []byte) []byte {
	buffer := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buffer, uint64(field<<3|2))
	result := append([]byte{}, buffer[:n]...)
	n = binary.PutUvarint(buffer, uint64(len(value)))
	result = append(result, buffer[:n]...)
	return append(result, value...)
}

func TestGeoCatalog(t *testing.T) {
	payload := append(encodedField(1, encodedField(1, []byte("RU-BLOCKED"))), encodedField(1, encodedField(1, []byte("US")))...)
	path := filepath.Join(t.TempDir(), "geo.dat")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := GeoCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "ru-blocked" || values[1] != "us" {
		t.Fatalf("unexpected catalog: %#v", values)
	}
}

func TestResolveGeoSource(t *testing.T) {
	if source, err := ResolveGeoSource("runetfreedom", "", ""); err != nil || source.ID != "runetfreedom" {
		t.Fatalf("preset: %#v %v", source, err)
	}
	if _, err := ResolveGeoSource("custom", "http://example.com/geoip.dat", "https://example.com/geosite.dat"); err == nil {
		t.Fatal("insecure custom URL accepted")
	}
}
