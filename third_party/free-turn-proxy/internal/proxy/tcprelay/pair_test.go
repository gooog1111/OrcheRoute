package tcprelay

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samosvalishe/free-turn-proxy/internal/logx"
	"github.com/samosvalishe/free-turn-proxy/internal/netconn"
	"github.com/samosvalishe/free-turn-proxy/internal/proxy/tcpserver"
	"github.com/samosvalishe/free-turn-proxy/internal/stats"
	"github.com/samosvalishe/free-turn-proxy/internal/transport/kcpmux"
	"github.com/xtaci/smux"
)

func echoBackend(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // тестовый сокет
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// pairedSession поднимает клиентскую smux-сессию против tcpserver.Handle через пару
// в памяти: TURN и DTLS в этом тесте не участвуют, проверяется слой tcprelay<->tcpserver.
func pairedSession(t *testing.T, ctx context.Context, registry *tcpserver.Registry, backendAddr string) *smux.Session {
	t.Helper()
	clientConn, serverConn := netconn.DatagramPipe(2048, 1024)
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	go tcpserver.Handle(ctx, logx.Nop(), registry, "test-client", serverConn, backendAddr, kcpmux.DefaultProfile())

	kcpSess, err := kcpmux.Dial(clientConn, kcpmux.DefaultProfile())
	if err != nil {
		t.Fatalf("kcp dial: %v", err)
	}
	sess, err := smux.Client(kcpSess, kcpmux.SmuxConfig())
	if err != nil {
		t.Fatalf("smux client: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func TestAcceptLoopForwardsToBackend(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backendAddr := echoBackend(t)

	var active atomic.Int32
	pool := newSessionPool(&active)
	registry := tcpserver.NewRegistry(logx.Nop())
	pool.Add(1, pairedSession(t, ctx, registry, backendAddr), nil)
	pool.Add(2, pairedSession(t, ctx, registry, backendAddr), nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // тестовый сокет
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	deps := &Deps{Log: logx.Nop(), ConnectedStreams: &active}
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		acceptLoop(ctx, deps, &Params{}, listener, pool)
	}()

	// По соединению на каждую сессию пула: round-robin должен развести их без потерь.
	for i := range 4 {
		conn, derr := net.Dial("tcp", listener.Addr().String()) //nolint:noctx // тестовый сокет
		if derr != nil {
			t.Fatalf("dial %d: %v", i, derr)
		}
		payload := bytes.Repeat([]byte{byte('a' + i)}, 32*1024)
		go func() { _, _ = conn.Write(payload) }()

		got := make([]byte, len(payload))
		if serr := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); serr != nil {
			t.Fatal(serr)
		}
		if _, rerr := io.ReadFull(conn, got); rerr != nil {
			t.Fatalf("conn %d read: %v", i, rerr)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("conn %d: echo mismatch", i)
		}
		_ = conn.Close()
	}

	cancel()
	_ = listener.Close()
	select {
	case <-loopDone:
	case <-time.After(30 * time.Second):
		t.Fatal("acceptLoop did not return after cancel")
	}
}

func TestAcceptLoopBondUsesEverySessionForOneConnection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backendAddr := echoBackend(t)

	var active atomic.Int32
	pool := newSessionPool(&active)
	registry := tcpserver.NewRegistry(logx.Nop())
	for id := 1; id <= 3; id++ {
		pool.Add(id, pairedSession(t, ctx, registry, backendAddr), stats.New(true))
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test socket
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		acceptLoop(ctx, &Deps{Log: logx.Nop(), ConnectedStreams: &active}, &Params{Bond: true}, listener, pool)
	}()

	conn, err := net.Dial("tcp", listener.Addr().String()) //nolint:noctx // test socket
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	payload := bytes.Repeat([]byte("orcheroute-bond-"), 32*1024)
	go func() { _, _ = conn.Write(payload) }()
	got := make([]byte, len(payload))
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("bond echo: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("bond echo mismatch")
	}
	for _, session := range pool.Snapshot() {
		if session.active.Load() != 1 {
			t.Fatalf("session %d active streams=%d, want 1 bonded lane", session.id, session.active.Load())
		}
		tx, rx := session.traffic.Counters()
		if tx == 0 || rx == 0 {
			t.Fatalf("session %d did not carry both directions: tx=%d rx=%d", session.id, tx, rx)
		}
	}

	_ = conn.Close()
	cancel()
	_ = listener.Close()
	select {
	case <-loopDone:
	case <-time.After(30 * time.Second):
		t.Fatal("bond acceptLoop did not return after cancel")
	}
}

func TestAcceptLoopRejectsWithoutSessions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // тестовый сокет
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	pool := newSessionPool(nil)
	go acceptLoop(ctx, &Deps{Log: logx.Nop()}, &Params{}, listener, pool)

	conn, err := net.Dial("tcp", listener.Addr().String()) //nolint:noctx // тестовый сокет
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if derr := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); derr != nil {
		t.Fatal(derr)
	}
	if _, rerr := conn.Read(make([]byte, 1)); rerr == nil {
		t.Error("connection must be rejected when pool is empty")
	}
}
