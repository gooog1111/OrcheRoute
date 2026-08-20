package online.gooog1111.orcheroute;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.util.Log;

import org.json.JSONObject;
import org.json.JSONException;
import org.json.JSONArray;

import java.net.HttpURLConnection;
import java.net.URL;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

import mobilecore.Mobilecore;

/**
 * The sole owner of physical-network diagnosis in the Android process.
 * Consumers read immutable snapshots; they never start connectivity probes.
 */
final class ConnectivityMonitor {
    interface SettingsProvider { Settings load() throws Exception; }
    interface Listener { void onChanged(Snapshot previous, Snapshot current); }

    static final class Settings {
        final String allowlistURL;
        final String openInternetURL;
        final String transport;

        Settings(String allowlistURL, String openInternetURL, String transport) {
            this.allowlistURL = allowlistURL;
            this.openInternetURL = openInternetURL;
            this.transport = transport;
        }
    }

    static final class Snapshot {
        final String state;
        final long confirmedAt;
        final long attemptedAt;
        final String error;

        Snapshot(String state, long confirmedAt, long attemptedAt, String error) {
            this.state = state;
            this.confirmedAt = confirmedAt;
            this.attemptedAt = attemptedAt;
            this.error = error == null ? "" : error;
        }

        boolean confirmed() { return confirmedAt > 0; }
        boolean stale() { return !confirmed() || now() - confirmedAt > 35; }

        JSONObject json() throws JSONException {
            return new JSONObject()
                    .put("state", state)
                    .put("confirmed", confirmed())
                    .put("stale", stale())
                    .put("confirmed_at", confirmedAt)
                    .put("attempted_at", attemptedAt)
                    .put("error", error);
        }
    }

    private final ConnectivityManager manager;
    private final SettingsProvider settingsProvider;
    private final Listener listener;
    private final ScheduledExecutorService worker = Executors.newSingleThreadScheduledExecutor();
    private final ExecutorService probeWorkers = Executors.newFixedThreadPool(4);
    private final Object queueLock = new Object();
    private volatile Snapshot snapshot = new Snapshot("unknown", 0, 0, "");
    private boolean queued;

    private final ConnectivityManager.NetworkCallback networkCallback = new ConnectivityManager.NetworkCallback() {
        @Override public void onAvailable(Network network) { queueProbe(250); }
        @Override public void onLost(Network network) { queueProbe(250); }
        @Override public void onCapabilitiesChanged(Network network, NetworkCapabilities capabilities) { queueProbe(500); }
    };

    ConnectivityMonitor(Context context, SettingsProvider settingsProvider, Listener listener) {
        manager = (ConnectivityManager) context.getApplicationContext().getSystemService(Context.CONNECTIVITY_SERVICE);
        this.settingsProvider = settingsProvider;
        this.listener = listener;
    }

    void start() {
        if (manager != null) {
            try {
                NetworkRequest request = new NetworkRequest.Builder()
                        .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                        .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
                        .build();
                manager.registerNetworkCallback(request, networkCallback);
            } catch (Throwable error) {
                Log.w("OrcheRouteNet", "network callback unavailable", error);
            }
        }
        queueProbe(0);
        worker.scheduleWithFixedDelay(() -> queueProbe(0), 15, 15, TimeUnit.SECONDS);
    }

    Snapshot snapshot() { return snapshot; }

    private void queueProbe(long delayMs) {
        synchronized (queueLock) {
            if (queued) return;
            queued = true;
        }
        worker.schedule(() -> {
            synchronized (queueLock) { queued = false; }
            probe();
        }, delayMs, TimeUnit.MILLISECONDS);
    }

