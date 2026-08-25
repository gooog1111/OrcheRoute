package online.gooog1111.orcheroute;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.net.Network;
import android.os.Build;
import android.os.IBinder;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import org.json.JSONObject;

import java.io.File;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

import mobilecore.Mobilecore;

/**
 * Owns Android's VPN permission and future TUN descriptor. No TUN is created
 * until the native Mihomo bridge is present, preventing accidental blackholes.
 */
public final class OrcheRouteVpnService extends VpnService {
    static final String ACTION_START = "online.gooog1111.orcheroute.START";
    static final String ACTION_STOP = "online.gooog1111.orcheroute.STOP";
    static final String ACTION_RELOAD = "online.gooog1111.orcheroute.RELOAD";
    static final String ACTION_STOP_ERROR = "online.gooog1111.orcheroute.STOP_ERROR";
	static final String ACTION_PAUSE_NETWORK = "online.gooog1111.orcheroute.PAUSE_NETWORK";
    private static final String CHANNEL_ID = "orcheroute_vpn";
    private static final int NOTIFICATION_ID = 1042;
    private static final String DIRECT_TEST_CONFIG = """
            mode: rule
            log-level: info
            ipv6: false
            find-process-mode: off
            dns:
              enable: true
              ipv6: false
              enhanced-mode: fake-ip
              fake-ip-range: 198.18.0.1/16
              nameserver:
                - 1.1.1.1
                - 8.8.8.8
            rules:
              - MATCH,DIRECT
            """;

    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private final ScheduledExecutorService healthWorker = Executors.newSingleThreadScheduledExecutor();
    private final ScheduledExecutorService trafficWorker = Executors.newSingleThreadScheduledExecutor();
    private final ScheduledExecutorService identityWorker = Executors.newSingleThreadScheduledExecutor();
    private final Object tunnelLock = new Object();
    private ParcelFileDescriptor vpnInterface;
    private volatile boolean connected;
    private volatile boolean starting;
    private volatile boolean stopping;
    private ScheduledFuture<?> healthMonitor;
    private ScheduledFuture<?> trafficMonitor;
    private ScheduledFuture<?> identityMonitor;
    private volatile String notificationNode = "";

