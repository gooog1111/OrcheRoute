package online.gooog1111.orcheroute;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.AtomicFile;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import mobilecore.Mobilecore;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.UUID;

/** Small private JSON registry. Subscription secrets and proxy credentials
 * never leave the application sandbox through list endpoints. */
final class MobileRepository {
    private static final String PREFS = "orcheroute_mobile_state";
    private static final String STATE = "registry_v1";
    private static final String STATE_BACKUP = "registry_v1_backup";
    private static final String STATE_CORRUPT = "registry_v1_corrupt";
    private final SharedPreferences preferences;
    private final AtomicFile snapshot;
    private final AtomicFile previousSnapshot;
    private final File initializedMarker;
    private JSONObject root;

    MobileRepository(Context context) {
        preferences = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        File stateDirectory = new File(context.getNoBackupFilesDir(), "state");
        if (!stateDirectory.isDirectory() && !stateDirectory.mkdirs()) {
            throw new IllegalStateException("Не удалось создать защищённое хранилище настроек");
        }
        snapshot = new AtomicFile(new File(stateDirectory, STATE + ".json"));
        previousSnapshot = new AtomicFile(new File(stateDirectory, STATE + ".previous.json"));
        initializedMarker = new File(stateDirectory, ".initialized");
        root = loadState();
        ensure();
		migrateEmergencyOnlyMode();
        migrateQualificationPolicy();
        migrateDetectedParsers();
        migrateDisplayNames();
        seedDefaults();
    }

    private JSONObject loadState() {
        String stored = preferenceString(STATE);
        JSONObject primary = parseObject(stored);
        if (primary != null) {
            ensureRecoveryFiles(stored);
            return primary;
        }
        for (String recovery : new String[]{
                preferenceString(STATE_BACKUP),
                readAtomic(snapshot),
                readAtomic(previousSnapshot)
        }) {
            JSONObject recovered = parseObject(recovery);
            if (recovered == null) continue;
            restorePrimary(stored, recovered.toString());
            return recovered;
        }
        if ((stored == null || stored.trim().isEmpty()) && !initializedMarker.exists()) return new JSONObject();
        // Never seed and persist factory defaults over an unreadable or unexpectedly
        // missing registry. The independent snapshots remain available for support.
        throw new IllegalStateException("Хранилище настроек повреждено; заводской сброс отменён");
    }

    private void ensureRecoveryFiles(String serialized) {
        if (parseObject(readAtomic(snapshot)) == null) writeAtomic(snapshot, serialized);
        ensureInitializedMarker();
    }

    private String preferenceString(String key) {
        try {
            return preferences.getString(key, null);
        } catch (ClassCastException ignored) {
            return null;
        }
    }

    private void restorePrimary(String corrupt, String recovered) {
        SharedPreferences.Editor editor = preferences.edit().putString(STATE, recovered);
        if (corrupt != null && !corrupt.trim().isEmpty()) editor.putString(STATE_CORRUPT, corrupt);
        if (!editor.commit()) throw new IllegalStateException("Не удалось восстановить резервную копию настроек");
        writeAtomic(snapshot, recovered);
        ensureInitializedMarker();
    }

    private void ensureInitializedMarker() {
        if (initializedMarker.exists()) return;
        try {
            if (!initializedMarker.createNewFile() && !initializedMarker.exists()) {
                throw new IOException("marker_not_created");
            }
        } catch (IOException error) {
            throw new IllegalStateException("Не удалось зафиксировать состояние хранилища", error);
        }
    }

    private static JSONObject parseObject(String value) {
        if (value == null || value.trim().isEmpty()) return null;
        try {
            return new JSONObject(value);
        } catch (JSONException ignored) {
            return null;
        }
    }

    private static String readAtomic(AtomicFile file) {
        try (FileInputStream input = file.openRead()) {
            long length = file.getBaseFile().length();
            if (length <= 0 || length > 64L * 1024L * 1024L) return null;
            byte[] bytes = new byte[(int) length];
            int offset = 0;
            while (offset < bytes.length) {
                int count = input.read(bytes, offset, bytes.length - offset);
                if (count < 0) break;
                offset += count;
            }
            return new String(bytes, 0, offset, StandardCharsets.UTF_8);
        } catch (IOException ignored) {
            return null;
        }
    }

    private static void writeAtomic(AtomicFile file, String value) {
        FileOutputStream output = null;
        try {
            output = file.startWrite();
            output.write(value.getBytes(StandardCharsets.UTF_8));
            output.getFD().sync();
            file.finishWrite(output);
        } catch (IOException error) {
            if (output != null) file.failWrite(output);
            throw new IllegalStateException("Не удалось записать резервную копию настроек", error);
        }
    }

    private void migrateDetectedParsers() {
        try {
            JSONArray stored = root.getJSONArray("subscriptions");
            boolean changed = false;
            for (int i = 0; i < stored.length(); i++) {
                JSONObject item = stored.getJSONObject(i);
                if ("standard".equals(item.optString("parser")) && looksLikeBlackTemple(item.optString("secret"))) {
                    item.put("parser", "blacktemple").put("updated_at", now());
                    changed = true;
                }
            }
            if (changed) save();
        } catch (JSONException error) {
            throw new IllegalStateException(error);
        }
    }

    private void ensure() {
        try {
            if (!root.has("subscriptions")) root.put("subscriptions", new JSONArray());
            if (!root.has("nodes")) root.put("nodes", new JSONArray());
            if (!root.has("whitelist_nodes")) root.put("whitelist_nodes", new JSONArray());
            if (!root.has("mode")) root.put("mode", "auto");
			if (!root.has("service_desired")) root.put("service_desired", false);
            if (!root.has("qualification_policy")) root.put("qualification_policy", defaultQualificationPolicy());
            if (!root.has("routes")) root.put("routes", new JSONObject()
                    .put("revision", 1).put("default", "proxy")
                    .put("lists", new JSONObject().put("direct", new JSONArray()).put("proxy", new JSONArray()).put("block", new JSONArray()))
                    .put("stats", new JSONObject()));
            if (!root.has("components")) root.put("components", new JSONObject()
                    .put("geo_auto_update", true).put("geo_interval_hours", 24)
                    .put("geo_source", "metacubex").put("geoip_url", "").put("geosite_url", ""));
            JSONObject components = root.getJSONObject("components");
            if (!components.has("geo_source")) components.put("geo_source", "metacubex");
            if (!components.has("geoip_url")) components.put("geoip_url", "");
            if (!components.has("geosite_url")) components.put("geosite_url", "");
            if (!root.has("network_desired")) root.put("network_desired", defaultNetworkProfile());
            if (!root.has("network_active")) root.put("network_active", new JSONObject(root.getJSONObject("network_desired").toString()));
        } catch (JSONException impossible) { throw new IllegalStateException(impossible); }
    }

