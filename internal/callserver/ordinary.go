package callserver

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
)

type OrdinaryClient struct {
	ID               string
	Name             string
	VLESSUUID        string
	TrojanPassword   string
	HysteriaPassword string
}

type OrdinarySnapshot struct {
	Enabled               bool
	VLESSListenAddress    string
	TrojanListenAddress   string
	HysteriaListenAddress string
	FakeSNI               string
	RealityPrivateKey     string
	RealityShortID        string
	TLSCertificate        string
	TLSPrivateKey         string
	Clients               []OrdinaryClient
	ControllerAddress     string
	ControllerSecret      string
	AllowLAN              bool
}

func ordinaryMihomoConfig(snapshot OrdinarySnapshot) ([]byte, error) {
	if !snapshot.Enabled {
		return nil, nil
	}
	if len(snapshot.Clients) == 0 {
		return nil, fmt.Errorf("call_server_no_active_clients")
	}
	vlessHost, vlessPort, err := splitListener(snapshot.VLESSListenAddress)
	if err != nil {
		return nil, err
	}
	trojanHost, trojanPort, err := splitListener(snapshot.TrojanListenAddress)
	if err != nil {
		return nil, err
	}
	hy2Host, hy2Port, err := splitListener(snapshot.HysteriaListenAddress)
	if err != nil {
		return nil, err
	}
	vlessUsers, trojanUsers, hysteriaUsers := []any{}, []any{}, map[string]string{}
	for _, client := range snapshot.Clients {
		vlessUsers = append(vlessUsers, map[string]any{"username": client.ID, "uuid": client.VLESSUUID})
		trojanUsers = append(trojanUsers, map[string]any{"username": client.ID, "password": client.TrojanPassword})
		hysteriaUsers[client.ID] = client.HysteriaPassword
	}
	reality := map[string]any{"dest": snapshot.FakeSNI + ":443", "private-key": snapshot.RealityPrivateKey,
		"short-id": []string{snapshot.RealityShortID}, "server-names": []string{snapshot.FakeSNI}}
	listeners := []any{
		map[string]any{"name": "orcheroute-vless", "type": "vless", "listen": vlessHost, "port": vlessPort,
			"users": vlessUsers, "reality-config": reality},
		map[string]any{"name": "orcheroute-trojan", "type": "trojan", "listen": trojanHost, "port": trojanPort,
			"users": trojanUsers, "certificate": snapshot.TLSCertificate, "private-key": snapshot.TLSPrivateKey},
		map[string]any{"name": "orcheroute-hysteria2", "type": "hysteria2", "listen": hy2Host, "port": hy2Port,
			"users": hysteriaUsers, "certificate": snapshot.TLSCertificate, "private-key": snapshot.TLSPrivateKey,
			"alpn": []string{"h3"}, "masquerade": "https://" + snapshot.FakeSNI},
	}
	config := map[string]any{"mode": "rule", "log-level": "warning", "ipv6": true,
		"listeners": listeners, "rules": ordinaryEgressRules(snapshot.AllowLAN)}
	if snapshot.ControllerAddress != "" {
		config["external-controller"] = snapshot.ControllerAddress
		config["secret"] = snapshot.ControllerSecret
	}
	return json.MarshalIndent(config, "", "  ")
}

// ordinaryEgressRules implements S5 for the VLESS/Trojan/Hysteria2 sidecar:
// subscribers reach the Internet only, not the server's own LAN, unless the
// administrator explicitly opts back in. REJECT-DROP silently discards the
// packet instead of returning a TCP RST, so a scanning client cannot use the
// response to distinguish "filtered" from "genuinely unreachable".
func ordinaryEgressRules(allowLAN bool) []string {
	if allowLAN {
		return []string{"MATCH,DIRECT"}
	}
	rules := make([]string, 0, len(privateIPv4CIDRs)+len(privateIPv6CIDRs)+1)
	for _, cidr := range privateIPv4CIDRs {
		rules = append(rules, "IP-CIDR,"+cidr+",REJECT-DROP")
	}
	for _, cidr := range privateIPv6CIDRs {
		rules = append(rules, "IP-CIDR6,"+cidr+",REJECT-DROP")
	}
	return append(rules, "MATCH,DIRECT")
}

func splitListener(endpoint string) (string, int, error) {
	host, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0, fmt.Errorf("call_server_invalid_protocol_address")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("call_server_invalid_protocol_address")
	}
	return host, port, nil
}
