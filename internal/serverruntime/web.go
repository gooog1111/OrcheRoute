package serverruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"

	"github.com/gooog1111/orcheroute/internal/network"
)

func (runtime *Runtime) WebHandler() http.Handler {
	files := http.FileServer(http.Dir(runtime.Config.WebRoot))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setWebHeaders(writer)
		if strings.HasPrefix(request.URL.Path, "/subscription/reverse/") {
			runtime.reverseVPNSubscription(writer, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/subscription/call/") {
			runtime.callServerSubscription(writer, request)
			return
		}
		if !runtime.managementAllowed(request.RemoteAddr) {
			writeJSON(writer, 403, map[string]any{"error": "management_network_required"})
			return
		}
		if !runtime.validBasic(request.Header.Get("Authorization")) {
			writer.Header().Set("WWW-Authenticate", `Basic realm="OrcheRoute", charset="UTF-8"`)
			writeJSON(writer, 401, map[string]any{"error": "authentication_required"})
			return
		}
		if request.URL.Path == "/api/v1/web/access" {
			if request.Method == http.MethodGet {
				runtime.webAccess(writer)
				return
			}
			if request.Method == http.MethodPut {
				runtime.updateWebAccess(writer, request)
				return
			}
			writeJSON(writer, 405, map[string]any{"error": "method_not_allowed"})
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/") {
			runtime.proxyAPI(writer, request)
			return
		}
		path := filepath.Join(runtime.Config.WebRoot, filepath.Clean("/"+request.URL.Path))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			request.URL.Path = "/"
		}
		files.ServeHTTP(writer, request)
	})
}

func (runtime *Runtime) callServerSubscription(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	if runtime.CallServer == nil {
		writeJSON(writer, 503, map[string]any{"error": "call_server_unavailable"})
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(request.URL.Path, "/subscription/call/"))
	profile, client, err := runtime.CallServer.SubscriptionProfile(token)
	if err != nil {
		status := http.StatusNotFound
		if err.Error() == "call_server_subscription_inactive" {
			status = http.StatusGone
		}
		writeJSON(writer, status, map[string]any{"error": err.Error()})
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Content-Disposition", `inline; filename="orcheroute-call.txt"`)
	writer.Header().Set("Profile-Title", client.Name)
	writer.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", client.TrafficRXBytes, client.TrafficTXBytes, client.TrafficLimitBytes, client.ExpiresAt))
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, profile)
}

func (runtime *Runtime) reverseVPNSubscription(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	if runtime.ReverseVPN == nil {
		writeJSON(writer, 503, map[string]any{"error": "reverse_vpn_unavailable"})
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(request.URL.Path, "/subscription/reverse/"))
	profile, client, err := runtime.ReverseVPN.SubscriptionProfile(token)
	if err != nil {
		status := http.StatusNotFound
		if err.Error() == "subscription_inactive" {
			status = http.StatusGone
		}
		writeJSON(writer, status, map[string]any{"error": err.Error()})
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Content-Disposition", `inline; filename="orcheroute-wireguard.conf"`)
	writer.Header().Set("Profile-Title", client.Name)
	writer.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", client.TrafficRXBytes, client.TrafficTXBytes, client.TrafficLimitBytes, client.ExpiresAt))
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, profile)
}

func (runtime *Runtime) validBasic(header string) bool {
	values, err := readEnv(runtime.webEnvironment())
	if err != nil {
		return false
	}
	username, encodedHash := values["webui_username"], values["webui_password_hash"]
	if username == "" || encodedHash == "" {
		return false
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		return false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return false
	}
	credentials := strings.SplitN(string(decoded), ":", 2)
	if len(credentials) != 2 {
		return false
	}
	hashParts := strings.Split(encodedHash, "$")
	if len(hashParts) != 4 || hashParts[0] != "pbkdf2_sha256" {
		return false
	}
	iterations, err := strconv.Atoi(hashParts[1])
	if err != nil {
		return false
	}
	salt, err := hex.DecodeString(hashParts[2])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(hashParts[3])
	if err != nil {
		return false
	}
	actual := pbkdf2.Key([]byte(credentials[1]), salt, iterations, len(expected), sha256.New)
	return subtle.ConstantTimeCompare([]byte(credentials[0]), []byte(username)) == 1 && subtle.ConstantTimeCompare(actual, expected) == 1
}

