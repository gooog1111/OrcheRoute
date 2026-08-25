package online.gooog1111.orcheroute;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.LinkAddress;
import android.net.LinkProperties;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.io.File;
import java.util.Locale;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import mobilecore.Mobilecore;
import mobilecore.QualificationObserver;

/**
 * Process-local Android adapter for the versioned OrcheRoute UI contract.
 * It deliberately does not establish a TUN until a Mihomo engine is attached.
 */
final class MobileRuntime {
    interface PermissionRequester { void request(); }

    private static volatile MobileRuntime instance;
    private final Context context;
    private final MobileRepository repository;
    private final ConnectivityMonitor connectivityMonitor;
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private String state = "disabled";
    private String message = "OrcheRoute выключен";
    private boolean desiredEnabled;
    private boolean refreshActive;
    private boolean refreshAllowlistScan;
    private volatile boolean refreshCancelRequested;
    private String refreshStatus = "idle";
    private String refreshPhase = "idle";
    private String refreshMessage = "";
    private String refreshError = "";
    private int refreshCurrent;
    private int refreshTotal;
    private long refreshUpdatedAt;
    private String detectedInternetMode = "unknown";
    private boolean allowlistRouteOverride;
    private boolean allowlistWorkingFound;
    private long allowlistLastScanAt;
    private boolean whitelistConnectPending;
	private int whitelistHealthSuccesses;
	private boolean vpnPermissionGranted;
    private String connectedNodeID = "";
	private boolean controllerQualificationActive;
    private boolean componentActive;
    private String componentStatus = "idle";
    private String componentPhase = "idle";
    private String componentMessage = "";
    private String componentError = "";
    private long componentCurrent;
    private long componentTotal;
    private long componentUpdatedAt;
    private String networkApplyStatus = "applied";
    private String networkApplyMessage = "Сетевой профиль применён";
    private String networkApplyError = "";
    private long networkApplyUpdatedAt;
    private int networkApplyRevision = 1;
    private JSONObject directIdentity = new JSONObject();
    private JSONObject proxyIdentity = new JSONObject();

    private MobileRuntime(Context context) {
        this.context = context.getApplicationContext();
        this.repository = new MobileRepository(this.context);
		this.desiredEnabled = repository.serviceDesired();
        this.connectivityMonitor = new ConnectivityMonitor(this.context, () -> {
            JSONObject defaults = repository.qualificationPolicy().getJSONObject("defaults");
            return new ConnectivityMonitor.Settings(
                    defaults.optString("allowlist_probe_url", "https://ya.ru/"),
                    defaults.optString("open_internet_probe_url", "https://www.cloudflare.com/cdn-cgi/trace"),
                    repository.activeTransport());
        }, this::onConnectivityChanged);
        // A process killed during a scan cannot have a live worker after the
        // restart. Clear only the stale activity marker; keep the working pool.
        try { repository.completeWhitelistScan(); } catch (JSONException ignored) { }
        allowlistWorkingFound = repository.whitelistCount() > 0;
        try {
            JSONObject settings = repository.componentSettings();
            GeoUpdateScheduler.apply(this.context, settings.optBoolean("geo_auto_update", true), settings.optInt("geo_interval_hours", 24));
        } catch (JSONException ignored) { }
        this.connectivityMonitor.start();
        initializeQualificationTransport();
    }

    private void initializeQualificationTransport() {
        File home = context.getDir("mihomo", Context.MODE_PRIVATE);
        try {
            JSONObject result = new JSONObject(Mobilecore.engineInit(
                    home.getAbsolutePath(), fd -> bindPhysicalSocket((int) fd)));
            if (!result.optBoolean("ok")) {
                Log.w("OrcheRouteEngine", "Qualification transport initialization failed: " + coreError(result));
            }
        } catch (Throwable error) {
            Log.w("OrcheRouteEngine", "Qualification transport initialization failed", error);
        }
    }

    private boolean bindPhysicalSocket(int fd) {
        Network network = connectivityMonitor.activePhysicalNetwork();
        if (network == null) return false;
        try (ParcelFileDescriptor duplicate = ParcelFileDescriptor.fromFd(fd)) {
            network.bindSocket(duplicate.getFileDescriptor());
            return true;
        } catch (Exception error) {
            Log.w("OrcheRouteEngine", "Unable to bind qualification socket to selected physical network", error);
            return false;
        }
    }

    static MobileRuntime get(Context context) {
        MobileRuntime value = instance;
        if (value == null) {
            synchronized (MobileRuntime.class) {
                value = instance;
                if (value == null) instance = value = new MobileRuntime(context);
            }
        }
        return value;
    }

    synchronized void onPermissionRequired() {
		vpnPermissionGranted = false;
		setDesiredEnabled(true);
        state = "permission_required";
        message = "Подтвердите системное разрешение Android на создание VPN";
    }

    synchronized void onPermissionGranted() {
		vpnPermissionGranted = true;
		setDesiredEnabled(true);
        state = "starting";
        message = "Разрешение получено, запускается VPN-служба";
    }

    synchronized void onPermissionDenied() {
		vpnPermissionGranted = false;
		setDesiredEnabled(false);
        state = "error";
        message = "Разрешение Android на создание VPN не предоставлено";
    }

    synchronized void onEngineUnavailable() {
		setDesiredEnabled(false);
        state = "error";
        message = "Нативное ядро Mihomo ещё не встроено в эту сборку";
    }

    synchronized void onEngineError(String detail) {
        // A node may fail exactly while an allowlist rescan is looking for a
        // replacement. Keep the user's ON intent so the first verified node
        // can restart the service without another tap.
		setDesiredEnabled(allowlistRouteOverride && refreshActive);
        state = "error";
        whitelistConnectPending = false;
        connectedNodeID = "";
        proxyIdentity = new JSONObject();
        message = detail == null || detail.isEmpty() ? "Не удалось запустить Mihomo" : detail;
        if ("applying".equals(networkApplyStatus)) {
            networkApplyStatus = "failed"; networkApplyError = message; networkApplyMessage = "Не удалось применить транспорт или DNS"; networkApplyUpdatedAt = now();
        }
    }

    synchronized void onDirectTestConnected() {
		vpnPermissionGranted = true;
		setDesiredEnabled(true);
        state = "direct_test";
        message = "Mihomo подключён к Android TUN в диагностическом режиме DIRECT";
        finishNetworkApply();
    }

    synchronized void onProxyConnected(String nodeName, String nodeID) {
		vpnPermissionGranted = true;
		setDesiredEnabled(true);
        state = "connected";
        whitelistConnectPending = false;
        if (!connectedNodeID.equals(nodeID == null ? "" : nodeID)) proxyIdentity = new JSONObject();
        connectedNodeID = nodeID == null ? "" : nodeID;
        if (allowlistRouteOverride) {
            try { repository.confirmWhitelistNode(connectedNodeID); } catch (JSONException error) { refreshError = readable(error); }
        }
        try { repository.confirmConnectedNode(connectedNodeID, allowlistRouteOverride); }
        catch (JSONException error) { refreshError = readable(error); }
        message = "Подключено через " + nodeName;
        finishNetworkApply();
    }

    private void finishNetworkApply() {
        if (!"applying".equals(networkApplyStatus)) return;
        networkApplyStatus = "applied"; networkApplyError = ""; networkApplyMessage = "Транспорт и DNS применены, VPN запущен"; networkApplyUpdatedAt = now();
    }

    synchronized void onDisabled() {
		setDesiredEnabled(false);
        state = "disabled";
        whitelistConnectPending = false;
        connectedNodeID = "";
        proxyIdentity = new JSONObject();
        message = "OrcheRoute выключен";
    }

    synchronized void onStopping() {
			setDesiredEnabled(true);
        state = "stopping";
        message = "Закрывается Android TUN и останавливается Mihomo";
    }

    Network identityPhysicalNetwork() { return connectivityMonitor.activePhysicalNetwork(); }

