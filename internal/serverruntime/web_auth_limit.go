package serverruntime

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	webAuthBurst    = 32
	webAuthRefill   = 3 * time.Second
	webAuthMaxPeers = 4096
	webAuthIdle     = 5 * time.Minute
)

type webAuthBucket struct {
	tokens   float64
	updated  time.Time
	inflight int
}

// Only the socket peer is trusted. Deployments behind a reverse proxy share
// its bucket and should additionally rate-limit real clients at that proxy.
// Successful authentication refunds its reservation, so normal UI polling does
// not spend the failure budget. No credentials or usernames are retained.
type webAuthLimiter struct {
	mu       sync.Mutex
	peers    map[string]*webAuthBucket
	inflight int
}

func (limiter *webAuthLimiter) begin(remote string, now time.Time) (func(bool), int) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	if address, err := netip.ParseAddr(host); err == nil {
		host = address.Unmap().String()
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.inflight >= webAuthBurst {
		return nil, 1
	}
	if limiter.peers == nil {
		limiter.peers = make(map[string]*webAuthBucket)
	}
	bucket := limiter.peers[host]
	if bucket == nil {
		if len(limiter.peers) >= webAuthMaxPeers {
			for peer, entry := range limiter.peers {
				if entry.inflight == 0 && now.Sub(entry.updated) >= webAuthIdle {
					delete(limiter.peers, peer)
				}
			}
			if len(limiter.peers) >= webAuthMaxPeers {
				return nil, 3
			}
		}
		bucket = &webAuthBucket{tokens: webAuthBurst, updated: now}
		limiter.peers[host] = bucket
	}
	if elapsed := now.Sub(bucket.updated); elapsed > 0 {
		bucket.tokens = min(float64(webAuthBurst), bucket.tokens+float64(elapsed)/float64(webAuthRefill))
		bucket.updated = now
	}
	if bucket.tokens < 1 {
		return nil, 3
	}
	bucket.tokens--
	bucket.inflight++
	limiter.inflight++
	return func(success bool) {
		limiter.mu.Lock()
		defer limiter.mu.Unlock()
		bucket.inflight--
		limiter.inflight--
		if success {
			bucket.tokens = min(float64(webAuthBurst), bucket.tokens+1)
		}
	}, 0
}
