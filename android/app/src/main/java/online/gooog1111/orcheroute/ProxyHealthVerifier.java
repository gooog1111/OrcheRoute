package online.gooog1111.orcheroute;

import org.json.JSONArray;
import org.json.JSONObject;

import mobilecore.Mobilecore;

/** Rechecks one active proxy through Mihomo without touching the running TUN. */
final class ProxyHealthVerifier {
	// Restricted mobile networks can take 5–20 seconds to complete an otherwise
	// valid proxied TLS request. Qualification remains fast; only the already
	// connected node receives this larger health window.
	private static final int ALLOWLIST_HEALTH_TIMEOUT_MS = 25_000;

    private ProxyHealthVerifier() { }

    static boolean verify(JSONObject node, JSONObject defaults) throws Exception {
        if (node == null || node.optJSONObject("proxy") == null || defaults == null) return false;
        JSONArray urls = defaults.optJSONArray("url_test_urls");
        if (urls == null || urls.length() == 0) return false;
        JSONArray proxies = new JSONArray().put(new JSONObject(node.getJSONObject("proxy").toString()));
        String raw = Mobilecore.engineTestProxiesMulti(
				proxies.toString(), urls.toString(),
				Math.max(defaults.optInt("url_timeout_ms", 3000), ALLOWLIST_HEALTH_TIMEOUT_MS), 1);
        JSONObject envelope = new JSONObject(raw);
        if (!envelope.optBoolean("ok")) return false;
        JSONArray checked = envelope.getJSONObject("result").optJSONArray("nodes");
        return checked != null && checked.length() == 1 && checked.getJSONObject(0).optBoolean("alive");
    }
}