    synchronized void updateConnectionIdentity(String route, JSONObject identity) throws JSONException {
        JSONObject value = identity == null ? new JSONObject() : new JSONObject(identity.toString());
        value.put("updated_at", now());
        if ("direct".equals(route)) directIdentity = value;
        else if ("proxy".equals(route)) proxyIdentity = value;
    }

    synchronized String request(String method, String path, String body, PermissionRequester permissionRequester) {
        String verb = method == null ? "GET" : method.toUpperCase(Locale.ROOT);
        try {
            if ("GET".equals(verb) && "/v1/status".equals(path)) return response(200, status());
            if ("GET".equals(verb) && "/v1/pools".equals(path)) return response(200, new JSONObject().put("pools", repository.pools(allowlistRouteOverride)));
            if ("GET".equals(verb) && "/v1/nodes".equals(path)) return response(200, new JSONObject().put("nodes", repository.nodes()));
            String poolNodeId = entityId(path, "/v1/nodes/");
            if ("DELETE".equals(verb) && poolNodeId != null) return deletePoolNode(poolNodeId);
            if ("GET".equals(verb) && "/v1/subscriptions".equals(path)) return response(200, new JSONObject().put("subscriptions", repository.subscriptions()));
            if ("GET".equals(verb) && "/v1/operations".equals(path)) return response(200, operations());
            if ("POST".equals(verb) && "/v1/operations/subscription-update/cancel".equals(path)) {
                return response(202, cancelRefresh());
            }
            if ("POST".equals(verb) && "/v1/whitelist/scan".equals(path)) {
                String networkState = connectivityState();
                if (!desiredEnabled) return error(409, "vpn_not_enabled", "Сначала включите VPN");
                if (!"allowlist".equals(networkState)) return error(409, "allowlist_not_detected",
                        "Монитор сети не подтверждает режим белых списков");
                allowlistRouteOverride = true;
                allowlistWorkingFound = repository.whitelistCount() > 0;
                allowlistLastScanAt = 0;
                message = "Вручную формируем список серверов для белых списков";
                return response(202, scheduleRefresh(null, true, null, true, false, false));
            }
            if ("GET".equals(verb) && "/v1/qualification".equals(path)) return response(200, qualification());
            if ("GET".equals(verb) && "/v1/routes".equals(path)) return response(200, repository.routes());
            if ("GET".equals(verb) && "/v1/network/profile".equals(path)) return response(200, repository.networkState());
            if ("GET".equals(verb) && "/v1/network/interfaces".equals(path)) return response(200, networkInterfaces());
            if ("GET".equals(verb) && "/v1/dns".equals(path)) return response(200, repository.dnsState());
            if ("PUT".equals(verb) && "/v1/qualification/policy".equals(path)) {
                JSONObject current = repository.qualificationPolicy();
                JSONObject input = new JSONObject(emptyObject(body));
                JSONObject changes = input.optJSONObject("defaults");
                if (changes != null) {
                    JSONObject defaults = current.getJSONObject("defaults");
                    for (java.util.Iterator<String> keys = changes.keys(); keys.hasNext();) {
                        String key = keys.next(); defaults.put(key, changes.get(key));
                    }
                }
                JSONObject poolChanges = input.optJSONObject("pools");
                if (poolChanges != null) {
                    JSONObject pools = current.getJSONObject("pools");
                    for (java.util.Iterator<String> poolKeys = poolChanges.keys(); poolKeys.hasNext();) {
                        String pool = poolKeys.next();
                        JSONObject target = pools.getJSONObject(pool);
                        JSONObject values = poolChanges.getJSONObject(pool);
                        for (java.util.Iterator<String> keys = values.keys(); keys.hasNext();) {
                            String key = keys.next(); target.put(key, values.get(key));
                        }
                    }
                }
                JSONObject validated = new JSONObject(Mobilecore.validateQualificationPolicy(current.toString()));
                if (!validated.optBoolean("ok")) return error(400, coreError(validated), "Некорректная политика проверки");
                JSONObject policy = validated.getJSONObject("result");
                repository.saveQualificationPolicy(policy);
                return response(200, new JSONObject().put("updated", true).put("policy", policy).put("effective_next_update", true));
            }
            if ("POST".equals(verb) && "/v1/network/validate".equals(path)) {
                JSONObject profile = new JSONObject(emptyObject(body)).getJSONObject("profile");
                repository.validateNetwork(profile);
                return response(200, new JSONObject().put("profile", profile));
            }
            if ("PUT".equals(verb) && "/v1/network/profile".equals(path)) {
                JSONObject desired = repository.saveNetworkProfile(new JSONObject(emptyObject(body)).getJSONObject("profile"));
                networkApplyStatus = "pending"; networkApplyMessage = "Настройки сохранены и ожидают применения"; networkApplyUpdatedAt = now(); networkApplyRevision = desired.optInt("revision");
                return response(200, new JSONObject().put("updated", true).put("desired", desired));
            }
            if ("POST".equals(verb) && "/v1/network/apply".equals(path)) {
                JSONObject active = repository.applyNetwork();
                networkApplyStatus = desiredEnabled ? "applying" : "applied";
                networkApplyMessage = desiredEnabled ? "Перезапускаем VPN с новым транспортом и DNS" : "Транспорт и DNS сохранены для следующего запуска";
                networkApplyError = ""; networkApplyUpdatedAt = now(); networkApplyRevision = active.optInt("revision");
                restartIfEnabled();
                return response(202, new JSONObject().put("accepted", true).put("revision", active.optInt("revision")));
            }
            if ("POST".equals(verb) && "/v1/dns/validate".equals(path)) {
                JSONObject dns = new JSONObject(emptyObject(body)).getJSONObject("dns");
                repository.validateDNSProfile(dns);
                return response(200, new JSONObject().put("config", dns));
            }
            if ("PUT".equals(verb) && "/v1/dns".equals(path)) {
                JSONObject desired = repository.saveDNS(new JSONObject(emptyObject(body)).getJSONObject("dns"));
                networkApplyStatus = "pending"; networkApplyMessage = "DNS сохранён и ожидает применения"; networkApplyUpdatedAt = now(); networkApplyRevision = desired.optInt("revision");
                return response(200, new JSONObject().put("updated", true).put("network_revision", desired.optInt("revision")));
            }
            if ("GET".equals(verb) && "/v1/components".equals(path)) return response(200, components());
            if ("PUT".equals(verb) && "/v1/components/settings".equals(path)) {
                JSONObject input = new JSONObject(emptyObject(body));
                String sourceID = input.optString("geo_source", "metacubex");
                String geoIPURL = input.optString("geoip_url", "").trim();
                String geoSiteURL = input.optString("geosite_url", "").trim();
                JSONObject resolved = new JSONObject(Mobilecore.resolveGeoSource(sourceID, geoIPURL, geoSiteURL));
                if (!resolved.optBoolean("ok")) throw new JSONException(coreError(resolved));
                JSONObject source = resolved.getJSONObject("result");
                JSONObject settings = repository.saveComponentSettings(
                        input.optBoolean("geo_auto_update", true), input.optInt("geo_interval_hours", 24),
                        source.getString("id"), geoIPURL, geoSiteURL);
                GeoUpdateScheduler.apply(context, settings.optBoolean("geo_auto_update"), settings.optInt("geo_interval_hours"));
                return response(200, new JSONObject().put("updated", true));
            }
            if ("POST".equals(verb) && "/v1/components/update".equals(path)) {
                String component = new JSONObject(emptyObject(body)).optString("component", "check");
                if ("check".equals(component)) {
                    updateComponent("success", "complete", "Встроенная версия Mihomo проверена", "", false);
                    return response(202, new JSONObject().put("accepted", true));
                }
                if ("core".equals(component)) return error(409, "embedded_core_updates_with_app", "Встроенное ядро обновляется вместе с APK OrcheRoute");
                if (!"geo".equals(component) && !"all".equals(component)) return error(400, "invalid_component", "Неизвестный компонент");
                return response(202, scheduleGeoUpdate());
            }
            if ("POST".equals(verb) && "/v1/routes/validate".equals(path)) {
                compileRoutes(new JSONObject(emptyObject(body)));
                return response(200, new JSONObject().put("valid", true));
            }
            if ("PUT".equals(verb) && "/v1/routes".equals(path)) {
                JSONObject input = new JSONObject(emptyObject(body));
                JSONObject compiled = compileRoutes(input);
                JSONObject routes = repository.saveRoutes(input.optString("default", "proxy"), input.getJSONObject("lists"), compiled.getJSONObject("stats"));
                restartIfEnabled();
                return response(200, new JSONObject().put("updated", true).put("routes", routes));
            }
            if ("POST".equals(verb) && "/v1/control/auto".equals(path)) {
                repository.setAuto();
                restartIfEnabled();
                return response(200, new JSONObject().put("accepted", true));
            }
            if ("POST".equals(verb) && "/v1/control/emergency".equals(path)) {
                repository.setEmergency();
                JSONObject check = scheduleRefresh(null, true, "emergency");
                restartIfEnabled();
                return response(200, new JSONObject().put("accepted", true).put("check_scheduled", check.optBoolean("accepted")));
            }
            if ("POST".equals(verb) && "/v1/control/manual".equals(path)) {
                String nodeId = new JSONObject(emptyObject(body)).optString("node_id");
                if (repository.select(nodeId) == null) return error(404, "node_not_found", "Сервер не найден");
                restartIfEnabled();
                return response(200, new JSONObject().put("accepted", true));
            }
            if ("POST".equals(verb) && "/v1/subscriptions".equals(path)) {
                JSONObject created = repository.create(new JSONObject(emptyObject(body)));
                return response(201, new JSONObject().put("subscription", created)
                        .put("refresh_scheduled", false).put("refresh_required", true));
            }
            if ("POST".equals(verb) && "/v1/subscriptions/import".equals(path)) {
                JSONArray input = new JSONObject(emptyObject(body)).optJSONArray("subscriptions");
                if (input == null || input.length() == 0 || input.length() > 100) return error(400, "invalid_subscription_batch", "Некорректный список источников");
                JSONArray created = new JSONArray(), skipped = new JSONArray();
                for (int i = 0; i < input.length(); i++) {
                    JSONObject candidate = input.getJSONObject(i);
                    try {
                        created.put(repository.create(candidate));
                    } catch (JSONException error) {
                        if (!"duplicate_subscription".equals(error.getMessage())) throw error;
                        skipped.put(new JSONObject().put("name", candidate.optString("name")).put("reason", "duplicate_subscription"));
                    }
                }
                return response(201, new JSONObject().put("created", created).put("skipped", skipped).put("refresh_scheduled", false).put("refresh_required", created.length() > 0));
            }
            if ("POST".equals(verb) && "/v1/subscriptions/refresh".equals(path)) {
                return response(202, scheduleRefresh(null, false, null));
            }
            if ("POST".equals(verb) && "/v1/subscriptions/check".equals(path)) {
                return response(202, scheduleManualCheck(null));
            }
            if ("PUT".equals(verb) && "/v1/subscriptions/default-emergency".equals(path)) {
                repository.updateDefaultEmergency(new JSONObject(emptyObject(body)).optJSONArray("enabled_ids"));
                return response(200, new JSONObject().put("updated", true)
                        .put("refresh_scheduled", false).put("refresh_required", true));
            }
            String subscriptionId = subscriptionId(path, "/refresh");
            if ("POST".equals(verb) && subscriptionId != null) return response(202, scheduleRefresh(subscriptionId, false, null));
            subscriptionId = subscriptionId(path, "/check");
            if ("POST".equals(verb) && subscriptionId != null) {
                return response(202, scheduleManualCheck(subscriptionId));
            }
            subscriptionId = subscriptionId(path, "/secret");
            if ("POST".equals(verb) && subscriptionId != null) {
                JSONObject item = repository.subscriptionPrivate(subscriptionId);
                if (item == null) return error(404, "subscription_not_found", "Подписка не найдена");
                return response(200, new JSONObject().put("id", subscriptionId).put("secret", item.optString("secret")));
            }
            subscriptionId = subscriptionId(path, "/export");
            if ("POST".equals(verb) && subscriptionId != null) {
                JSONObject item = repository.subscriptionPrivate(subscriptionId);
                if (item == null) return error(404, "subscription_not_found", "Подписка не найдена");
                JSONArray links = item.optJSONArray("cached_links");
                if (links == null && "inline".equals(item.optString("parser"))) {
                    JSONObject decoded = new JSONObject(Mobilecore.decodeSubscriptionBody(item.optString("secret")));
                    links = decoded.optJSONArray("result");
                }
                if (links == null) links = new JSONArray();
                return response(200, new JSONObject().put("id", subscriptionId).put("name", item.optString("name"))
                        .put("parser", item.optString("parser")).put("secret", item.optString("secret")).put("links", links));
            }
            subscriptionId = subscriptionId(path, "");
            if ("PATCH".equals(verb) && subscriptionId != null) {
                JSONObject updated = repository.update(subscriptionId, new JSONObject(emptyObject(body)));
                if (updated == null) return error(404, "subscription_not_found", "Подписка не найдена");
                return response(200, new JSONObject().put("subscription", updated)
                        .put("refresh_scheduled", false).put("refresh_required", updated.optBoolean("enabled", true)));
            }
            if ("DELETE".equals(verb) && subscriptionId != null) {
                return response(200, new JSONObject().put("deleted", repository.delete(subscriptionId)));
            }
            if ("POST".equals(verb) && "/v1/service/enable".equals(path)) {
				setDesiredEnabled(true);
                state = "permission_required";
                message = "Ожидается системное разрешение Android";
                permissionRequester.request();
                return response(202, new JSONObject().put("accepted", true).put("enabled", true));
            }
            if ("POST".equals(verb) && "/v1/service/disable".equals(path)) {
                onStopping();
                OrcheRouteVpnService.stop(context);
                return response(200, new JSONObject().put("accepted", true).put("enabled", false));
            }
            if ("GET".equals(verb) && "/healthz".equals(path)) {
                return response(200, new JSONObject().put("ok", true).put("platform", "android"));
            }
            return error(501, "mobile_endpoint_not_implemented", "Этот раздел Android runtime ещё не реализован");
        } catch (JSONException error) {
            return error(500, "mobile_runtime_encoding_failed", error.getMessage());
        }
    }

