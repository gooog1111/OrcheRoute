package online.gooog1111.orcheroute;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.Context;
import android.graphics.Color;
import android.graphics.PixelFormat;
import android.net.Uri;
import android.os.Build;
import android.provider.Settings;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.webkit.JavascriptInterface;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.TextView;

import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import java.util.Set;
import java.util.concurrent.atomic.AtomicBoolean;

final class VkCaptchaDialog {
    interface Callback {
        void onSuccess(String successToken);
        void onCancel();
        void onError(String message);
    }

    private static final String BRIDGE_NAME = "OrcheRouteCaptcha";
    private static final Set<String> SCRIPT_ORIGINS = Set.of("https://id.vk.ru", "https://api.vk.ru");
    private static final String CAPTURE_SCRIPT = """
            (() => {
              if (window.__orcheRouteCaptchaCapture) return;
              window.__orcheRouteCaptchaCapture = true;
              const report = value => {
                try {
                  const token = value && value.response && value.response.success_token;
                  if (typeof token === 'string' && token.length >= 16) OrcheRouteCaptcha.complete(token);
                } catch (_) {}
              };
              const originalFetch = window.fetch;
              if (originalFetch) window.fetch = async (...args) => {
                const response = await originalFetch(...args);
                try { response.clone().json().then(report).catch(() => {}); } catch (_) {}
                return response;
              };
              const originalOpen = XMLHttpRequest.prototype.open;
              XMLHttpRequest.prototype.open = function(...args) {
                this.addEventListener('load', () => {
                  try { report(JSON.parse(this.responseText)); } catch (_) {}
                });
                return originalOpen.apply(this, args);
              };
            })();
            """;

    private final Activity activity;
    private final FrameLayout parent;
    private final Callback callback;
    private final AtomicBoolean finished = new AtomicBoolean();
    private final AtomicBoolean submitting = new AtomicBoolean();
    private FrameLayout overlay;
    private WebView webView;
    private TextView status;
    private WindowManager overlayWindowManager;
    private boolean systemOverlay;

    VkCaptchaDialog(Activity activity, FrameLayout parent, Callback callback) {
        this.activity = activity;
        this.parent = parent;
        this.callback = callback;
    }

    boolean isOpen() {
        return overlay != null;
    }

    @SuppressLint({"SetJavaScriptEnabled", "JavascriptInterface"})
    boolean open(String rawURL) {
        return open(rawURL, false);
    }

    @SuppressLint({"SetJavaScriptEnabled", "JavascriptInterface"})
    boolean open(String rawURL, boolean aboveOtherApps) {
        Uri uri = Uri.parse(rawURL == null ? "" : rawURL.trim());
        if (!allowedCaptchaURL(uri)) return false;
        close(false);
        finished.set(false);
        submitting.set(false);

        overlay = new FrameLayout(activity);
        overlay.setBackgroundColor(Color.rgb(15, 18, 20));
        webView = new WebView(activity);
        webView.setBackgroundColor(Color.rgb(15, 18, 20));
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setAllowFileAccess(false);
        settings.setAllowContentAccess(false);
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        webView.addJavascriptInterface(new CaptchaBridge(), BRIDGE_NAME);
        if (WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
            WebViewCompat.addDocumentStartJavaScript(webView, CAPTURE_SCRIPT, SCRIPT_ORIGINS);
        }
        webView.setWebViewClient(new CaptchaClient());
        overlay.addView(webView, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT
        ));

