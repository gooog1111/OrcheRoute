package online.gooog1111.orcheroute;

public final class MobileWebPolicyTest {
    public static void main(String[] args) {
        String host = "appassets.androidplatform.net";
        for (String url : new String[]{"https://"+host+"/", "https://"+host+":443/_next/static/app.js"}) {
            if (!MobileWebPolicy.trusted(url, host)) throw new AssertionError(url);
        }
        for (String url : new String[]{"http://"+host, "https://"+host+".evil.test/", "https://evil.test/", "https://evil@"+host,
                "https://"+host+":444/", "file:///data/user/0/private", "javascript:alert(1)", "data:text/html,test", "about:blank", "intent://test", "bad uri"}) {
            if (MobileWebPolicy.trusted(url, host)) throw new AssertionError(url);
        }
        if (!MobileWebPolicy.externalWebLink("https://example.org/path") || MobileWebPolicy.externalWebLink("intent://example.org")) {
            throw new AssertionError("external navigation policy");
        }
        System.out.println("MobileWebPolicy: trusted assets and external navigation cases passed");
    }
}
