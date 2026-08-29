package xray

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
)

type Client struct {
	ID    string
	Email string
}

type ServerInput struct {
	ListenAddress string
	ListenPort    int
	Clients       []Client
}

func ServerConfig(input ServerInput) ([]byte, error) {
	if input.ListenAddress == "" {
		input.ListenAddress = "127.0.0.1"
	}
	if net.ParseIP(input.ListenAddress) == nil || input.ListenPort < 1 || input.ListenPort > 65535 {
		return nil, fmt.Errorf("call_transport_xray_invalid_listen")
	}
	if len(input.Clients) == 0 {
		return nil, fmt.Errorf("call_transport_xray_clients_required")
	}
	clients := make([]any, 0, len(input.Clients))
	seen := map[string]bool{}
	for _, client := range input.Clients {
		id := strings.ToLower(strings.TrimSpace(client.ID))
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			return nil, fmt.Errorf("call_transport_xray_invalid_client")
		}
		seen[id] = true
		clients = append(clients, map[string]any{"id": id, "email": strings.TrimSpace(client.Email)})
	}
	config := map[string]any{
		"log":   map[string]any{"loglevel": "warning"},
		"stats": map[string]any{},
		"policy": map[string]any{"levels": map[string]any{
			"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true},
		}},
		"inbounds": []any{map[string]any{
			"tag": "orcheroute-call-in", "listen": input.ListenAddress, "port": input.ListenPort, "protocol": "vless",
			"settings":       map[string]any{"clients": clients, "decryption": "none"},
			"streamSettings": map[string]any{"network": "tcp", "security": "none"},
			"sniffing":       map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true},
		}},
		"outbounds": []any{
			map[string]any{"tag": "internet", "protocol": "freedom"},
			map[string]any{"tag": "blocked", "protocol": "blackhole"},
		},
	}
	result, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("call_transport_xray_marshal: %w", err)
	}
	return result, nil
}

type MihomoInput struct {
	Name          string
	LocalAddress  string
	LocalPort     int
	ClientID      string
	InterfaceName string
}

// MihomoProxy describes the local VLESS hop. Mihomo sends VLESS to the local
// call-carrier listener; the remote carrier forwards the same byte stream to
// Xray without terminating or rewriting VLESS.
func MihomoProxy(input MihomoInput) (map[string]any, error) {
	if input.LocalAddress == "" {
		input.LocalAddress = "127.0.0.1"
	}
	if net.ParseIP(input.LocalAddress) == nil || input.LocalPort < 1 || input.LocalPort > 65535 {
		return nil, fmt.Errorf("call_transport_xray_invalid_local_endpoint")
	}
	id := strings.ToLower(strings.TrimSpace(input.ClientID))
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("call_transport_xray_invalid_client")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "OrcheRoute Call"
	}
	proxy := map[string]any{
		"name": name, "type": "vless", "server": input.LocalAddress, "port": input.LocalPort,
		"uuid": id, "network": "tcp", "tls": false, "udp": true, "packet-encoding": "xudp",
	}
	if input.InterfaceName != "" {
		proxy["interface-name"] = input.InterfaceName
	}
	return proxy, nil
}
