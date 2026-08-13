package subscriptions

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValidateFields(t *testing.T) {
	got, err := ValidateFields(map[string]any{
		"name": " Main ", "group": "PRIMARY", "parser": "STANDARD",
		"secret": " https://example.test/sub ", "enabled": false, "interval_seconds": "900",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Main" || got["group_name"] != "primary" || got["interval_seconds"] != 900 || got["enabled"] != false {
		t.Fatalf("unexpected result: %#v", got)
	}
	_, err = ValidateFields(map[string]any{"group": "other"}, true)
	if err == nil || err.Error() != "invalid_group" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodePlainAndBase64(t *testing.T) {
	body := "ignored\nVLESS://id@example.test:443?x=1&amp;y=2\nss://abc\n"
	want := []string{"VLESS://id@example.test:443?x=1&y=2", "ss://abc"}
	if got := Decode([]byte(body)); !reflect.DeepEqual(got, want) {
		t.Fatalf("plain: %#v", got)
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte("trojan://pw@example.test:443\n"))
	if got := Decode([]byte(encoded)); !reflect.DeepEqual(got, []string{"trojan://pw@example.test:443"}) {
		t.Fatalf("base64: %#v", got)
	}
}

func TestDecodeWireGuardConfigCanonicalizesWhitespace(t *testing.T) {
	one := "[Interface]\nAddress = 10.0.0.2/32\nPrivateKey = private\n\n[Peer]\nPublicKey = public\nEndpoint = example.test:51820\nAllowedIPs = 0.0.0.0/0\n"
	two := "[interface]\r\n address=10.0.0.2/32\r\nprivatekey=private\r\n[peer]\r\npublickey=public\r\nendpoint=example.test:51820\r\nallowedips=0.0.0.0/0"
	first, second := Decode([]byte(one)), Decode([]byte(two))
	if len(first) != 1 || !reflect.DeepEqual(first, second) || !strings.HasPrefix(first[0], "wireguard://") {
		t.Fatalf("unexpected canonical wireguard links: %#v %#v", first, second)
	}
}

func TestDefaultsDoNotOverwriteExisting(t *testing.T) {
	existing := []Subscription{{ID: "ebrasha-public", Enabled: false}}
	got := MissingDefaults(existing, true)
	if len(got) != 1 || got[0].ID != "default-au1rxx" {
		t.Fatalf("unexpected defaults: %#v", got)
	}
}

func TestCacheAndRefreshDecision(t *testing.T) {
	now := time.Unix(10_000, 0)
	cache := NewCache([]string{"vless://one", "vless://one", "bad"}, now)
	if !reflect.DeepEqual(cache.Links, []string{"vless://one", "bad"}) {
		t.Fatal(cache.Links)
	}
	if RefreshDue(now, 9_500, 900, false, []string{"vless://one"}) {
		t.Fatal("fresh cache marked due")
	}
	if !RefreshDue(now, 9_500, 900, false, nil) {
		t.Fatal("missing cache must refresh")
	}
}

func TestPlanRefresh(t *testing.T) {
	now := time.Unix(10_000, 0)
	items := []Subscription{
		{ID: "fresh", GroupName: Primary, Enabled: true, LastSuccess: 9_500, IntervalSeconds: 900},
		{ID: "due", GroupName: Primary, Enabled: true, LastSuccess: 8_000, IntervalSeconds: 900},
		{ID: "off", GroupName: Primary, Enabled: false, LastSuccess: 0, IntervalSeconds: 900},
	}
	plan := PlanRefresh(items, []Group{Primary}, nil, map[string]bool{"fresh": true, "due": true}, false, now)
	if len(plan) != 2 || plan[0].Fetch || plan[0].Reason != "cached" || !plan[1].Fetch || plan[1].Reason != "interval_elapsed" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if !ShouldRebuildProvider(Emergency, false, nil, false) || ShouldRebuildProvider(Emergency, false, nil, true) {
		t.Fatal("unexpected rebuild decision")
	}
}