func (runtime *Runtime) proxyAPI(writer http.ResponseWriter, request *http.Request) {
	request.URL.Scheme = "http"
	request.URL.Host = runtime.Config.Listen
	request.URL.Path = strings.TrimPrefix(request.URL.Path, "/api")
	request.RequestURI = ""
	request.Host = runtime.Config.Listen
	request.Header.Set("Authorization", "Bearer "+runtime.apiToken)
	response, err := runtime.client.Do(request)
	if err != nil {
		writeJSON(writer, 502, map[string]any{"error": "go_api_unavailable"})
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}

func (runtime *Runtime) webAccess(writer http.ResponseWriter) {
	values, _ := readEnv(runtime.webEnvironment())
	cidrs := runtime.managementCIDRs()
	addresses := []map[string]any{}
	if topology, err := discoverTopology(requestContext()); err == nil {
		for _, item := range topology.Interfaces {
			for _, address := range item.Addresses {
				prefix, err := netip.ParsePrefix(address.CIDR)
				if err != nil || (!prefix.Addr().IsLoopback() && !addressAllowed(prefix.Addr(), cidrs)) {
					continue
				}
				host := prefix.Addr().String()
				if prefix.Addr().Is6() {
					host = "[" + host + "]"
				}
				entry := map[string]any{"interface": item.Name, "address": prefix.Addr().String(), "cidr": address.CIDR, "scope": address.Scope, "http_url": "http://" + host + ":19110/", "https_url": nil, "certificate_matches": false}
				if runtime.Config.WebTLSListen != "" {
					entry["https_url"] = "https://" + host + ":19111/"
				}
				addresses = append(addresses, entry)
			}
		}
	}
	_, _, tlsEnabled := runtime.WebTLSSettings()
	writeJSON(writer, 200, map[string]any{"username": values["webui_username"], "management_cidrs": cidrs, "addresses": addresses, "https": map[string]any{"mode": valueOr(values, "webui_tls_mode", "disabled"), "enabled": tlsEnabled, "canonical_url": nil, "certificate_name": values["webui_tls_name"], "cert_path": values["webui_tls_cert"], "key_path": values["webui_tls_key"], "local_ca_available": false, "ca_download_url": nil, "error": nil}, "runtime": "go"})
}

func requestContext() context.Context { return context.Background() }

func (runtime *Runtime) managementCIDRs() []string {
	profile := network.Profile{}
	if readJSON(filepath.Join(runtime.Config.StateDirectory, "network-active.json"), &profile) == nil && len(profile.Capture.ManagementCIDRs) > 0 {
		return profile.Capture.ManagementCIDRs
	}
	return []string{"127.0.0.0/8", "::1/128"}
}

func addressAllowed(address netip.Addr, cidrs []string) bool {
	for _, raw := range cidrs {
		if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (runtime *Runtime) managementAllowed(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && (address.IsLoopback() || addressAllowed(address, runtime.managementCIDRs()))
}

func (runtime *Runtime) WebTLSSettings() (string, string, bool) {
	values, err := readEnv(runtime.webEnvironment())
	if err != nil || valueOr(values, "webui_tls_mode", "disabled") == "disabled" {
		return "", "", false
	}
	certificate, key := values["webui_tls_cert"], values["webui_tls_key"]
	if certificate == "" || key == "" {
		return "", "", false
	}
	if _, err := os.Stat(certificate); err != nil {
		return "", "", false
	}
	if _, err := os.Stat(key); err != nil {
		return "", "", false
	}
	return certificate, key, true
}

func setWebHeaders(writer http.ResponseWriter) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "SAMEORIGIN")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; font-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'self'")
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,64}$`)

func (runtime *Runtime) webEnvironment() string {
	return runtime.Config.RuntimeEnv
}
func valueOr(values map[string]string, key, fallback string) string {
	if value := values[key]; value != "" {
		return value
	}
	return fallback
}

func (runtime *Runtime) updateWebAccess(writer http.ResponseWriter, request *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 65536)).Decode(&payload); err != nil {
		writeJSON(writer, 400, map[string]any{"error": "invalid_json"})
		return
	}
	updates := map[string]string{}
	response := map[string]any{"updated": true, "system_mutated": true}
	switch stringValue(payload["section"]) {
	case "credentials":
		username, password := strings.TrimSpace(stringValue(payload["username"])), stringValue(payload["password"])
		if !usernamePattern.MatchString(username) {
			writeJSON(writer, 400, map[string]any{"error": "invalid_webui_username"})
			return
		}
		if len(password) < 12 || len(password) > 256 {
			writeJSON(writer, 400, map[string]any{"error": "invalid_webui_password"})
			return
		}
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			writeJSON(writer, 503, map[string]any{"error": "random_source_failed"})
			return
		}
		iterations := 310000
		digest := pbkdf2.Key([]byte(password), salt, iterations, 32, sha256.New)
		updates["webui_username"], updates["webui_password_hash"] = username, "pbkdf2_sha256$"+strconv.Itoa(iterations)+"$"+hex.EncodeToString(salt)+"$"+hex.EncodeToString(digest)
		response["username"], response["reauthentication_required"] = username, true
	case "tls":
		mode := strings.ToLower(strings.TrimSpace(stringValue(payload["mode"])))
		if mode != "auto" && mode != "custom" && mode != "disabled" {
			writeJSON(writer, 400, map[string]any{"error": "invalid_tls_mode"})
			return
		}
		updates["webui_tls_mode"] = mode
		if mode == "custom" {
			cert, key := strings.TrimSpace(stringValue(payload["cert_path"])), strings.TrimSpace(stringValue(payload["key_path"]))
			if !filepath.IsAbs(cert) || !filepath.IsAbs(key) {
				writeJSON(writer, 400, map[string]any{"error": "invalid_custom_tls_paths"})
				return
			}
			if _, err := os.Stat(cert); err != nil {
				writeJSON(writer, 400, map[string]any{"error": "custom_certificate_not_found"})
				return
			}
			if _, err := os.Stat(key); err != nil {
				writeJSON(writer, 400, map[string]any{"error": "custom_certificate_not_found"})
				return
			}
			updates["webui_tls_cert"], updates["webui_tls_key"], updates["webui_tls_name"] = cert, key, strings.TrimSpace(stringValue(payload["certificate_name"]))
		}
		response["restart_required"] = true
	default:
		writeJSON(writer, 400, map[string]any{"error": "unknown_web_access_section"})
		return
	}
	if err := updateEnv(runtime.webEnvironment(), updates); err != nil {
		writeJSON(writer, 503, map[string]any{"error": "backend_unavailable", "message": err.Error()})
		return
	}
	writeJSON(writer, 200, response)
}

func updateEnv(path string, updates map[string]string) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	keys := map[string]bool{}
	for key := range updates {
		keys[strings.ToLower(key)] = true
	}
	lines := []string{}
	for _, line := range strings.Split(strings.TrimRight(string(payload), "\r\n"), "\n") {
		key := strings.ToLower(strings.TrimSpace(strings.SplitN(line, "=", 2)[0]))
		if !keys[key] {
			lines = append(lines, line)
		}
	}
	names := make([]string, 0, len(updates))
	for key := range updates {
		names = append(names, key)
	}
	sort.Strings(names)
	for _, key := range names {
		lines = append(lines, strings.ToUpper(key)+"="+updates[key])
	}
	return atomicWrite(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}
