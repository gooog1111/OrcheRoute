import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("exports the OrcheRoute dashboard", async () => {
  const html = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
  assert.match(html, /<html lang="ru" data-release-channel="(?:stable|beta)" data-theme="matrix">/i);
  assert.match(html, /<title>OrcheRoute<\/title>/i);
  assert.match(html, /orcheroute\.ui\.theme/i);
  assert.doesNotMatch(html, />\s*WebUI\s*</i);
  assert.match(html, /Управление маршрутизацией и VPN на сервере/i);
});

test("dashboard server count belongs to the server list named below it", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  assert.match(dashboard, /const aliveNodes = activePool\?\.alive \?\? allAliveNodes/);
  assert.match(dashboard, /const totalNodes = activePool\?\.total \?\? allTotalNodes/);
  assert.match(dashboard, /: "Все списки серверов"/);
});

test("android persists a successful node switch and displays its date and time", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  assert.match(runtime, /repository\.confirmConnectedNode\(connectedNodeID, allowlistRouteOverride\)/);
  assert.match(runtime, /put\("last_switch", repository\.lastSwitch\(\)\)/);
  assert.match(repository, /put\("connected_node_key", connectionKey\)\.put\("last_switch", now\(\)\)/);
  assert.match(dashboard, /year: "numeric"/);
  assert.match(dashboard, /minute: "2-digit"/);
});

test("keeps API credentials out of the client bundle", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  assert.match(api, /\/api\$\{path\}/);
  assert.doesNotMatch(api, /api_token|Authorization:\s*["']Bearer/i);
  assert.match(api, /X-OrcheRoute-UI/);
});

test("pauses dashboard polling while settings are being edited", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  assert.match(dashboard, /if \(settingsOpen\) return;/);
  assert.match(dashboard, /platform\.dashboardPollMs/);
  assert.match(dashboard, /refresh, settingsOpen/);
});

test("embedded settings use the expanded workspace and hide web publication controls", async () => {
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(css, /settings-modal-wide[^}]*1480px/);
  assert.match(css, /height:\s*min\(1080px, calc\(100dvh - 32px\)\)/);
  assert.match(settings, /platform\.showAccessSettings\s*&&\s*\(\s*<Tab\s+active=\{activeTab === "access"\}/);
});

test("common UI receives platform differences from one capability module", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const platform = await readFile(new URL("../app/platform/runtime.ts", import.meta.url), "utf8");
  assert.doesNotMatch(dashboard, /isAndroidRuntime|OrcheRouteAndroid|window\.runtime/);
  assert.doesNotMatch(settings, /isAndroidRuntime|OrcheRouteAndroid|window\.runtime/);
  for (const capability of ["networkEditor", "showAccessSettings", "editServerLists", "appUpdater"]) {
    assert.match(platform, new RegExp(capability));
  }
  assert.match(platform, /const sharedCapabilities = \{[\s\S]*editServerLists: true,[\s\S]*controlWhitelistScan: true,[\s\S]*cancelLongOperations: true,[\s\S]*rebuildEmergencyOnSelection: false/);
  assert.doesNotMatch(platform, /kind: "android",[\s\S]*editServerLists:/);
});

test("exposes separate subscription refresh and cached server checks", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(api, /\/v1\/subscriptions\/check/);
  assert.match(settings, /Проверить серверы/);
  assert.match(settings, /subscription\.last_tested/);
  assert.match(settings, /subscription\.last_available/);
  assert.doesNotMatch(settings, /Только аварийный список серверов/);
});

test("uses an embedded subscription delete confirmation", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(settings, /window\.confirm/);
  assert.match(settings, /role="alertdialog"/);
  assert.match(settings, /actions\.deleteSubscription\(deleting\.id\)/);
});

test("renders automatic and manual modes without a redundant emergency-only setting", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  assert.match(settings, /proxy\.mode === "auto" \? "selected"/);
  assert.doesNotMatch(settings, /actions\.setEmergency/);
  assert.doesNotMatch(settings, /emergency-pool-option/);
  assert.match(settings, /<strong>Ручной режим<\/strong>/);
  assert.doesNotMatch(settings, /<strong>Ручной сервер<\/strong>/);
  assert.match(repository, /void setAuto\(\)[\s\S]*root\.remove\("selected_node"\);[\s\S]*selectBestLocked\(\);[\s\S]*save\(\);/);
  assert.match(repository, /migrateEmergencyOnlyMode\(\);/);
  assert.match(repository, /migrateEmergencyOnlyMode\(\)[\s\S]*"emergency"\.equals[\s\S]*setAuto\(\);/);
});