    synchronized EngineProfile engineProfile() throws Exception {
        JSONObject node = repository.activeNode(allowlistRouteOverride);
        if (node == null) {
            if (allowlistRouteOverride) throw new IllegalStateException("Формируется список серверов для белых списков");
            return new EngineProfile(null, null, null, null);
        }
        JSONObject built = new JSONObject(Mobilecore.buildMobileProxyConfigWithNetwork(
                node.getJSONObject("proxy").toString(), repository.routesForEngine(allowlistRouteOverride), repository.activeDNSForEngine()));
        if (!built.optBoolean("ok")) throw new IllegalStateException(coreError(built));
        String config = built.getJSONObject("result").getString("config");
        return new EngineProfile(config, node.optString("display_name"), node.optString("id"), node.optString("pool"));
    }

    static final class EngineProfile {
        final String config;
        final String nodeName;
        final String nodeID;
        final String pool;
        EngineProfile(String config, String nodeName, String nodeID, String pool) { this.config = config; this.nodeName = nodeName; this.nodeID = nodeID; this.pool = pool; }
        boolean proxy() { return config != null; }
    }

    private JSONObject scheduleRefresh(String onlyId, boolean checkOnly, String onlyGroup) throws JSONException {
        return scheduleRefresh(onlyId, checkOnly, onlyGroup, false);
    }

    private JSONObject scheduleManualCheck(String onlyId) throws JSONException {
        boolean restricted = "allowlist".equals(connectivityState());
        if (restricted) enterAllowlistMode();
        return restricted
                ? scheduleRefresh(onlyId, true, null, true, false, false)
                : scheduleRefresh(onlyId, true, null);
    }