	private void migrateEmergencyOnlyMode() {
		if (!"emergency".equals(root.optString("mode", "auto"))) return;
		try {
			setAuto();
		} catch (JSONException error) {
			throw new IllegalStateException(error);
		}
	}

    private static JSONObject defaultQualificationPolicy() throws JSONException {
        JSONObject defaults = new JSONObject()
                .put("excluded_countries", new JSONArray())
                .put("min_speed_mbps", 10.0)
                .put("stability_ratio", 0.65)
                .put("tcp_timeout_ms", 2000)
                .put("url_timeout_ms", 3000)
                .put("geo_timeout_ms", 5000)
                .put("speed_timeout_ms", 15000)
				.put("url_test_urls", defaultURLTestURLs())
                .put("allowlist_probe_url", "https://ya.ru/")
                .put("open_internet_probe_url", "https://www.cloudflare.com/cdn-cgi/trace");
        JSONObject unlimited = new JSONObject().put("url_limit", 0).put("speed_candidates", 0).put("speed_candidates_per_source", 0).put("keep", 0);
        return new JSONObject().put("version", 1).put("defaults", defaults)
                .put("pools", new JSONObject()
                        .put("primary", new JSONObject(unlimited.toString()))
                        .put("emergency", new JSONObject(unlimited.toString()).put("speed_candidates_per_source", 100)));
    }

    private void migrateQualificationPolicy() {
        try {
            JSONObject policy = root.getJSONObject("qualification_policy");
            JSONObject defaults = policy.getJSONObject("defaults");
            if (!defaults.has("tcp_timeout_ms")) defaults.put("tcp_timeout_ms", 2000);
            if (!defaults.has("url_timeout_ms")) defaults.put("url_timeout_ms", 3000);
            if (!defaults.has("geo_timeout_ms")) defaults.put("geo_timeout_ms", 5000);
            if (!defaults.has("speed_timeout_ms")) defaults.put("speed_timeout_ms", 15000);
			if (!defaults.has("url_test_urls")) defaults.put("url_test_urls", defaultURLTestURLs());
            if (!defaults.has("allowlist_probe_url")) defaults.put("allowlist_probe_url", "https://ya.ru/");
            String openProbe = defaults.optString("open_internet_probe_url", "");
            if (openProbe.isEmpty() || "https://www.gstatic.com/generate_204".equalsIgnoreCase(openProbe)) {
                // generate_204 is used by Android itself and is commonly admitted by
                // operator allowlists. It therefore cannot prove open Internet access.
                defaults.put("open_internet_probe_url", "https://www.cloudflare.com/cdn-cgi/trace");
            }
            JSONObject pools = policy.getJSONObject("pools");
            JSONObject primary = pools.getJSONObject("primary");
            JSONObject emergency = pools.getJSONObject("emergency");
            if (!primary.has("speed_candidates_per_source")) primary.put("speed_candidates_per_source", 0);
            if (!emergency.has("speed_candidates_per_source")) emergency.put("speed_candidates_per_source", 100);
            save();
        } catch (JSONException error) {
            throw new IllegalStateException(error);
        }
    }

	private static JSONArray defaultURLTestURLs() {
		return new JSONArray()
				.put("https://www.gstatic.com/generate_204")
				.put("https://cp.cloudflare.com/generate_204")
				.put("https://www.msftconnecttest.com/connecttest.txt");
	}

    synchronized JSONObject qualificationPolicy() throws JSONException {
        return new JSONObject(root.getJSONObject("qualification_policy").toString());
    }

    synchronized void saveQualificationPolicy(JSONObject policy) throws JSONException {
        root.put("qualification_policy", new JSONObject(policy.toString()));
        save();
    }

    private static JSONObject defaultNetworkProfile() throws JSONException {
        JSONObject dns = new JSONObject()
                .put("direct", new JSONArray().put("1.1.1.1").put("8.8.8.8"))
                .put("proxy", new JSONArray().put("https://1.1.1.1/dns-query").put("https://dns.google/dns-query"))
                .put("vpn_underlay", new JSONArray().put("1.1.1.1").put("8.8.8.8"))
                .put("bootstrap", new JSONArray().put("1.1.1.1").put("8.8.8.8"))
                .put("cache_algorithm", "arc").put("prefer_h3", false).put("use_hosts", true).put("ipv6", false);
        JSONObject role = new JSONObject().put("interface", "auto").put("gateway", JSONObject.NULL).put("source", JSONObject.NULL);
        return new JSONObject().put("version", 1).put("revision", 1).put("updated_at", now())
                .put("roles", new JSONObject().put("direct", new JSONObject(role.toString())).put("vpn_underlay", new JSONObject(role.toString())))
                .put("capture", new JSONObject().put("mode", "system").put("interfaces", new JSONArray())
                        .put("bypass_local", true).put("bypass_cidrs", new JSONArray())
                        .put("management_cidrs", new JSONArray()).put("dns_hijack", true).put("strict_route", true))
                .put("dns", dns);
    }

    private void seedDefaults() {
        try {
            addDefault("ebrasha-public", "EbraSha",
                    "https://raw.githubusercontent.com/ebrasha/free-v2ray-public-list/refs/heads/main/V2Ray-Config-By-EbraSha.txt",
                    "https://github.com/ebrasha/free-v2ray-public-list",
                    "");
            addDefault("default-au1rxx", "Au1rxx",
                    "https://raw.githubusercontent.com/Au1rxx/free-vpn-subscriptions/main/output/v2ray-base64.txt",
                    "https://github.com/Au1rxx/free-vpn-subscriptions",
                    "");
            save();
        } catch (JSONException error) { throw new IllegalStateException(error); }
    }

    private void addDefault(String id, String name, String secret, String repository, String description) throws JSONException {
        JSONObject existing = findSubscription(id);
        if (existing != null) {
            existing.put("name", name).put("repository", repository).put("description", description)
                    .put("builtin_default", true);
            return;
        }
        long now = now();
        root.getJSONArray("subscriptions").put(new JSONObject()
                .put("id", id).put("name", name).put("group", "emergency").put("parser", "standard")
                .put("secret", secret).put("enabled", true).put("interval_seconds", 3600)
                .put("last_status", "new").put("last_links", 0).put("last_attempt", 0).put("last_success", 0)
                .put("created_at", now).put("updated_at", now).put("builtin_default", true)
                .put("repository", repository).put("description", description));
    }

    synchronized JSONArray subscriptions() throws JSONException {
        JSONArray output = new JSONArray();
        JSONArray stored = root.getJSONArray("subscriptions");
        for (int i = 0; i < stored.length(); i++) output.put(publicSubscription(stored.getJSONObject(i)));
        return output;
    }

