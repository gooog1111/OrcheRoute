package online.gooog1111.orcheroute;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.util.Log;

import org.json.JSONObject;
import org.json.JSONException;

import java.util.concurrent.Executors;
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

        Settings(String allowlistURL, String openInternetURL) {
            this.allowlistURL = allowlistURL;
            this.openInternetURL = openInternetURL;
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
            JSONObject payload = new JSONObject(Mobilecore.probeConnectivity(
                    settings.allowlistURL, settings.openInternetURL, 3500));
            JSONObject result = payload.optJSONObject("result");
            if (!payload.optBoolean("ok") || result == null) throw new IllegalStateException(coreError(payload));
            String state = result.optString("state", "offline");
            if (!"normal".equals(state) && !"allowlist".equals(state) && !"offline".equals(state)) {
                throw new IllegalStateException("invalid_connectivity_state");
            }
            Snapshot current = new Snapshot(state, attemptedAt, attemptedAt, "");
            snapshot = current;
            Log.i("OrcheRouteNet", "monitor=" + state + " confirmed_at=" + attemptedAt);
            if (!previous.state.equals(current.state) || !previous.confirmed()) listener.onChanged(previous, current);
        } catch (Throwable error) {
            Snapshot current = new Snapshot(previous.state, previous.confirmedAt, attemptedAt, readable(error));
            snapshot = current;
            Log.w("OrcheRouteNet", "monitor probe failed; preserving " + previous.state, error);
        }
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
