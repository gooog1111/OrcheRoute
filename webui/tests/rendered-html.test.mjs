import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("exports the OrcheRoute dashboard", async () => {
  const html = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
  assert.match(html, /<html lang="ru">/i);
  assert.match(html, /<title>OrcheRoute<\/title>/i);
  assert.match(html, /class="matrix-rain"/i);
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
  assert.match(dashboard, /\[refresh, settingsOpen\]/);
});

test("embedded settings use the expanded workspace and hide web publication controls", async () => {
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(css, /settings-modal-wide[^}]*1480px/);
  assert.match(css, /height:\s*min\(1080px, calc\(100dvh - 32px\)\)/);
  assert.match(settings, /!desktopMode\s*&&\s*\(\s*<Tab\s+active=\{activeTab === "access"\}/);
});

test("exposes separate subscription refresh and cached server checks", async () => {
  const api = await readFile(new URL("../app/lib/api.ts", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(api, /\/v1\/subscriptions\/check/);
  assert.match(settings, /Проверить серверы/);
  assert.match(settings, /Только аварийный список серверов/);
});

test("uses an embedded subscription delete confirmation", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(settings, /window\.confirm/);
  assert.match(settings, /role="alertdialog"/);
  assert.match(settings, /actions\.deleteSubscription\(deleting\.id\)/);
});

test("renders automatic/manual modes and an emergency-only option", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(settings, /proxy\.mode === "auto" \? "selected"/);
  assert.match(settings, /checked=\{data\?\.status\.proxy\.mode === "emergency"\}/);
  assert.match(settings, /В автоматическом режиме аварийный список серверов и так используется/);
  assert.match(settings, /<strong>Ручной режим<\/strong>/);
  assert.doesNotMatch(settings, /<strong>Ручной сервер<\/strong>/);
});

test("android qualification checks the complete parsed set in ordered stages", async () => {
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.doesNotMatch(runtime, /sampleProxies/);
  assert.match(runtime, /engineTestTCP/);
  assert.match(runtime, /engineTestTCP\(batch\.toString\(\), 2000, 128\)/);
  assert.match(runtime, /engineTestProxiesMulti/);
  assert.match(runtime, /final int urlBatchSize = 80/);
  assert.match(runtime, /engineTestProxiesMulti\(batch\.toString\(\), testURLs\.toString\(\), 3000, 80\)/);
  assert.match(runtime, /effectivePolicy\.optInt\("url_limit", 0\)/);
  assert.match(runtime, /effectivePolicy\.optInt\("speed_candidates", 0\)/);
  assert.match(runtime, /effectivePolicy\.optInt\("keep", 0\)/);
  assert.match(runtime, /engineSpeedAvailable/);
  assert.match(runtime, /engineTestSpeed/);
  assert.match(runtime, /engineTestSpeedAdaptive/);
  assert.match(runtime, /baselineMbps \* 0\.10/);
  assert.match(runtime, /speed_candidates_per_source/);
  assert.match(runtime, /Speed-test пропущен/);
});

test("android connectivity monitor binds DNS and HTTP to a physical network", async () => {
  const monitor = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/ConnectivityMonitor.java", import.meta.url), "utf8");
  assert.match(monitor, /NET_CAPABILITY_NOT_VPN/);
  assert.match(monitor, /network\.openConnection/);
  assert.match(monitor, /Mobilecore\.connectivityTargets/);
  assert.match(monitor, /Mobilecore\.classifyConnectivity/);
  assert.match(monitor, /Mobilecore\.confirmConnectivity/);
  assert.match(monitor, /observed=[\s\S]*confirmed=[\s\S]*candidate=[\s\S]*streak=/);
  assert.doesNotMatch(monitor, /probeConnectivity/);
});

test("android whitelist scan finishes before VPN connect and checks one source contextually", async () => {
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  const repository = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRepository.java", import.meta.url), "utf8");
  assert.match(runtime, /allowlistRouteOverride[\s\S]*scheduleRefresh\(subscriptionId, true, null, true, false, false\)/);
  assert.match(runtime, /scheduleRefresh\(subscriptionId, true, null, true, false, false\)/);
  assert.doesNotMatch(runtime, /Найден доступный сервер, подключаемся/);
  assert.match(repository, /\.put\("selected", !whitelistMode && poolSelected\)/);
  assert.match(repository, /\.put\("selected", whitelistMode\)/);
});