    synchronized JSONObject create(JSONObject payload) throws JSONException {
        long now = now();
        JSONObject item = new JSONObject()
                .put("id", UUID.randomUUID().toString())
                .put("name", subscriptionName(payload))
                .put("group", payload.optString("group", "primary"))
                .put("parser", payload.optString("parser", "standard"))
                .put("secret", required(payload, "secret"))
                .put("enabled", payload.optBoolean("enabled", true))
                .put("interval_seconds", payload.optInt("interval_seconds", 900))
                .put("last_status", "new")
                .put("last_links", 0)
                .put("last_attempt", 0)
                .put("last_success", 0)
                .put("created_at", now)
                .put("updated_at", now);
        validate(item);
        ensureUnique(item, null);
        root.getJSONArray("subscriptions").put(item);
        save();
        return publicSubscription(item);
    }

    synchronized JSONObject update(String id, JSONObject changes) throws JSONException {
        JSONObject item = findSubscription(id);
        if (item == null) return null;
        for (String key : new String[]{"name", "group", "parser", "secret", "enabled", "interval_seconds"}) {
            if (changes.has(key)) item.put(key, changes.get(key));
        }
        validate(item);
        ensureUnique(item, id);
        item.put("updated_at", now());
        if (!item.optBoolean("enabled", true)) { removeNodesForLocked(id); removeWhitelistSourceLocked(id); }
        save();
        return publicSubscription(item);
    }

    synchronized boolean delete(String id) throws JSONException {
        JSONArray source = root.getJSONArray("subscriptions");
        JSONArray next = new JSONArray();
        boolean deleted = false;
        for (int i = 0; i < source.length(); i++) {
            JSONObject item = source.getJSONObject(i);
            if (id.equals(item.optString("id"))) deleted = true; else next.put(item);
        }
        if (!deleted) return false;
        root.put("subscriptions", next);
        removeNodesForLocked(id);
        removeWhitelistSourceLocked(id);
        save();
        return true;
    }

    synchronized JSONObject subscriptionPrivate(String id) throws JSONException {
        return findSubscription(id);
    }

    synchronized JSONArray enabledSubscriptions(String onlyId, String onlyGroup) throws JSONException {
        JSONArray result = new JSONArray();
        JSONArray source = root.getJSONArray("subscriptions");
        for (int i = 0; i < source.length(); i++) {
            JSONObject item = source.getJSONObject(i);
            if (item.optBoolean("enabled", true)
                    && (onlyId == null || onlyId.equals(item.optString("id")))
                    && (onlyGroup == null || onlyGroup.equals(item.optString("group")))) result.put(new JSONObject(item.toString()));
        }
        return result;
    }

    synchronized void refreshSucceeded(String id, JSONArray proxies, JSONArray tests, JSONArray links, int testedCount) throws JSONException {
        JSONObject subscription = findSubscription(id);
        if (subscription == null) return;
		JSONObject history = nodeHistoryForSourceLocked(id);
        int aliveCount = 0;
        for (int i = 0; i < tests.length(); i++) if (tests.optJSONObject(i) != null && tests.optJSONObject(i).optBoolean("alive", false)) aliveCount++;
        subscription.put("cached_links", copyArray(links))
                .put("last_links", subscription.getJSONArray("cached_links").length()).put("last_attempt", now()).put("updated_at", now());
        removeNodesForLocked(id);
        JSONArray nodes = root.getJSONArray("nodes");
        for (int i = 0; i < proxies.length(); i++) {
            JSONObject proxy = proxies.getJSONObject(i);
            JSONObject test = i < tests.length() ? tests.getJSONObject(i) : new JSONObject();
            boolean alive = test.optBoolean("alive", false);
            String name = proxy.optString("name", "Сервер " + (i + 1));
			JSONObject node = new JSONObject()
                    .put("id", name)
                    .put("display_name", displayName(proxy, i + 1))
                    .put("pool", subscription.optString("group", "primary"))
                    .put("priority", i + 1)
                    .put("alive", alive)
					.put("delay_ms", alive ? test.optInt("delay_ms") : JSONObject.NULL)
					.put("speed_mbps", alive && test.has("speed_mbps") ? test.optDouble("speed_mbps") : JSONObject.NULL)
					.put("stability_ratio", alive && test.has("stability_ratio") ? test.optDouble("stability_ratio") : JSONObject.NULL)
                    .put("source_id", id)
                    .put("source_name", subscription.optString("name"))
					.put("last_tested_at", now())
                    .put("proxy", proxy);
			JSONObject previous = history.optJSONObject(name);
			if (previous != null) node.put("health_successes", previous.optInt("health_successes", 0))
					.put("health_failures", previous.optInt("health_failures", 0));
			nodes.put(node);
        }
		rankNodesLocked("nodes");
        subscription.put("last_status", "success").put("last_error", JSONObject.NULL)
                .put("last_result", aliveCount == 0 ? "no_available_servers" : "available_servers")
                .put("last_available", aliveCount).put("last_tested", testedCount).put("last_success", now());
        clearMissingSelectionLocked();
        save();
    }

    synchronized void cacheRefreshSucceeded(String id, JSONArray freshLinks) throws JSONException {
        JSONObject subscription = findSubscription(id);
        if (subscription == null) return;
        JSONArray replacement = copyArray(freshLinks);
        subscription.put("cached_links", replacement).put("last_links", replacement.length())
                .put("last_status", "success").put("last_error", JSONObject.NULL)
                .put("last_attempt", now()).put("last_success", now()).put("updated_at", now());
        save();
    }

    synchronized void updateDetectedParser(String id, String parser) throws JSONException {
        if (!"standard".equals(parser) && !"blacktemple".equals(parser)) return;
        JSONObject subscription = findSubscription(id);
        if (subscription == null || parser.equals(subscription.optString("parser"))) return;
        subscription.put("parser", parser).put("updated_at", now());
        save();
    }

    synchronized void refreshUnavailable(String id, JSONArray links, String result, int tested) throws JSONException {
        JSONObject subscription = findSubscription(id);
        if (subscription == null) return;
        JSONArray merged = mergeArrays(links, subscription.optJSONArray("cached_links"));
        subscription.put("cached_links", merged).put("last_links", merged.length())
                .put("last_status", "success").put("last_error", JSONObject.NULL)
                .put("last_result", result).put("last_available", 0).put("last_tested", tested)
                .put("last_attempt", now()).put("last_success", now()).put("updated_at", now());
        JSONArray nodes = root.getJSONArray("nodes");
        for (int i = 0; i < nodes.length(); i++) {
            JSONObject node = nodes.getJSONObject(i);
            if (id.equals(node.optString("source_id"))) {
                node.put("alive", false).put("delay_ms", JSONObject.NULL).put("last_test_result", result);
            }
        }
		rankNodesLocked("nodes");
        clearMissingSelectionLocked();
        save();
    }

