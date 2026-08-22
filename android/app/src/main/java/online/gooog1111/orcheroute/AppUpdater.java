package online.gooog1111.orcheroute;

import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.content.pm.Signature;
import android.net.Uri;
import android.os.Build;
import android.provider.Settings;

import androidx.core.content.FileProvider;

import org.json.JSONObject;
import org.json.JSONArray;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

final class AppUpdater {
	private static final String STABLE_MANIFEST_URL = "https://github.com/gooog1111/OrcheRoute/releases/latest/download/android-update.json";
	private static final String BETA_MANIFEST_URL = "https://github.com/gooog1111/OrcheRoute/releases/download/android-beta/android-update.json";
	private static final String PREFERENCES = "orcheroute_app_update";
	private static final String BETA_ENABLED = "beta_enabled";
    private static final Pattern VERSION_CODE_PATTERN = Pattern.compile("(?:code|vc)([0-9]+)", Pattern.CASE_INSENSITIVE);
    private static final long MAX_APK_BYTES = 256L * 1024L * 1024L;
    private final MainActivity activity;
	private final SharedPreferences preferences;
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private final String currentVersion;
    private final long currentVersionCode;
    private String state = "idle", message = "Обновление приложения ещё не проверялось", error = "";
    private String latestVersion = "", downloadURL = "", expectedSHA256 = "";
	private String channel = "stable";
	private boolean betaEnabled;
    private int latestVersionCode;
    private long current, total;
    private boolean active;
    private File pendingAPK;

    AppUpdater(MainActivity activity) {
        this.activity = activity;
        String version = "unknown"; long code = 0;
        try {
            PackageInfo installed = activity.getPackageManager().getPackageInfo(activity.getPackageName(), 0);
            version = installed.versionName == null ? "unknown" : installed.versionName;
            code = installed.getLongVersionCode();
        } catch (PackageManager.NameNotFoundException ignored) { }
        currentVersion = version; currentVersionCode = code;
		preferences = activity.getSharedPreferences(PREFERENCES, 0);
		betaEnabled = preferences.getBoolean(BETA_ENABLED, currentVersion.contains("-"));
		channel = betaEnabled ? "beta" : "stable";
    }

    synchronized String status() {
        try {
            JSONObject result = new JSONObject()
                    .put("state", state).put("message", message).put("active", active)
                    .put("current_version", currentVersion)
					.put("current_version_code", currentVersionCode)
					.put("current_prerelease", currentVersion.contains("-"))
					.put("beta_enabled", betaEnabled)
					.put("channel", channel)
                    .put("current", current).put("total", total);
            if (!latestVersion.isEmpty()) result.put("latest_version", latestVersion).put("latest_version_code", latestVersionCode);
            if (!error.isEmpty()) result.put("error", error);
            return result.toString();
        } catch (Exception impossible) { return "{\"state\":\"error\",\"message\":\"status_encode_failed\",\"active\":false}"; }
    }

    synchronized boolean check() {
        if (active) return false;
		final boolean beta = betaEnabled;
		channel = beta ? "beta" : "stable";
        set("checking", beta ? "Проверяем обновления Beta" : "Проверяем обновления Stable", "", true, 0, 0);
        worker.execute(() -> {
			try { loadManifest(beta); }
            catch (Throwable failure) { fail(failure); }
        });
        return true;
    }

    synchronized boolean downloadAndInstall() {
		return downloadAndInstall(betaEnabled);
	}

	synchronized boolean setBetaEnabled(boolean enabled) {
		if (active) return false;
		betaEnabled = enabled;
		channel = enabled ? "beta" : "stable";
		preferences.edit().putBoolean(BETA_ENABLED, enabled).apply();
		latestVersion = "";
		latestVersionCode = 0;
		downloadURL = "";
		expectedSHA256 = "";
		current = 0;
		total = 0;
		pendingAPK = null;
		set("idle", enabled ? "Выбраны обновления Beta" : "Выбраны обновления Stable", "", false, 0, 0);
		return true;
	}

	private synchronized boolean downloadAndInstall(boolean beta) {
        if (active) return false;
		channel = beta ? "beta" : "stable";
		set("checking", beta ? "Проверяем доступную Beta-версию" : "Проверяем обновление перед загрузкой", "", true, 0, 0);
        worker.execute(() -> {
            try {
				loadManifest(beta);
                synchronized (this) {
                    if (latestVersionCode <= currentVersionCode) return;
                    set("downloading", "Загружаем APK", "", true, 0, total);
                }
                File apk = download();
                verifyPackage(apk);
                synchronized (this) {
                    pendingAPK = apk;
                    set("installer", "APK проверен. Открываем системный установщик", "", false, total, total);
                }
                activity.runOnUiThread(() -> requestInstall(apk));
            } catch (Throwable failure) { fail(failure); }
        });
        return true;
    }