test("android settings expose a signed GitHub release updater without transport explanation", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const updater = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/AppUpdater.java", import.meta.url), "utf8");
  assert.doesNotMatch(settings, /Один транспорт для исходящих соединений/);
  assert.doesNotMatch(settings, />\s*Проверить версию\s*</);
  assert.doesNotMatch(settings, /Ядро обновляется с приложением/);
  assert.match(settings, /Обновление OrcheRoute/);
	assert.match(updater, /releases\/latest\/download\/android-update\.json/);
  assert.match(updater, /SHA-256 загруженного APK не совпадает/);
  assert.match(updater, /Сертификат подписи APK не совпадает/);
});

test("android updater exposes an explicit prerelease channel with a warning", async () => {
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const updater = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/AppUpdater.java", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  assert.match(settings, /Обновиться до Beta/);
  assert.match(settings, /Это тестовая сборка с непроверенными изменениями VPN-автоматики/);
  assert.match(settings, /Понимаю риск, установить Beta/);
	assert.match(updater, /releases\/download\/android-beta\/android-update\.json/);
	assert.match(updater, /Канал manifest не совпадает/);
  assert.match(activity, /installBetaAppUpdate/);
});

test("android prerelease builds use the red interface accent without changing stable colors", async () => {
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  const rain = await readFile(new URL("../app/ui/MatrixRain.tsx", import.meta.url), "utf8");
  const activity = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MainActivity.java", import.meta.url), "utf8");
  assert.match(css, /html\.prerelease-theme[\s\S]*--accent: #ff5b61/);
  assert.match(css, /:root[\s\S]*--accent: #45f3c2/);
  assert.match(rain, /prerelease-theme/);
  assert.match(activity, /BuildConfig\.VERSION_NAME\.contains\("-"\)/);
  assert.match(activity, /class=\\"prerelease-theme\\"/);
});

test("android prerelease branding adds a beta header and red launcher icon", async () => {
  const dashboard = await readFile(new URL("../app/ui/Dashboard.tsx", import.meta.url), "utf8");
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  const gradle = await readFile(new URL("../../android/app/build.gradle", import.meta.url), "utf8");
  const manifest = await readFile(new URL("../../android/app/src/main/AndroidManifest.xml", import.meta.url), "utf8");
  const icon = await readFile(new URL("../../android/app/src/main/res/drawable/ic_launcher.xml", import.meta.url), "utf8");
  assert.match(dashboard, /className="brand-beta">BETA/);
  assert.match(css, /html\.prerelease-theme \.brand-beta \{ display: inline-flex/);
  assert.match(gradle, /orcheRoutePrerelease \? "#FF5B61" : "#45F3C2"/);
  assert.match(gradle, /orcheRoutePrerelease \? "OrcheRoute BETA" : "OrcheRoute"/);
  assert.match(manifest, /android:label="@string\/app_name"/);
  assert.match(icon, /@color\/launcher_accent/);
});

test("android locks portrait mode and settings save only changed drafts", async () => {
  const manifest = await readFile(new URL("../../android/app/src/main/AndroidManifest.xml", import.meta.url), "utf8");
  const settings = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(manifest, /android:screenOrientation="portrait"/);
  assert.match(settings, /disabled=\{busy \|\| !policy \|\| !policyChanged\}/);
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
  const service = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.match(dashboard, /proxy_ok: "Подключено"/);
  assert.match(dashboard, /enabled \? "OrcheRoute включён"/);
  assert.doesNotMatch(dashboard, /Соединение под контролем/);
  assert.doesNotMatch(dashboard, /Автоматика продолжит/);
  assert.match(dashboard, /<span>Direct<\/span>/);
  assert.match(dashboard, /<span>Proxy<\/span>/);
  assert.match(dashboard, /android \? 1000 : 5000/);
  assert.match(api, /loadLiveDashboard/);
  assert.match(service, /network == null \? url\.openConnection\(\) : network\.openConnection\(url\)/);
  assert.match(service, /Mobilecore\.parseConnectionIdentity/);
  assert.match(runtime, /\.put\("identity", new JSONObject\(directIdentity\.toString\(\)\)\)/);
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
  assert.match(settings, /Доступно при белых списках/);
  assert.match(settings, /Доступно в обычном интернете/);
  assert.match(settings, /Speed-test аварийной подписки/);
  assert.match(settings, /speed_candidates_per_source/);
});

test("android exposes selectable geo sources and applies the selected source", async () => {
  const runtime = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.match(runtime, /Mobilecore\.geoSources\(\)/);
  assert.match(runtime, /Mobilecore\.geoCatalog/);
  assert.match(runtime, /Mobilecore\.resolveGeoSource/);
  assert.match(runtime, /Mobilecore\.updateGeoFromSource/);
  assert.match(runtime, /installed_geo_source/);
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
  assert.match(runtime, /refreshUnavailable\(id, links, "url_unavailable", urlSource\.length\(\)\)/);
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
