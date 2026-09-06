package online.gooog1111.orcheroute;

import org.json.JSONArray;
import org.json.JSONObject;

import mobilecore.Mobilecore;

/** Rechecks one active proxy through Mihomo without HTTP or touching the running TUN. */
final class ProxyHealthVerifier {
	private static final int ALLOWLIST_HEALTH_TIMEOUT_MS = 8_000;

    private ProxyHealthVerifier() { }

    static boolean verify(JSONObject node, JSONObject defaults) throws Exception {
        if (node == null || node.optJSONObject("proxy") == null || defaults == null) return false;
        JSONArray urls = defaults.optJSONArray("url_test_urls");
        if (urls == null || urls.length() == 0) return false;
        String raw = Mobilecore.engineVerifyProxyTLS(
				node.getJSONObject("proxy").toString(), urls.toString(), ALLOWLIST_HEALTH_TIMEOUT_MS);
        JSONObject envelope = new JSONObject(raw);
        if (!envelope.optBoolean("ok")) return false;
        return envelope.getJSONObject("result").optBoolean("alive");
    }
}