    void resumeInstallIfPermitted() {
        File apk;
        synchronized (this) {
            if (!"permission".equals(state) || pendingAPK == null || !canInstallPackages()) return;
            apk = pendingAPK;
            set("installer", "Разрешение получено. Открываем системный установщик", "", false, total, total);
        }
        requestInstall(apk);
    }

	private void loadManifest(boolean beta) throws Exception {
		JSONObject manifest = readJSON(beta ? BETA_MANIFEST_URL : STABLE_MANIFEST_URL);
		if (manifest.optBoolean("prerelease", false) != beta) throw new SecurityException("Канал manifest не совпадает с выбранным каналом обновления");
		JSONObject release = new JSONObject().put("tag_name", manifest.getString("tag_name"))
				.put("assets", new JSONArray().put(new JSONObject()
						.put("name", manifest.getString("asset_name"))
						.put("browser_download_url", manifest.getString("download_url"))
						.put("digest", "sha256:" + manifest.getString("sha256"))
						.put("size", manifest.getLong("size"))));
        JSONArray assets = release.getJSONArray("assets");
        JSONObject asset = null;
        for (int i = 0; i < assets.length(); i++) {
            JSONObject candidate = assets.getJSONObject(i);
            String name = candidate.optString("name", "");
            if (name.toLowerCase(java.util.Locale.ROOT).endsWith(".apk") &&
                    name.toLowerCase(java.util.Locale.ROOT).contains("arm64")) { asset = candidate; break; }
        }
		if (asset == null) throw new Exception(beta ? "В Beta release нет Android arm64 APK" : "В latest release нет Android arm64 APK");
        Matcher code = VERSION_CODE_PATTERN.matcher(asset.getString("name"));
        if (!code.find()) throw new Exception("Имя APK не содержит code<versionCode>");
        int versionCode = Integer.parseInt(code.group(1));
        String url = asset.getString("browser_download_url");
        URL parsed = new URL(url);
        if (!"https".equals(parsed.getProtocol()) || !"github.com".equalsIgnoreCase(parsed.getHost())) throw new SecurityException("Недопустимый адрес release-asset");
        String digest = asset.optString("digest", "").toLowerCase(java.util.Locale.ROOT);
        if (!digest.startsWith("sha256:")) throw new SecurityException("GitHub release не содержит SHA-256 для APK");
        String sha = digest.substring("sha256:".length());
        if (!sha.matches("[0-9a-f]{64}")) throw new SecurityException("Некорректная контрольная сумма обновления");
        long size = asset.getLong("size");
        if (size <= 0 || size > MAX_APK_BYTES) throw new SecurityException("Некорректный размер обновления");
        synchronized (this) {
			latestVersion = release.getString("tag_name"); latestVersionCode = versionCode; channel = beta ? "beta" : "stable";
            downloadURL = url; expectedSHA256 = sha; total = size; current = 0;
            if (versionCode > currentVersionCode) set("available", "Доступна версия " + latestVersion, "", false, 0, size);
            else set("current", "Установлена актуальная версия", "", false, 0, size);
        }
    }

    private File download() throws Exception {
        File directory = new File(activity.getCacheDir(), "updates");
        if (!directory.exists() && !directory.mkdirs()) throw new Exception("Не удалось создать каталог обновлений");
        File temporary = new File(directory, "orcheroute-update.apk.part");
        File target = new File(directory, "orcheroute-update.apk");
        HttpURLConnection connection = open(downloadURL);
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        long written = 0;
        try (InputStream input = connection.getInputStream(); FileOutputStream output = new FileOutputStream(temporary)) {
            byte[] buffer = new byte[64 * 1024];
            for (int count; (count = input.read(buffer)) >= 0; ) {
                written += count;
                if (written > total || written > MAX_APK_BYTES) throw new SecurityException("Размер APK превышает заявленный");
                output.write(buffer, 0, count); digest.update(buffer, 0, count);
                synchronized (this) { current = written; message = "Загружаем APK · " + formatBytes(written) + " / " + formatBytes(total); }
            }
            output.getFD().sync();
        } finally { connection.disconnect(); }
        if (written != total) throw new SecurityException("Размер загруженного APK не совпадает");
        if (!hex(digest.digest()).equals(expectedSHA256)) throw new SecurityException("SHA-256 загруженного APK не совпадает");
        if (target.exists() && !target.delete()) throw new Exception("Не удалось заменить предыдущий APK");
        if (!temporary.renameTo(target)) throw new Exception("Не удалось подготовить APK к установке");
        return target;
    }