    private JSONObject scheduleRefresh(String onlyId, boolean checkOnly, String onlyGroup, boolean allowlistScan) throws JSONException {
        return scheduleRefresh(onlyId, checkOnly, onlyGroup, allowlistScan, allowlistScan, allowlistScan);
    }

    private JSONObject scheduleRefresh(String onlyId, boolean checkOnly, String onlyGroup, boolean allowlistScan,
                                       boolean resetWhitelistPool, boolean refreshSubscriptionsAfter) throws JSONException {
        if (refreshActive) return new JSONObject().put("accepted", false).put("already_running", true);
        JSONArray items = repository.enabledSubscriptions(onlyId, onlyGroup);
        if (onlyId != null && items.length() == 0) return new JSONObject().put("accepted", false).put("missing_or_disabled", true);
        refreshActive = true; refreshAllowlistScan = allowlistScan; refreshStatus = "queued"; refreshPhase = "queued";
        refreshCancelRequested = false;
        refreshMessage = checkOnly ? "Проверка серверов поставлена в очередь" : "Обновление подписок поставлено в очередь"; refreshError = "";
        refreshCurrent = 0; refreshTotal = items.length(); refreshUpdatedAt = now();
        if (allowlistScan && resetWhitelistPool) repository.beginWhitelistScan();
        worker.execute(() -> refresh(items, checkOnly, allowlistScan, refreshSubscriptionsAfter));
        return new JSONObject().put("accepted", true);
    }

    private void refresh(JSONArray items, boolean checkOnly, boolean allowlistScan, boolean refreshSubscriptionsAfter) {
        int success = 0;
        int unavailable = 0;
        String lastError = "";
        try {
            updateRefresh("running", checkOnly ? "url_test" : "fetch", checkOnly ? "Проверяем сохранённые серверы" : "Получаем подписки", 0, items.length(), "");
            // Use exactly the same physical-underlay classifier as the VPN
            // service. The previous duplicate probe converted an already
            // detected restricted network back to "offline" and aborted scans.
            String connectivityState = connectivityState();
            if ("offline".equals(connectivityState)) {
                updateRefresh("success", "offline", "Интернет недоступен. Статусы серверов не изменены.", 0, items.length(), "");
                return;
            }
            updateRefresh("running", "connectivity", "allowlist".equals(connectivityState)
                    ? "Обнаружен режим белых списков" : "Доступен обычный интернет", 0, items.length(), "");
            for (int i = 0; i < items.length(); i++) {
                ensureRefreshContinues(allowlistScan);
                JSONObject item = items.getJSONObject(i);
                String id = item.getString("id");
                // A per-source allowlist check must always write its qualified
                // nodes to the derived allowlist list. The monitor already
                // validated the mode before this loop; tying persistence to a
                // captured label caused successful manual checks to update only
                // the normal list after a transient monitor transition.
                boolean restrictedScan = allowlistScan;
                updateRefresh("running", checkOnly ? "parse" : "fetch",
                        (checkOnly ? "Подготавливаем сохранённые серверы «" : "Загружается «") + item.optString("name") + "»",
                        i, items.length(), "");
                try {
                    JSONArray links;
                    if (checkOnly) {
                        links = item.optJSONArray("cached_links");
                        if (links == null || links.length() == 0) throw new IllegalStateException("Нет сохранённых серверов. Сначала обновите подписку");
                    } else {
                        JSONObject fetched = new JSONObject(Mobilecore.fetchSubscription(
                                item.optString("parser", "standard"), item.optString("secret"), new java.io.File(context.getFilesDir(), "subscriptions").getAbsolutePath()));
                        if (!fetched.optBoolean("ok")) throw new IllegalStateException(coreError(fetched));
                        JSONObject fetchResult = fetched.getJSONObject("result");
                        links = fetchResult.getJSONArray("links");
                        repository.updateDetectedParser(id, fetchResult.optString("parser", item.optString("parser", "standard")));
                        repository.cacheRefreshSucceeded(id, links);
						if (!controllerQualificationActive) {
							success++;
							continue;
						}
                    }
                    JSONObject parsed = new JSONObject(Mobilecore.parseSubscription(links.toString(), sourceKey(id)));
                    if (!parsed.optBoolean("ok")) throw new IllegalStateException(coreError(parsed));
                    JSONArray proxies = parsed.getJSONObject("result").getJSONArray("proxies");
                    if (proxies.length() == 0) throw new IllegalStateException("Подписка не содержит поддерживаемых серверов");
					JSONObject effectivePolicy = effectiveQualification(item.optString("group", "primary"));
					if (restrictedScan) {
						// Region preferences and speed requirements apply to normal
						// Internet only. An allowlist scan retains every endpoint that
						// is reachable through the physical network.
						effectivePolicy.put("excluded_countries", new JSONArray());
						effectivePolicy.put("url_limit", 0);
						effectivePolicy.put("speed_candidates", 0);
						effectivePolicy.put("speed_candidates_per_source", 0);
						effectivePolicy.put("keep", 0);
						effectivePolicy.put("skip_speed", true);
					}
					JSONObject sources = new JSONObject();
					for (int index = 0; index < proxies.length(); index++) {
						String nodeName = proxies.getJSONObject(index).optString("name");
						if (!nodeName.isEmpty()) sources.put(nodeName, new JSONObject()
								.put("id", id).put("name", item.optString("name")));
					}
					final String sourceName = item.optString("name");
					JSONObject qualifiedEnvelope = new JSONObject(Mobilecore.qualifyNodes(
							restrictedScan ? "whitelist" : item.optString("group", "primary"),
							proxies.toString(), effectivePolicy.toString(), sources.toString(),
							new QualificationObserver() {
								@Override public boolean isCancelled() {
									return refreshCancelRequested || (allowlistScan && !isAllowlistModeActive());
								}

								@Override public void onProgress(String stage, long current, long total) {
									int safeCurrent = (int) Math.min(Integer.MAX_VALUE, current);
									int safeTotal = (int) Math.min(Integer.MAX_VALUE, total);
									updateRefresh("running", stage, qualificationProgressMessage(stage, current, total, sourceName), safeCurrent, safeTotal, "");
								}
							}));
					if (!qualifiedEnvelope.optBoolean("ok")) throw new IllegalStateException(coreError(qualifiedEnvelope));
					JSONObject qualificationResult = qualifiedEnvelope.getJSONObject("result");
					JSONArray qualified = qualificationResult.getJSONArray("proxies");
					JSONArray finalTests = qualificationResult.getJSONArray("tests");
					JSONObject report = qualificationResult.getJSONObject("report");
					if (qualified.length() == 0) {
						JSONObject failures = qualificationResult.optJSONObject("failures");
						if (failures != null) {
							int logged = 0;
							java.util.Iterator<String> keys = failures.keys();
							while (keys.hasNext() && logged++ < 12) {
								String failed = keys.next();
								Log.w("OrcheRouteScan", "Qualification rejected " + failed + ": " + failures.optString(failed));
							}
						}
						String reason = report.optInt("tcp_alive") == 0 ? "tcp_unavailable"
								: report.optInt("url_alive") == 0 ? "url_unavailable"
								: report.optBoolean("geo_enabled") && report.optInt("geo_passed") == 0 ? "country_excluded"
								: "speed_unavailable";
						ensureRefreshContinues(allowlistScan);
						if (restrictedScan) repository.replaceWhitelistSource(id, new JSONArray(), new JSONArray());
                        else repository.refreshUnavailable(id, links, reason, proxies.length(), !checkOnly);
						updateRefresh("running", "qualification", "Подходящих серверов нет · «" + sourceName + "»", proxies.length(), proxies.length(), reason);
						success++;
						unavailable++;
						continue;
					}
                    ensureRefreshContinues(allowlistScan);
                    if (restrictedScan) repository.replaceWhitelistSource(id, qualified, finalTests);
                    else repository.refreshSucceeded(id, qualified, finalTests, links, proxies.length(), !checkOnly);
                    success++;
                    // Do not start the VPN from a partially built pool. The
                    // remaining probes must keep using the same physical
                    // underlay; connection begins only after every selected
                    // source has completed below.
                } catch (Throwable error) {
                    if (error instanceof RefreshStopped) throw (RefreshStopped) error;
                    lastError = readable(error);
                    repository.refreshFailed(id, lastError);
                }
            }
            ensureRefreshContinues(allowlistScan);
            String text = success + " из " + items.length() + (checkOnly ? " источников проверено" : " подписок обновлено")
                    + (unavailable > 0 ? " · без доступных серверов: " + unavailable : "")
                    + ("allowlist".equals(connectivityState) ? " · сеть: белые списки" : " · сеть: обычный интернет");
            String finalError = success < items.length() && items.length() > 0
                    ? (lastError.isEmpty() ? "Часть серверов недоступна" : lastError)
                    : "";
            String finalMessage = finalError.isEmpty() ? text : text + " · " + finalError;
            updateRefresh(success == items.length() ? "success" : "warning", "complete", finalMessage, items.length(), items.length(), finalError);
			if (allowlistScan) {
                int working = repository.whitelistCount();
				if (working == 0) {
					if (desiredEnabled) onWhitelistPoolEmpty();
					else updateRefresh("success", "complete",
							"В выбранной подписке нет серверов, доступных в белых списках",
							items.length(), items.length(), "");
					return;
                }
                allowlistWorkingFound = true;
                updateRefresh("running", "whitelist_pool", "Список для белых списков сформирован: " + working + " серверов", items.length(), items.length(), "");
                boolean connectionConfirmed = false;
                if (desiredEnabled) {
                    if (requestWhitelistConnection()) OrcheRouteVpnService.reload(context);
                    connectionConfirmed = awaitStableWhitelistConnection(45_000);
                }
                ensureRefreshContinues(true);
                if (!connectionConfirmed && repository.whitelistCount() == 0) {
					onWhitelistPoolEmpty(); return;
                }
                if (refreshSubscriptionsAfter && isAllowlistModeActive() && connectionConfirmed) {
                    JSONArray changed = refreshChangedWhitelistSubscriptions(items);
                    if (changed.length() > 0 && isAllowlistModeActive()) {
                        refresh(changed, true, true, false);
                        if (repository.whitelistCount() > 0) restartIfEnabled();
                    }
                }
                updateRefresh("success", "complete", "Список для белых списков готов: " + repository.whitelistCount() + " серверов", items.length(), items.length(), "");
			} else if (checkOnly && success > 0) {
				restartIfEnabled();
			}
        } catch (RefreshStopped stopped) {
            updateRefresh("cancelled", "cancelled", stopped.getMessage(), refreshCurrent, refreshTotal, "");
        } catch (Throwable error) {
            updateRefresh("error", "failed", "Обновление завершилось с ошибкой", refreshCurrent, refreshTotal, readable(error));
        } finally {
            synchronized (this) {
                if (allowlistScan) {
                    try { repository.completeWhitelistScan(); } catch (JSONException ignored) { }
                }
                refreshActive = false; refreshAllowlistScan = false; refreshUpdatedAt = now();
				controllerQualificationActive = false;
                refreshCancelRequested = false;
                if (allowlistScan && allowlistRouteOverride) {
                    allowlistWorkingFound = repository.whitelistCount() > 0;
                    allowlistLastScanAt = now();
                }
            }
        }
    }