    synchronized void refreshFailed(String id, String error) throws JSONException {
        JSONObject item = findSubscription(id);
        if (item == null) return;
        item.put("last_status", "error").put("last_error", error).put("last_attempt", now()).put("updated_at", now());
        save();
    }

    synchronized JSONArray nodes() throws JSONException {
        JSONArray result = new JSONArray();
        JSONArray stored = root.getJSONArray("nodes");
        String selected = root.optString("selected_node", "");
        for (int i = 0; i < stored.length(); i++) {
            JSONObject source = stored.getJSONObject(i);
            result.put(new JSONObject()
                    .put("id", source.getString("id"))
                    .put("display_name", displayName(source.optJSONObject("proxy"), i + 1))
                    .put("pool", source.getString("pool"))
                    .put("priority", source.optInt("priority", i + 1))
                    .put("alive", source.optBoolean("alive", true))
                    .put("delay_ms", source.opt("delay_ms"))
					.put("speed_mbps", source.opt("speed_mbps"))
					.put("stability_ratio", source.opt("stability_ratio"))
					.put("score", source.optDouble("score", 0))
                    .put("selected", selected.equals(source.getString("id")))
                    .put("source_id", source.optString("source_id"))
                    .put("source_name", source.optString("source_name")));
        }
        JSONArray whitelist = root.getJSONArray("whitelist_nodes");
        String whitelistSelected = root.optString("selected_whitelist_node", "");
        for (int i = 0; i < whitelist.length(); i++) {
            JSONObject source = whitelist.getJSONObject(i);
            result.put(new JSONObject(source.toString())
                    .put("display_name", displayName(source.optJSONObject("proxy"), i + 1))
                    .put("selected", whitelistSelected.equals(source.optString("id"))));
        }
        return result;
    }

    synchronized JSONObject select(String id) throws JSONException {
        JSONObject node = findNode(id);
        if (node == null || !node.optBoolean("alive", false)) return null;
        root.put("selected_node", id).put("mode", "manual");
        save();
        return new JSONObject(node.toString());
    }

    synchronized void setAuto() throws JSONException {
        root.put("mode", "auto");
		// Manual mode pins exactly the node chosen by the user. Returning to
		// automatic mode must discard that pin, otherwise an emergency or a
		// lower-ranked primary node remains selected indefinitely.
		root.remove("selected_node");
		selectBestLocked();
        save();
    }

    synchronized void setEmergency() throws JSONException {
        root.put("mode", "emergency");
        JSONObject selected = findNode(root.optString("selected_node", ""));
        if (selected == null || !"emergency".equals(selected.optString("pool"))) {
            root.remove("selected_node");
            selectBestLocked();
        }
        save();
    }

    synchronized JSONObject activeNode() throws JSONException {
        JSONObject selected = findNode(root.optString("selected_node", ""));
        if (selected != null && "emergency".equals(mode()) && !"emergency".equals(selected.optString("pool"))) selected = null;
        if (selected != null && !selected.optBoolean("alive", false)) selected = null;
        if (selected == null) selected = selectBestLocked();
        return selected == null ? null : new JSONObject(selected.toString());
    }

    synchronized JSONObject activeNode(boolean whitelistMode) throws JSONException {
        if (!whitelistMode) return activeNode();
        return whitelistTransitionLocked(new JSONObject().put("operation", "active"));
    }

    synchronized void confirmConnectedNode(String nodeId, boolean whitelistMode) throws JSONException {
        if (nodeId == null || nodeId.isEmpty()) return;
        String connectionKey = (whitelistMode ? "whitelist:" : "normal:") + nodeId;
        if (connectionKey.equals(root.optString("connected_node_key", ""))) return;
        root.put("connected_node_key", connectionKey).put("last_switch", now());
        save();
    }

    synchronized long lastSwitch() { return root.optLong("last_switch", 0); }

	synchronized void recordHealth(String nodeId, boolean whitelistMode, boolean success) throws JSONException {
		if (nodeId == null || nodeId.isEmpty()) return;
		String key = whitelistMode ? "whitelist_nodes" : "nodes";
		JSONArray nodes = root.getJSONArray(key);
		for (int i = 0; i < nodes.length(); i++) {
			JSONObject node = nodes.getJSONObject(i);
			if (!nodeId.equals(node.optString("id"))) continue;
			String counter = success ? "health_successes" : "health_failures";
			node.put(counter, node.optInt(counter, 0) + 1);
			int successes = node.optInt("health_successes", 0), failures = node.optInt("health_failures", 0);
			if (successes + failures > 200) {
				node.put("health_successes", successes / 2).put("health_failures", failures / 2);
			}
			rankNodesLocked(key);
			// Health probes run every 15 seconds. Keep every sample in memory,
			// but persist successful streaks once per minute to avoid thousands
			// of SharedPreferences writes per day. Failures are always durable.
			if (!success || (successes + failures) % 4 == 0) save();
			return;
		}
	}

	synchronized boolean preferPrimaryIfAvailable(long testedAfter) throws JSONException {
		if (!"auto".equals(mode())) return false;
		JSONObject current = findNode(root.optString("selected_node", ""));
		if (current != null && "primary".equals(current.optString("pool")) && current.optBoolean("alive", false)) return false;
		rankNodesLocked("nodes");
		JSONArray nodes = root.getJSONArray("nodes");
		for (int i = 0; i < nodes.length(); i++) {
			JSONObject node = nodes.getJSONObject(i);
			if (node.optBoolean("alive", false) && "primary".equals(node.optString("pool"))
					&& node.optLong("last_tested_at", 0) >= testedAfter) {
				root.put("selected_node", node.getString("id"));
				save();
				return true;
			}
		}
		return false;
	}

	synchronized boolean serviceDesired() { return root.optBoolean("service_desired", false); }

	synchronized void setServiceDesired(boolean desired) throws JSONException {
		root.put("service_desired", desired);
		save();
	}

    synchronized JSONObject requestWhitelistNode() throws JSONException {
        return whitelistTransitionLocked(new JSONObject().put("operation", "request"));
    }

    synchronized void confirmWhitelistNode(String nodeId) throws JSONException {
        whitelistTransitionLocked(new JSONObject().put("operation", "confirm").put("node_id", nodeId == null ? "" : nodeId));
    }

    synchronized void beginWhitelistScan() throws JSONException {
        whitelistTransitionLocked(new JSONObject().put("operation", "begin"));
    }

    synchronized void completeWhitelistScan() throws JSONException {
        whitelistTransitionLocked(new JSONObject().put("operation", "complete"));
    }

