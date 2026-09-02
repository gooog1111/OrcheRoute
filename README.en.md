<!-- LANG_START -->
🇷🇺 [Русская версия](README.md)
<!-- LANG_END -->

<!-- STATS_START -->
<!-- auto-updated by GitHub Actions · 2026-09-02 19:19 UTC -->

[![Views local](https://img.shields.io/badge/Views_local-2-ff6900?style=for-the-badge&logo=github)](https://github.com/gooog1111/OrcheRoute)
[![Views GitHub](https://img.shields.io/badge/Views_GitHub-6-ff6900?style=for-the-badge&logo=github)](https://github.com/gooog1111/OrcheRoute)
[![Unique visitors](https://img.shields.io/badge/Unique-3-blue?style=for-the-badge&logo=github)](https://github.com/gooog1111/OrcheRoute)
[![Clones](https://img.shields.io/badge/Clones-333-purple?style=for-the-badge&logo=github)](https://github.com/gooog1111/OrcheRoute)
[![Stars](https://img.shields.io/badge/Stars-0-yellow?style=for-the-badge&logo=github)](https://github.com/gooog1111/OrcheRoute/stargazers)
[![Forks](https://img.shields.io/badge/Forks-0-green?style=for-the-badge&logo=github)](https://github.com/gooog1111/OrcheRoute/network/members)
[![Downloads latest release](https://img.shields.io/badge/Downloads_latest_release-6-brightgreen?style=for-the-badge)](https://github.com/gooog1111/OrcheRoute/releases/latest)
[![Downloads total assets](https://img.shields.io/badge/Downloads_total_assets-164-brightgreen?style=for-the-badge)](https://github.com/gooog1111/OrcheRoute/releases)

<!-- STATS_END -->

<!-- GRAPH_START -->
<p align="center">
  <img src="./traffic-views.png" width="100%" alt="GitHub Traffic">
</p>
<!-- GRAPH_END -->

<!-- ISSUES_START -->
<!-- auto-updated by GitHub Actions · 2026-09-02 19:19 UTC -->

## Issues

<p>
  <a href="https://github.com/gooog1111/OrcheRoute/issues">
    <img alt="Open issues" src="https://img.shields.io/badge/Open_issues-0-blue?style=for-the-badge&logo=github">
  </a>
  <a href="https://github.com/gooog1111/OrcheRoute/issues/new/choose">
    <img alt="Create issue" src="https://img.shields.io/badge/Create_issue-new-success?style=for-the-badge&logo=github">
  </a>
</p>

<details open>
<summary><b>Open issues</b></summary>


<p align="center">
  <b>No open issues.</b><br>
  <sub>The service issue <code>views-counter</code> is hidden from the list.</sub>
</p>


</details>

<p>
  <a href="https://github.com/gooog1111/OrcheRoute/issues/new/choose">Create new issue</a> ·
  <a href="https://github.com/gooog1111/OrcheRoute/issues">All issues</a>
</p>

<!-- ISSUES_END -->

## OrcheRoute

OrcheRoute - VPN client and routing controller based on
[Mihomo](https://github.com/MetaCubeX/mihomo). The application accepts regular
links to servers and subscriptions, checks the availability of nodes, selects
working connection and routes traffic according to the rules `direct`, `proxy` and
`block`.

> Supported project targets are Android `arm64` and Linux Server `amd64`.
> The remaining platform components have been temporarily taken out of development in order to
> the common Go code and the two working adapters evolved without divergence.

## Download

- **Current stable version: 0.7.4** (Android `versionCode 85`)
- [Скачать APK для Android arm64](https://github.com/gooog1111/OrcheRoute/releases/download/v0.7.4/OrcheRoute-Android-0.7.4-code85-arm64.apk)
- [Скачать DEB для Linux Server amd64](https://github.com/gooog1111/OrcheRoute/releases/download/v0.7.4/OrcheRoute-Linux-Server-0.7.4-amd64.deb)
- [Страница последнего стабильного выпуска](https://github.com/gooog1111/OrcheRoute/releases/latest)
- [Все выпуски и beta-версии](https://github.com/gooog1111/OrcheRoute/releases)

Android requires Android 8.0 or later. Root access is not needed.

1. Download the APK from the latest stable release.
2. Allow installation from the selected browser or file manager.
3. When you turn it on for the first time, confirm the creation of an Android VPN connection.
4. Allow notifications and, if Android prompts, exclude OrcheRoute from
   energy saving - this helps the system not to stop the VPN in the background.5. Add your own subscription in the “Subscriptions” section or enable the ones you need
   built-in emergency sources.
6. Return to the main screen and tap "Enable".

Updates are installed from the application itself via GitHub Releases. For
For regular updates, leave the “Beta versions” checkbox unchecked.

## #Linux Server

Install the downloaded DEB via APT so that the necessary network components
were installed automatically:

```bash
sudo apt install ./OrcheRoute-Linux-Server-0.7.4-amd64.deb
```

With direct `dpkg -i` WebUI also starts, but to enable VPN transport
You will need `nftables` installed. After setting the interface address and
the generated login data is output to the terminal and saved in
`/etc/orcheroute/initial-credentials`.

An inbound VPN server is also available in the Linux Server beta channel. One personal
the subscription contains FreeTURN, VLESS Reality, Trojan TLS and Hysteria2; default
SNI `m.vk.ru` is used and ports TCP 24443, TCP 24444 and UDP 24445.
FreeTURN accepts external TCP traffic and forwards it to the built-in Xray;
VLESS/Trojan/Hysteria2 listeners work through an isolated instance
complete Mihomo and do not change the current outgoingrouting. The expiration date applies to the entire subscription; traffic counter beta
For now, only the FreeTURN path is taken into account.

`sudo apt remove orcheroute` saves settings, authorization, subscriptions,
routes and condition. `sudo apt purge orcheroute` completely removes this data
and OrcheRoute backups.

## What OrcheRoute can do

- add subscriptions, individual servers, files, lists from the buffer and QR codes;
- automatically recognize the supported subscription format;
- discard duplicate servers and duplicate subscriptions;
- check servers at the TCP, URL-test and speed-test stages;
- rank production servers by latency, speed and stability;
- switch between the main and emergency lists of servers;
- separately find servers operating under the operator’s “white list” mode;
- apply routes by domains, IP/CIDR, ports, protocols, GeoIP and GeoSite;
- intercept normal DNS requests inside Android VPN and apply to them
  selected routing;
- update GeoIP and GeoSite from the selected source;
- save settings, subscriptions and routes between restarts and updates.
- change the design theme common to Android and Linux: “The Matrix”, Hello Kitty,Liquid Glass, Windows 95, dark or light.

## How automation works

OrcheRoute independently monitors the state of the physical network and distinguishes between three
states:

- **Internet available.** The best working server of the main one is used first
  list. If there are no primary servers, the application goes to the emergency list.
- **Only white lists are available.** The application generates a separate list from
  servers actually available on a limited network. Regional filter for this
  no verification is applied, and all traffic, except for explicitly blocked traffic, goes through the VPN.
- **No network.** The VPN is turned off so as not to hold the broken tunnel, and
  is restored after the appearance of a suitable network, if it was turned on before the accident.

In automatic mode, the server is held until failure. Manual mode secures
selected server until the user re-enables automatic mode.

## Routing

The rule can be directed to one of three actions:

- `direct` - bypass VPN;
- `proxy` — through the selected VPN server;
- `block` — block the connection.

Supports individual IP addresses, CIDR and IP ranges, domain masks,GeoIP/GeoSite, protocols and independent port expressions, such as `:5000`,
`:5000-6000` or `:5000,5002,5005`.

Before applying non-standard rules, leave access to the local network and
To the VPN server: an erroneous `block` or `direct` rule may make the connection unavailable.

## Used projects and built-in sources

## # Network core

- [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo) — TUN processing,
  proxy protocols, DNS and routing rules. In Android, the kernel is built into the APK;
  its version is updated along with the application.

## # Transport FreeTURN

- [samosvalishe/free-turn-proxy](https://github.com/samosvalishe/free-turn-proxy)
  used as a single client and server FreeTURN runtime;
- `third_party/free-turn-proxy` contains a verified upstream revision
  with a minimal patch for isolating failures of independent VK Call providers.

Credit(s), license, and original revision(s) are listed in `THIRD_PARTY_NOTICES.md`.
The previous proprietary VKCall/DTLS/KCP/smux path has been removed.

## # Built-in emergency server lists

With a clean installation, two public sources are available. They can be turned off in
"Subscriptions" section:

- [EbraSha/free-v2ray-public-list](https://github.com/ebrasha/free-v2ray-public-list)
  — [используемый список серверов](https://raw.githubusercontent.com/ebrasha/free-v2ray-public-list/refs/heads/main/V2Ray-Config-By-EbraSha.txt);
- [Au1rxx/free-vpn-subscriptions](https://github.com/Au1rxx/free-vpn-subscriptions)
  — [используемая V2Ray/Base64-подписка](https://raw.githubusercontent.com/Au1rxx/free-vpn-subscriptions/main/output/v2ray-base64.txt).These repositories are not owned by OrcheRoute. Composition, availability and safety
public servers are controlled by their authors and node owners. Consider
them as an emergency reserve: for permanent operation it is better to add your own trusted
subscription. Do not transmit sensitive data through unknown servers without application
encryption, such as HTTPS.

## GeoIP and GeoSite

The application supports three ready-made sources:

- [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat) —
  [GeoIP](https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat)
  and [GeoSite](https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat);
- [RunetFreedom/russia-v2ray-rules-dat](https://github.com/runetfreedom/russia-v2ray-rules-dat) —
  [GeoIP](https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geoip.dat)
  and [GeoSite](https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat);
- [Loyalsoldier/v2ray-rules-dat](https://github.com/Loyalsoldier/v2ray-rules-dat) —
  [GeoIP](https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat)
  and [GeoSite](https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat).

You can also specify your own direct HTTPS links to compatible
`geoip.dat` and `geosite.dat`. One selected set is used at a time.

## Android data and permissions

Settings, routes and imported subscriptions are stored locally in data
applications. OrcheRoute does not require registration. To operate the application, you need access to
networks, VPN system permission, foreground notification and camera access only when scanning
QR code.

Uninstalling an application deletes its local data. Before reinstalling with anotherExport important subscriptions and servers with a signing certificate.

## Limitations

- stable Android APK is now published only for `arm64`;
- availability of free servers and subscriptions is not guaranteed;
- some Android firmwares aggressively close background applications even after
  issuing permits;
- VPN transfer to a mobile hotspot depends on the implementation of the Android firmware and without
  system rights may not be available;
- Linux Server and Android use different system transport adapters;
  their general functionality is consistently transferred to single Go modules.

## Development documentation

- [API.md](API.md) — local HTTP API;
- [ANDROID_ARCHITECTURE.md](ANDROID_ARCHITECTURE.md) — Android application device;
- [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) - third-party components and licenses.

## Local check and build

The single sign-on checks only supported targets - Android and Linux Server:

```powershell
.\scripts\build-all.ps1 -Target all
```

On Windows, Android is built locally, and Linux Server is built in WSL
`Ubuntu-24.04`. Individual goals: `android`, `linux-server`, `web` and `common`.
On Linux, `./scripts/build-all.sh all` is used. Build via GitHub Actions
prohibited in the project.Current development is carried out in a single branch `main`. Stable versions
are fixed with the `v*` tags, and test Android builds with the `android-beta` tag.
Builds and checks are performed locally only; workflow GitHub Actions have been removed.

## Licenses and attribution

OrcheRoute uses third party projects and data under their own licenses.
The links above lead to the original repositories, where authors, licenses and rules are indicated
use of each component.
