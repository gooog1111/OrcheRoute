package tcpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samosvalishe/free-turn-proxy/internal/logx"
	"github.com/samosvalishe/free-turn-proxy/internal/wire/bondframe"
	"github.com/xtaci/smux"
)

type bondKey struct {
	clientID string
	connID   uint64
}

// Registry joins lanes arriving through independent DTLS/KCP/smux sessions.
// Client ID is part of the key so two subscribers may reuse the same ConnID.
type Registry struct {
	log logx.Logger
	mu  sync.Mutex
	all map[bondKey]*serverBondConn
}

func NewRegistry(log logx.Logger) *Registry {
	if log == nil {
		log = logx.Nop()
	}
	return &Registry{log: log, all: make(map[bondKey]*serverBondConn)}
}

func (r *Registry) HandleLane(ctx context.Context, clientID string, stream *smux.Stream, connectAddr string, magic [4]byte) {
	defer func() {
		if err := stream.Close(); err != nil && !errors.Is(err, smux.ErrGoAway) {
			r.log.Debugf("bondserver: close lane: %v", err)
		}
	}()
	hello, err := bondframe.ReadHelloAfterMagic(stream, magic)
	if err != nil {
		r.log.Errorf("bondserver: hello: %v", err)
		return
	}
	key := bondKey{clientID: clientID, connID: hello.ConnID}
	conn := r.get(ctx, key, connectAddr)
	if !conn.addLane(&serverBondLane{index: hello.LaneIndex, stream: stream}, hello.LaneCount) {
		return
	}
	select {
	case <-ctx.Done():
	case <-conn.done:
	}
}

func (r *Registry) get(ctx context.Context, key bondKey, connectAddr string) *serverBondConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	if conn := r.all[key]; conn != nil {
		return conn
	}
	connCtx, cancel := context.WithCancel(ctx)
	conn := &serverBondConn{
		log:         r.log,
		key:         key,
		connectAddr: connectAddr,
		ctx:         connCtx,
		cancel:      cancel,
		done:        make(chan struct{}),
		ready:       make(chan struct{}, 1),
		recv:        make(chan bondframe.Frame, bondframe.PendingCap),
		laneIndexes: make(map[uint16]struct{}),
	}
	r.all[key] = conn
	go func() {
		<-conn.done
		r.mu.Lock()
		if r.all[key] == conn {
			delete(r.all, key)
		}
		r.mu.Unlock()
	}()
	return conn
}

type serverBondLane struct {
	index  uint16
	stream *smux.Stream
	jobs   chan bondframe.Frame
	dead   atomic.Bool
}

type serverBondConn struct {
	log         logx.Logger
	key         bondKey
	connectAddr string
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}

	lanesMu     sync.RWMutex
	lanes       []*serverBondLane
	laneIndexes map[uint16]struct{}
	want        uint16
	ready       chan struct{}
	recv        chan bondframe.Frame
	runOnce     sync.Once
}

func (c *serverBondConn) addLane(lane *serverBondLane, laneCount uint16) bool {
	c.lanesMu.Lock()
	if c.want != 0 && c.want != laneCount {
		c.lanesMu.Unlock()
		c.log.Errorf("[bond %d] lane count changed %d -> %d", c.key.connID, c.want, laneCount)
		return false
	}
	if _, duplicate := c.laneIndexes[lane.index]; duplicate {
		c.lanesMu.Unlock()
		c.log.Errorf("[bond %d] duplicate lane %d", c.key.connID, lane.index)
		return false
	}
	c.want = laneCount
	c.laneIndexes[lane.index] = struct{}{}
	lane.jobs = make(chan bondframe.Frame)
	c.lanes = append(c.lanes, lane)
	count := len(c.lanes)
	c.lanesMu.Unlock()
	c.log.Debugf("[bond %d] lane %d attached (%d/%d)", c.key.connID, lane.index, count, laneCount)
	select {
	case c.ready <- struct{}{}:
	default:
	}
	c.runOnce.Do(func() { go c.run() })
	go c.readLane(lane)
	return true
}

func (c *serverBondConn) snapshotLanes() []*serverBondLane {
	c.lanesMu.RLock()
	defer c.lanesMu.RUnlock()
	return append([]*serverBondLane(nil), c.lanes...)
}

func (c *serverBondConn) removeLane(lane *serverBondLane) int {
	c.lanesMu.Lock()
	defer c.lanesMu.Unlock()
	for index, candidate := range c.lanes {
		if candidate == lane {
			c.lanes = append(c.lanes[:index], c.lanes[index+1:]...)
			delete(c.laneIndexes, lane.index)
			break
		}
	}
	return len(c.lanes)
}

