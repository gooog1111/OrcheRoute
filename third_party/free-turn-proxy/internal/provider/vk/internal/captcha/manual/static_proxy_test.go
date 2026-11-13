package manual

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestStaticProxyPreservesWebpackChunkDirectory(t *testing.T) {
	upstream, _ := url.Parse("https://id.vk.ru/not_robot_captcha")
	entry := "https://st.vk.ru/vkid/app/entrypoints/entry.123.js"
	html := rewriteHTMLAttrsServerSide(`<script src="`+entry+`"></script>`, upstream)
	match := regexp.MustCompile(`src="([^"]+)"`).FindStringSubmatch(html)
	if len(match) != 2 || !strings.HasPrefix(match[1], "/static_proxy/") {
		t.Fatalf("directory-preserving script proxy missing: %s", html)
	}
	localEntry, _ := url.Parse("http://localhost:8765" + match[1])
	// VK's current webpack runtime: dirname(currentScript.src) + '../'.
	base := *localEntry
	base.Path = base.Path[:strings.LastIndex(base.Path, "/")+1]
	for _, path := range []string{"../chunks/js/394.123.js?retry=1", "../chunks/css/394.css", "../assets/fonts/400.woff2"} {
		relative, _ := url.Parse(path)
		localChunk := base.ResolveReference(relative)
		target, ok := staticProxyTarget(localChunk)
		want, _ := url.Parse(entry)
		want.Path = "/vkid/app/entrypoints/"
		want = want.ResolveReference(relative)
		if !ok || target.String() != want.String() {
			t.Fatalf("chunk %q routed to %v, want %v", path, target, want)
		}
	}
}

func TestStaticProxyRejectsUntrustedTargets(t *testing.T) {
	for _, path := range []string{
		"/static_proxy/evil.example/app.js", "/static_proxy/st.vk.ru.evil.example/app.js",
		"/static_proxy/127.0.0.1/app.js", "/static_proxy/user@st.vk.ru/app.js",
		"/static_proxy/st.vk.ru:8080/app.js", "/static_proxy/ads.vk.ru/app.js",
		"/static_proxy/st.vk.ru%2f.evil.example/app.js",
	} {
		local, err := url.Parse(path)
		if err != nil {
			continue
		}
		if target, ok := staticProxyTarget(local); ok {
			t.Fatalf("unsafe target accepted: %v", target)
		}
	}
}
