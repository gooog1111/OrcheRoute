package xray

import (
	"encoding/json"
	"testing"
)

const testClientID = "11111111-1111-4111-8111-111111111111"

func TestServerConfigBuildsLoopbackVLESSInbound(t *testing.T) {
	data, err := ServerConfig(ServerInput{ListenPort: 32101, Clients: []Client{{ID: testClientID, Email: "phone"}}})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	inbound := config["inbounds"].([]any)[0].(map[string]any)
	if inbound["listen"] != "127.0.0.1" || inbound["protocol"] != "vless" || inbound["port"] != float64(32101) {
		t.Fatalf("unexpected inbound: %#v", inbound)
	}
	stream := inbound["streamSettings"].(map[string]any)
	if stream["security"] != "none" || stream["network"] != "tcp" {
		t.Fatalf("unexpected stream settings: %#v", stream)
	}
	level := config["policy"].(map[string]any)["levels"].(map[string]any)["0"].(map[string]any)
	if config["stats"] == nil || level["statsUserUplink"] != true || level["statsUserDownlink"] != true {
		t.Fatalf("per-client traffic accounting is disabled: %#v", config)
	}
}

func TestMihomoProxyTargetsOnlyLocalCarrier(t *testing.T) {
	proxy, err := MihomoProxy(MihomoInput{LocalPort: 32102, ClientID: testClientID, InterfaceName: "wlan0"})
	if err != nil {
		t.Fatal(err)
	}
	if proxy["server"] != "127.0.0.1" || proxy["port"] != 32102 || proxy["type"] != "vless" || proxy["interface-name"] != "wlan0" {
		t.Fatalf("unexpected proxy: %#v", proxy)
	}
	if proxy["tls"] != false || proxy["packet-encoding"] != "xudp" {
		t.Fatalf("unexpected VLESS settings: %#v", proxy)
	}
}

func TestVLESSConfigsRejectInvalidOrDuplicateClients(t *testing.T) {
	if _, err := MihomoProxy(MihomoInput{LocalPort: 1, ClientID: "not-a-uuid"}); err == nil {
		t.Fatal("accepted invalid client id")
	}
	if _, err := ServerConfig(ServerInput{ListenPort: 1, Clients: []Client{{ID: testClientID}, {ID: testClientID}}}); err == nil {
		t.Fatal("accepted duplicate client id")
	}
}
