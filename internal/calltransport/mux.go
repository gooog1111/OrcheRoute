// Package calltransport carries independent TCP streams through one connection
// supplied by a call provider. Provider-specific code owns signalling and the
// call carrier; this package deliberately knows nothing about VK, Jitsi or
// Xray.
package calltransport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/metacubex/smux"
)

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

func muxConfig() *smux.Config {
	config := smux.DefaultConfig()
	config.KeepAliveInterval = 10 * time.Second
	config.KeepAliveTimeout = 30 * time.Second
	config.MaxReceiveBuffer = 4 * 1024 * 1024
	config.MaxStreamBuffer = 1024 * 1024
	return config
}

// ServeClient accepts local VLESS/Xray connections and opens one multiplexed
// stream for each of them inside the call carrier.
func ServeClient(ctx context.Context, carrier io.ReadWriteCloser, listener net.Listener) error {
	session, err := smux.Client(carrier, muxConfig())
	if err != nil {
		return fmt.Errorf("call_transport_client_session: %w", err)
	}
	defer session.Close()
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
			_ = session.Close()
		case <-done:
		}
	}()
	defer close(done)

	var connections sync.WaitGroup
	defer connections.Wait()
	for {
		local, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || session.IsClosed() || errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("call_transport_local_accept: %w", acceptErr)
		}
		stream, openErr := session.OpenStream()
		if openErr != nil {
			_ = local.Close()
			if ctx.Err() != nil || session.IsClosed() {
				return nil
			}
			return fmt.Errorf("call_transport_open_stream: %w", openErr)
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			pipe(local, stream)
		}()
	}
}

// ServeServer accepts multiplexed streams from the call carrier and forwards
// each stream to the local Xray VLESS inbound.
func ServeServer(ctx context.Context, carrier io.ReadWriteCloser, backendAddress string, dial DialContextFunc) error {
	if _, _, err := net.SplitHostPort(backendAddress); err != nil {
		return fmt.Errorf("call_transport_invalid_backend: %w", err)
	}
	if dial == nil {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		dial = dialer.DialContext
	}
	session, err := smux.Server(carrier, muxConfig())
	if err != nil {
		return fmt.Errorf("call_transport_server_session: %w", err)
	}
	defer session.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-done:
		}
	}()
	defer close(done)

	var connections sync.WaitGroup
	defer connections.Wait()
	for {
		stream, acceptErr := session.AcceptStream()
		if acceptErr != nil {
			if ctx.Err() != nil || session.IsClosed() {
				return nil
			}
			return fmt.Errorf("call_transport_stream_accept: %w", acceptErr)
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			backend, dialErr := dial(ctx, "tcp", backendAddress)
			if dialErr != nil {
				_ = stream.Close()
				return
			}
			pipe(stream, backend)
		}()
	}
}

func pipe(left, right io.ReadWriteCloser) {
	var copies sync.WaitGroup
	copies.Add(2)
	copyOne := func(destination io.Writer, source io.Reader) {
		defer copies.Done()
		_, _ = io.Copy(destination, source)
		if closer, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
	}
	go copyOne(right, left)
	go copyOne(left, right)
	copies.Wait()
	_ = left.Close()
	_ = right.Close()
}
