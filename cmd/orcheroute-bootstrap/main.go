package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

func main() {
	output := flag.String("output", "/etc/orcheroute/runtime.env", "runtime environment file")
	username := flag.String("username", "admin", "initial WebUI username")
	flag.Parse()
	values := readEnv(*output)
	if values["api_token"] == "" {
		values["api_token"] = randomHex(32)
	}
	if values["controller_secret"] == "" {
		values["controller_secret"] = randomHex(32)
	}
	password := ""
	if values["webui_username"] == "" || values["webui_password_hash"] == "" {
		password = randomHex(12)
		values["webui_username"] = *username
		salt := randomBytes(16)
		digest := pbkdf2.Key([]byte(password), salt, 310000, 32, sha256.New)
		values["webui_password_hash"] = "pbkdf2_sha256$310000$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(digest)
	}
	if values["webui_tls_mode"] == "" {
		values["webui_tls_mode"] = "disabled"
	}
	if err := writeEnv(*output, values); err != nil {
		panic(err)
	}
	if password != "" {
		fmt.Printf("username=%s\npassword=%s\n", values["webui_username"], password)
	}
}

func randomBytes(size int) []byte {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return value
}

func randomHex(size int) string { return hex.EncodeToString(randomBytes(size)) }

func readEnv(path string) map[string]string {
	result := map[string]string{}
	payload, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	for _, raw := range strings.Split(string(payload), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		result[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
	}
	return result
}

func writeEnv(path string, values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values[key])
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(strings.Join(lines, "\n")+"\n"), 0o640); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
