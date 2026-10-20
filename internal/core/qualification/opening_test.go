package qualification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpeningFilterHTTPStatusAndInverse(t *testing.T) {
	for _, code := range []int{200, 204, 403, 404, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		for _, mode := range []string{"require", "exclude"} {
			want := code >= 200 && code < 300
			if mode == "exclude" {
				want = !want
			}
			if got := OpeningAllowed(context.Background(), server.Client(), server.URL, mode); got != want {
				t.Errorf("code=%d mode=%s got=%v", code, mode, got)
			}
		}
		server.Close()
	}
}

func TestOpeningFilterPolicyDefaultsAndHostname(t *testing.T) {
	policy := DefaultPolicy()
	d := policy["defaults"].(map[string]any)
	delete(d, "opening_filter_mode")
	delete(d, "opening_filter_url")
	valid, err := Validate(policy)
	if err != nil || valid["defaults"].(map[string]any)["opening_filter_mode"] != "off" {
		t.Fatal(valid, err)
	}
	d["opening_filter_mode"], d["opening_filter_url"] = "require", "youtube.com"
	valid, err = Validate(policy)
	if err != nil || valid["defaults"].(map[string]any)["opening_filter_url"] != "https://youtube.com" {
		t.Fatal(valid, err)
	}
	d["opening_filter_url"] = "file:///etc/passwd"
	if _, err := Validate(policy); err == nil {
		t.Fatal("non-HTTP target accepted")
	}
}
