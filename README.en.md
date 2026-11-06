# OrcheRoute

[Русский](README.md)

An Android VPN client and Linux Server VPN controller built on [Mihomo](https://github.com/MetaCubeX/mihomo), with subscriptions, server qualification, DNS, and `direct` / `proxy` / `block` routing.

## Downloads

- [Android — arm64 APK](https://github.com/gooog1111/OrcheRoute/releases/download/v0.8.3/OrcheRoute-Android-0.8.3-code93-arm64.apk)
- [Linux Server — amd64 DEB](https://github.com/gooog1111/OrcheRoute/releases/download/v0.8.5/OrcheRoute-Linux-Server-0.8.5-amd64.deb)
- [All releases](https://github.com/gooog1111/OrcheRoute/releases)

## Android

Requires Android 8.0 or later, without root.

1. Install the APK and grant permission to create a VPN connection.
2. Add a subscription or import a file, link, or QR code. Enable built-in emergency sources if needed.
3. Refresh and check an individual subscription or the full list.
4. Select a server manually and enable the VPN.

The server list is grouped by subscription. Your manually selected server stays pinned. The notification provides Restart and Disable actions.

For FreeTURN, confirm the VK CAPTCHA when prompted. Drag the window by its header to move it.

Use Updates to install new versions. Enable Beta versions for the test channel, or disable it for stable updates.

Allow notifications and background operation. Subscription export is available in Settings.

## Linux Server

Required packages: `ca-certificates`, `iproute2`, `systemd`, `nftables`, `iptables`, `curl`, `tar`, `gzip`, `dpkg`, `apt`, and one time service: `systemd-timesyncd`, `chrony`, `ntpsec`, or `ntp`. APT installs dependencies with the DEB.

Install on Debian/Ubuntu:

```bash
sudo apt install ./OrcheRoute-Linux-Server-0.8.5-amd64.deb
```

Open `http://127.0.0.1:19110/` on the server, or `http://SERVER_ADDRESS:19110/` from another device. The installer prints initial credentials and saves them to `/etc/orcheroute/initial-credentials`. Configure HTTPS in Access settings for external access.

```bash
systemctl status orcheroute-go --no-pager
```

1. In Network and DNS, select the interfaces for direct access, VPN-server connections, and traffic capture.
2. Add subscriptions and configure their refresh intervals.
3. Check an individual subscription or the full server list.
4. Select manual or automatic control and enable the VPN.

Scheduled refreshes download subscriptions; server qualification is a separate operation.

Automatic mode uses the primary list, followed by emergency servers. Restricted networks use a separate qualified list. Selection considers rating and connection stability. Manual selections remain pinned.

### DNS and time

DNS settings are separate for Direct, Proxy, VPN-server resolution, and Bootstrap. The network monitor and Direct IP lookup use DNS from the selected physical interface, with Bootstrap as the fallback. Interface DNS changes are picked up automatically.

The dashboard shows the server's date, time, and timezone. Select Synchronize time in Updates to enable system NTP synchronization. Its status is displayed alongside the clock.

### Qualification

Qualification includes TCP, URL checks, the actual exit region, and speed tests. Ratings use latency, speed, and stability. Checking one subscription preserves the ratings of other subscriptions.

Speed providers: Cachefly, Cloudflare, and OVH. When measurement is unavailable, a repeated URL check confirms connectivity and the speed remains unknown.

The Qualification section offers timeouts, test URLs, and filters. Linux Server also provides an optional target-opening filter:

- disabled;
- admit a server if the target opens;
- reject a server if the target opens.

The target, such as `youtube.com`, is requested through each candidate VPN after the ordinary URL test. HTTP `2xx` counts as successful opening.

### VPN server

Create personal subscriptions containing FreeTURN, VLESS Reality, Trojan TLS, and Hysteria2. Each subscription can have an expiry, traffic limit, and LAN permission: inherit the global setting, allow, or deny.

Default ports: FreeTURN UDP `4443`, VLESS TCP `24443`, Trojan TCP `24444`, Hysteria2 UDP `24445`. Open the required ports in your firewall. The public endpoint accepts an IP address or hostname; configure the HTTPS subscription address separately. Default SNI: `m.vk.ru`.

### Updates and removal

Update from the WebUI or install the new DEB over the existing package. Authentication, subscriptions, and settings are preserved.

```bash
sudo apt remove orcheroute  # keep settings and authentication
sudo apt purge orcheroute   # delete settings, state, and backups
```

## Routing and GEO data

Rules support IP addresses, CIDRs, address ranges, domains, GeoIP/GeoSite, protocols, and ports. Choose `direct`, `proxy`, or `block` for each rule.

Select a GeoIP/GeoSite source and update schedule in Settings:

- [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat): [GeoIP](https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat), [GeoSite](https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat).
- [RunetFreedom/russia-v2ray-rules-dat](https://github.com/runetfreedom/russia-v2ray-rules-dat): [GeoIP](https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geoip.dat), [GeoSite](https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat).
- [Loyalsoldier/v2ray-rules-dat](https://github.com/Loyalsoldier/v2ray-rules-dat): [GeoIP](https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat), [GeoSite](https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat).

Custom HTTPS links to compatible databases are also supported.

## Appearance

Choose a theme in Appearance settings. Display preferences are saved on the device.

## Sources and licenses

- [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo) — networking core, protocols, DNS, and routing.
- [samosvalishe/free-turn-proxy](https://github.com/samosvalishe/free-turn-proxy) — FreeTURN transport.
- [Au1rxx/free-vpn-subscriptions](https://github.com/Au1rxx/free-vpn-subscriptions) — [built-in subscription](https://raw.githubusercontent.com/Au1rxx/free-vpn-subscriptions/main/output/v2ray-base64.txt).

Enable or disable the built-in subscription in subscription settings.

See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for authors, licenses, and component details, and [LICENSE](LICENSE) for the project license.