    private boolean awaitStableWhitelistConnection(long timeoutMs) throws InterruptedException {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (System.currentTimeMillis() < deadline && isAllowlistModeActive()) {
            if (refreshCancelRequested) return false;
            synchronized (this) {
                if ("connected".equals(state) && whitelistHealthSuccesses >= 2) return true;
            }
            Thread.sleep(500);
        }
        return false;
    }

    private JSONArray refreshChangedWhitelistSubscriptions(JSONArray items) throws Exception {
        JSONArray changed = new JSONArray();
        for (int i = 0; i < items.length() && isAllowlistModeActive(); i++) {
            ensureRefreshContinues(true);
            JSONObject item = items.getJSONObject(i);
            String id = item.getString("id");
            updateRefresh("running", "whitelist_subscriptions", "Обновляем подписку " + (i + 1) + "/" + items.length() + " · «" + item.optString("name") + "»", i, items.length(), "");
            try {
                JSONObject fetched = new JSONObject(Mobilecore.fetchSubscription(item.optString("parser", "standard"),
                        item.optString("secret"), new java.io.File(context.getFilesDir(), "subscriptions").getAbsolutePath()));
                if (!fetched.optBoolean("ok")) throw new IllegalStateException(coreError(fetched));
                ensureRefreshContinues(true);
                JSONObject fetchResult = fetched.getJSONObject("result");
                JSONArray links = fetchResult.getJSONArray("links");
                repository.updateDetectedParser(id, fetchResult.optString("parser", item.optString("parser", "standard")));
                JSONArray previous = item.optJSONArray("cached_links");
                if (previous == null || !previous.toString().equals(links.toString())) {
                    repository.cacheRefreshSucceeded(id, links);
                    changed.put(new JSONObject(item.toString()).put("cached_links", new JSONArray(links.toString())));
                }
            } catch (Throwable error) {
                repository.refreshFailed(id, readable(error));
            }
        }
        return changed;
    }

    private synchronized JSONObject cancelRefresh() throws JSONException {
        if (!refreshActive) return new JSONObject().put("accepted", false).put("active", false);
        refreshCancelRequested = true;
        refreshStatus = "cancelling";
        refreshPhase = "cancelling";
        refreshMessage = "Останавливаем после завершения текущей группы тестов";
        refreshUpdatedAt = now();
        return new JSONObject().put("accepted", true).put("active", true);
    }

    private void ensureRefreshContinues(boolean allowlistScan) throws RefreshStopped {
        if (refreshCancelRequested) throw new RefreshStopped("Операция остановлена пользователем. Завершённые результаты сохранены.");
        if (!allowlistScan) return;
        String current = connectivityState();
        if (!"allowlist".equals(current) || !isAllowlistModeActive()) {
            String message = "normal".equals(current)
                    ? "Обычный интернет восстановлен. Формирование списка серверов остановлено."
                    : "Состояние ограниченной сети изменилось. Формирование списка остановлено.";
            throw new RefreshStopped(message);
        }
    }

    private static final class RefreshStopped extends Exception {
        RefreshStopped(String message) { super(message); }
    }

    private synchronized void updateRefresh(String status, String phase, String message, int current, int total, String error) {
        refreshStatus = status; refreshPhase = phase; refreshMessage = message; refreshCurrent = current; refreshTotal = total; refreshError = error; refreshUpdatedAt = now();
        Log.i("OrcheRouteScan", status + " " + phase + " " + current + "/" + total + " " + message
                + (error == null || error.isEmpty() ? "" : " error=" + error));
    }

    private synchronized JSONObject operations() throws JSONException {
        JSONObject subscription = new JSONObject().put("status", refreshStatus).put("phase", refreshPhase)
                .put("message", refreshMessage).put("current", refreshCurrent).put("total", refreshTotal)
                .put("updated_at", refreshUpdatedAt).put("active", refreshActive)
                .put("allowlist_scan", refreshAllowlistScan).put("connectivity", detectedInternetMode);
        if (!refreshError.isEmpty()) subscription.put("error", refreshError);
        return new JSONObject().put("subscription_update", subscription)
                .put("network_apply", networkOperation())
                .put("component_update", componentOperation());
    }

    private JSONObject qualification() throws JSONException {
        JSONObject policy = repository.qualificationPolicy();
        JSONObject effective = new JSONObject();
        for (String pool : new String[]{"primary", "emergency"}) effective.put(pool, effectiveQualification(pool));
        return new JSONObject().put("policy", policy).put("effective", effective)
                .put("reports", new JSONObject().put("primary", JSONObject.NULL).put("emergency", JSONObject.NULL));
    }

