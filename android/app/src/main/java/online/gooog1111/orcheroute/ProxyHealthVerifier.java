package online.gooog1111.orcheroute;

import org.json.JSONArray;
import org.json.JSONObject;

import mobilecore.Mobilecore;

/** Rechecks one active proxy through Mihomo without touching the running TUN. */
final class ProxyHealthVerifier {
    private ProxyHealthVerifier() { }

    static boolean verify(JSONObject node, JSONObject defaults) throws Exception {
        if (node == null || node.optJSONObject("proxy") == null || defaults == null) return false;
        JSONArray urls = defaults.optJSONArray("url_test_urls");
        if (urls == null || urls.length() == 0) return false;
        JSONArray proxies = new JSONArray().put(new JSONObject(node.getJSONObject("proxy").toString()));
        String raw = Mobilecore.engineTestProxiesMulti(
                proxies.toString(), urls.toString(), defaults.optInt("url_timeout_ms", 3000), 1);
        JSONObject envelope = new JSONObject(raw);
        if (!envelope.optBoolean("ok")) return false;
        JSONArray checked = envelope.getJSONObject("result").optJSONArray("nodes");
        return checked != null && checked.length() == 1 && checked.getJSONObject(0).optBoolean("alive");
    }
}