    synchronized void deactivateWhitelist() throws JSONException { whitelistTransitionLocked(new JSONObject().put("operation", "deactivate")); }

    synchronized void addWhitelistWorking(String sourceId, JSONArray proxies, JSONArray tests) throws JSONException {
        JSONObject subscription = findSubscription(sourceId);
        if (subscription == null) return;
        JSONArray nodes = new JSONArray();
        for (int i = 0; i < proxies.length(); i++) {
            JSONObject test = i < tests.length() ? tests.optJSONObject(i) : null;
            if (test == null || !test.optBoolean("alive", false)) continue;
            JSONObject proxy = proxies.getJSONObject(i);
			nodes.put(new JSONObject().put("display_name", displayName(proxy, i + 1))
                    .put("pool", "whitelist").put("origin_pool", subscription.optString("group", "primary"))
                    .put("priority", nodes.length() + 1).put("alive", true)
					.put("delay_ms", test.optInt("delay_ms", 0))
					.put("speed_mbps", test.has("speed_mbps") ? test.optDouble("speed_mbps") : JSONObject.NULL)
					.put("stability_ratio", test.has("stability_ratio") ? test.optDouble("stability_ratio") : JSONObject.NULL)
					.put("source_id", sourceId)
                    .put("source_name", subscription.optString("name")).put("proxy", new JSONObject(proxy.toString())));
        }
        whitelistTransitionLocked(new JSONObject().put("operation", "add_source").put("source_id", sourceId).put("nodes", nodes));
    }

    synchronized void replaceWhitelistSource(String sourceId, JSONArray proxies, JSONArray tests) throws JSONException {
        JSONObject subscription = findSubscription(sourceId);
		JSONObject history = whitelistHistoryForSourceLocked(sourceId);
        JSONArray nodes = new JSONArray();
        if (subscription != null) for (int i = 0; i < proxies.length(); i++) {
            JSONObject test = i < tests.length() ? tests.optJSONObject(i) : null;
            if (test == null || !test.optBoolean("alive", false)) continue;
            JSONObject proxy = proxies.getJSONObject(i);
			JSONObject node = new JSONObject().put("display_name", displayName(proxy, i + 1))
                    .put("origin_pool", subscription.optString("group", "primary")).put("priority", nodes.length() + 1)
					.put("alive", true).put("delay_ms", test.optInt("delay_ms", 0))
					.put("speed_mbps", test.has("speed_mbps") ? test.optDouble("speed_mbps") : JSONObject.NULL)
					.put("stability_ratio", test.has("stability_ratio") ? test.optDouble("stability_ratio") : JSONObject.NULL)
					.put("source_id", sourceId)
					.put("source_name", subscription.optString("name")).put("proxy", new JSONObject(proxy.toString()));
			JSONObject previous = history.optJSONObject(proxy.optString("name"));
			if (previous != null) node.put("health_successes", previous.optInt("health_successes", 0))
					.put("health_failures", previous.optInt("health_failures", 0));
			nodes.put(node);
        }
        whitelistTransitionLocked(new JSONObject().put("operation", "replace_source").put("source_id", sourceId).put("nodes", nodes));
    }

    synchronized JSONObject failoverWhitelistNode() throws JSONException {
        return whitelistTransitionLocked(new JSONObject().put("operation", "fail"));
    }

    synchronized int whitelistCount() { JSONArray nodes = root.optJSONArray("whitelist_nodes"); return nodes == null ? 0 : nodes.length(); }

    synchronized JSONObject deleteNode(String id) throws JSONException {
        JSONObject normal = findNode(id);
        JSONObject whitelist = findWhitelistNode(id);
        if (normal == null && whitelist == null) return null;
        String pool;
        boolean wasSelected;
        int remaining;
        if (normal != null) {
            pool = normal.optString("pool", "primary");
            wasSelected = id.equals(root.optString("selected_node", ""));
            JSONArray source = root.getJSONArray("nodes"), next = new JSONArray();
            for (int i = 0; i < source.length(); i++) if (!id.equals(source.getJSONObject(i).optString("id"))) next.put(source.getJSONObject(i));
            root.put("nodes", next);
            if (wasSelected) {
                root.remove("selected_node");
                if ("manual".equals(mode())) root.put("mode", "auto");
                selectBestLocked();
            }
            remaining = 0;
            for (int i = 0; i < next.length(); i++) if (pool.equals(next.getJSONObject(i).optString("pool"))) remaining++;
        } else {
            pool = "whitelist";
            wasSelected = id.equals(root.optString("selected_whitelist_node", ""));
            whitelistTransitionLocked(new JSONObject().put("operation", "remove_node").put("node_id", id));
            remaining = root.getJSONArray("whitelist_nodes").length();
        }
        if (normal != null && findNode(root.optString("selected_node", "")) == null) {
            root.remove("selected_node");
            selectBestLocked();
        }
        clearMissingSelectionLocked();
        save();
        return new JSONObject().put("id", id).put("pool", pool).put("was_selected", wasSelected).put("remaining", remaining);
    }

    private JSONObject whitelistTransitionLocked(JSONObject command) throws JSONException {
        JSONObject state = new JSONObject().put("nodes", root.optJSONArray("whitelist_nodes") == null ? new JSONArray() : root.getJSONArray("whitelist_nodes"))
                .put("selected_node", root.optString("selected_whitelist_node", ""))
                .put("pending_node", root.optString("pending_whitelist_node", ""))
                .put("scan_active", root.optBoolean("whitelist_scan_active", false))
                .put("generation", root.optLong("whitelist_generation", 0));
        JSONObject envelope = new JSONObject(Mobilecore.whitelistTransition(state.toString(), command.toString()));
        if (!envelope.optBoolean("ok")) throw new JSONException(envelope.optJSONObject("error") == null ? "whitelist_transition_failed" : envelope.getJSONObject("error").optString("error"));
        JSONObject result = envelope.getJSONObject("result"), next = result.getJSONObject("state");
        root.put("whitelist_nodes", next.getJSONArray("nodes"));
        setOrRemove("selected_whitelist_node", next.optString("selected_node", ""));
        setOrRemove("pending_whitelist_node", next.optString("pending_node", ""));
        root.put("whitelist_scan_active", next.optBoolean("scan_active", false));
        root.put("whitelist_generation", next.optLong("generation", 0));
        save();
        JSONObject candidate = result.optJSONObject("candidate");
        return candidate == null ? null : new JSONObject(candidate.toString());
    }

    private void setOrRemove(String key, String value) throws JSONException {
        if (value == null || value.isEmpty()) root.remove(key); else root.put(key, value);
    }

    synchronized String mode() { return root.optString("mode", "auto"); }