    static void start(Context context) {
        Intent intent = new Intent(context, OrcheRouteVpnService.class).setAction(ACTION_START);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) context.startForegroundService(intent);
        else context.startService(intent);
    }

    static void stop(Context context) {
        context.startService(new Intent(context, OrcheRouteVpnService.class).setAction(ACTION_STOP));
    }

    static void stopWithError(Context context) {
        context.startService(new Intent(context, OrcheRouteVpnService.class).setAction(ACTION_STOP_ERROR));
    }

    static void reload(Context context) {
        Intent intent = new Intent(context, OrcheRouteVpnService.class).setAction(ACTION_RELOAD);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) context.startForegroundService(intent);
        else context.startService(intent);
    }

	static void pauseForNetwork(Context context) {
		Intent intent = new Intent(context, OrcheRouteVpnService.class).setAction(ACTION_PAUSE_NETWORK);
		if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) context.startForegroundService(intent);
		else context.startService(intent);
	}

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? ACTION_START : intent.getAction();
        if (ACTION_STOP.equals(action)) {
            stopping = true;
            stopTunnel();
            MobileRuntime.get(this).onDisabled();
            stopForeground(true);
            stopSelfResult(startId);
            return START_NOT_STICKY;
        }
        if (ACTION_STOP_ERROR.equals(action)) {
            stopping = true; stopTunnel(); stopForeground(true); stopSelfResult(startId);
            return START_NOT_STICKY;
        }
		if (ACTION_PAUSE_NETWORK.equals(action)) {
			createChannel();
			startForeground(NOTIFICATION_ID, notification("Ожидаем доступную сеть…"));
			stopping = true;
			worker.execute(() -> {
				stopTunnel();
				starting = false;
				stopping = false;
			});
			return START_STICKY;
		}

        createChannel();
        startForeground(NOTIFICATION_ID, notification("Запускаем VPN…"));
        if (ACTION_RELOAD.equals(action)) {
            stopping = true;
            worker.execute(() -> {
                stopTunnel();
                stopping = false;
                starting = true;
                startEngine();
            });
            return START_STICKY;
        }
        if (connected) {
            return START_STICKY;
        }
        if (!connected && !starting) {
            starting = true;
            stopping = false;
            worker.execute(this::startEngine);
        }
        return START_STICKY;
    }

    private void startEngine() {
        ParcelFileDescriptor descriptor = null;
        try {
            Log.i("OrcheRouteEngine", "start requested");
            File home = new File(getFilesDir(), "mihomo");
            if (!home.isDirectory() && !home.mkdirs()) throw new IllegalStateException("Не удалось создать каталог Mihomo");
            MobileRuntime runtime = MobileRuntime.get(this);
            Network[] underlying = runtime.selectedUnderlyingNetworks();
            if (underlying != null && underlying.length == 0) throw new IllegalStateException("Выбранный транспорт сейчас недоступен");
			// NET_CAPABILITY_VALIDATED is not sufficient: mobile operators may
			// report a validated network while only an allowlist is reachable.
			String initialNetworkMode = runtime.connectivityState();
			if ("unknown".equals(initialNetworkMode)) {
				runtime.onAwaitingNetworkDiagnosis();
				showWaiting("Определяем состояние сети…");
				return;
			}
			if ("offline".equals(initialNetworkMode)) {
				runtime.onUnderlyingOfflineDetected();
				showWaiting("Нет интернета · ожидаем сеть");
				return;
			}
			if ("allowlist".equals(initialNetworkMode) && runtime.prepareAllowlistAtStart()) {
				showWaiting("Белые списки · ищем доступный сервер");
				return;
			}
			if ("normal".equals(initialNetworkMode)) runtime.leaveAllowlistMode();
            MobileRuntime.EngineProfile profile = runtime.engineProfile();
            Log.i("OrcheRouteEngine", "profile selected proxy=" + profile.proxy() + " node=" + profile.nodeName);
            requireOk(Mobilecore.engineInit(home.getAbsolutePath(), fd -> protectAndBind((int) fd)));
            Log.i("OrcheRouteEngine", "native engine initialized");
            requireOk(Mobilecore.engineLoadConfig(profile.proxy() ? profile.config : DIRECT_TEST_CONFIG));
            Log.i("OrcheRouteEngine", "configuration loaded");
            if (stopping) return;

            Builder builder = new Builder()
                    .setSession("OrcheRoute")
                    .setMtu(9000)
                    .addAddress("172.19.0.1", 30)
                    .addRoute("0.0.0.0", 0)
                    .addDnsServer("172.19.0.2")
                    .setBlocking(false)
                    .setConfigureIntent(contentIntent());
            String tunGateway = "172.19.0.1/30";
            if (runtime.ipv6Enabled()) {
                builder.addAddress("fd00:6f72:6368::1", 126).addRoute("::", 0);
                tunGateway += ",fd00:6f72:6368::1/126";
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) builder.setMetered(false);
            descriptor = builder.establish();
            if (descriptor == null) throw new IllegalStateException("Android отклонил создание TUN");
            // There is no VPN NetworkAgent before establish(). Samsung and
            // some other Android builds return false when an underlying
            // network is assigned too early. In automatic mode Android's
            // default network tracking is already the desired behaviour.
            if (underlying != null && !setUnderlyingNetworks(underlying)) {
                throw new IllegalStateException("Android не применил выбранный транспорт");
            }
            ParcelFileDescriptor engineDescriptor = ParcelFileDescriptor.dup(descriptor.getFileDescriptor());
            int engineFd = engineDescriptor.detachFd();
            synchronized (tunnelLock) {
                if (stopping) {
                    ParcelFileDescriptor.adoptFd(engineFd).close();
                    return;
                }
                vpnInterface = descriptor;
                descriptor = null;
                requireOk(Mobilecore.engineStartTun(engineFd, "gvisor", tunGateway, "172.19.0.2"));
                connected = true;
            }
            Log.i("OrcheRouteEngine", "TUN started node=" + profile.nodeName);
            if (profile.proxy()) MobileRuntime.get(this).onProxyConnected(profile.nodeName, profile.nodeID);
            else MobileRuntime.get(this).onDirectTestConnected();
            if (profile.proxy()) startHealthMonitor();
            notificationNode = profile.proxy() ? profile.nodeName : "DIRECT";
            startTrafficMonitor();
            startIdentityMonitor();
            NotificationManager manager = getSystemService(NotificationManager.class);
            manager.notify(NOTIFICATION_ID, notification(profile.proxy()
                    ? "VPN работает · " + profile.nodeName
                    : "VPN работает · DIRECT"));
        } catch (Throwable error) {
            Log.e("OrcheRouteEngine", "start failed", error);
            if (descriptor != null) {
                try { descriptor.close(); } catch (Exception ignored) { }
            }
            stopTunnel();
            MobileRuntime runtime = MobileRuntime.get(this);
            if (runtime.isWhitelistPoolBuilding()) {
                notificationNode = "Белые списки";
                NotificationManager manager = getSystemService(NotificationManager.class);
                manager.notify(NOTIFICATION_ID, notification("Проверяем серверы и формируем рабочий список серверов…"));
                return;
            }
            if (runtime.isAllowlistModeActive()) {
                try {
                    String next = runtime.failoverWhitelistNode();
                    if (!next.isEmpty() && !stopping) {
                        runtime.onRestrictedNetworkDetected();
                        reload(this);
                        return;
                    }
                } catch (Throwable ignored) { }
            }
            runtime.onEngineError(error.getMessage());
            stopForeground(true);
            stopSelf();
        } finally {
            starting = false;
        }
    }

    private static void requireOk(String result) throws Exception {
        JSONObject payload = new JSONObject(result);
        if (payload.optBoolean("ok", false)) return;
        JSONObject error = payload.optJSONObject("error");
        throw new IllegalStateException(error == null ? "Ошибка Mihomo" : error.optString("error", "Ошибка Mihomo"));
    }

	private void showWaiting(String text) {
		NotificationManager manager = getSystemService(NotificationManager.class);
		manager.notify(NOTIFICATION_ID, notification(text));
	}

    @Override
    public void onRevoke() {
        stopping = true;
        stopTunnel();
        MobileRuntime.get(this).onDisabled();
        stopSelf();
        super.onRevoke();
    }

    @Override
    public void onDestroy() {
        stopping = true;
        stopTunnel();
        worker.shutdownNow();
        healthWorker.shutdownNow();
        trafficWorker.shutdownNow();
        identityWorker.shutdownNow();
        if (connected) MobileRuntime.get(this).onDisabled();
        connected = false;
        super.onDestroy();
    }

    private void stopTunnel() {
        stopHealthMonitor();
        stopTrafficMonitor();
        stopIdentityMonitor();
        synchronized (tunnelLock) {
            closeVpnInterface();
            Mobilecore.engineStopTun();
            setUnderlyingNetworks(null);
            connected = false;
            notificationNode = "";
        }
    }

    /**
     * Keeps every Mihomo outbound socket outside the Android TUN and on the
     * physical network selected by the connectivity monitor. Protecting the
     * descriptor alone is insufficient while Wi-Fi is the system default: a
     * cellular qualification would otherwise produce Wi-Fi false positives.
     */
    private boolean protectAndBind(int fd) {
        if (!protect(fd)) return false;
        Network network = MobileRuntime.get(this).identityPhysicalNetwork();
        if (network == null) return false;
        try (ParcelFileDescriptor duplicate = ParcelFileDescriptor.fromFd(fd)) {
            network.bindSocket(duplicate.getFileDescriptor());
            return true;
        } catch (Exception error) {
            Log.w("OrcheRouteEngine", "Unable to bind outbound socket to selected physical network", error);
            return false;
        }
    }

    private void startTrafficMonitor() {
        stopTrafficMonitor();
        trafficMonitor = trafficWorker.scheduleWithFixedDelay(this::updateTrafficNotification, 1, 1, TimeUnit.SECONDS);
    }

    private void stopTrafficMonitor() {
        ScheduledFuture<?> monitor = trafficMonitor;
        trafficMonitor = null;
        if (monitor != null) monitor.cancel(false);
    }

    private void updateTrafficNotification() {
        if (!connected || stopping) return;
        try {
            JSONObject payload = new JSONObject(Mobilecore.engineTraffic());
            if (!payload.optBoolean("ok")) return;
            JSONObject traffic = payload.getJSONObject("result");
            String text = "↓ " + formatRate(traffic.optLong("download_bps"))
                    + " · ↑ " + formatRate(traffic.optLong("upload_bps"));
            getSystemService(NotificationManager.class).notify(NOTIFICATION_ID, notification(text));
        } catch (Throwable ignored) { }
    }

    private static String formatRate(long bytes) {
        if (bytes < 1024) return bytes + " Б/с";
        if (bytes < 1024L * 1024L) return String.format(java.util.Locale.ROOT, "%.1f КБ/с", bytes / 1024.0);
        if (bytes < 1024L * 1024L * 1024L) return String.format(java.util.Locale.ROOT, "%.1f МБ/с", bytes / (1024.0 * 1024.0));
        return String.format(java.util.Locale.ROOT, "%.2f ГБ/с", bytes / (1024.0 * 1024.0 * 1024.0));
    }

    private void startIdentityMonitor() {
        stopIdentityMonitor();
        identityMonitor = identityWorker.scheduleWithFixedDelay(this::updateConnectionIdentities, 2, 30, TimeUnit.SECONDS);
    }

    private void stopIdentityMonitor() {
        ScheduledFuture<?> monitor = identityMonitor;
        identityMonitor = null;
        if (monitor != null) monitor.cancel(false);
    }

    private void updateConnectionIdentities() {
        if (!connected || stopping) return;
        MobileRuntime runtime = MobileRuntime.get(this);
        JSONObject direct = probeIdentity(runtime.identityPhysicalNetwork());
        if (direct != null) {
            try { runtime.updateConnectionIdentity("direct", direct); } catch (Throwable ignored) { }
        }
        JSONObject proxy = probeIdentity(null);
        if (proxy != null) {
            try { runtime.updateConnectionIdentity("proxy", proxy); } catch (Throwable ignored) { }
        }
    }

    private static JSONObject probeIdentity(Network network) {
        HttpURLConnection connection = null;
        try {
            URL url = new URL("https://www.cloudflare.com/cdn-cgi/trace");
            connection = (HttpURLConnection) (network == null ? url.openConnection() : network.openConnection(url));
            connection.setConnectTimeout(5000);
            connection.setReadTimeout(5000);
            connection.setUseCaches(false);
            connection.setInstanceFollowRedirects(false);
            connection.setRequestProperty("Cache-Control", "no-cache, no-store");
            connection.setRequestProperty("User-Agent", "OrcheRoute Android identity monitor");
            if (connection.getResponseCode() != HttpURLConnection.HTTP_OK) return null;
            StringBuilder trace = new StringBuilder();
            try (BufferedReader reader = new BufferedReader(new InputStreamReader(connection.getInputStream(), StandardCharsets.UTF_8))) {
                String line;
                while ((line = reader.readLine()) != null && trace.length() < 8192) trace.append(line).append('\n');
            }
            JSONObject envelope = new JSONObject(Mobilecore.parseConnectionIdentity(trace.toString()));
            return envelope.optBoolean("ok") ? envelope.optJSONObject("result") : null;
        } catch (Throwable error) {
            Log.d("OrcheRouteIdentity", (network == null ? "proxy" : "direct") + " identity unavailable", error);
            return null;
        } finally {
            if (connection != null) connection.disconnect();
        }
    }

    private void startHealthMonitor() {
        stopHealthMonitor();
        healthMonitor = healthWorker.scheduleWithFixedDelay(this::probeActiveProxy, 8, 15, TimeUnit.SECONDS);
    }

    private void stopHealthMonitor() {
        ScheduledFuture<?> monitor = healthMonitor;
        healthMonitor = null;
        if (monitor != null) monitor.cancel(false);
    }

    private void probeActiveProxy() {
        if (!connected || stopping) return;
        MobileRuntime runtime = MobileRuntime.get(this);
        if (!runtime.automaticFailoverEnabled()) {
            return;
        }
        HttpURLConnection connection = null;
        try {
			String networkMode = runtime.connectivityState();
			boolean restrictedNetwork = "allowlist".equals(networkMode);
			if (restrictedNetwork) {
				boolean changed = runtime.enterAllowlistMode();
				if (changed) return;
			}
			if ("normal".equals(networkMode) && runtime.leaveAllowlistMode()) {
				if (connected && !stopping) reload(this);
				return;
			}
            connection = (HttpURLConnection) new URL("https://cp.cloudflare.com/generate_204").openConnection();
            connection.setConnectTimeout(7000);
            connection.setReadTimeout(7000);
            connection.setInstanceFollowRedirects(false);
            int status = connection.getResponseCode();
            if (status == 204 || (status >= 200 && status < 400)) {
				if ("select".equals(runtime.onProxyHealth(true)) && connected && !stopping) reload(this);
                return;
            }
            throw new IllegalStateException("HTTP " + status);
        } catch (Throwable ignored) {
			String networkMode = runtime.connectivityState();
			if ("offline".equals(networkMode)) {
				runtime.onUnderlyingOfflineDetected();
				return;
			}
			if ("allowlist".equals(networkMode)) {
				runtime.onProxyHealth(false);
				runtime.onRestrictedNetworkDetected();
				try {
					String next = runtime.failoverWhitelistNode();
					if (!next.isEmpty() && connected && !stopping) reload(this);
				} catch (Throwable error) {
					runtime.onEngineError(error.getMessage());
				}
				return;
			}
			if ("select".equals(runtime.onProxyHealth(false)) && connected && !stopping) reload(this);
        } finally {
            if (connection != null) connection.disconnect();
        }
    }

    private void closeVpnInterface() {
        ParcelFileDescriptor descriptor = vpnInterface;
        vpnInterface = null;
        if (descriptor != null) {
            try { descriptor.close(); } catch (Exception ignored) { }
        }
    }

    @Override
    public IBinder onBind(Intent intent) {
        return super.onBind(intent);
    }

    private void createChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return;
        NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID,
                "OrcheRoute VPN",
                NotificationManager.IMPORTANCE_LOW
        );
        channel.setDescription("Состояние VPN OrcheRoute");
        getSystemService(NotificationManager.class).createNotificationChannel(channel);
    }

    private Notification notification(String text) {
        Notification.Builder builder = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL_ID)
                : new Notification.Builder(this);
        return builder
                .setSmallIcon(R.drawable.ic_notification)
                .setContentTitle(notificationNode.isEmpty() ? "OrcheRoute" : "OrcheRoute · " + notificationNode)
                .setContentText(text)
                .setContentIntent(contentIntent())
                .setCategory(Notification.CATEGORY_SERVICE)
                .setVisibility(Notification.VISIBILITY_PUBLIC)
                .setOnlyAlertOnce(true)
                .setShowWhen(false)
                .addAction(new Notification.Action.Builder(
                        android.R.drawable.ic_menu_close_clear_cancel,
                        "Выключить",
                        stopIntent()
                ).build())
                .setOngoing(true)
                .build();
    }

    private PendingIntent contentIntent() {
        Intent launch = new Intent(this, MainActivity.class);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) flags |= PendingIntent.FLAG_IMMUTABLE;
        return PendingIntent.getActivity(this, 0, launch, flags);
    }

    private PendingIntent stopIntent() {
        Intent stop = new Intent(this, OrcheRouteVpnService.class).setAction(ACTION_STOP);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) flags |= PendingIntent.FLAG_IMMUTABLE;
        return PendingIntent.getService(this, 1, stop, flags);
    }
}
