package reversevpn

import (
	"fmt"
	"net/netip"
	"strings"
	"time"
)

func WireGuardServerConfig(config Config, outboundInterface string) (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	if config.PrivateKey == "" {
		return "", fmt.Errorf("server_private_key_missing")
	}
	if !interfaceNamePattern.MatchString(outboundInterface) {
		return "", fmt.Errorf("invalid_outbound_interface")
	}
	prefix, _ := netip.ParsePrefix(config.ServerCIDR)
	sourceCIDR := prefix.Masked().String()
	var result strings.Builder
	fmt.Fprintf(&result, "[Interface]\nAddress = %s\nListenPort = %d\nPrivateKey = %s\nMTU = %d\n", config.ServerCIDR, config.ListenPort, config.PrivateKey, config.MTU)
	fmt.Fprintf(&result, "PostUp = iptables -I FORWARD 1 -i %%i -j ACCEPT; iptables -I FORWARD 1 -o %%i -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT; iptables -t nat -I POSTROUTING 1 -s %s -o %s -j MASQUERADE\n", sourceCIDR, outboundInterface)
	fmt.Fprintf(&result, "PostDown = iptables -D FORWARD -i %%i -j ACCEPT; iptables -D FORWARD -o %%i -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT; iptables -t nat -D POSTROUTING -s %s -o %s -j MASQUERADE\n", sourceCIDR, outboundInterface)
	for _, client := range config.SortedClients() {
		if !client.AvailableAt(time.Now()) {
			continue
		}
		fmt.Fprintf(&result, "\n[Peer]\n# %s (%s)\nPublicKey = %s\nAllowedIPs = %s\n", client.Name, client.ID, client.PublicKey, client.Address)
	}
	return result.String(), nil
}