test("android qualification checks the complete parsed set in ordered stages", async () => {
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.doesNotMatch(runtime, /sampleProxies/);
  assert.match(runtime, /Mobilecore\.qualifyNodes/);
  assert.match(runtime, /new QualificationObserver\(\)/);
  assert.match(runtime, /qualificationProgressMessage/);
  assert.match(runtime, /speed_candidates_per_source/);
  assert.doesNotMatch(runtime, /Mobilecore\.engineTestTCP/);
  assert.doesNotMatch(runtime, /Mobilecore\.engineTestProxiesMulti/);
  assert.doesNotMatch(runtime, /Mobilecore\.engineTestSpeedAdaptive/);
});

test("android connectivity monitor binds DNS and HTTP to a physical network", async () => {
  const monitor = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/ConnectivityMonitor.java", import.meta.url), "utf8");
  assert.match(monitor, /NET_CAPABILITY_NOT_VPN/);
  assert.match(monitor, /network\.openConnection/);
  assert.match(monitor, /Mobilecore\.connectivityTargets/);
  assert.match(monitor, /Mobilecore\.classifyConnectivity/);
  assert.match(monitor, /Mobilecore\.confirmConnectivity/);
  assert.match(monitor, /physical_network_available[\s\S]*underlay != null/);
  assert.match(monitor, /observed=[\s\S]*confirmed=[\s\S]*candidate=[\s\S]*streak=/);
  assert.doesNotMatch(monitor, /probeConnectivity/);
});

test("android qualification sockets use the physical network selected by the monitor", async () => {
  const vpnService = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.match(vpnService, /engineInit\(home\.getAbsolutePath\(\), fd -> protectAndBind\(\(int\) fd\)\)/);
  assert.match(vpnService, /Network network = MobileRuntime\.get\(this\)\.identityPhysicalNetwork\(\)/);
  assert.match(vpnService, /network\.bindSocket\(duplicate\.getFileDescriptor\(\)\)/);
  assert.match(vpnService, /if \(!protect\(fd\)\) return false/);
  assert.match(runtime, /initializeQualificationTransport\(\)/);
  assert.match(runtime, /engineInit\([\s\S]*fd -> bindPhysicalSocket\(\(int\) fd\)\)/);
  assert.match(runtime, /Network network = connectivityMonitor\.activePhysicalNetwork\(\)/);
  assert.match(runtime, /network\.bindSocket\(duplicate\.getFileDescriptor\(\)\)/);
});

test("android whitelist scan finishes before VPN connect and checks one source contextually", async () => {
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  assert.match(runtime, /private JSONObject scheduleManualCheck\(String onlyId\)[\s\S]*boolean restricted = "allowlist"\.equals\(connectivityState\(\)\)/);
  assert.match(runtime, /if \(restricted\) enterAllowlistMode\(\)/);
  assert.match(runtime, /\/v1\/subscriptions\/check[\s\S]*scheduleManualCheck\(null\)/);
  assert.match(runtime, /subscriptionId = subscriptionId\(path, "\/check"\)[\s\S]*scheduleManualCheck\(subscriptionId\)/);
  assert.match(runtime, /scheduleRefresh\(onlyId, true, null, true, false, false\)/);
  assert.match(runtime, /boolean restrictedScan = allowlistScan;/);
  assert.match(runtime, /restrictedScan[\s\S]*effectivePolicy\.put\("skip_speed", true\)/);
  assert.match(runtime, /restrictedScan[\s\S]*repository\.replaceWhitelistSource/);
	assert.match(runtime, /awaitStableWhitelistConnection\(45_000\)/);
	assert.match(runtime, /whitelistHealthSuccesses >= 2/);
	assert.match(runtime, /if \(desiredEnabled\) onWhitelistPoolEmpty\(\)/);
	assert.match(runtime, /if \("allowlist"\.equals\(current\.state\)\) enterAllowlistMode\(\)/);
  assert.doesNotMatch(runtime, /Найден доступный сервер, подключаемся/);
  assert.match(repository, /\.put\("selected", !whitelistMode && poolSelected\)/);
	assert.match(repository, /\.put\("selected", whitelistMode\)/);
	assert.match(runtime, /allowlist_use_emergency_subscriptions/);
	assert.match(runtime, /!"emergency"\.equals\(item\.optString\("group", "primary"\)\)/);
	assert.match(repository, /removeEmergencyWhitelistSources/);
});