    private void verifyPackage(File apk) throws Exception {
        PackageManager manager = activity.getPackageManager();
        PackageInfo archive = manager.getPackageArchiveInfo(apk.getAbsolutePath(), signingFlags());
        PackageInfo installed = manager.getPackageInfo(activity.getPackageName(), signingFlags());
        if (archive == null || !activity.getPackageName().equals(archive.packageName)) throw new SecurityException("APK принадлежит другому приложению");
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            if (archive.signingInfo == null || installed.signingInfo == null ||
                    !sameSignatures(archive.signingInfo.getApkContentsSigners(), installed.signingInfo.getApkContentsSigners()))
                throw new SecurityException("Сертификат подписи APK не совпадает");
        } else if (!sameSignatures(archive.signatures, installed.signatures)) {
            throw new SecurityException("Сертификат подписи APK не совпадает");
        }
        if (archive.getLongVersionCode() <= currentVersionCode) throw new SecurityException("Загруженная версия не новее установленной");
    }

    private void requestInstall(File apk) {
        if (!canInstallPackages()) {
            synchronized (this) { set("permission", "Разрешите OrcheRoute устанавливать обновления", "", false, total, total); }
            try { activity.startActivity(new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + activity.getPackageName()))); }
            catch (ActivityNotFoundException failure) { fail(failure); }
            return;
        }
        Uri uri = FileProvider.getUriForFile(activity, activity.getPackageName() + ".files", apk);
        Intent intent = new Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive")
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_ACTIVITY_NEW_TASK);
        try { activity.startActivity(intent); }
        catch (ActivityNotFoundException failure) { fail(failure); }
    }

    private boolean canInstallPackages() { return Build.VERSION.SDK_INT < Build.VERSION_CODES.O || activity.getPackageManager().canRequestPackageInstalls(); }
    private static int signingFlags() { return Build.VERSION.SDK_INT >= Build.VERSION_CODES.P ? PackageManager.GET_SIGNING_CERTIFICATES : PackageManager.GET_SIGNATURES; }
    private static boolean sameSignatures(Signature[] left, Signature[] right) {
        if (left == null || right == null || left.length != right.length) return false;
        byte[][] a = new byte[left.length][], b = new byte[right.length][];
        for (int i = 0; i < left.length; i++) a[i] = left[i].toByteArray();
        for (int i = 0; i < right.length; i++) b[i] = right[i].toByteArray();
        Arrays.sort(a, Arrays::compare); Arrays.sort(b, Arrays::compare);
        return Arrays.deepEquals(a, b);
    }

    private static JSONObject readJSON(String value) throws Exception {
		return new JSONObject(readText(value));
	}
	private static String readText(String value) throws Exception {
        HttpURLConnection connection = open(value);
        try (InputStream input = connection.getInputStream(); ByteArrayOutputStream output = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[8192]; int total = 0;
            for (int count; (count = input.read(buffer)) >= 0; ) { total += count; if (total > 64 * 1024) throw new SecurityException("Manifest обновления слишком большой"); output.write(buffer, 0, count); }
			return output.toString(StandardCharsets.UTF_8.name());
        } finally { connection.disconnect(); }
    }
    private static HttpURLConnection open(String value) throws Exception {
        URL url = new URL(value);
        if (!"https".equals(url.getProtocol()) || !allowedGitHubHost(url.getHost())) throw new SecurityException("Сторонний адрес обновления заблокирован");
        HttpURLConnection connection = (HttpURLConnection) url.openConnection();
        connection.setConnectTimeout(10000); connection.setReadTimeout(30000); connection.setUseCaches(false);
        connection.setRequestProperty("User-Agent", "OrcheRoute Android updater/1");
        connection.setRequestProperty("Accept", "application/vnd.github+json");
        connection.setRequestProperty("X-GitHub-Api-Version", "2022-11-28");
        int status = connection.getResponseCode();
        if (status < 200 || status >= 300) { connection.disconnect(); throw new Exception("Сервер обновлений вернул HTTP " + status); }
        if (!allowedGitHubHost(connection.getURL().getHost())) { connection.disconnect(); throw new SecurityException("GitHub перенаправил загрузку на неизвестный адрес"); }
        return connection;
    }
    private static boolean allowedGitHubHost(String host) {
        String value = host == null ? "" : host.toLowerCase(java.util.Locale.ROOT);
        return "api.github.com".equals(value) || "github.com".equals(value) ||
                value.endsWith(".githubusercontent.com") || value.endsWith(".github.com");
    }
    private synchronized void fail(Throwable failure) { set("error", "Не удалось обновить приложение", readable(failure), false, current, total); }
    private synchronized void set(String state, String message, String error, boolean active, long current, long total) { this.state=state; this.message=message; this.error=error; this.active=active; this.current=current; this.total=total; }
    private static String readable(Throwable failure) { String value=failure.getMessage(); return value==null || value.trim().isEmpty() ? failure.getClass().getSimpleName() : value; }
    private static String hex(byte[] value) { StringBuilder result=new StringBuilder(value.length*2); for(byte item:value) result.append(String.format("%02x", item & 0xff)); return result.toString(); }
    private static String formatBytes(long bytes) { if(bytes>=1024L*1024L) return String.format(java.util.Locale.ROOT,"%.1f МБ",bytes/(1024d*1024d)); if(bytes>=1024) return String.format(java.util.Locale.ROOT,"%.1f КБ",bytes/1024d); return bytes+" Б"; }
}
