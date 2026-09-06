package serverruntime

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestWebAuthFailureBudgetAndRecovery(t *testing.T) {
	var limiter webAuthLimiter
	now := time.Unix(1000, 0)
	for i := 0; i < webAuthBurst; i++ {
		finish, _ := limiter.begin("192.0.2.1:"+strconv.Itoa(1000+i), now)
		if finish == nil {
			t.Fatal("premature throttle")
		}
		finish(false)
	}
	if finish, retry := limiter.begin("[::ffff:192.0.2.1]:9999", now); finish != nil || retry <= 0 {
		t.Fatal("IP/port alias bypass")
	}
	finish, _ := limiter.begin("192.0.2.2:1000", now)
	if finish == nil {
		t.Fatal("unrelated peer blocked")
	}
	finish(false)
	finish, _ = limiter.begin("192.0.2.1:1000", now.Add(webAuthRefill))
	if finish == nil {
		t.Fatal("budget did not recover")
	}
	finish(true)
	for i := 0; i < 100; i++ {
		finish, _ := limiter.begin("192.0.2.1:1000", now.Add(webAuthRefill))
		if finish == nil {
			t.Fatal("valid UI requests spent failure budget")
		}
		finish(true)
	}
}

func TestWebAuthLimitsConcurrentVerification(t *testing.T) {
	var limiter webAuthLimiter
	now := time.Unix(1000, 0)
	var pending []func(bool)
	for i := 0; i < webAuthBurst; i++ {
		finish, _ := limiter.begin("192.0.2."+strconv.Itoa(i+1)+":1000", now)
		if finish == nil {
			t.Fatal("premature concurrency limit")
		}
		pending = append(pending, finish)
	}
	if finish, _ := limiter.begin("198.51.100.1:1000", now); finish != nil {
		t.Fatal("unbounded password verification")
	}
	for _, finish := range pending {
		finish(false)
	}
	finish, _ := limiter.begin("198.51.100.1:1000", now)
	if finish == nil {
		t.Fatal("concurrency slot not released")
	}
	finish(true)
}

func TestWebAuthPeerStorageIsBounded(t *testing.T) {
	var limiter webAuthLimiter
	now := time.Unix(1000, 0)
	for i := 0; i < webAuthMaxPeers; i++ {
		finish, _ := limiter.begin("peer-"+strconv.Itoa(i), now)
		if finish == nil {
			t.Fatal("premature capacity limit")
		}
		finish(false)
	}
	if finish, _ := limiter.begin("new-peer", now); finish != nil {
		t.Fatal("unbounded peer map")
	}
	finish, _ := limiter.begin("new-peer", now.Add(webAuthIdle))
	if finish == nil {
		t.Fatal("idle peers not reclaimed")
	}
	finish(true)
}

func TestWebAuthHandlerRejectsBurstAndIgnoresForwardedIP(t *testing.T) {
	runtime := &Runtime{Config: Config{RuntimeEnv: t.TempDir() + "/missing"}}
	handler := runtime.WebHandler()
	for i := 0; i <= webAuthBurst; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://panel.example/", nil)
		req.RemoteAddr = "127.0.0.1:" + strconv.Itoa(1000+i)
		req.SetBasicAuth("wrong", "wrong")
		req.Header.Set("X-Forwarded-For", "192.0.2."+strconv.Itoa(i+1))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		want := http.StatusUnauthorized
		if i == webAuthBurst {
			want = http.StatusTooManyRequests
		}
		if res.Code != want {
			t.Fatalf("attempt %d: status=%d want=%d", i, res.Code, want)
		}
		if res.Code == 429 && res.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry time")
		}
	}
}

func TestWebAuthConcurrentRefunds(t *testing.T) {
	var limiter webAuthLimiter
	var workers sync.WaitGroup
	for i := 0; i < 128; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for attempt := 0; attempt < 20; attempt++ {
				if finish, _ := limiter.begin("192.0.2.1:1000", time.Now()); finish != nil {
					finish(true)
				}
			}
		}()
	}
	workers.Wait()
	if limiter.inflight != 0 || limiter.peers["192.0.2.1"].inflight != 0 {
		t.Fatal("verification slots leaked")
	}
	if limiter.peers["192.0.2.1"].tokens != webAuthBurst {
		t.Fatal("successful verification lost tokens")
	}
}