        Button close = new Button(activity);
        close.setText("Закрыть");
        close.setOnClickListener(view -> close(true));
        FrameLayout.LayoutParams closeLayout = new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT,
                ViewGroup.LayoutParams.WRAP_CONTENT,
                Gravity.TOP | Gravity.END
        );
        int margin = Math.round(12 * activity.getResources().getDisplayMetrics().density);
        closeLayout.setMargins(margin, margin, margin, margin);
        overlay.addView(close, closeLayout);

        status = new TextView(activity);
        status.setTextColor(Color.WHITE);
        status.setBackgroundColor(Color.argb(220, 28, 32, 35));
        status.setPadding(margin, margin, margin, margin);
        status.setVisibility(View.GONE);
        FrameLayout.LayoutParams statusLayout = new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT,
                Gravity.BOTTOM
        );
        overlay.addView(status, statusLayout);
        systemOverlay = false;
        overlayWindowManager = null;
        if (aboveOtherApps && Build.VERSION.SDK_INT >= Build.VERSION_CODES.M
                && Settings.canDrawOverlays(activity)) {
            WindowManager manager = (WindowManager) activity.getApplicationContext()
                    .getSystemService(Context.WINDOW_SERVICE);
            WindowManager.LayoutParams window = new WindowManager.LayoutParams(
                    WindowManager.LayoutParams.MATCH_PARENT,
                    WindowManager.LayoutParams.MATCH_PARENT,
                    WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY,
                    WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON,
                    PixelFormat.OPAQUE
            );
            window.gravity = Gravity.TOP | Gravity.START;
            window.softInputMode = WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE;
            window.setTitle("OrcheRoute VK CAPTCHA");
            try {
                manager.addView(overlay, window);
                overlayWindowManager = manager;
                systemOverlay = true;
            } catch (RuntimeException ignored) {
                overlayWindowManager = null;
            }
        }
        if (!systemOverlay) {
            parent.addView(overlay, new FrameLayout.LayoutParams(
                    ViewGroup.LayoutParams.MATCH_PARENT,
                    ViewGroup.LayoutParams.MATCH_PARENT
            ));
        }
        webView.loadUrl(uri.toString());
        return true;
    }

    void close(boolean notify) {
        if (overlay == null) return;
        FrameLayout oldOverlay = overlay;
        WebView oldWebView = webView;
        overlay = null;
        webView = null;
        status = null;
        WindowManager oldWindowManager = overlayWindowManager;
        boolean wasSystemOverlay = systemOverlay;
        overlayWindowManager = null;
        systemOverlay = false;
        if (wasSystemOverlay && oldWindowManager != null) {
            try {
                oldWindowManager.removeViewImmediate(oldOverlay);
            } catch (RuntimeException ignored) {
                // The OS may already have detached the overlay while the activity stopped.
            }
        } else {
            parent.removeView(oldOverlay);
        }
        if (oldWebView != null) {
            oldWebView.stopLoading();
            oldWebView.removeJavascriptInterface(BRIDGE_NAME);
            oldWebView.destroy();
        }
        if (notify && finished.compareAndSet(false, true)) callback.onCancel();
    }

    void complete() {
        if (!isOpen()) return;
        finished.set(true);
        close(false);
    }

    void submissionFailed(String message) {
        activity.runOnUiThread(() -> {
            if (!isOpen()) return;
            submitting.set(false);
            if (status != null) {
                status.setText(message == null || message.isBlank()
                        ? "VK не принял подтверждение. Попробуйте ещё раз."
                        : message + " Попробуйте ещё раз.");
                status.setVisibility(View.VISIBLE);
            }
        });
    }

    private static boolean allowedCaptchaURL(Uri uri) {
        if (isLocalCaptchaProxy(uri)) return true;
        return "https".equalsIgnoreCase(uri.getScheme()) && "id.vk.ru".equalsIgnoreCase(uri.getHost())
                && uri.getPath() != null && uri.getPath().startsWith("/not_robot_captcha");
    }

    private static boolean allowedNavigation(Uri uri) {
        if (isLocalCaptchaProxy(uri)) return true;
        if (!"https".equalsIgnoreCase(uri.getScheme())) return false;
        String host = uri.getHost();
        return "id.vk.ru".equalsIgnoreCase(host) || "api.vk.ru".equalsIgnoreCase(host)
                || "vk.com".equalsIgnoreCase(host) || "vk.ru".equalsIgnoreCase(host);
    }

    private static boolean isLocalCaptchaProxy(Uri uri) {
        if (!"http".equalsIgnoreCase(uri.getScheme())) return false;
        String host = uri.getHost();
        return ("localhost".equalsIgnoreCase(host) || "127.0.0.1".equals(host))
                && uri.getPort() == 8765;
    }

    private final class CaptchaBridge {
        @JavascriptInterface
        public void complete(String token) {
            activity.runOnUiThread(() -> {
                if (!isOpen() || token == null || token.length() < 16 || token.length() > 8192) return;
                Uri current = Uri.parse(webView == null ? "" : webView.getUrl());
                if (!"id.vk.ru".equalsIgnoreCase(current.getHost())) return;
                if (!submitting.compareAndSet(false, true)) return;
                if (status != null) {
                    status.setText("Проверяем подтверждение VK…");
                    status.setVisibility(View.VISIBLE);
                }
                callback.onSuccess(token);
            });
        }
    }

    private final class CaptchaClient extends WebViewClient {
        @Override
        public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
            if (allowedNavigation(request.getUrl())) return false;
            callback.onError("VK CAPTCHA попыталась открыть неподдерживаемый адрес.");
            return true;
        }

        @Override
        public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
            if (!request.isForMainFrame()) return;
            String message = error == null ? "Не удалось открыть VK CAPTCHA." : String.valueOf(error.getDescription());
            submissionFailed(message);
            callback.onError(message);
        }
    }
}