	synchronized String activePool() throws JSONException {
		JSONObject node = findNode(root.optString("selected_node", ""));
		return node == null ? "" : node.optString("pool", "");
	}

    synchronized JSONObject failoverActiveNode() throws JSONException {
        if ("manual".equals(mode())) return null;
        JSONObject active = findNode(root.optString("selected_node", ""));
		if (active != null) active.put("alive", false).put("delay_ms", JSONObject.NULL);
        root.remove("selected_node");
		rankNodesLocked("nodes");
        JSONObject next = selectBestLocked();
        save();
        return next == null ? null : new JSONObject(next.toString());
    }

    synchronized JSONArray pools() throws JSONException { return pools(false); }

    synchronized JSONArray pools(boolean whitelistMode) throws JSONException {
        JSONArray nodes = root.getJSONArray("nodes");
        String selected = root.optString("selected_node", "");
        JSONArray output = new JSONArray();
        for (String pool : new String[]{"primary", "emergency"}) {
            int total = 0, alive = 0; boolean poolSelected = false;
            for (int i = 0; i < nodes.length(); i++) {
                JSONObject node = nodes.getJSONObject(i);
                if (!pool.equals(node.optString("pool"))) continue;
                total++;
                if (node.optBoolean("alive", true)) alive++;
                if (selected.equals(node.optString("id"))) poolSelected = true;
            }
            output.put(new JSONObject().put("id", pool).put("priority", "primary".equals(pool) ? 1 : 2)
                    .put("total", total).put("alive", alive).put("selected", !whitelistMode && poolSelected));
        }
        JSONArray whitelist = root.getJSONArray("whitelist_nodes");
        output.put(new JSONObject().put("id", "whitelist").put("priority", 0)
                .put("total", whitelist.length()).put("alive", whitelist.length())
                .put("selected", whitelistMode));
        return output;
    }

    synchronized JSONObject routes() throws JSONException { return new JSONObject(root.getJSONObject("routes").toString()); }

    synchronized String routesForEngine(boolean allowlistMode) throws JSONException {
        JSONObject routes = root.getJSONObject("routes");
        if (allowlistMode) {
            // During an operator allowlist, DIRECT rules only expose traffic to
            // the restricted underlay. Preserve explicit rejects, but send
            // every other destination through the working tunnel.
            JSONObject lists = new JSONObject().put("block", copyArray(routes.getJSONObject("lists").optJSONArray("block")))
                    .put("direct", new JSONArray()).put("proxy", new JSONArray());
            return new JSONObject().put("default", "proxy").put("lists", lists).toString();
        }
        return new JSONObject().put("default", routes.optString("default", "proxy")).put("lists", routes.getJSONObject("lists")).toString();
    }

    synchronized void selectFirstAliveFromSource(String sourceId) throws JSONException {
        JSONArray nodes = root.getJSONArray("nodes");
        for (int i = 0; i < nodes.length(); i++) {
            JSONObject node = nodes.getJSONObject(i);
            if (sourceId.equals(node.optString("source_id")) && node.optBoolean("alive", false)) {
                root.put("selected_node", node.getString("id"));
                save();
                return;
            }
        }
    }

    synchronized JSONObject saveRoutes(String defaultAction, JSONObject lists, JSONObject stats) throws JSONException {
        JSONObject routes = root.getJSONObject("routes");
        routes.put("revision", routes.optInt("revision", 1) + 1).put("default", defaultAction)
                .put("lists", new JSONObject(lists.toString())).put("stats", new JSONObject(stats.toString()));
        save();
        return new JSONObject(routes.toString());
    }

    synchronized void updateDefaultEmergency(JSONArray enabledIds) throws JSONException {
        if (enabledIds == null) enabledIds = new JSONArray();
        JSONArray subscriptions = root.getJSONArray("subscriptions");
        for (int i = 0; i < subscriptions.length(); i++) {
            JSONObject item = subscriptions.getJSONObject(i);
            if (!item.optBoolean("builtin_default", false)) continue;
            boolean enabled = contains(enabledIds, item.optString("id"));
            item.put("enabled", enabled).put("updated_at", now());
            if (!enabled) removeNodesForLocked(item.optString("id"));
        }
        save();
    }

    synchronized JSONObject componentSettings() throws JSONException {
        return new JSONObject(root.getJSONObject("components").toString());
    }

    synchronized JSONObject saveComponentSettings(boolean autoUpdate, int intervalHours, String source, String geoIPURL, String geoSiteURL) throws JSONException {
        if (intervalHours < 6 || intervalHours > 168) throw new JSONException("invalid_geo_interval");
        JSONObject settings = root.getJSONObject("components");
        settings.put("geo_auto_update", autoUpdate).put("geo_interval_hours", intervalHours)
                .put("geo_source", source)
                .put("geoip_url", "custom".equals(source) ? geoIPURL : "")
                .put("geosite_url", "custom".equals(source) ? geoSiteURL : "");
        save();
        return new JSONObject(settings.toString());
    }

    synchronized void saveInstalledGeoSource(JSONObject source) throws JSONException {
        root.getJSONObject("components").put("installed_geo_source", new JSONObject(source.toString()));
        save();
    }

    synchronized JSONObject networkState() throws JSONException {
        JSONObject desired = new JSONObject(root.getJSONObject("network_desired").toString());
        JSONObject active = new JSONObject(root.getJSONObject("network_active").toString());
        boolean inSync = sameNetwork(desired, active);
        return new JSONObject().put("desired", desired).put("active", active).put("in_sync", inSync)
                .put("apply", new JSONObject().put("status", inSync ? "success" : "pending")
                        .put("revision", active.optInt("revision", 1)).put("updated_at", active.optLong("updated_at", 0)));
    }

    synchronized JSONObject dnsState() throws JSONException {
        JSONObject desired = root.getJSONObject("network_desired").getJSONObject("dns");
        JSONObject active = root.getJSONObject("network_active").getJSONObject("dns");
        return new JSONObject().put("active", new JSONObject(active.toString())).put("in_sync", desired.toString().equals(active.toString()));
    }

    synchronized JSONObject saveNetworkProfile(JSONObject profile) throws JSONException {
        validateNetworkProfile(profile);
        JSONObject current = root.getJSONObject("network_desired");
        JSONObject next = new JSONObject(profile.toString());
        next.put("version", 1).put("revision", current.optInt("revision", 1) + 1).put("updated_at", now());
        root.put("network_desired", next);
        save();
        return new JSONObject(next.toString());
    }

    synchronized void validateNetwork(JSONObject profile) throws JSONException { validateNetworkProfile(profile); }

    synchronized JSONObject saveDNS(JSONObject dns) throws JSONException {
        validateDNS(dns);
        JSONObject desired = root.getJSONObject("network_desired");
        desired.put("dns", new JSONObject(dns.toString())).put("revision", desired.optInt("revision", 1) + 1).put("updated_at", now());
        save();
        return new JSONObject(desired.toString());
    }