    private JSONObject effectiveQualification(String pool) throws JSONException {
        JSONObject value = new JSONObject(Mobilecore.effectiveQualificationPolicy(repository.qualificationPolicy().toString(), pool));
        if (!value.optBoolean("ok")) throw new JSONException(coreError(value));
        return value.getJSONObject("result");
    }

    private synchronized JSONObject networkOperation() throws JSONException {
        JSONObject result = new JSONObject().put("status", networkApplyStatus).put("message", networkApplyMessage)
                .put("revision", networkApplyRevision).put("updated_at", networkApplyUpdatedAt).put("active", "applying".equals(networkApplyStatus));
        if (!networkApplyError.isEmpty()) result.put("error", networkApplyError);
        return result;
    }

    private JSONObject networkInterfaces() throws JSONException {
        JSONArray result = new JSONArray();
        result.put(transportInfo("auto", "Автоматически", -1));
        result.put(transportInfo("wifi", "Wi-Fi", NetworkCapabilities.TRANSPORT_WIFI));
        result.put(transportInfo("cellular", "Мобильная сеть", NetworkCapabilities.TRANSPORT_CELLULAR));
        result.put(transportInfo("ethernet", "Ethernet", NetworkCapabilities.TRANSPORT_ETHERNET));
        return new JSONObject().put("interfaces", result);
    }

    private JSONObject transportInfo(String name, String kind, int transport) throws JSONException {
        ConnectivityManager manager = (ConnectivityManager) context.getSystemService(Context.CONNECTIVITY_SERVICE);
        JSONArray addresses = new JSONArray();
        boolean up = false;
        if (manager != null) {
            Network[] networks = transport < 0 ? new Network[]{manager.getActiveNetwork()} : manager.getAllNetworks();
            for (Network network : networks) {
                if (network == null) continue;
                NetworkCapabilities capabilities = manager.getNetworkCapabilities(network);
                if (capabilities == null || (transport >= 0 && !capabilities.hasTransport(transport))) continue;
                if (capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) up = true;
                LinkProperties properties = manager.getLinkProperties(network);
                if (properties == null) continue;
                for (LinkAddress address : properties.getLinkAddresses()) {
                    addresses.put(new JSONObject().put("family", address.getAddress().getAddress().length == 4 ? "inet" : "inet6")
                            .put("cidr", address.toString()).put("scope", "global"));
                }
            }
        }
        return new JSONObject().put("name", name).put("kind", kind).put("state", up ? "up" : "down")
                .put("loopback", false).put("addresses", addresses);
    }

    synchronized Network[] selectedUnderlyingNetworks() throws JSONException {
        String transport = repository.activeTransport();
        if ("auto".equals(transport)) return null;
        int required = "wifi".equals(transport) ? NetworkCapabilities.TRANSPORT_WIFI
                : "cellular".equals(transport) ? NetworkCapabilities.TRANSPORT_CELLULAR
                : NetworkCapabilities.TRANSPORT_ETHERNET;
        ConnectivityManager manager = (ConnectivityManager) context.getSystemService(Context.CONNECTIVITY_SERVICE);
        if (manager == null) return new Network[0];
        for (Network network : manager.getAllNetworks()) {
            NetworkCapabilities capabilities = manager.getNetworkCapabilities(network);
            if (capabilities != null && capabilities.hasTransport(required)
                    && capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) return new Network[]{network};
        }
        return new Network[0];
    }

    synchronized boolean ipv6Enabled() throws JSONException { return repository.activeIPv6(); }

    synchronized boolean automaticFailoverEnabled() { return !"manual".equals(repository.mode()); }

    synchronized boolean enterAllowlistMode() {
        boolean changed = !allowlistRouteOverride;
        allowlistRouteOverride = true;
        if (desiredEnabled) message = "Обнаружены белые списки. Ищем доступный сервер без региональных ограничений";
        if (changed) {
            allowlistWorkingFound = repository.whitelistCount() > 0;
            allowlistLastScanAt = 0;
			whitelistHealthSuccesses = 0;
        }
        return changed;
    }

    synchronized boolean isAllowlistModeActive() { return allowlistRouteOverride; }

	synchronized void onAwaitingNetworkDiagnosis() {
		setDesiredEnabled(true);
		state = "waiting_network";
		message = "Определяем состояние физической сети";
	}

	synchronized boolean prepareAllowlistAtStart() {
		enterAllowlistMode();
		if (repository.whitelistCount() > 0) return false;
		state = "waiting_network";
		message = allowlistLastScanAt > 0 && now() - allowlistLastScanAt < 300
				? "В белых списках нет доступных серверов. Повтор через 5 минут"
				: "Формируем список серверов для белых списков";
		if (!refreshActive && (allowlistLastScanAt == 0 || now() - allowlistLastScanAt >= 300)) {
			try { scheduleRefresh(null, true, null, true); }
			catch (JSONException error) { refreshError = readable(error); }
		}
		return true;
	}

    synchronized boolean isWhitelistPoolBuilding() {
        return allowlistRouteOverride && refreshActive && repository.whitelistCount() == 0;
    }

    synchronized boolean requestWhitelistConnection() throws JSONException {
        if (!allowlistRouteOverride || whitelistConnectPending) return false;
        JSONObject candidate = repository.requestWhitelistNode();
        if (candidate == null) return false;
        if ("connected".equals(state) && connectedNodeID.equals(candidate.optString("id"))) return false;
        whitelistConnectPending = true;
		whitelistHealthSuccesses = 0;
		setDesiredEnabled(true);
        state = "starting";
        message = "Подключаемся через " + candidate.optString("display_name") + " из списка для белых списков";
        return true;
    }

    synchronized boolean leaveAllowlistMode() {
        boolean changed = allowlistRouteOverride;
        allowlistRouteOverride = false;
        allowlistWorkingFound = repository.whitelistCount() > 0;
        allowlistLastScanAt = 0;
		whitelistHealthSuccesses = 0;
        try { repository.deactivateWhitelist(); } catch (JSONException ignored) { }
        if (changed && desiredEnabled) message = "Обычный интернет восстановлен. Возвращаем пользовательскую маршрутизацию";
        return changed;
    }

    /** Returns the monitor's last confirmed state without starting network I/O. */
    synchronized String connectivityState() { return connectivityMonitor.snapshot().state; }

	synchronized String[] proxyHealthURLs() {
		try {
			JSONArray configured = repository.qualificationPolicy()
					.getJSONObject("defaults")
					.optJSONArray("url_test_urls");
			if (configured == null || configured.length() == 0) {
				return new String[] { "https://cp.cloudflare.com/generate_204" };
			}
			int count = Math.min(3, configured.length());
			String[] urls = new String[count];
			for (int i = 0; i < count; i++) urls[i] = configured.optString(i, "");
			return urls;
		} catch (JSONException error) {
			return new String[] { "https://cp.cloudflare.com/generate_204" };
		}
	}

