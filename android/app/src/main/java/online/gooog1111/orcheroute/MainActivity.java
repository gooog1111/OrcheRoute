package online.gooog1111.orcheroute;

import android.Manifest;
import android.annotation.SuppressLint;
import android.content.ActivityNotFoundException;
import android.content.Context;
import android.graphics.Color;
import android.net.Uri;
import android.net.VpnService;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Build;
import android.os.Bundle;
import android.os.PowerManager;
import android.provider.Settings;
import android.util.Log;
import android.webkit.JavascriptInterface;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.widget.FrameLayout;

import androidx.core.graphics.Insets;
import androidx.activity.ComponentActivity;
import androidx.activity.OnBackPressedCallback;
import androidx.core.view.ViewCompat;
import androidx.core.view.WindowCompat;
import androidx.core.view.WindowInsetsCompat;
import androidx.webkit.WebViewAssetLoader;
import androidx.webkit.WebViewClientCompat;

import com.google.mlkit.vision.barcode.common.Barcode;
import com.google.mlkit.vision.codescanner.GmsBarcodeScanner;
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions;
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning;

import org.json.JSONException;
import org.json.JSONObject;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.util.Collections;
import mobilecore.Mobilecore;

public final class MainActivity extends ComponentActivity {
    private static volatile MainActivity activeInstance;
    private static final String APP_HOST = WebViewAssetLoader.DEFAULT_DOMAIN;
    private static final int VPN_PERMISSION_REQUEST = 1042;
    private static final int NOTIFICATION_PERMISSION_REQUEST = 1043;
    private static final int SAVE_TEXT_FILE_REQUEST = 1044;
    private static final int OPEN_TEXT_FILE_REQUEST = 1045;
    private static final int MAX_IMPORT_BYTES = 8 * 1024 * 1024;
    private static final String PERMISSION_PREFS = "orcheroute_permission_prompts";
    private static final String BATTERY_PROMPTED = "battery_optimization_prompted";
    private static final String CAPTCHA_OVERLAY_PROMPTED = "captcha_overlay_prompted";
    private static final String EXTRA_FREETURN_CAPTCHA = "free_turn_captcha_url";
    private static volatile String pendingFreeTURNCaptchaURL = "";
    private WebView webView;
    private FrameLayout root;
    private WebViewAssetLoader assetLoader;
    private String pendingTextFile = "";
    private AppUpdater appUpdater;
    private VkCaptchaDialog vkCaptchaDialog;
    private boolean vpnPermissionReload;
    private volatile String pendingVkCaptchaURL = "";
    private volatile boolean waitingForCaptchaOverlayPermission;
    private volatile boolean freeTURNCaptchaActive;

    @Override
    @SuppressLint("SetJavaScriptEnabled")
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        activeInstance = this;
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        WindowCompat.setDecorFitsSystemWindows(getWindow(), false);
        getWindow().setStatusBarColor(Color.rgb(2, 7, 6));
        getWindow().setNavigationBarColor(Color.rgb(2, 7, 6));

