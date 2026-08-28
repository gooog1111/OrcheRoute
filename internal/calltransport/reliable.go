package calltransport

import (
	"fmt"
	"net"
	"time"

	kcp "github.com/metacubex/kcp-go"
)

// datagramConn adapts a message-preserving DTLS connection to net.PacketConn.
// DTLS Read and Write calls preserve record boundaries, which KCP requires.
type datagramConn struct{ net.Conn }

func (connection *datagramConn) ReadFrom(buffer []byte) (int, net.Addr, error) {
	count, err := connection.Read(buffer)
	return count, connection.RemoteAddr(), err
}

func (connection *datagramConn) WriteTo(buffer []byte, _ net.Addr) (int, error) {
	return connection.Write(buffer)
}

type reliableConn struct {
	*kcp.UDPSession
	listener *kcp.Listener
}

func (connection *reliableConn) Close() error {
	err := connection.UDPSession.Close()
	if connection.listener != nil {
		_ = connection.listener.Close()
	}
	return err
}

// NewReliableClient turns the datagram-oriented call carrier into an ordered
// stream suitable for smux. Encryption belongs to the carrier (DTLS/WebRTC),
// so KCP intentionally uses no second encryption layer.
func NewReliableClient(carrier net.Conn) (net.Conn, error) {
	block, err := kcp.NewNoneBlockCrypt(nil)
	if err != nil {
		return nil, err
	}
	session, err := kcp.NewConn2(carrier.RemoteAddr(), block, 0, 0, &datagramConn{carrier})
	if err != nil {
		return nil, fmt.Errorf("call_transport_kcp_client: %w", err)
	}
	tuneKCP(session)
	return &reliableConn{UDPSession: session}, nil
}

// AcceptReliableServer waits for the KCP side of an authenticated call
// carrier. The caller must establish DTLS or WebRTC before invoking it.
func AcceptReliableServer(carrier net.Conn, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	block, err := kcp.NewNoneBlockCrypt(nil)
	if err != nil {
		return nil, err
	}
	listener, err := kcp.ServeConn(block, 0, 0, &datagramConn{carrier})
	if err != nil {
		return nil, fmt.Errorf("call_transport_kcp_listen: %w", err)
	}
	if err := listener.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = listener.Close()
		return nil, err
	}
	session, err := listener.AcceptKCP()
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("call_transport_kcp_accept: %w", err)
	}
	tuneKCP(session)
	return &reliableConn{UDPSession: session, listener: listener}, nil
}

func tuneKCP(session *kcp.UDPSession) {
	session.SetNoDelay(1, 20, 2, 1)
	session.SetWindowSize(256, 256)
	session.SetMtu(1200)
	session.SetACKNoDelay(true)
}
