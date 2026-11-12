package subscriptions

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceHWIDIsStableAndOpaque(t *testing.T) {
	first, err := DeviceHWID("private-device-seed")
	if err != nil || len(first) != 64 || !ValidHWID(first) || strings.Contains(first, "private") {
		t.Fatal("invalid device identifier")
	}
	second, _ := DeviceHWID("private-device-seed")
	other, _ := DeviceHWID("another-device")
	if first != second || first == other {
		t.Fatal("unstable or shared device identifier")
	}
	if ValidHWID("id\r\nX-Injected: value") {
		t.Fatal("header injection accepted")
	}
}

func TestSavedHWIDValidation(t *testing.T) {
	settings, err := NormalizeHWIDSettings(HWIDSettings{Saved: []SavedHWID{{Name: " Phone ", Value: " 0123456789abcdef "}}})
	if err != nil || len(settings.Saved) != 1 || settings.Saved[0].Name != "Phone" || settings.Saved[0].ID == "" {
		t.Fatal("saved identifier failed")
	}
	_, err = NormalizeHWIDSettings(HWIDSettings{Saved: []SavedHWID{{Name: "one", Value: "same-id"}, {Name: "two", Value: "same-id"}}})
	if err == nil {
		t.Fatal("duplicate saved identifier accepted")
	}
	for _, mode := range []string{"none", "current"} {
		if ValidateHWIDSelection(mode, "") != nil {
			t.Fatal(mode)
		}
	}
	if ValidateHWIDSelection("custom", "") == nil || ValidateHWIDSelection("unknown", "abc") == nil {
		t.Fatal("invalid selection accepted")
	}
}

func TestHTTPFetcherHWIDIsOptInAndStable(t *testing.T) {
	var received []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Header.Get("X-Hwid"))
		fmt.Fprintln(w, "vless://test@example.test:443#server")
	}))
	defer server.Close()
	fetcher := HTTPFetcher{Client: server.Client(), DeviceHWID: "stable-device-id"}
	for _, item := range []Subscription{{}, {HWIDMode: "current"}, {HWIDMode: "current"}, {HWIDMode: "custom", HWID: "saved-phone-id"}, {HWIDMode: "none", HWID: "must-not-send"}} {
		item.Parser = Standard
		item.Secret = server.URL
		links, err := fetcher.Fetch(context.Background(), item)
		if err != nil || len(links) != 1 {
			t.Fatal("subscription fetch failed", err)
		}
	}
	want := []string{"", "stable-device-id", "stable-device-id", "saved-phone-id", ""}
	if strings.Join(received, ",") != strings.Join(want, ",") {
		t.Fatal("HWID changed or sent without selection")
	}
}

func TestHTTPFetcherDoesNotLeakHWIDOnRedirect(t *testing.T) {
	var leaked string
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-Hwid")
		fmt.Fprintln(w, "vless://test@example.test:443")
	}))
	defer target.Close()
	from := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer from.Close()
	client := from.Client()
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		r.Header.Set("X-Hwid", "injected-by-delegate")
		return nil
	}
	_, err := (HTTPFetcher{Client: client}).Fetch(context.Background(), Subscription{Parser: Standard, Secret: from.URL, HWIDMode: "custom", HWID: "private-hwid"})
	if err != nil {
		t.Fatal(err)
	}
	if leaked != "" {
		t.Fatal("identifier leaked to another origin")
	}
}

func TestHTTPFetcherPreservesHWIDOnSameOriginRedirect(t *testing.T) {
	seen := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/finish", 302)
			return
		}
		seen = r.Header.Get("X-Hwid")
		fmt.Fprintln(w, "vless://test@example.test:443")
	}))
	defer server.Close()
	_, err := (HTTPFetcher{Client: server.Client()}).Fetch(context.Background(), Subscription{Parser: Standard, Secret: server.URL + "/start", HWIDMode: "custom", HWID: "owned-device"})
	if err != nil || seen != "owned-device" {
		t.Fatal("same-origin identifier lost", err)
	}
}

func TestHTTPFetcherHWIDRequiresTLSAndRecognizesNotice(t *testing.T) {
	_, err := (HTTPFetcher{}).Fetch(context.Background(), Subscription{Parser: Standard, Secret: "http://example.test/subscription", HWIDMode: "custom", HWID: "owned-device"})
	if err == nil || err.Error() != "hwid_requires_https" {
		t.Fatal("plaintext HWID request allowed")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "vless://test@0.0.0.0:1#Enable%20HWID") }))
	defer server.Close()
	for _, mode := range []string{"none", "current"} {
		_, err := (HTTPFetcher{Client: server.Client(), DeviceHWID: "owned-device"}).Fetch(context.Background(), Subscription{Parser: Standard, Secret: server.URL, HWIDMode: mode})
		want := "subscription_hwid_required"
		if mode == "current" {
			want = "subscription_hwid_rejected"
		}
		if err == nil || err.Error() != want {
			t.Fatal("HWID notice accepted as a VPN server")
		}
	}
}
