# OrcheRoute

[Русский](README.md)

An Android VPN client and Linux Server VPN controller built on [Mihomo](https://github.com/MetaCubeX/mihomo), with subscriptions, server qualification, DNS, and `direct` / `proxy` / `block` routing.

## Downloads

- [Android 0.8.2 — arm64 APK](https://github.com/gooog1111/OrcheRoute/releases/download/v0.8.2/OrcheRoute-Android-0.8.2-code92-arm64.apk)
- [Linux Server 0.8.3 — amd64 DEB](https://github.com/gooog1111/OrcheRoute/releases/download/v0.8.3/OrcheRoute-Linux-Server-0.8.3-amd64.deb)
- [All releases](https://github.com/gooog1111/OrcheRoute/releases)

## Android

Requires Android 8.0 or later, without root.

1. Install the APK and grant permission to create a VPN connection.
2. Add a subscription or import a file, link, or QR code. Enable built-in emergency sources if needed.
3. Refresh and check an individual subscription or the full list.
4. Select a server manually and enable the VPN.

Android has one server list grouped by subscription, without automatic server selection or network-state switching. FreeTURN may require a VK CAPTCHA confirmation; drag the window by its header to move it.

Use Updates to install new versions. Enable Beta versions for the test channel, or disable it for stable updates. Allow notifications and adjust battery optimization if needed to keep the VPN running in the background.

Settings and subscriptions are stored in application data. Uninstalling the app removes that data; export important subscriptions first.

## Linux Server

Install on Debian/Ubuntu:

```bash
sudo apt install ./OrcheRoute-Linux-Server-0.8.3-amd64.deb
```

Open `http://127.0.0.1:19110/` on the server, or `http://SERVER_ADDRESS:19110/` from another device. The installer prints initial login credentials. Normal package upgrades preserve authentication and settings.

```bash
systemctl status orcheroute-go --no-pager
```

Add subscriptions in the WebUI, configure refresh intervals, qualify servers, and enable the VPN. Scheduled refreshes download subscriptions without a full qualification run; failed downloads are retried after a delay.

Automatic mode prefers the primary list, falling back to emergency servers. Restricted networks use a separate qualified list. Pending changes in physical-network state do not immediately replace the selected server. A substantially higher-rated primary server can be selected after repeated observations; small score differences do not trigger switching. Manual selections remain pinned.

### Qualification

Qualification includes TCP, URL checks, region, and speed tests. Ratings use latency, speed, and stability. Checking one subscription does not requalify all other subscriptions.

The Qualification section offers timeouts, test URLs, and filters. Linux Server also provides an optional target-opening filter:

- disabled;
- admit a server if the target opens;
- reject a server if the target opens.

The target, such as `youtube.com`, is requested through each candidate VPN after the ordinary URL test. Only HTTP `2xx` counts as successful opening; `403` does not.

### VPN server

Create personal subscriptions containing FreeTURN, VLESS Reality, Trojan TLS, and Hysteria2. Each subscription can have an expiry, traffic limit, and LAN permission: inherit the global setting, allow, or deny.

Default ports: FreeTURN UDP `4443`, VLESS TCP `24443`, Trojan TCP `24444`, Hysteria2 UDP `24445`. Open the required ports in your firewall. The public endpoint accepts an IP address or hostname; configure the HTTPS subscription address separately. Default SNI: `m.vk.ru`.

VLESS/Trojan/Hysteria2 traffic accounting polls active connections and may miss short sessions. It is not an exact billing counter.

### Updates and removal

Update from the WebUI or install the new DEB over the existing package. Preserve access to the server before changing network settings.

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

## Sources and licenses

- [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo) — networking core, protocols, DNS, and routing.
- [samosvalishe/free-turn-proxy](https://github.com/samosvalishe/free-turn-proxy) — FreeTURN transport.
- [EbraSha/free-v2ray-public-list](https://github.com/ebrasha/free-v2ray-public-list) — [built-in server list](https://raw.githubusercontent.com/ebrasha/free-v2ray-public-list/refs/heads/main/V2Ray-Config-By-EbraSha.txt).
- [Au1rxx/free-vpn-subscriptions](https://github.com/Au1rxx/free-vpn-subscriptions) — [built-in subscription](https://raw.githubusercontent.com/Au1rxx/free-vpn-subscriptions/main/output/v2ray-base64.txt).

Built-in subscriptions can be disabled. Public server availability is not guaranteed; prefer trusted sources for regular use and protect sensitive data with application-layer encryption such as HTTPS.

See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for authors, licenses, and component details, and [LICENSE](LICENSE) for the project license.