func (c *serverBondConn) waitForInitialLanes() {
	timer := time.NewTimer(bondframe.LaneAttachTimeout)
	defer timer.Stop()
	for {
		c.lanesMu.RLock()
		count, want := len(c.lanes), int(c.want)
		c.lanesMu.RUnlock()
		if want > 0 && count >= want {
			return
		}
		select {
		case <-c.ctx.Done():
			return
		case <-c.ready:
		case <-timer.C:
			c.log.Warnf("[bond %d] starting with %d/%d lanes", c.key.connID, count, want)
			return
		}
	}
}

func (c *serverBondConn) readLane(lane *serverBondLane) {
	for {
		frame, err := bondframe.ReadFrame(lane.stream)
		if err != nil {
			lane.dead.Store(true)
			left := c.removeLane(lane)
			if c.ctx.Err() == nil && !errors.Is(err, io.EOF) {
				c.log.Errorf("[bond %d] lane %d read: %v (left=%d)", c.key.connID, lane.index, err, left)
				// A lane may have carried an unacknowledged sequence. Closing the
				// TCP connection is safer than hanging the reorder buffer forever.
				c.cancel()
			}
			if left == 0 {
				c.cancel()
			}
			return
		}
		select {
		case c.recv <- frame:
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *serverBondConn) run() {
	defer close(c.done)
	defer c.cancel()
	c.waitForInitialLanes()
	if c.ctx.Err() != nil {
		return
	}
	backend, err := (&net.Dialer{Timeout: backendDialTimeout}).DialContext(c.ctx, "tcp", c.connectAddr)
	if err != nil {
		c.log.Errorf("[bond %d] backend dial %s: %v", c.key.connID, c.connectAddr, err)
		return
	}
	defer backend.Close() //nolint:errcheck
	stopDeadline := context.AfterFunc(c.ctx, func() {
		now := time.Now()
		_ = backend.SetDeadline(now)
		for _, lane := range c.snapshotLanes() {
			_ = lane.stream.SetDeadline(now)
		}
	})
	defer stopDeadline()
	c.log.Infof("[bond %d] backend connected lanes=%d client=%s", c.key.connID, len(c.snapshotLanes()), c.key.clientID)

	var copies sync.WaitGroup
	copies.Go(func() {
		chunks := bondframe.Reorder(c.ctx, backend, c.recv, bondframe.ReorderHooks{
			OnOverflow: func(have int) {
				c.log.Errorf("[bond %d] upload reorder overflow at %d", c.key.connID, have)
				c.cancel()
			},
			OnWriteError: func(err error) {
				if c.ctx.Err() == nil {
					c.log.Errorf("[bond %d] backend write: %v", c.key.connID, err)
				}
			},
			OnCloseWrite: c.log.Debugf,
		})
		c.log.Debugf("[bond %d] upload complete chunks=%d", c.key.connID, chunks)
	})
	copies.Go(func() {
		c.copyBackendToLanes(backend, c.snapshotLanes())
	})
	copies.Wait()
}

func (c *serverBondConn) copyBackendToLanes(backend net.Conn, lanes []*serverBondLane) {
	if len(lanes) == 0 {
		return
	}
	writerCtx, stopWriters := context.WithCancel(c.ctx)
	defer stopWriters()
	available := make(chan *serverBondLane, len(lanes))
	var workers sync.WaitGroup
	for _, lane := range lanes {
		workers.Go(func() {
			for {
				select {
				case available <- lane:
				case <-writerCtx.Done():
					return
				}
				select {
				case frame := <-lane.jobs:
					if err := bondframe.WriteFrame(lane.stream, frame.Type, frame.Seq, frame.Data); err != nil {
						lane.dead.Store(true)
						if c.ctx.Err() == nil {
							c.log.Errorf("[bond %d] lane %d write: %v", c.key.connID, lane.index, err)
							c.cancel()
						}
						return
					}
				case <-writerCtx.Done():
					return
				}
			}
		})
	}

	buf := make([]byte, bondframe.MaxChunk)
	var seq uint64
	for {
		n, err := backend.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			select {
			case lane := <-available:
				select {
				case lane.jobs <- bondframe.Frame{Type: bondframe.FrameData, Seq: seq, Data: payload}:
					seq++
				case <-c.ctx.Done():
					workers.Wait()
					return
				}
			case <-c.ctx.Done():
				workers.Wait()
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && c.ctx.Err() == nil {
				c.log.Debugf("[bond %d] backend read: %v", c.key.connID, err)
			}
			break
		}
	}
	if c.ctx.Err() == nil {
		for range lanes {
			select {
			case <-available:
			case <-c.ctx.Done():
				stopWriters()
				workers.Wait()
				return
			}
		}
		var fin sync.WaitGroup
		for _, lane := range lanes {
			if lane.dead.Load() {
				continue
			}
			fin.Go(func() { _ = bondframe.WriteFrame(lane.stream, bondframe.FrameFIN, seq, nil) })
		}
		fin.Wait()
	}
	c.log.Debugf("[bond %d] download complete chunks=%d", c.key.connID, seq)
	stopWriters()
	workers.Wait()
}
