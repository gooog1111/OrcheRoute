package nodes

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	callprofile "github.com/gooog1111/orcheroute/internal/calltransport/profile"
)

func TestSupportedProtocolLinks(t *testing.T) {
	vmessPayload, _ := json.Marshal(map[string]any{
		"v": "2", "ps": "VMess node", "add": "vmess.example", "port": "443",
		"id": "b831381d-6324-4d53-ad4f-8cda48b30811", "aid": "0", "scy": "auto",
		"net": "ws", "host": "cdn.example", "path": "/ws", "tls": "tls", "sni": "vmess.example",
	})
	ssCredentials := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:password"))
	links := []string{
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@vless.example:443?security=reality&type=grpc&sni=vless.example&fp=chrome&pbk=public-key&sid=01&serviceName=route#VLESS%20node",
		"trojan://secret@trojan.example:443?security=tls&type=ws&sni=trojan.example&path=%2Fsocket#Trojan%20node",
		"vmess://" + base64.RawURLEncoding.EncodeToString(vmessPayload),
		"ss://" + ssCredentials + "@ss.example:8388#SS%20node",
		"hy2://secret@hy2.example:443?sni=edge.example&insecure=1&obfs=salamander&obfs-password=mask&alpn=h3%2Ch2#Hysteria2%20node",
	}
	result := ConvertLinks(links, "mobile")
	if len(result.Errors) != 0 || len(result.Proxies) != 5 {
		t.Fatalf("unexpected conversion: %#v", result)
	}
	expectedTypes := []string{"vless", "trojan", "vmess", "ss", "hysteria2"}
	for index, proxy := range result.Proxies {
		if proxy["type"] != expectedTypes[index] {
			t.Fatalf("unexpected proxy %d: %#v", index, proxy)
		}
	}
}

func TestVLESSGRPCCustomPathKeepsLeadingSlash(t *testing.T) {
	proxy, err := ParseLink("vless://00000000-0000-0000-0000-000000000001@192.0.2.1:443?type=grpc&security=reality&serviceName=%2Fapi%2Fv1%2Fstream&sni=example.com&pbk=test#custom", "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	opts, ok := proxy["grpc-opts"].(map[string]any)
	if !ok || opts["grpc-service-name"] != "/api/v1/stream" {
		t.Fatalf("custom gRPC path was changed: %#v", proxy["grpc-opts"])
	}
}

func TestHysteria2Fields(t *testing.T) {
	proxy, err := ParseLink("hysteria2://password@example.com:8443?sni=cdn.example&obfs=salamander&obfs-password=secret&insecure=true", "test", 1)
	if err != nil || proxy["type"] != "hysteria2" || proxy["sni"] != "cdn.example" || proxy["skip-cert-verify"] != true {
		t.Fatalf("unexpected hysteria2 proxy: %#v, %v", proxy, err)
	}
}

func TestFreeTURNProfileBecomesCanonicalNode(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	profile, err := callprofile.New(callprofile.NewInput{
		Name:          "Call test",
		InvitationURL: "https://vk.com/call/join/mJm6labHtIX06vO54fNKJ0B4TBQI1fsK8jHIND9GBq8",
		PeerAddress:   "192.0.2.1:4443",
		Random:        strings.NewReader(strings.Repeat("a", 64)),
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	link, err := callprofile.Encode(profile, now)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := ParseLink(link, "mobile", 1)
	if err != nil {
		t.Fatal(err)
	}
	if proxy["type"] != "freeturn" || proxy["server"] != "192.0.2.1" || proxy["port"] != 4443 || proxy["profile"] != link {
		t.Fatalf("unexpected freeturn node: %#v", proxy)
	}
	if _, exposed := proxy["psk"]; exposed {
		t.Fatal("FreeTURN legacy PSK was exposed as a node field")
	}
}

func TestWireGuardAndAmneziaConfig(t *testing.T) {
	config := `[Interface]
Address = 10.8.0.2/32, fd00::2/128
PrivateKey = private-key
DNS = 1.1.1.1, 2606:4700:4700::1111
MTU = 1420
Jc = 5
Jmin = 40
Jmax = 70
S1 = 30
S2 = 40
H1 = 1234-1240

[Peer]
PublicKey = public-key
PresharedKey = shared-key
Endpoint = wg.example:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25`
	link := "wireguard://" + base64.RawURLEncoding.EncodeToString([]byte(config))
	proxy, err := ParseLink(link, "test", 1)
	if err != nil || proxy["type"] != "wireguard" || proxy["ip"] != "10.8.0.2" || proxy["ipv6"] != "fd00::2" {
		t.Fatalf("unexpected wireguard proxy: %#v, %v", proxy, err)
	}
	options, ok := proxy["amnezia-wg-option"].(map[string]any)
	if !ok || options["jc"] != 5 || options["h1"] != "1234-1240" {
		t.Fatalf("unexpected amnezia options: %#v", proxy["amnezia-wg-option"])
	}
	peers, ok := proxy["peers"].([]map[string]any)
	if !ok || len(peers) != 1 || peers[0]["server"] != "wg.example" || peers[0]["port"] != 51820 {
		t.Fatalf("unexpected wireguard peers: %#v", proxy["peers"])
	}
}

func TestRealityRequiresPublicKey(t *testing.T) {
	_, err := ParseLink("vless://id@example.com:443?security=reality", "test", 1)
	if err == nil || err.Error() != "reality public key missing" {
		t.Fatalf("unexpected error: %v", err)
	}
}
