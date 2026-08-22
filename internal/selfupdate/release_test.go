package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rewrite struct{ base string }

func (r rewrite) RoundTrip(q *http.Request) (*http.Response, error) {
	q.URL.Scheme = "http"
	q.URL.Host = strings.TrimPrefix(r.base, "http://")
	return http.DefaultTransport.RoundTrip(q)
}
func TestLatestSelectsDigestedServerDeb(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"OrcheRoute-Linux-Server-1.2.3-amd64.deb","browser_download_url":"https://github.com/x","digest":"sha256:abcd","size":42}]}`))
	}))
	defer s.Close()
	got, err := Latest(context.Background(), &http.Client{Transport: rewrite{s.URL}}, false, "amd64")
	if err != nil || got.Version != "1.2.3" || got.Asset.Digest != "abcd" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
