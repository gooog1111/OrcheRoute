// Package tcpserver - серверная сторона tcp-режима: KCP+smux поверх DTLS, каждый
// smux-поток форвардится в локальный TCP backend.
package tcpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/samosvalishe/free-turn-proxy/internal/logx"
	"github.com/samosvalishe/free-turn-proxy/internal/netconn"
	"github.com/samosvalishe/free-turn-proxy/internal/transport/kcpmux"
	"github.com/samosvalishe/free-turn-proxy/internal/wire/bondframe"
	"github.com/xtaci/smux"
)

const backendDialTimeout = 10 * time.Second

// Handle блокирует вызывающую горутину до закрытия сессии клиентом или ctx.
func Handle(ctx context.Context, logger logx.Logger, registry *Registry, clientID string, dtlsConn net.Conn, connectAddr string, profile kcpmux.Profile) {
	if registry == nil {
		registry = NewRegistry(logger)
	}
	kcpSess, err := kcpmux.Accept(dtlsConn, profile)
	if err != nil {
		logger.Errorf("tcpserver: %s", err)
		return
	}
	defer func() {
		if closeErr := kcpSess.Close(); closeErr != nil {
			logger.Warnf("tcpserver: close KCP session: %v", closeErr)
		}
	}()

	smuxSess, err := smux.Server(kcpSess, kcpmux.SmuxConfig())
	if err != nil {
		logger.Errorf("tcpserver: smux server: %s", err)
		return
	}
	defer func() {
		if closeErr := smuxSess.Close(); closeErr != nil {
			logger.Warnf("tcpserver: close smux session: %v", closeErr)
		}
	}()
	logger.Debugf("tcpserver: smux session established")

	// ctx живёт всё время процесса - без stop() хук копился бы на каждую сессию.
	stopOnCancel := context.AfterFunc(ctx, func() { _ = smuxSess.Close() })
	defer stopOnCancel()

	var wg sync.WaitGroup
	for {
		stream, err := smuxSess.AcceptStream()
		if err != nil {
			if ctx.Err() == nil {
				logger.Debugf("tcpserver: smux accept: %s", err)
			}
			break
		}
		wg.Go(func() { handleStream(ctx, logger, registry, clientID, stream, connectAddr) })
	}
	wg.Wait()
}

func handleStream(ctx context.Context, logger logx.Logger, registry *Registry, clientID string, s *smux.Stream, connectAddr string) {
	var prefix [4]byte
	if _, err := io.ReadFull(s, prefix[:]); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			logger.Debugf("tcpserver: stream prefix: %v", err)
		}
		_ = s.Close()
		return
	}
	if string(prefix[:]) == bondframe.Magic {
		registry.HandleLane(ctx, clientID, s, connectAddr, prefix)
		return
	}
	defer func() {
		if err := s.Close(); err != nil && err != smux.ErrGoAway {
			logger.Warnf("tcpserver: close smux stream: %v", err)
		}
	}()

	backend, err := (&net.Dialer{Timeout: backendDialTimeout}).DialContext(ctx, "tcp", connectAddr)
	if err != nil {
		logger.Errorf("tcpserver: backend dial %s: %s", connectAddr, err)
		return
	}
	defer func() {
		if closeErr := backend.Close(); closeErr != nil {
			logger.Warnf("tcpserver: close backend connection: %v", closeErr)
		}
	}()

	netconn.BiCopy(ctx, &prefixedConn{Conn: s, prefix: prefix[:]}, backend, logger.Debugf)
}

type prefixedConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixedConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}