    private synchronized void onConnectivityChanged(ConnectivityMonitor.Snapshot previous, ConnectivityMonitor.Snapshot current) {
        detectedInternetMode = current.state;
        Log.i("OrcheRouteNet", "state " + previous.state + " -> " + current.state);
        if (!desiredEnabled) {
			if ("allowlist".equals(current.state)) enterAllowlistMode();
			else if ("normal".equals(current.state) && allowlistRouteOverride) leaveAllowlistMode();
            return;
        }
		try {
			JSONObject input = new JSONObject()
					.put("network_mode", current.state).put("desired_enabled", true)
					.put("connected", "connected".equals(state)).put("whitelist_active", allowlistRouteOverride)
					.put("whitelist_count", repository.whitelistCount()).put("whitelist_scan_active", refreshActive && refreshAllowlistScan)
					.put("whitelist_retry_due", allowlistLastScanAt == 0 || now() - allowlistLastScanAt >= 300);
			JSONObject envelope = new JSONObject(Mobilecore.networkDecision(input.toString()));
			if (!envelope.optBoolean("ok")) throw new JSONException(coreError(envelope));
			String action = envelope.getJSONObject("result").optString("action", "none");
			switch (action) {
				case "pause_offline" -> onUnderlyingOfflineDetected();
				case "start_normal" -> { leaveAllowlistMode(); restartIfEnabled(); }
				case "connect_whitelist" -> {
					enterAllowlistMode();
					if (requestWhitelistConnection()) OrcheRouteVpnService.reload(context);
				}
				case "scan_whitelist" -> {
					enterAllowlistMode();
					scheduleRefresh(null, true, null, true);
					state = "waiting_network";
					message = "Формируем список серверов для белых списков";
					OrcheRouteVpnService.pauseForNetwork(context);
				}
				case "wait_whitelist_scan", "wait_whitelist_retry" -> {
					enterAllowlistMode();
					boolean alreadyWaiting = "waiting_network".equals(state);
					state = "waiting_network";
					message = "wait_whitelist_retry".equals(action)
							? "В белых списках нет доступных серверов. Повтор через 5 минут"
							: "Продолжаем формирование списка серверов";
					if (!alreadyWaiting) OrcheRouteVpnService.pauseForNetwork(context);
				}
				default -> { }
			}
		} catch (JSONException error) {
			refreshError = readable(error);
		}
    }

    synchronized void onRestrictedNetworkDetected() {
        message = "Обнаружены белые списки. Проверяем все сохранённые серверы";
    }

    synchronized void onUnderlyingOfflineDetected() {
		if (!desiredEnabled) return;
		if ("waiting_network".equals(state) && message.startsWith("Нет доступа в интернет")) return;
		state = "waiting_network";
		connectedNodeID = "";
		message = "Нет доступа в интернет. VPN приостановлен до восстановления сети";
		OrcheRouteVpnService.pauseForNetwork(context);
    }

    synchronized void onWhitelistPoolEmpty() {
		setDesiredEnabled(true);
		state = "waiting_network";
        allowlistWorkingFound = false;
        whitelistConnectPending = false;
		message = "В белых списках нет доступных серверов. Повторная проверка через 5 минут";
        refreshError = message;
		allowlistLastScanAt = now();
		OrcheRouteVpnService.pauseForNetwork(context);
    }

    synchronized String failoverWhitelistNode() throws JSONException {
        whitelistConnectPending = false;
		whitelistHealthSuccesses = 0;
        JSONObject next = repository.failoverWhitelistNode();
        if (next != null) {
            whitelistConnectPending = true;
			setDesiredEnabled(true);
            state = "starting";
            message = "Переключаемся на следующий сервер из списка для белых списков";
            return next.optString("display_name");
        }
        allowlistWorkingFound = false;
		if (refreshActive) {
			state = "waiting_network";
			message = "Текущий сервер белых списков недоступен. Продолжаем формирование списка";
			OrcheRouteVpnService.pauseForNetwork(context);
		} else {
			onWhitelistPoolEmpty();
		}
        return "";
    }

    private JSONObject components() throws JSONException {
        File home = new File(context.getFilesDir(), "mihomo");
        JSONObject status = new JSONObject(Mobilecore.geoStatus(home.getAbsolutePath()));
        if (!status.optBoolean("ok")) throw new JSONException(coreError(status));
        JSONObject result = status.getJSONObject("result");
        JSONObject settings = repository.componentSettings();
        JSONObject sources = new JSONObject(Mobilecore.geoSources());
        if (!sources.optBoolean("ok")) throw new JSONException(coreError(sources));
        JSONObject catalog = new JSONObject(Mobilecore.geoCatalog(home.getAbsolutePath()));
        if (!catalog.optBoolean("ok")) throw new JSONException(coreError(catalog));
        long checked = result.optLong("checked_at", now());
        long lastGeo = Math.max(result.getJSONObject("geoip").optLong("updated_at"), result.getJSONObject("geosite").optLong("updated_at"));
        int interval = settings.optInt("geo_interval_hours", 24);
        boolean automatic = settings.optBoolean("geo_auto_update", true);
        String version = result.optString("mihomo_version", "embedded");
        JSONObject mihomo = new JSONObject()
                .put("installed", true).put("version", version).put("installed_version", version)
                .put("latest_version", version).put("update_available", false).put("checked_at", checked)
                .put("release_url", "https://github.com/MetaCubeX/mihomo/releases");
        return new JSONObject().put("mihomo", mihomo)
                .put("geoip", result.getJSONObject("geoip"))
                .put("geosite", result.getJSONObject("geosite"))
                .put("auto_update", automatic).put("interval_hours", interval)
                .put("next_geo_update", automatic ? (lastGeo > 0 ? lastGeo : now()) + interval * 3600L : JSONObject.NULL)
                .put("geo_source", settings.optString("geo_source", "metacubex"))
                .put("geoip_url", settings.optString("geoip_url", ""))
                .put("geosite_url", settings.optString("geosite_url", ""))
                .put("geo_sources", sources.getJSONArray("result"))
                .put("installed_geo_source", settings.has("installed_geo_source")
                        ? settings.getJSONObject("installed_geo_source") : JSONObject.NULL)
                .put("catalog", catalog.getJSONObject("result"))
                .put("operation", componentOperation());
    }

    private JSONObject scheduleGeoUpdate() throws JSONException {
        if (componentActive) return new JSONObject().put("accepted", false).put("already_running", true);
        updateComponent("queued", "queued", "Обновление GEO поставлено в очередь", "", true);
        worker.execute(this::updateGeo);
        return new JSONObject().put("accepted", true);
    }

    private void updateGeo() {
        try {
            updateComponent("running", "download", "Загружаем и проверяем GeoIP и GeoSite", "", true);
            File home = new File(context.getFilesDir(), "mihomo");
            JSONObject settings = repository.componentSettings();
            JSONObject result = new JSONObject(Mobilecore.updateGeoFromSourceWithProgress(home.getAbsolutePath(),
                    settings.optString("geo_source", "metacubex"), settings.optString("geoip_url", ""),
                    settings.optString("geosite_url", ""), (stage, current, total) ->
                            updateComponent("running", stage, geoProgressMessage(stage, current, total), "", true, current, total)));
            if (!result.optBoolean("ok")) throw new IllegalStateException(coreError(result));
            repository.saveInstalledGeoSource(result.getJSONObject("result").getJSONObject("source"));
            updateComponent("success", "complete", "GeoIP и GeoSite обновлены и проверены", "", false);
            restartIfEnabled();
        } catch (Throwable error) {
            updateComponent("error", "rollback", "Обновление GEO отменено, сохранены предыдущие файлы", readable(error), false);
        }
    }

    private synchronized void updateComponent(String status, String phase, String message, String error, boolean active) {
        updateComponent(status, phase, message, error, active, 0, 0);
    }

    private synchronized void updateComponent(String status, String phase, String message, String error, boolean active, long current, long total) {
        componentStatus = status; componentPhase = phase; componentMessage = message;
        componentError = error; componentActive = active; componentCurrent = current; componentTotal = total; componentUpdatedAt = now();
    }

    private synchronized JSONObject componentOperation() throws JSONException {
        JSONObject result = new JSONObject().put("status", componentStatus).put("phase", componentPhase)
                .put("message", componentMessage).put("current", componentCurrent).put("total", componentTotal)
                .put("updated_at", componentUpdatedAt).put("active", componentActive);
        if (!componentError.isEmpty()) result.put("error", componentError);
        return result;
    }

    private static String geoProgressMessage(String stage, long current, long total) {
        String progress = total > 0 ? formatBytes(current) + " / " + formatBytes(total) : formatBytes(current);
        if ("geoip_download".equals(stage)) return "Загружаем GeoIP · " + progress;
        if ("geosite_download".equals(stage)) return "Загружаем GeoSite · " + progress;
        if ("validation".equals(stage)) return "Проверяем геобазы · " + current + " / " + total;
        if ("install".equals(stage)) return "Устанавливаем геобазы · " + current + " / " + total;
        return "Обновляем геобазы · " + progress;
    }