        root = new FrameLayout(this);
        root.setBackgroundColor(Color.rgb(2, 7, 6));
        webView = new WebView(this);
        webView.setBackgroundColor(Color.rgb(2, 7, 6));
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setAllowFileAccess(false);
        settings.setAllowContentAccess(false);
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        assetLoader = new WebViewAssetLoader.Builder()
                .addPathHandler("/", new EmbeddedPathHandler())
                .build();
        webView.addJavascriptInterface(new AndroidBridge(), "OrcheRouteAndroid");
        webView.setWebViewClient(new EmbeddedClient());
        root.addView(webView, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT
        ));
        ViewCompat.setOnApplyWindowInsetsListener(root, (view, windowInsets) -> {
            Insets safe = windowInsets.getInsets(
                    WindowInsetsCompat.Type.systemBars() | WindowInsetsCompat.Type.displayCutout()
            );
            Insets keyboard = windowInsets.getInsets(WindowInsetsCompat.Type.ime());
            view.setPadding(safe.left, safe.top, safe.right, Math.max(safe.bottom, keyboard.bottom));
            return WindowInsetsCompat.CONSUMED;
        });
        setContentView(root);
        appUpdater = new AppUpdater(this);
        vkCaptchaDialog = new VkCaptchaDialog(this, root, new VkCaptchaDialog.Callback() {
            @Override
            public void onSuccess(String successToken) {
                // FreeTURN's localhost proxy receives the CAPTCHA response.
            }

            @Override
            public void onCancel() {
                if (freeTURNCaptchaActive) {
                    freeTURNCaptchaActive = false;
                    OrcheRouteVpnService.stopWithError(MainActivity.this);
                    MobileRuntime.get(MainActivity.this).onEngineError("Подключение FreeTURN отменено");
                    return;
                }
            }

            @Override
            public void onError(String message) {
                if (freeTURNCaptchaActive) {
                    // A transient WebView or upstream resource failure must not
                    // cancel the FreeTURN session. The local CAPTCHA proxy stays
                    // alive and VkCaptchaDialog shows the retryable error. Only
                    // an explicit user cancellation stops the VPN startup.
                    Log.w("OrcheRouteFreeTURN", "CAPTCHA page error: " + message);
                }
            }
        });
        handleFreeTURNCaptcha(getIntent().getStringExtra(EXTRA_FREETURN_CAPTCHA));
        ViewCompat.requestApplyInsets(root);
        getOnBackPressedDispatcher().addCallback(this, new OnBackPressedCallback(true) {
            @Override
            public void handleOnBackPressed() {
                if (vkCaptchaDialog != null && vkCaptchaDialog.isOpen()) vkCaptchaDialog.close(true);
                else if (webView != null && webView.canGoBack()) webView.goBack();
                else MainActivity.this.finish();
            }
        });
        webView.loadUrl("https://" + APP_HOST + "/index.html");
        requestOperationalPermissions();
    }

    @Override
    protected void onResume() {
        super.onResume();
        if (appUpdater != null) appUpdater.resumeInstallIfPermitted();
        if (waitingForCaptchaOverlayPermission) {
            waitingForCaptchaOverlayPermission = false;
            String redirectURL = pendingVkCaptchaURL;
            pendingVkCaptchaURL = "";
            if (!redirectURL.isEmpty() && Settings.canDrawOverlays(this)
                    && vkCaptchaDialog != null && vkCaptchaDialog.isOpen()) {
                vkCaptchaDialog.close(false);
                vkCaptchaDialog.open(redirectURL, true);
            }
        }
    }

    @Override
    protected void onDestroy() {
        if (activeInstance == this) activeInstance = null;
        if (vkCaptchaDialog != null) vkCaptchaDialog.close(false);
        super.onDestroy();
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleFreeTURNCaptcha(intent.getStringExtra(EXTRA_FREETURN_CAPTCHA));
    }

    static void showFreeTURNCaptcha(Context context, String rawURL) {
        String url = rawURL == null ? "" : rawURL.trim();
        MainActivity activity = activeInstance;
        if (activity == null) {
            pendingFreeTURNCaptchaURL = url;
            if (url.isEmpty() || context == null) return;
            Intent intent = new Intent(context, MainActivity.class)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_SINGLE_TOP)
                    .putExtra(EXTRA_FREETURN_CAPTCHA, url);
            try {
                context.startActivity(intent);
            } catch (RuntimeException error) {
                pendingFreeTURNCaptchaURL = "";
                OrcheRouteVpnService.stopWithError(context);
                MobileRuntime.get(context).onEngineError("Android не разрешил открыть CAPTCHA FreeTURN");
            }
            return;
        }
        activity.handleFreeTURNCaptcha(url);
    }

    private void handleFreeTURNCaptcha(String rawURL) {
        String requested = rawURL == null || rawURL.isBlank() ? pendingFreeTURNCaptchaURL : rawURL.trim();
        pendingFreeTURNCaptchaURL = "";
        runOnUiThread(() -> {
            String url = requested;
            if (url.isEmpty()) {
                freeTURNCaptchaActive = false;
                if (vkCaptchaDialog != null) vkCaptchaDialog.complete();
                return;
            }
            freeTURNCaptchaActive = true;
            if (!openVkCaptcha(url)) {
                freeTURNCaptchaActive = false;
                OrcheRouteVpnService.stopWithError(this);
                MobileRuntime.get(this).onEngineError("Получен недопустимый адрес CAPTCHA FreeTURN");
            }
        });
    }

    private void requestOperationalPermissions() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(
                    new String[]{Manifest.permission.POST_NOTIFICATIONS},
                    NOTIFICATION_PERMISSION_REQUEST
            );
            return;
        }
        requestBatteryOptimizationExemptionOnce();
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode == NOTIFICATION_PERMISSION_REQUEST) {
            requestBatteryOptimizationExemptionOnce();
        }
    }

    private void requestBatteryOptimizationExemptionOnce() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M) return;
        PowerManager powerManager = (PowerManager) getSystemService(POWER_SERVICE);
        if (powerManager == null || powerManager.isIgnoringBatteryOptimizations(getPackageName())) return;
        if (getSharedPreferences(PERMISSION_PREFS, MODE_PRIVATE).getBoolean(BATTERY_PROMPTED, false)) return;

        getSharedPreferences(PERMISSION_PREFS, MODE_PRIVATE)
                .edit()
                .putBoolean(BATTERY_PROMPTED, true)
                .apply();
        try {
            startActivity(new Intent(
                    Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
                    Uri.parse("package:" + getPackageName())
            ));
        } catch (ActivityNotFoundException ignored) {
            try {
                startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS));
            } catch (ActivityNotFoundException unavailable) {
                // Some vendor Android builds expose neither settings screen.
            }
        }
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == OPEN_TEXT_FILE_REQUEST) {
            if (resultCode == RESULT_OK && data != null && data.getData() != null) {
                readSelectedTextFile(data.getData());
            }
            return;
        }
        if (requestCode == SAVE_TEXT_FILE_REQUEST) {
            if (resultCode == RESULT_OK && data != null && data.getData() != null) {
                try (OutputStream output = getContentResolver().openOutputStream(data.getData(), "wt")) {
                    if (output == null) throw new IOException("Не удалось открыть выбранный файл");
                    output.write(pendingTextFile.getBytes(StandardCharsets.UTF_8));
                    output.flush();
                } catch (IOException error) {
                    dispatchFileSaveError(error.getMessage() == null ? "Не удалось сохранить файл." : error.getMessage());
                }
            }
            pendingTextFile = "";
            return;
        }
        if (requestCode != VPN_PERMISSION_REQUEST) return;
        if (resultCode == RESULT_OK) {
            MobileRuntime.get(this).onPermissionGranted();
            if (vpnPermissionReload) OrcheRouteVpnService.reload(this);
            else OrcheRouteVpnService.start(this);
        } else {
            MobileRuntime.get(this).onPermissionDenied();
        }
        vpnPermissionReload = false;
    }

    private void requestVpnPermission() {
        requestVpnPermission(false);
    }

    private void requestVpnPermissionForReload() {
        requestVpnPermission(true);
    }

    private void requestVpnPermission(boolean reload) {
        runOnUiThread(() -> {
            vpnPermissionReload = reload;
            Intent permission = VpnService.prepare(this);
            if (permission == null) {
                MobileRuntime.get(this).onPermissionGranted();
                if (vpnPermissionReload) OrcheRouteVpnService.reload(this);
                else OrcheRouteVpnService.start(this);
                vpnPermissionReload = false;
                return;
            }
            MobileRuntime.get(this).onPermissionRequired();
            startActivityForResult(permission, VPN_PERMISSION_REQUEST);
        });
    }

    private void scanQrCode() {
        runOnUiThread(() -> {
            GmsBarcodeScannerOptions options = new GmsBarcodeScannerOptions.Builder()
                    .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
                    .enableAutoZoom()
                    .build();
            GmsBarcodeScanner scanner = GmsBarcodeScanning.getClient(this, options);
            scanner.startScan()
                    .addOnSuccessListener(barcode -> {
                        String value = barcode.getRawValue();
                        if (value == null || value.trim().isEmpty()) {
                            dispatchQrError("QR-код не содержит данных.");
                            return;
                        }
                        dispatchQrResult(value);
                    })
                    .addOnCanceledListener(() -> {
                        // Закрытие системного сканера пользователем не является ошибкой.
                    })
                    .addOnFailureListener(error -> dispatchQrError(
                            error.getMessage() == null
                                    ? "Не удалось открыть QR-сканер."
                                    : error.getMessage()
                    ));
        });
    }

    private void saveTextFile(String filename, String content) {
        runOnUiThread(() -> {
            pendingTextFile = content == null ? "" : content;
            Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT);
            intent.addCategory(Intent.CATEGORY_OPENABLE);
            intent.setType("text/plain");
            intent.putExtra(Intent.EXTRA_TITLE, filename == null || filename.trim().isEmpty() ? "orcheroute.txt" : filename);
            try {
                startActivityForResult(intent, SAVE_TEXT_FILE_REQUEST);
            } catch (ActivityNotFoundException error) {
                pendingTextFile = "";
                dispatchFileSaveError("На устройстве нет приложения для сохранения файлов.");
            }
        });
    }

    private void openTextFile() {
        runOnUiThread(() -> {
            Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
            intent.addCategory(Intent.CATEGORY_OPENABLE);
            intent.setType("*/*");
            intent.putExtra(Intent.EXTRA_MIME_TYPES, new String[]{
                    "text/plain", "application/json", "application/yaml", "application/x-yaml",
                    "application/octet-stream"
            });
            try {
                startActivityForResult(intent, OPEN_TEXT_FILE_REQUEST);
            } catch (ActivityNotFoundException error) {
                dispatchFileOpenError("На устройстве нет приложения для выбора файлов.");
            }
        });
    }

    private void hideKeyboard() {
        runOnUiThread(() -> {
            if (webView == null) return;
            WindowCompat.getInsetsController(getWindow(), webView)
                    .hide(WindowInsetsCompat.Type.ime());
        });
    }

    private void readSelectedTextFile(Uri uri) {
        try (InputStream input = getContentResolver().openInputStream(uri);
             ByteArrayOutputStream output = new ByteArrayOutputStream()) {
            if (input == null) throw new IOException("Не удалось открыть выбранный файл");
            byte[] buffer = new byte[32 * 1024];
            int total = 0;
            for (int count; (count = input.read(buffer)) >= 0; ) {
                total += count;
                if (total > MAX_IMPORT_BYTES) throw new IOException("Файл больше 8 МБ");
                output.write(buffer, 0, count);
            }
            String content = new String(output.toByteArray(), StandardCharsets.UTF_8);
            if (content.trim().isEmpty()) throw new IOException("Выбранный файл пуст");
            dispatchFileOpenResult(content);
        } catch (IOException | SecurityException error) {
            dispatchFileOpenError(error.getMessage() == null ? "Не удалось прочитать выбранный файл." : error.getMessage());
        }
    }

    private void dispatchQrResult(String value) {
        runOnUiThread(() -> webView.evaluateJavascript(
                "window.dispatchEvent(new CustomEvent('orcheroute:qr-scan',{detail:{value:"
                        + JSONObject.quote(value)
                        + "}}));",
                null
        ));
    }

    private void dispatchQrError(String message) {
        runOnUiThread(() -> webView.evaluateJavascript(
                "window.dispatchEvent(new CustomEvent('orcheroute:qr-error',{detail:{message:"
                        + JSONObject.quote(message)
                        + "}}));",
                null
        ));
    }

    private void dispatchFileSaveError(String message) {
        runOnUiThread(() -> webView.evaluateJavascript(
                "window.dispatchEvent(new CustomEvent('orcheroute:file-save-error',{detail:{message:"
                        + JSONObject.quote(message)
                        + "}}));",
                null
        ));
    }

    private void dispatchFileOpenResult(String content) {
        runOnUiThread(() -> webView.evaluateJavascript(
                "window.dispatchEvent(new CustomEvent('orcheroute:file-open',{detail:{content:"
                        + JSONObject.quote(content)
                        + "}}));",
                null
        ));
    }

    private void dispatchFileOpenError(String message) {
        runOnUiThread(() -> webView.evaluateJavascript(
                "window.dispatchEvent(new CustomEvent('orcheroute:file-open-error',{detail:{message:"
                        + JSONObject.quote(message)
                        + "}}));",
                null
        ));
    }

    private boolean openVkCaptcha(String redirectURL) {
        if (vkCaptchaDialog == null) return false;
        boolean canOverlay = Settings.canDrawOverlays(this);
        if (!vkCaptchaDialog.open(redirectURL, canOverlay)) return false;
        if (canOverlay) return true;

        boolean prompted = getSharedPreferences(PERMISSION_PREFS, MODE_PRIVATE)
                .getBoolean(CAPTCHA_OVERLAY_PROMPTED, false);
        if (prompted) return true;
        getSharedPreferences(PERMISSION_PREFS, MODE_PRIVATE).edit()
                .putBoolean(CAPTCHA_OVERLAY_PROMPTED, true)
                .apply();
        pendingVkCaptchaURL = redirectURL;
        waitingForCaptchaOverlayPermission = true;
        try {
            startActivity(new Intent(
                    Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
                    Uri.parse("package:" + getPackageName())
            ));
        } catch (ActivityNotFoundException error) {
            waitingForCaptchaOverlayPermission = false;
            pendingVkCaptchaURL = "";
        }
        return true;
    }

    private final class EmbeddedClient extends WebViewClientCompat {
        @Override
        public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest request) {
            return assetLoader.shouldInterceptRequest(request.getUrl());
        }

        @Override
        @SuppressWarnings("deprecation")
        public WebResourceResponse shouldInterceptRequest(WebView view, String url) {
            return assetLoader.shouldInterceptRequest(Uri.parse(url));
        }
    }

    private final class EmbeddedPathHandler implements WebViewAssetLoader.PathHandler {
        @Override
        public WebResourceResponse handle(String path) {
            if (path == null || path.isEmpty()) path = "index.html";
            if (path.startsWith("api/")) return json(503, "{\"error\":\"mobile_runtime_not_started\"}");
            if (unsafeAssetPath(path)) return blocked();
            try {
                InputStream stream = getAssets().open("web/" + path);
                return new WebResourceResponse(mimeType(path), "UTF-8", 200, "OK", Collections.singletonMap("Cache-Control", "no-cache"), stream);
            } catch (IOException ignored) {
                return response(404, "Not Found", "text/plain", new byte[0]);
            }
        }
    }

    public final class AndroidBridge {
        @JavascriptInterface
        public String platform() {
            return "android";
        }

        @JavascriptInterface
        public String capabilities() {
            for (String className : new String[]{"mobilecore.Mobilecore", "go.mobilecore.Mobilecore"}) {
                try {
                    Class<?> type = Class.forName(className);
                    Method method = type.getMethod("capabilities");
                    return String.valueOf(method.invoke(null));
                } catch (ReflectiveOperationException ignored) {
                }
            }
            return "{\"ok\":false,\"error\":{\"error\":\"mobilecore_unavailable\"}}";
        }

        @JavascriptInterface
        public String request(String method, String path, String body) {
            return MobileRuntime.get(MainActivity.this).request(
                    method, path, body, MainActivity.this::requestVpnPermission
            );
        }

        @JavascriptInterface
        public void scanQr() {
            scanQrCode();
        }

        @JavascriptInterface
        public void openTextFile() {
            MainActivity.this.openTextFile();
        }

        @JavascriptInterface
        public void saveTextFile(String filename, String content) {
            MainActivity.this.saveTextFile(filename, content);
        }

        @JavascriptInterface
        public void hideKeyboard() {
            MainActivity.this.hideKeyboard();
        }

        @JavascriptInterface
        public String appUpdateStatus() {
            return appUpdater.status();
        }

        @JavascriptInterface
        public boolean checkAppUpdate() {
            return appUpdater.check();
        }

        @JavascriptInterface
		public boolean installAppUpdate() {
			return appUpdater.downloadAndInstall();
		}

		@JavascriptInterface
		public boolean setAppUpdateBetaEnabled(boolean enabled) {
			return appUpdater.setBetaEnabled(enabled);
		}
    }

    private static WebResourceResponse json(int status, String value) {
        return response(status, "Unavailable", "application/json", value.getBytes(StandardCharsets.UTF_8));
    }

    private static WebResourceResponse blocked() {
        return response(403, "Forbidden", "text/plain", new byte[0]);
    }

    private static boolean unsafeAssetPath(String path) {
        if (path.startsWith("/") || path.contains("\\")) return true;
        for (String segment : path.split("/")) {
            if (segment.equals(".") || segment.equals("..")) return true;
        }
        return false;
    }

    private static WebResourceResponse response(int status, String reason, String mimeType, byte[] value) {
        return new WebResourceResponse(mimeType, "UTF-8", status, reason, Collections.singletonMap("Cache-Control", "no-store"), new ByteArrayInputStream(value));
    }

    private static String mimeType(String path) {
        if (path.endsWith(".html")) return "text/html";
        if (path.endsWith(".js")) return "application/javascript";
        if (path.endsWith(".css")) return "text/css";
        if (path.endsWith(".svg")) return "image/svg+xml";
        if (path.endsWith(".png")) return "image/png";
        if (path.endsWith(".webp")) return "image/webp";
        if (path.endsWith(".woff2")) return "font/woff2";
        if (path.endsWith(".json")) return "application/json";
        return "application/octet-stream";
    }
}