    private void probe() {
        long attemptedAt = now();
        Snapshot previous = snapshot;
        try {
            Settings settings = settingsProvider.load();
            JSONObject targetPayload = new JSONObject(Mobilecore.connectivityTargets(
                    settings.allowlistURL, settings.openInternetURL));
            if (!targetPayload.optBoolean("ok")) throw new IllegalStateException(coreError(targetPayload));
            JSONArray targets = targetPayload.getJSONObject("result").getJSONArray("targets");
            Network underlay = physicalNetwork(settings.transport);
            JSONObject observation = emptyObservation();
            if (underlay != null) {
                List<Future<Boolean>> results = new ArrayList<>();
                for (int i = 0; i < targets.length(); i++) {
                    JSONObject target = new JSONObject(targets.getJSONObject(i).toString());
                    results.add(probeWorkers.submit(() -> probeTarget(underlay, target, 3500)));
                }
                for (int i = 0; i < targets.length(); i++) {
                    boolean available = false;
                    try { available = results.get(i).get(); } catch (Throwable ignored) { }
                    setObservation(observation, targets.getJSONObject(i).optString("name"), available);
                }
            }
            JSONObject payload = new JSONObject(Mobilecore.classifyConnectivity(observation.toString()));
            JSONObject result = payload.optJSONObject("result");
            if (!payload.optBoolean("ok") || result == null) throw new IllegalStateException(coreError(payload));
            String state = result.optString("state", "offline");
            if (!"normal".equals(state) && !"allowlist".equals(state) && !"offline".equals(state)) {
                throw new IllegalStateException("invalid_connectivity_state");
            }
            Snapshot current = new Snapshot(state, attemptedAt, attemptedAt, "");
            snapshot = current;
            Log.i("OrcheRouteNet", "monitor=" + state + " underlay=" + underlay + " probes=" + observation);
			// The listener also receives unchanged confirmed snapshots. This is
			// the scheduler tick for deferred recovery (for example, a whitelist
			// pool retry every five minutes); consumers still never start probes.
			listener.onChanged(previous, current);
        } catch (Throwable error) {
            Snapshot current = new Snapshot(previous.state, previous.confirmedAt, attemptedAt, readable(error));
            snapshot = current;
            Log.w("OrcheRouteNet", "monitor probe failed; preserving " + previous.state, error);
        }
    }

    private Network physicalNetwork(String transport) {
        if (manager == null) return null;
        Network selected = null;
        int selectedScore = Integer.MIN_VALUE;
        for (Network network : manager.getAllNetworks()) {
            NetworkCapabilities capabilities = manager.getNetworkCapabilities(network);
            if (capabilities == null
                    || capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
                    || !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                    || !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
                    || !matchesTransport(capabilities, transport)) continue;
            int score = capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) ? 100 : 0;
            if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) score += 3;
            else if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) score += 2;
            else if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) score += 1;
            if (score > selectedScore) {
                selected = network;
                selectedScore = score;
            }
        }
        return selected;
    }

    private static boolean matchesTransport(NetworkCapabilities capabilities, String transport) {
        if (transport == null || transport.isEmpty() || "auto".equals(transport)) return true;
        if ("wifi".equals(transport)) return capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI);
        if ("cellular".equals(transport)) return capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR);
        if ("ethernet".equals(transport)) return capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET);
        return false;
    }

    private static boolean probeTarget(Network network, JSONObject target, int timeoutMs) {
        HttpURLConnection connection = null;
        try {
            connection = (HttpURLConnection) network.openConnection(new URL(target.getString("url")));
            connection.setConnectTimeout(timeoutMs);
            connection.setReadTimeout(timeoutMs);
            connection.setUseCaches(false);
            connection.setInstanceFollowRedirects(!target.optBoolean("open_internet", false));
            connection.setRequestProperty("Cache-Control", "no-cache, no-store");
            connection.setRequestProperty("Pragma", "no-cache");
            connection.setRequestProperty("User-Agent", "OrcheRoute Android connectivity monitor");
            int status = connection.getResponseCode();
            return target.optBoolean("expect_no_content", false)
                    ? status == HttpURLConnection.HTTP_NO_CONTENT
                    : status >= 200 && status < 300;
        } catch (Throwable ignored) {
            return false;
        } finally {
            if (connection != null) connection.disconnect();
        }
    }

    private static JSONObject emptyObservation() throws JSONException {
        return new JSONObject()
                .put("allowlist_available", false)
                .put("configured_open_available", false)
                .put("open_anchor_github_available", false)
                .put("open_anchor_mozilla_available", false);
    }

    private static void setObservation(JSONObject observation, String name, boolean available) throws JSONException {
        if ("allowlist".equals(name)) observation.put("allowlist_available", available);
        else if ("open_internet".equals(name)) observation.put("configured_open_available", available);
        else if ("open_anchor_github".equals(name)) observation.put("open_anchor_github_available", available);
        else if ("open_anchor_mozilla".equals(name)) observation.put("open_anchor_mozilla_available", available);
    }

    private static String coreError(JSONObject payload) {
        JSONObject error = payload.optJSONObject("error");
        return error == null ? "connectivity_probe_failed" : error.optString("error", "connectivity_probe_failed");
    }

    private static String readable(Throwable error) {
        return error.getMessage() == null ? error.getClass().getSimpleName() : error.getMessage();
    }

    private static long now() { return System.currentTimeMillis() / 1000L; }
}