test("android whitelist health trusts qualification and requires repeated multi-url failures", async () => {
  const service = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");
  const verifier = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/ProxyHealthVerifier.java", import.meta.url), "utf8");
  assert.match(service, /consecutiveWhitelistHealthFailures/);
  assert.match(service, /proxyConnectedAtElapsedMs < 45_000/);
  assert.match(service, /runtime\.proxyHealthURLs\(\)/);
  assert.match(service, /runtime\.verifyActiveProxyTransport\(\)/);
  assert.match(service, /Active whitelist proxy passed transport URL-test/);
  assert.match(service, /if \(restrictedNetwork\) \{[\s\S]*probeAllowlistProxy\(runtime\);[\s\S]*return;/);
  assert.match(verifier, /Mobilecore\.engineTestProxiesMulti/);
	assert.match(verifier, /ALLOWLIST_HEALTH_TIMEOUT_MS = 25_000/);
	assert.match(verifier, /Math\.max\(defaults\.optInt\("url_timeout_ms", 3000\), ALLOWLIST_HEALTH_TIMEOUT_MS\)/);
  assert.match(service, /int failures = \+\+consecutiveWhitelistHealthFailures/);
  assert.match(service, /if \(failures < 3\)[\s\S]*Keeping whitelist node after inconclusive transport round/);
  assert.match(service, /Proxy health failed in/);
  assert.match(service, /consecutiveWhitelistHealthFailures = 0;[\s\S]*runtime\.onRestrictedNetworkDetected\(\)/);
});

test("android status polling never claims VPN ownership", async () => {
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  assert.doesNotMatch(runtime, /VpnService\.prepare/);
  assert.match(runtime, /permission_granted", vpnPermissionGranted/);
  assert.match(activity, /VpnService\.prepare\(this\)/);
});

test("android settings expose a signed GitHub release updater without transport explanation", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const updater = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/AppUpdater.java", import.meta.url), "utf8");
  assert.doesNotMatch(settings, /Один транспорт для исходящих соединений/);
  assert.doesNotMatch(settings, />\s*Проверить версию\s*</);
  assert.doesNotMatch(settings, /Ядро обновляется с приложением/);
  assert.doesNotMatch(settings, /Mihomo встроен в приложение и обновляется вместе с новой подписанной APK/);
  assert.match(settings, /<span>Последняя версия<\/span>/);
  assert.match(settings, /<strong>\{versionState\}<\/strong>/);
  assert.match(settings, /Обновление OrcheRoute/);
	assert.match(updater, /releases\/latest\/download\/android-update\.json/);
  assert.match(updater, /SHA-256 загруженного APK не совпадает/);
  assert.match(updater, /Сертификат подписи APK не совпадает/);
});

test("android updater selects a persistent prerelease channel with a stable-to-beta warning", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  const updater = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/AppUpdater.java", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  assert.doesNotMatch(settings, /Обновиться до Beta/);
  assert.match(settings, /<strong>Beta-версии<\/strong>/);
  assert.match(settings, /toggle-row app-update-channel/);
  assert.match(css, /\.app-update-channel > input[^}]*accent-color: var\(--accent\)/);
  assert.match(css, /\.app-update-channel > span[^}]*display: grid[^}]*gap: 3px/);
  assert.match(settings, /appUpdate\?\.current_prerelease/);
  assert.match(settings, /Это тестовая сборка с непроверенными изменениями VPN-автоматики/);
  assert.match(settings, /Понимаю риск, включить Beta/);
	assert.match(settings, /actions\.setAppUpdateChannel\(enabled\)/);
	assert.match(settings, /setBetaChannel\(true\)/);
	assert.match(updater, /releases\/download\/android-beta\/android-update\.json/);
	assert.match(updater, /Канал manifest не совпадает/);
  assert.match(updater, /preferences\.getBoolean\(BETA_ENABLED, currentVersion\.contains\("-"\)\)/);
  assert.match(updater, /loadManifest\(beta\)/);
  assert.doesNotMatch(activity, /installBetaAppUpdate/);
  assert.match(activity, /setAppUpdateBetaEnabled/);
});

test("android checks its selected update channel on launch and prompts on the dashboard", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const updater = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/AppUpdater.java", import.meta.url), "utf8");
  assert.match(updater, /channel = betaEnabled \? "beta" : "stable";[\s\S]*check\(\);/);
  assert.match(dashboard, /getAndroidAppUpdateStatus/);
  assert.match(dashboard, /latest_version_code/);
  assert.match(dashboard, /Доступно обновление OrcheRoute/);
  assert.match(dashboard, /Скачать и установить/);
  assert.match(dashboard, /Это тестовая версия/);
  assert.match(dashboard, /Позже/);
});