    synchronized void validateDNSProfile(JSONObject dns) throws JSONException { validateDNS(dns); }

    synchronized JSONObject applyNetwork() throws JSONException {
        JSONObject desired = new JSONObject(root.getJSONObject("network_desired").toString());
        desired.put("updated_at", now());
        root.put("network_active", desired);
        save();
        return new JSONObject(desired.toString());
    }

    synchronized String activeTransport() throws JSONException {
        return root.getJSONObject("network_active").getJSONObject("roles").getJSONObject("vpn_underlay").optString("interface", "auto");
    }

    synchronized String activeDNSForEngine() throws JSONException {
        return root.getJSONObject("network_active").getJSONObject("dns").toString();
    }

    synchronized boolean activeIPv6() throws JSONException {
        return root.getJSONObject("network_active").getJSONObject("dns").optBoolean("ipv6", false);
    }

    private static boolean sameNetwork(JSONObject left, JSONObject right) throws JSONException {
        JSONObject a = new JSONObject(left.toString()), b = new JSONObject(right.toString());
        a.remove("revision"); a.remove("updated_at"); b.remove("revision"); b.remove("updated_at");
        return a.toString().equals(b.toString());
    }

    private static void validateNetworkProfile(JSONObject profile) throws JSONException {
        JSONObject roles = profile.optJSONObject("roles");
        JSONObject capture = profile.optJSONObject("capture");
        JSONObject dns = profile.optJSONObject("dns");
        if (roles == null || capture == null || dns == null) throw new JSONException("invalid_network_profile");
        for (String role : new String[]{"direct", "vpn_underlay"}) {
            JSONObject value = roles.optJSONObject(role);
            if (value == null || !validTransport(value.optString("interface", "auto"))) throw new JSONException("invalid_android_transport");
        }
        if (!"system".equals(capture.optString("mode"))) throw new JSONException("android_capture_must_be_system");
        validateDNS(dns);
    }

    private static boolean validTransport(String value) {
        return "auto".equals(value) || "wifi".equals(value) || "cellular".equals(value) || "ethernet".equals(value);
    }

    private static void validateDNS(JSONObject dns) throws JSONException {
        for (String key : new String[]{"direct", "proxy", "vpn_underlay", "bootstrap"}) {
            JSONArray values = dns.optJSONArray(key);
            if (values == null || values.length() == 0) throw new JSONException("dns_" + key + "_required");
            for (int i = 0; i < values.length(); i++) if (values.optString(i).trim().isEmpty()) throw new JSONException("invalid_dns_value");
        }
        String cache = dns.optString("cache_algorithm", "arc");
        if (!"arc".equals(cache) && !"lru".equals(cache)) throw new JSONException("invalid_dns_cache");
    }

    private JSONObject selectBestLocked() throws JSONException {
        JSONArray nodes = root.getJSONArray("nodes");
		rankNodesLocked("nodes");
		JSONObject envelope = new JSONObject(Mobilecore.selectNode(root.getJSONArray("nodes").toString(), mode()));
		if (!envelope.optBoolean("ok")) throw new JSONException("node_selection_failed");
		JSONObject selected = envelope.getJSONObject("result").optJSONObject("node");
		if (selected == null) return null;
		JSONObject original = findNodeBySource(selected.optString("id"), selected.optString("source_id"));
		if (original != null) root.put("selected_node", original.getString("id"));
		return original;
    }

	private JSONObject nodeHistoryForSourceLocked(String sourceId) throws JSONException {
		JSONObject history = new JSONObject();
		JSONArray nodes = root.getJSONArray("nodes");
		for (int i = 0; i < nodes.length(); i++) {
			JSONObject node = nodes.getJSONObject(i);
			if (sourceId.equals(node.optString("source_id"))) history.put(node.optString("id"), new JSONObject(node.toString()));
		}
		return history;
	}

	private JSONObject whitelistHistoryForSourceLocked(String sourceId) throws JSONException {
		JSONObject history = new JSONObject();
		JSONArray nodes = root.getJSONArray("whitelist_nodes");
		for (int i = 0; i < nodes.length(); i++) {
			JSONObject node = nodes.getJSONObject(i);
			if (!sourceId.equals(node.optString("source_id"))) continue;
			JSONObject proxy = node.optJSONObject("proxy");
			if (proxy != null) history.put(proxy.optString("name"), new JSONObject(node.toString()));
		}
		return history;
	}

	private JSONObject findNodeBySource(String id, String sourceId) throws JSONException {
		JSONArray nodes = root.getJSONArray("nodes");
		for (int i = 0; i < nodes.length(); i++) {
			JSONObject node = nodes.getJSONObject(i);
			if (id.equals(node.optString("id")) && sourceId.equals(node.optString("source_id"))) return node;
		}
		return null;
	}

	private void rankNodesLocked(String key) throws JSONException {
		JSONArray original = root.optJSONArray(key);
		if (original == null || original.length() < 1) return;
		JSONObject envelope = new JSONObject(Mobilecore.rankNodes(original.toString()));
		if (!envelope.optBoolean("ok")) throw new JSONException("node_ranking_failed");
		JSONArray ranked = envelope.getJSONObject("result").getJSONArray("nodes");
		JSONArray reordered = new JSONArray();
		boolean[] used = new boolean[original.length()];
		for (int i = 0; i < ranked.length(); i++) {
			JSONObject rank = ranked.getJSONObject(i), found = null;
			for (int j = 0; j < original.length(); j++) {
				if (used[j]) continue;
				JSONObject candidate = original.getJSONObject(j);
				if (rank.optString("id").equals(candidate.optString("id"))
						&& rank.optString("source_id").equals(candidate.optString("source_id"))) {
					found = candidate; used[j] = true; break;
				}
			}
			if (found != null) reordered.put(found.put("score", rank.optDouble("score", 0)).put("priority", i + 1));
		}
		root.put(key, reordered);
	}

    private static JSONArray mergeArrays(JSONArray fresh, JSONArray cached) {
        JSONArray result = new JSONArray();
        java.util.HashSet<String> seen = new java.util.HashSet<>();
        for (JSONArray source : new JSONArray[]{fresh == null ? new JSONArray() : fresh, cached == null ? new JSONArray() : cached}) {
            for (int i = 0; i < source.length(); i++) {
                String value = source.optString(i, "").trim();
                if (!value.isEmpty() && seen.add(value)) result.put(value);
            }
        }
        return result;
    }

    private static JSONArray copyArray(JSONArray source) throws JSONException {
        return source == null ? new JSONArray() : new JSONArray(source.toString());
    }

