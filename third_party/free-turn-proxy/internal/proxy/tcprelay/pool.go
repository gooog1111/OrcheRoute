// Package tcprelay реализует tcp-режим клиента: локальный TCP-listener, а каждое
// принятое соединение уходит отдельным smux-потоком в одну из сессий пула
// TURN+DTLS+KCP+smux (round-robin).
package tcprelay

import (
	"sync"
	"sync/atomic"

	"github.com/samosvalishe/free-turn-proxy/internal/stats"
	"github.com/xtaci/smux"
)

// pooledSession - одна сессия TURN+DTLS+KCP+smux.
type pooledSession struct {
	id      int
	sess    *smux.Session
	traffic *stats.Stats
	active  atomic.Int32
}

// sessionPool - конкурентно-безопасный пул живых сессий. Среди одинаково
// загруженных сессий выбор остаётся round-robin.
type sessionPool struct {
	mu       sync.RWMutex
	sessions []*pooledSession
	counter  atomic.Uint64
	connID   atomic.Uint64

	// active - зеркало числа живых сессий для UI (те же "N/M", что и в udp-режиме).
	active *atomic.Int32

	readyOnce sync.Once
	ready     chan struct{}
}

func newSessionPool(active *atomic.Int32) *sessionPool {
	return &sessionPool{active: active, ready: make(chan struct{})}
}

// publishActive вызывать под p.mu.
func (p *sessionPool) publishActive() {
	if p.active != nil {
		p.active.Store(int32(len(p.sessions))) //nolint:gosec // сессий десятки, int32 не переполнить
	}
}

// Ready закрывается на первой поднявшейся сессии.
func (p *sessionPool) Ready() <-chan struct{} { return p.ready }

func (p *sessionPool) Add(id int, s *smux.Session, traffic *stats.Stats) *pooledSession {
	ps := &pooledSession{id: id, sess: s, traffic: traffic}
	p.mu.Lock()
	p.sessions = append(p.sessions, ps)
	p.publishActive()
	p.mu.Unlock()
	p.readyOnce.Do(func() { close(p.ready) })
	return ps
}

// Remove no-op, если ps не найден.
func (p *sessionPool) Remove(ps *pooledSession) {
	p.mu.Lock()
	for i, s := range p.sessions {
		if s == ps {
			p.sessions = append(p.sessions[:i], p.sessions[i+1:]...)
			break
		}
	}
	p.publishActive()
	p.mu.Unlock()
}

// Pick выбирает наименее занятую живую сессию. Это не отправляет новый TCP
// поток в KCP/smux-сессию, чей send queue уже занят длительной передачей.
// nil возвращается, если живых сессий нет.
func (p *sessionPool) Pick() *pooledSession {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := len(p.sessions)
	if n == 0 {
		return nil
	}
	start := p.counter.Add(1) - 1
	var best *pooledSession
	var bestActive int32
	for offset := range n {
		ps := p.sessions[(start+uint64(offset))%uint64(n)]
		if !ps.sess.IsClosed() {
			active := ps.active.Load()
			if best == nil || active < bestActive {
				best, bestActive = ps, active
				if active == 0 {
					break
				}
			}
		}
	}
	return best
}

func (p *sessionPool) NextConnID() uint64 { return p.connID.Add(1) }

func (p *sessionPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sessions)
}

// Snapshot returns every currently live session. Bonded connections use the
// snapshot as independent lanes for the lifetime of one local TCP connection.
func (p *sessionPool) Snapshot() []*pooledSession {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*pooledSession, 0, len(p.sessions))
	for _, ps := range p.sessions {
		if !ps.sess.IsClosed() {
			out = append(out, ps)
		}
	}
	return out
}

// CloseAll рвёт сессии, не трогая listener: рецикл не должен ронять локальный порт.
func (p *sessionPool) CloseAll() {
	p.mu.RLock()
	snapshot := make([]*pooledSession, len(p.sessions))
	copy(snapshot, p.sessions)
	p.mu.RUnlock()
	for _, ps := range snapshot {
		_ = ps.sess.Close()
	}
}
