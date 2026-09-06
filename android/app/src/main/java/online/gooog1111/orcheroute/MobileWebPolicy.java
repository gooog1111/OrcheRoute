package online.gooog1111.orcheroute;

import java.net.URI;
import java.net.URISyntaxException;

/** Only packaged HTTPS assets may execute in the WebView containing the native bridge. */
final class MobileWebPolicy {
    private MobileWebPolicy() { }

    static boolean trusted(String value, String host) {
        try {
            URI uri = new URI(value);
            return "https".equals(uri.getScheme()) && host.equalsIgnoreCase(uri.getHost())
                    && uri.getRawUserInfo() == null && (uri.getPort() == -1 || uri.getPort() == 443);
        } catch (URISyntaxException | NullPointerException error) {
            return false;
        }
    }

    static boolean externalWebLink(String value) {
        try {
            URI uri = new URI(value);
            return ("https".equals(uri.getScheme()) || "http".equals(uri.getScheme()))
                    && uri.getHost() != null && uri.getRawUserInfo() == null;
        } catch (URISyntaxException | NullPointerException error) {
            return false;
        }
    }
}