    private JSONObject findSubscription(String id) throws JSONException {
        JSONArray items = root.getJSONArray("subscriptions");
        for (int i = 0; i < items.length(); i++) if (id.equals(items.getJSONObject(i).optString("id"))) return items.getJSONObject(i);
        return null;
    }

    private JSONObject findNode(String id) throws JSONException {
        JSONArray items = root.getJSONArray("nodes");
        for (int i = 0; i < items.length(); i++) if (id.equals(items.getJSONObject(i).optString("id"))) return items.getJSONObject(i);
        return null;
    }

    private JSONObject findWhitelistNode(String id) throws JSONException {
        JSONArray items = root.getJSONArray("whitelist_nodes");
        for (int i = 0; i < items.length(); i++) if (id.equals(items.getJSONObject(i).optString("id"))) return items.getJSONObject(i);
        return null;
    }

    private void removeNodesForLocked(String sourceId) throws JSONException {
        JSONArray source = root.getJSONArray("nodes"), next = new JSONArray();
        for (int i = 0; i < source.length(); i++) if (!sourceId.equals(source.getJSONObject(i).optString("source_id"))) next.put(source.getJSONObject(i));
        root.put("nodes", next);
        clearMissingSelectionLocked();
    }

    private void removeWhitelistSourceLocked(String sourceId) throws JSONException {
        whitelistTransitionLocked(new JSONObject().put("operation", "remove_source").put("source_id", sourceId));
    }

    private void clearMissingSelectionLocked() throws JSONException {
        String selected = root.optString("selected_node", "");
        JSONObject node = findNode(selected);
        if (!selected.isEmpty() && (node == null || !node.optBoolean("alive", false))) root.remove("selected_node");
    }

    private static JSONObject publicSubscription(JSONObject source) throws JSONException {
        JSONObject result = new JSONObject(source.toString());
        result.remove("secret");
        result.remove("cached_links");
        result.put("secret_configured", !source.optString("secret", "").isEmpty());
        return result;
    }

    private static boolean contains(JSONArray array, String value) {
        for (int i = 0; i < array.length(); i++) if (value.equals(array.optString(i))) return true;
        return false;
    }

    private static String required(JSONObject payload, String key) throws JSONException {
        String value = payload.optString(key, "").trim();
        if (value.isEmpty()) throw new JSONException("missing_" + key);
        return value;
    }

    private String subscriptionName(JSONObject payload) throws JSONException {
        String explicit = payload.optString("name", "").trim();
        if (!explicit.isEmpty()) return explicit;
        String parser = payload.optString("parser", "standard");
        String secret = payload.optString("secret", "").trim();
        if ("blacktemple".equals(parser)) return "BlackTemple";
        if ("wireguard".equals(parser)) return "WireGuard";
        if ("inline".equals(parser)) return "Добавленные серверы";
        try {
            java.net.URI uri = new java.net.URI(secret.split("\\s+", 2)[0]);
            String host = uri.getHost();
            if (host != null && !host.trim().isEmpty()) return host;
        } catch (Exception ignored) { }
        return "Источник " + (root.getJSONArray("subscriptions").length() + 1);
    }

    private static String displayName(JSONObject proxy, int index) {
        if (proxy == null) return "Сервер " + index;
        String internal = proxy.optString("name", "").trim();
        String label = internal.replaceFirst("^[A-Z0-9-]+-[0-9a-fA-F]{12}\\s*", "").trim();
        if (!label.isEmpty() && !label.equals(internal)) return label;
        if (!internal.isEmpty() && label.equals(internal)) return internal;
        String type = proxy.optString("type", "VPN").toUpperCase(java.util.Locale.ROOT);
        String server = proxy.optString("server", "").trim();
        return server.isEmpty() ? type + " сервер" : type + " · " + server;
    }

    private void migrateDisplayNames() {
        boolean changed = false;
        try {
            for (String key : new String[]{"nodes", "whitelist_nodes"}) {
                JSONArray nodes = root.optJSONArray(key);
                if (nodes == null) continue;
                for (int i = 0; i < nodes.length(); i++) {
                    JSONObject node = nodes.getJSONObject(i);
                    String display = displayName(node.optJSONObject("proxy"), i + 1);
                    if (!display.equals(node.optString("display_name"))) {
                        node.put("display_name", display);
                        changed = true;
                    }
                }
            }
            if (changed) save();
        } catch (JSONException ignored) { }
    }

    private static void validate(JSONObject item) throws JSONException {
        required(item, "name"); required(item, "secret");
        String group = item.optString("group"), parser = item.optString("parser");
        if (!"primary".equals(group) && !"emergency".equals(group)) throw new JSONException("invalid_group");
        if (!"standard".equals(parser) && !"blacktemple".equals(parser) && !"inline".equals(parser) && !"wireguard".equals(parser)) throw new JSONException("invalid_parser");
        int interval = item.optInt("interval_seconds", 900);
        if (interval < 300 || interval > 604800) throw new JSONException("interval_out_of_range");
    }

    private void ensureUnique(JSONObject candidate, String excludeId) throws JSONException {
        JSONArray stored = root.getJSONArray("subscriptions");
        for (int i = 0; i < stored.length(); i++) {
            JSONObject current = stored.getJSONObject(i);
            if (excludeId != null && excludeId.equals(current.optString("id"))) continue;
            if (candidate.optString("parser").equals(current.optString("parser"))
                    && candidate.optString("secret").trim().equals(current.optString("secret").trim())) {
                throw new JSONException("duplicate_subscription");
            }
        }
    }

    private void save() {
        String serialized = root.toString();
        String previous = preferenceString(STATE);
        if (serialized.equals(previous)) return;
        if (isJSONObject(previous)) writeAtomic(previousSnapshot, previous);
        writeAtomic(snapshot, serialized);
        SharedPreferences.Editor editor = preferences.edit().putString(STATE, serialized);
        if (isJSONObject(previous)) editor.putString(STATE_BACKUP, previous);
        if (!editor.commit()) throw new IllegalStateException("Не удалось сохранить настройки");
        ensureInitializedMarker();
    }

    private static boolean isJSONObject(String value) {
        if (value == null || value.trim().isEmpty()) return false;
        try {
            new JSONObject(value);
            return true;
        } catch (JSONException ignored) {
            return false;
        }
    }
    private static boolean looksLikeBlackTemple(String secret) {
        String normalizedSecret = secret == null ? "" : secret.trim().toLowerCase(java.util.Locale.ROOT);
        return normalizedSecret.startsWith("blacktemple://")
                || (normalizedSecret.startsWith("intent://") && normalizedSecret.matches("(?s).*[;#]scheme=blacktemple(?:;.*)?$"));
    }
    private static long now() { return System.currentTimeMillis() / 1000L; }
}