    private static String qualificationProgressMessage(String stage, long current, long total, String sourceName) {
        String progress = current + "/" + total + " · «" + sourceName + "»";
        if ("tcp".equals(stage)) return "TCP-проверка " + progress;
        if ("url_test".equals(stage)) return "URL-test " + progress;
        if ("geo".equals(stage)) return "Определяем регионы " + progress;
        if ("baseline".equals(stage)) return "Измеряем доступную скорость " + progress;
        if ("speed_test".equals(stage)) return "Speed-test " + progress;
        return "Квалификация " + progress;
    }

    private static String formatBytes(long bytes) {
        if (bytes < 1024) return bytes + " Б";
        if (bytes < 1024L * 1024L) return String.format(Locale.ROOT, "%.1f КБ", bytes / 1024.0);
        return String.format(Locale.ROOT, "%.1f МБ", bytes / (1024.0 * 1024.0));
    }

    private static JSONObject compileRoutes(JSONObject input) throws JSONException {
        String defaultAction = input.optString("default", "proxy");
        if (!"proxy".equals(defaultAction) && !"direct".equals(defaultAction) && !"block".equals(defaultAction)) throw new JSONException("invalid_route_default");
        JSONObject lists = input.optJSONObject("lists");
        if (lists == null) throw new JSONException("missing_route_lists");
        JSONObject compiled = new JSONObject(Mobilecore.compileRoutes(lists.toString()));
        if (!compiled.optBoolean("ok")) throw new JSONException(coreError(compiled));
        return compiled.getJSONObject("result");
    }

    private void restartIfEnabled() { if (desiredEnabled) OrcheRouteVpnService.reload(context); }

	synchronized String onProxyHealth(boolean successful) {
		try {
			if (allowlistRouteOverride) {
				whitelistHealthSuccesses = successful ? whitelistHealthSuccesses + 1 : 0;
			}
			repository.recordHealth(connectedNodeID, allowlistRouteOverride, successful);
			if (allowlistRouteOverride || !"normal".equals(connectivityState()) || !"auto".equals(repository.mode())) return "keep";
			JSONObject decision = repository.failoverStep(successful);
			String action = decision.optString("action", "keep");
			if ("refresh".equals(action) && !refreshActive) {
				controllerQualificationActive = true;
				String pool = decision.optString("pool", "");
				JSONObject scheduled = scheduleRefresh(null, false, pool.isEmpty() ? null : pool);
				if (!scheduled.optBoolean("accepted", false)) controllerQualificationActive = false;
			}
			return action;
		}
		catch (JSONException error) {
			controllerQualificationActive = false;
			refreshError = readable(error);
			return "keep";
		}
	}

	private void setDesiredEnabled(boolean enabled) {
		desiredEnabled = enabled;
		try { repository.setServiceDesired(enabled); }
		catch (JSONException error) { refreshError = readable(error); }
	}

    private static String subscriptionId(String path, String suffix) {
        String prefix = "/v1/subscriptions/";
        if (path == null || !path.startsWith(prefix) || (!suffix.isEmpty() && !path.endsWith(suffix))) return null;
        String raw = path.substring(prefix.length(), path.length() - suffix.length());
        if (raw.isEmpty() || raw.contains("/")) return null;
        return Uri.decode(raw);
    }
    private static String entityId(String path, String prefix) {
        if (path == null || !path.startsWith(prefix)) return null;
        String raw = path.substring(prefix.length());
        if (raw.isEmpty() || raw.contains("/")) return null;
        return Uri.decode(raw);
    }

    private synchronized String deletePoolNode(String id) throws JSONException {
        JSONObject deleted = repository.deleteNode(id);
        if (deleted == null) return error(404, "node_not_found", "Сервер не найден");
        String pool = deleted.optString("pool");
        boolean selected = deleted.optBoolean("was_selected");
        if ("whitelist".equals(pool)) allowlistWorkingFound = repository.whitelistCount() > 0;
        if (selected) connectedNodeID = "";
        if (selected && desiredEnabled) {
            if ("whitelist".equals(pool) && allowlistRouteOverride) {
                whitelistConnectPending = false;
                if (requestWhitelistConnection()) {
                    OrcheRouteVpnService.reload(context);
                } else {
                    allowlistWorkingFound = false;
                    onWhitelistPoolEmpty();
                    refreshError = "Список серверов для белых списков пуст после удаления";
                    OrcheRouteVpnService.stopWithError(context);
                }
            } else {
                restartIfEnabled();
            }
        }
        return response(200, new JSONObject().put("deleted", true).put("node", deleted));
    }

    private static String emptyObject(String body) { return body == null || body.trim().isEmpty() ? "{}" : body; }
    private static String sourceKey(String id) { return id.replaceAll("[^A-Za-z0-9]", "").substring(0, Math.min(10, id.replaceAll("[^A-Za-z0-9]", "").length())); }
    private static String readable(Throwable error) { return error.getMessage() == null ? error.getClass().getSimpleName() : error.getMessage(); }
    private static String coreError(JSONObject value) { JSONObject error = value.optJSONObject("error"); return error == null ? "Ошибка ядра" : error.optString("error", "Ошибка ядра"); }
    private static long now() { return System.currentTimeMillis() / 1000L; }

    private JSONObject status() throws JSONException {
        long now = System.currentTimeMillis() / 1000L;
        ConnectivityMonitor.Snapshot connectivitySnapshot = connectivityMonitor.snapshot();
        boolean internet = "normal".equals(connectivitySnapshot.state) || "allowlist".equals(connectivitySnapshot.state);
        String connectivity;
        if ("error".equals(state)) connectivity = "controller_error";
        else if ("connected".equals(state)) connectivity = "proxy_ok";
        else if ("direct_test".equals(state)) connectivity = "mobile_direct";
        else if ("disabled".equals(state)) connectivity = "disabled";
        else connectivity = "starting";

        JSONObject mobile = new JSONObject()
                .put("state", state)
                .put("message", message)
                .put("permission_granted", vpnPermissionGranted)
                .put("engine_ready", true);
        JSONObject network = new JSONObject()
                .put("capture_mode", "system")
                .put("direct_interface", repository.activeTransport())
                .put("vpn_underlay_interface", repository.activeTransport());
        JSONObject active = repository.activeNode(allowlistRouteOverride);
        JSONObject proxy = new JSONObject()
                .put("mode", repository.mode())
                .put("active_node", active == null ? JSONObject.NULL : active.optString("display_name"))
                .put("active_pool", active == null ? JSONObject.NULL : active.optString("pool"))
                .put("failure_streak", 0)
                .put("last_switch", repository.lastSwitch())
                .put("manual_until", 0)
                .put("identity", new JSONObject(proxyIdentity.toString()));
        return new JSONObject()
                .put("version", 1)
                .put("timestamp", now)
                .put("updated_at", now)
                .put("stale", false)
                .put("connectivity", connectivity)
                .put("service", new JSONObject().put("enabled", desiredEnabled))
                .put("wan", new JSONObject().put("interface", "android").put("available", internet)
                        .put("mode", connectivitySnapshot.state)
                        .put("identity", new JSONObject(directIdentity.toString()))
                        .put("diagnostics", connectivitySnapshot.json()))
                .put("network", network)
                .put("proxy", proxy)
                .put("mobile", mobile);
    }

    private static String response(int status, JSONObject body) throws JSONException {
        return new JSONObject().put("status", status).put("body", body).toString();
    }

    private static String error(int status, String code, String message) {
        try {
            JSONObject body = new JSONObject().put("error", code).put("message", message == null ? code : message);
            return new JSONObject().put("status", status).put("body", body).toString();
        } catch (JSONException ignored) {
            return "{\"status\":500,\"body\":{\"error\":\"mobile_runtime_encoding_failed\"}}";
        }
    }
}
