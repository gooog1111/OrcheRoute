package callserver

// privateIPv4CIDRs are destinations excluded from subscriber egress by
// default (audit S5): RFC1918 private ranges, link-local space (which also
// covers cloud provider instance-metadata endpoints at 169.254.169.254),
// carrier-grade NAT shared space, and loopback. An administrator opts a
// deployment back into reaching these — typically the server's own LAN — by
// enabling AllowLAN; this list is not applied at all when it is set.
var privateIPv4CIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"100.64.0.0/10",
	"127.0.0.0/8",
}

// privateIPv6CIDRs mirrors privateIPv4CIDRs for the ordinary VLESS/Trojan/
// Hysteria2 listeners, which route IPv6 destinations directly. The packet
// (AWG) backend is IPv4-only today, so it only ever needs the v4 list.
var privateIPv6CIDRs = []string{
	"fc00::/7",
	"fe80::/10",
	"::1/128",
}