test("all platforms derive prerelease colors from the shared release module", async () => {
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  const rain = await readFile(new URL("../app/ui/MatrixRain.tsx", import.meta.url), "utf8");
  const layout = await readFile(new URL("../app/layout.tsx", import.meta.url), "utf8");
  const releaseModule = await readFile(new URL("../app/platform/release.ts", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  assert.match(css, /html\[data-release-channel="beta"\][\s\S]*--accent: #ff5b61/);
  assert.match(css, /:root[\s\S]*--accent: #45f3c2/);
  assert.match(rain, /releaseBranding\.prerelease/);
  assert.match(layout, /data-release-channel=\{releaseBranding\.channel\}/);
  assert.match(releaseModule, /NEXT_PUBLIC_ORCHEROUTE_RELEASE_CHANNEL/);
  assert.doesNotMatch(activity, /prerelease-theme/);
  assert.doesNotMatch(activity, /replaceFirst\("<html"/);
});

test("android delegates network and DNS validation to the shared Go validator", async () => {
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  assert.match(repository, /Mobilecore\.validateMobileNetworkProfile/);
  assert.match(repository, /Mobilecore\.validateMobileDNSProfile/);
  assert.doesNotMatch(repository, /"wifi"\.equals\(value\)/);
  assert.doesNotMatch(repository, /new String\[\]\{"direct", "proxy", "vpn_underlay", "bootstrap"\}/);
});

test("android and linux use the shared failover controller", async () => {
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  const service = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");
  const server = await readFile(new URL("../../internal/serverruntime/controller.go", import.meta.url), "utf8");
  assert.match(repository, /Mobilecore\.failoverStep/);
  assert.match(server, /controller\.Step/);
  assert.doesNotMatch(service, /healthFailureStreak/);
  assert.doesNotMatch(repository, /failoverActiveNode/);
  assert.doesNotMatch(repository, /preferPrimaryIfAvailable/);
});

test("shared prerelease branding labels every UI and feeds the Android native shell", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const nextConfig = await readFile(new URL("../next.config.ts", import.meta.url), "utf8");
  const gradle = await readFile(new URL("../../android/app/build.gradle", import.meta.url), "utf8");
  const manifest = await readFile(new URL("../../android/app/src/main/AndroidManifest.xml", import.meta.url), "utf8");
  const icon = await readFile(new URL("../../android/app/src/main/res/drawable/ic_launcher.xml", import.meta.url), "utf8");
  const release = JSON.parse(await readFile(new URL("../../release.json", import.meta.url), "utf8"));
  const debianControl = await readFile(new URL("../../packaging/debian/control", import.meta.url), "utf8");
  const builder = await readFile(new URL("../../cmd/orcheroute-build/main.go", import.meta.url), "utf8");
  assert.match(dashboard, /releaseBranding\.badge/);
  assert.match(nextConfig, /\.\.\/release\.json/);
  assert.match(nextConfig, /NEXT_PUBLIC_ORCHEROUTE_RELEASE_CHANNEL/);
  assert.match(gradle, /rootProject\.file\("\.\.\/release\.json"\)/);
  assert.match(gradle, /canonicalWebAssets[\s\S]*\.\.\/webui\/out/);
  assert.doesNotMatch(gradle, /generatedWebAssets\.isPresent/);
  assert.match(gradle, /\.\.\/dist\/verify\/android[\s\S]*mobilecore\.aar/);
  assert.doesNotMatch(gradle, /libs\/mobilecore\.aar/);
  assert.match(gradle, /orcheRoutePrerelease \? "#FF5B61" : "#45F3C2"/);
  assert.match(gradle, /orcheRoutePrerelease \? "OrcheRoute BETA" : "OrcheRoute"/);
  assert.match(manifest, /android:label="@string\/app_name"/);
  assert.match(icon, /@color\/launcher_accent/);
  assert.equal(debianControl.match(/^Version:\s*(.+)$/m)?.[1], release.version);
  assert.match(builder, /include shared Server WebUI/);
  assert.match(builder, /filepath\.Join\(b\.dist, "linux-server"\)/);
  assert.match(builder, /-PorcherouteWebAssets=/);
  assert.doesNotMatch(builder, /syncDesktopWeb|windowsDesktop|linuxDesktop/);
});

test("android locks portrait mode and settings save only changed drafts", async () => {
  const manifest = await readFile(new URL("../../android/app/src/main/AndroidManifest.xml", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(manifest, /android:screenOrientation="portrait"/);
  assert.match(settings, /disabled=\{busy \|\| !policy \|\| !policyChanged \|\| !validTestURLs\}/);
  assert.match(settings, /disabled=\{busy \|\| !transportChanged\}/);
  assert.match(settings, /disabled=\{busy \|\| !dnsChanged\}/);
  assert.match(settings, /disabled=\{busy \|\| !profileChanged\}/);
  assert.match(settings, /disabled=\{busy \|\| !routes \|\| !routesChanged\}/);
});

test("mobile dashboard centers power controls and places switch time on its own line", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  assert.match(dashboard, /Переключение<time>\{formatTime/);
  assert.match(css, /\.metric-card small time \{ display: block/);
  assert.match(css, /grid-template-rows: auto minmax\(0, 1fr\) auto/);
  assert.match(css, /\.power-stage \{ align-self: center; justify-self: center/);
});

test("android dashboard follows live VPN state and shows direct and proxy identities", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const platform = await readFile(new URL("../app/platform/runtime.ts", import.meta.url), "utf8");
  const service = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.match(dashboard, /proxy_ok: "Подключено"/);
  assert.match(dashboard, /enabled \? "OrcheRoute включён"/);
  assert.doesNotMatch(dashboard, /Соединение под контролем/);
  assert.doesNotMatch(dashboard, /Автоматика продолжит/);
  assert.match(dashboard, /<span>Direct<\/span>/);
  assert.match(dashboard, /<span>Proxy<\/span>/);
  assert.match(dashboard, /className="connected-server"/);
  assert.match(dashboard, /activeServerName/);
  assert.match(dashboard, /data\?\.status\.proxy\.active_node \|\| activeNode\?\.display_name/);
  assert.match(dashboard, /data\?\.status\.wan\.mode === "allowlist" \? "Недоступен при белых списках"/);
  assert.match(dashboard, /platform\.dashboardPollMs/);
  assert.match(platform, /kind: "android"[\s\S]*dashboardPollMs: 1000[\s\S]*liveDashboard: true/);
  assert.match(api, /loadLiveDashboard/);
  assert.match(service, /network == null \? url\.openConnection\(\) : network\.openConnection\(url\)/);
  assert.match(service, /Mobilecore\.parseConnectionIdentity/);
  assert.match(runtime, /"allowlist"\.equals\(connectivitySnapshot\.state\)[\s\S]*new JSONObject\(\) : new JSONObject\(directIdentity\.toString\(\)\)/);
  assert.match(runtime, /\.put\("identity", new JSONObject\(proxyIdentity\.toString\(\)\)\)/);
});

test("android uses OrcheRoute notification glyph and user-facing server-list terminology", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  const service = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");
  const icon = await readFile(new URL("../../android/app/src/main/res/drawable/ic_notification.xml", import.meta.url), "utf8");
  assert.match(service, /setSmallIcon\(R\.drawable\.ic_notification\)/);
  assert.doesNotMatch(service, /stat_sys_warning/);
  assert.match(icon, /M4,14h4v6H4zM10,4h4v16h-4zM16,9h4v11h-4z/);
  assert.doesNotMatch(icon, /M0,0h24v24/);
  assert.doesNotMatch(`${dashboard}\n${settings}\n${runtime}`, /пул/iu);
  assert.match(settings, /Встроенный аварийный список серверов/);
  assert.doesNotMatch(settings, /subscription\.description/);
  assert.match(repository, /addDefault\("ebrasha-public", "EbraSha"/);
  assert.match(repository, /addDefault\("default-au1rxx", "Au1rxx"/);
  assert.match(repository, /existing\.put\("name", name\).*\.put\("description", description\)/s);
  assert.doesNotMatch(repository, /Обновляемый универсальный аварийный список/);
  assert.doesNotMatch(repository, /Умеренный V2Ray\/Base64-набор/);
});

test("qualification UI exposes connectivity anchors and emergency per-source top", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const styles = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.match(settings, /Доступно при белых списках/);
  assert.match(settings, /Доступно в обычном интернете/);
  assert.match(settings, /Speed-test аварийной подписки/);
  assert.match(settings, /speed_candidates_per_source/);
  assert.match(settings, /activeTab === "qualification"/);
  assert.match(settings, /label="Квалификация"/);
  assert.match(settings, /tcp_timeout_ms/);
  assert.match(settings, /url_timeout_ms/);
  assert.match(settings, /geo_timeout_ms/);
  assert.match(settings, /speed_timeout_ms/);
  assert.match(settings, /url_test_urls/);
  assert.match(settings, /allowlist_use_emergency_subscriptions/);
  assert.match(settings, /Контрольные ссылки/);
  assert.match(settings, /Добавить ссылку/);
  assert.match(settings, /Удалить URL-test/);
  assert.match(runtime, /Mobilecore\.qualifyNodes/);
  assert.match(runtime, /effectivePolicy\.put\("skip_speed", true\)/);
  assert.match(settings, /nodes\.slice\(0, 5\)/);
  assert.match(settings, /className={`node-list-toggle/);
  assert.match(settings, /aria-expanded={expanded}/);
  assert.doesNotMatch(settings, /settings-nav-swipe-hint/);
  assert.match(settings, /settings-nav-edge left/);
  assert.match(settings, /settings-nav-edge right/);
  assert.match(settings, /scrollNavigation\(-1\)/);
  assert.match(settings, /scrollNavigation\(1\)/);
  assert.match(settings, /querySelector<HTMLElement>\("button\.active"\)/);
  assert.match(settings, /right - nav\.clientWidth/);
  assert.match(styles, /\.settings-nav-edge\.left/);
  assert.match(styles, /\.settings-nav-edge\.right/);
  assert.match(styles, /grid-template-columns: 30px minmax\(0, 1fr\) 30px/);
  assert.match(styles, /\.settings-nav-edge:disabled/);
  assert.match(settings, /const baseOperation: OperationView \| null/);
  assert.match(settings, /: null;\s*const displayedOperation/);
  assert.match(styles, /overflow-x: auto/);
});

test("android exposes selectable geo sources and applies the selected source", async () => {
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.match(runtime, /Mobilecore\.geoSources\(\)/);
  assert.match(runtime, /Mobilecore\.geoCatalog/);
  assert.match(runtime, /Mobilecore\.resolveGeoSource/);
  assert.match(runtime, /Mobilecore\.updateGeoFromSource/);
  assert.match(runtime, /installed_geo_source/);
});

test("geo source, schedule, and update action share one settings card", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(settings, /editor-card geo-settings-card[\s\S]*geo-source-list[\s\S]*Расписание обновления[\s\S]*Обновить геобазы/);
  assert.doesNotMatch(settings, /editor-card component-schedule/);
  assert.match(settings, /if \(geoSettingsChanged\)[\s\S]*updateComponents\("geo"\)/);
});

test("android subscription export uses the system file picker", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  assert.match(api, /saveTextFile\?:/);
  assert.match(activity, /Intent\.ACTION_CREATE_DOCUMENT/);
  assert.match(activity, /openOutputStream/);
});

test("android subscription import uses the system document picker", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  assert.match(api, /openTextFile\?:/);
  assert.match(settings, /orcheroute:file-open/);
  assert.match(activity, /Intent\.ACTION_OPEN_DOCUMENT/);
  assert.match(activity, /dispatchFileOpenResult/);
  assert.doesNotMatch(settings, /Поле не скрыто/);
});

test("android keeps focused settings fields above the software keyboard", async () => {
  const manifest = await readFile(new URL("../../android/app/src/main/AndroidManifest.xml", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(manifest, /android:windowSoftInputMode="adjustResize"/);
  assert.match(activity, /WindowInsetsCompat\.Type\.ime\(\)/);
  assert.match(activity, /Math\.max\(safe\.bottom, keyboard\.bottom\)/);
  assert.match(settings, /addEventListener\("focusin", revealFocusedField\)/);
  assert.match(settings, /scrollIntoView\(\{ block: "center"/);
});

test("android dismisses the IME before opening compact dropdowns", async () => {
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const styles = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  assert.match(activity, /public void hideKeyboard\(\)/);
  assert.match(activity, /WindowInsetsCompat\.Type\.ime\(\)/);
  assert.match(api, /export function dismissAndroidKeyboard/);
  assert.match(settings, /select, \.picker-trigger, \.protocol-menu > summary/);
  assert.match(settings, /active\.blur\(\);[\s\S]*dismissAndroidKeyboard\(\)/);
  assert.doesNotMatch(settings, /autoFocus\s*\n\s*value=\{query\}/);
  assert.match(styles, /\.route-option-list \{ min-height: 0; max-height: none; grid-auto-rows: minmax\(37px, auto\); \}/);
});

test("subscription parser is automatic and zero available nodes is a completed test", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  assert.match(settings, /looksLikeBlackTemple/);
  assert.match(settings, /\^blacktemple:/);
  assert.match(settings, /scheme=blacktemple/);
  assert.doesNotMatch(settings, /argonaft1/);
  assert.doesNotMatch(settings, /name\.trim\(\).*black/);
  assert.doesNotMatch(settings, /<option value="blacktemple">/);
  assert.doesNotMatch(settings, /<option value="standard">/);
  assert.match(runtime, /report\.optInt\("url_alive"\) == 0 \? "url_unavailable"/);
  assert.match(runtime, /repository\.refreshUnavailable\(id, links, reason, proxies\.length\(\), !checkOnly\)/);
  assert.match(repository, /if \(fetched\) subscription\.put\("last_success", now\(\)\)/);
  assert.match(repository, /node\.put\("alive", false\)/);
  assert.match(repository, /last_result", aliveCount == 0 \? "no_available_servers"/);
  assert.doesNotMatch(repository, /normalizedName/);
  assert.doesNotMatch(repository, /last_status", "error"\)\.put\("last_error", "Ни один проверенный сервер/);
});

test("android QR results use the common subscription importer", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(api, /scanQr\?: \(\) => void/);
  assert.match(settings, /orcheroute:qr-scan/);
  assert.match(settings, /setSecret\(\(current\)/);
  assert.match(settings, /Сканировать QR/);
});

test("removes verbose dashboard and settings explanations", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const content = `${dashboard}\n${settings}`;
  for (const removed of [
    "OrcheRoute · API v",
    "Автоматический режим удерживает рабочий узел до отказа",
    "DNS-защита включена",
    "Безопасное обновление Mihomo",
    "Обновление загружает новые данные и сохраняет прежние серверы",
  ]) assert.doesNotMatch(content, new RegExp(removed));
});

test("mobile dashboard contains long status and server names", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const styles = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  assert.match(dashboard, /className="connected-server" title=\{activeServerName\}/);
  assert.match(styles, /\.hero-copy \{ width: 100%; min-width: 0;/);
  assert.match(styles, /\.connected-server strong \{ min-width: 0; flex: 1 1 auto;[\s\S]*text-overflow: ellipsis/);
  assert.match(styles, /\.hero-copy h1 \{ width: 100%;[\s\S]*overflow-wrap: anywhere/);
});

test("checked subscription nodes are shown by rating without retesting other sources", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(api, /score\?: number/);
  assert.match(settings, /\(right\.score \?\? 0\) - \(left\.score \?\? 0\)/);
  assert.match(settings, /★\$\{Math\.round\(node\.score\)\}/);
  assert.match(settings, /проверены и добавлены в список по рейтингу/);
});

test("subscription cards show actual and next refresh timestamps", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const mobileRepository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  const serverAPI = await readFile(new URL("../../internal/serverruntime/api.go", import.meta.url), "utf8");
  assert.match(settings, /Обновлена:[\s\S]*subscription\.last_success/);
  assert.match(settings, /Следующее обновление:[\s\S]*subscription\.next_update/);
  assert.match(mobileRepository, /Mobilecore\.nextSubscriptionUpdate/);
  assert.match(serverAPI, /subscriptions\.NextUpdate/);
});

test("shared UI exposes and persists all six appearance themes", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const layout = await readFile(new URL("../app/layout.tsx", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const theme = await readFile(new URL("../app/ui/theme.ts", import.meta.url), "utf8");
  const backdrop = await readFile(new URL("../app/ui/ThemeBackdrop.tsx", import.meta.url), "utf8");
  for (const id of ["matrix", "hello-kitty", "liquid-glass", "windows-95", "dark", "light"]) {
    assert.match(theme, new RegExp(`id: "${id}"`));
  }
  assert.match(theme, /name: "Матрица"/);
  assert.match(dashboard, /localStorage\.getItem\(themeStorageKey\)/);
  assert.match(dashboard, /document\.documentElement\.dataset\.theme = theme/);
  assert.match(dashboard, /localStorage\.setItem\(themeStorageKey, theme\)/);
  assert.match(layout, /localStorage\.getItem/);
  assert.match(layout, /document\.documentElement\.dataset\.theme=value/);
  assert.match(layout, /suppressHydrationWarning/);
  assert.match(settings, /role="radiogroup"/);
  assert.match(settings, /"appearance"/);
  assert.match(settings, /label="Оформление"/);
  assert.match(settings, /activeTab === "appearance"/);
  assert.match(settings, /onTheme\(item\.id\)/);
  assert.match(backdrop, /<MatrixRain \/>/);
});

test("theme bootstrap does not flash Matrix and rain resumes without catch-up", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const rain = await readFile(new URL("../app/ui/MatrixRain.tsx", import.meta.url), "utf8");
  assert.match(dashboard, /useLayoutEffect/);
  assert.match(dashboard, /const \[themeReady, setThemeReady\]/);
  assert.match(dashboard, /themeReady && !settingsOpen/);
  assert.match(dashboard, /if \(!themeReady\) return/);
  assert.doesNotMatch(dashboard, /requestAnimationFrame\(\(\) => setTheme/);
  assert.match(rain, /if \(document\.hidden\) \{[\s\S]*cancelAnimationFrame\(frame\)/);
  assert.match(rain, /lastFrame = window\.performance\.now\(\)/);
  assert.match(rain, /if \(!running\) return/);
});

test("light skins keep diagnostics and power controls readable", async () => {
  const styles = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  assert.match(styles, /\.pool-audit[^}]+background: var\(--surface-soft\)/);
  assert.match(styles, /data-theme="light"[^}]+\.power-button\.is-on/);
  assert.match(styles, /data-theme="windows-95"[^}]+\.power-halo::before,[\s\S]+display: none/);
  assert.match(styles, /data-theme="windows-95"[^}]+\.hero-copy[\s\S]+color: #fff/);
  for (const asset of ["flowers.svg", "standing.svg", "sitting.svg", "logo.svg"]) {
    assert.match(styles, new RegExp(`/themes/hello-kitty/${asset.replace(".", "\\.")}`));
    await readFile(new URL(`../public/themes/hello-kitty/${asset}`, import.meta.url));
  }
});

test("inbound VPN uses the managed Call Server instead of external WireGuard tools", async () => {
  const panel = await readFile(new URL("../app/ui/CallServerPanel.tsx", import.meta.url), "utf8");
  const backend = await readFile(new URL("../../internal/callserver/backend_xray_linux.go", import.meta.url), "utf8");
  assert.match(panel, /callServerError\(state\.status\.last_error\)/);
  assert.match(panel, /OrcheRoute VPN Server/);
  assert.doesNotMatch(panel, /wireguard-tools|iptables/);
  assert.match(backend, /xraycore\.StartInstance/);
});

test("inbound VPN subscription copy works outside the secure Clipboard API", async () => {
  const panel = await readFile(new URL("../app/ui/CallServerPanel.tsx", import.meta.url), "utf8");
  const clipboard = await readFile(new URL("../app/lib/clipboard.ts", import.meta.url), "utf8");
  assert.match(panel, /copyText\(await loadSubscriptionURL\(\)\)/);
  assert.match(panel, /Показать ссылку/);
  assert.match(panel, /target="_blank"/);
  assert.match(panel, /Скопировано/);
  assert.match(panel, /Ошибка копирования/);
  assert.match(clipboard, /window\.isSecureContext/);
  assert.match(clipboard, /document\.execCommand\("copy"\)/);
  assert.match(clipboard, /setSelectionRange\(0, field\.value\.length\)/);
});

test("Call profiles are saved as ordinary vkcall server sources before activation", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  assert.doesNotMatch(api, /beginVkCallProfile/);
  assert.match(settings, /\^orcheroute:\\\/\\\/call\\\//);
  assert.match(settings, /proxyLinkPattern\.test\(value\) \|\| callProfilePattern\.test\(value\)/);
  assert.doesNotMatch(settings, /Подключить профиль/);
  assert.doesNotMatch(settings, /if \(callProfile\) \{\s*setScanError/);
  assert.match(runtime, /"vkcall"\.equals\(proxy\.optString\("type"\)\)/);
  assert.match(runtime, /requester\.request\(profile\)/);
  assert.match(runtime, /if \(isVKCallNode\(node\)\)/);
  assert.match(activity, /requestVpnPermissionForReload/);
  assert.match(repository, /"activation_required", true/);
  assert.match(settings, /node\.activation_required/);
  assert.match(activity, /MainActivity\.this::beginVkCallProfile/);
  assert.doesNotMatch(activity, /public void beginVkCallProfile\(String profile\)/);
});

test("call server accepts a domain, tests VK and exports profile QR", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const panel = await readFile(new URL("../app/ui/CallServerPanel.tsx", import.meta.url), "utf8");
  assert.match(api, /call-server\/auto-configure/);
  assert.match(api, /call-server\/test/);
  assert.match(panel, /Построить тракт/);
  assert.match(panel, /IP позже можно заменить доменным именем/);
  assert.match(panel, /Проверить VK Call/);
  assert.match(panel, /Публичный IP или доменное имя и UDP-порт/);
  assert.match(panel, /placeholder="Необязательно"/);
  assert.match(panel, /Файл и QR работают без домена/);
  assert.match(panel, /QRCode\.toDataURL\(await loadSubscriptionURL\(\)/);
  assert.match(panel, /Показать QR/);
  assert.doesNotMatch(panel, /Обычные протоколы|обычные VPN-протоколы/);
});
