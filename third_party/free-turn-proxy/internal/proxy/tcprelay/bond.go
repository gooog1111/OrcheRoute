package tcprelay

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samosvalishe/free-turn-proxy/internal/logx"
	"github.com/samosvalishe/free-turn-proxy/internal/wire/bondframe"
	"github.com/xtaci/smux"
)

type clientBondLane struct {
	ps     *pooledSession
	stream *smux.Stream
	jobs   chan bondframe.Frame
	dead   atomic.Bool
}

type openedBondLane struct {
	ps     *pooledSession
	stream *smux.Stream
	err    error
}

// proxyBondConn stripes both directions of one local TCP connection over all
// smux sessions that were live when it was accepted. Writes are performed by
// one worker per lane, so a blocked TURN allocation does not serialize the
// other lanes.
func proxyBondConn(ctx context.Context, log logx.Logger, conn net.Conn, candidates []*pooledSession, connID uint64) {
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	openedCh := make(chan openedBondLane, len(candidates))
	for _, ps := range candidates {
		go func() {
			stream, err := ps.sess.OpenStream()
			openedCh <- openedBondLane{ps: ps, stream: stream, err: err}
		}()
	}
	opened := make([]openedBondLane, 0, len(candidates))
	for range candidates {
		result := <-openedCh
		if result.err != nil {
			log.Errorf("[bond %d] session %d open stream: %v", connID, result.ps.id, result.err)
			continue
		}
		opened = append(opened, result)
	}
	if len(opened) > math.MaxUint16 {
		for _, result := range opened[math.MaxUint16:] {
			_ = result.stream.Close()
		}
		opened = opened[:math.MaxUint16]
	}
	if len(opened) == 0 {
		log.Errorf("[bond %d] no usable lanes", connID)
		return
	}

	lanes := make([]*clientBondLane, 0, len(opened))
	laneCount := uint16(len(opened)) //nolint:gosec // bounded above.
	for index, result := range opened {
		_ = result.stream.SetWriteDeadline(time.Now().Add(bondframe.LaneAttachTimeout))
		if err := bondframe.WriteHello(result.stream, connID, uint16(index), laneCount); err != nil { //nolint:gosec // index is bounded.
			log.Errorf("[bond %d] session %d hello: %v", connID, result.ps.id, err)
			_ = result.stream.Close()
			continue
		}
		_ = result.stream.SetWriteDeadline(time.Time{})
		result.ps.active.Add(1)
		lanes = append(lanes, &clientBondLane{ps: result.ps, stream: result.stream, jobs: make(chan bondframe.Frame)})
	}
	if len(lanes) == 0 {
		log.Errorf("[bond %d] every lane failed hello", connID)
		return
	}
	defer func() {
		for _, lane := range lanes {
			_ = lane.stream.Close()
			lane.ps.active.Add(-1)
		}
	}()

	stopDeadline := context.AfterFunc(ctx, func() {
		now := time.Now()
		_ = conn.SetDeadline(now)
		for _, lane := range lanes {
			_ = lane.stream.SetDeadline(now)
		}
	})
	defer stopDeadline()
	log.Infof("[bond %d] connected lanes=%d from=%s", connID, len(lanes), conn.RemoteAddr())

	recv := make(chan bondframe.Frame, bondframe.PendingCap)
	var readers sync.WaitGroup
	for _, lane := range lanes {
		readers.Go(func() {
			for {
				frame, err := bondframe.ReadFrame(lane.stream)
				if err != nil {
					lane.dead.Store(true)
					if ctx.Err() == nil && !errors.Is(err, io.EOF) {
						log.Errorf("[bond %d] session %d read: %v", connID, lane.ps.id, err)
						cancel()
					}
					return
				}
				if frame.Type == bondframe.FrameData && lane.ps.traffic != nil {
					lane.ps.traffic.AddRx(len(frame.Data))
				}
				select {
				case recv <- frame:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	go func() {
		readers.Wait()
		close(recv)
	}()

	var copies sync.WaitGroup
	copies.Go(func() { copyTCPToBond(ctx, cancel, log, conn, lanes, connID) })
	copies.Go(func() {
		chunks := bondframe.Reorder(ctx, conn, recv, bondframe.ReorderHooks{
			OnOverflow: func(have int) {
				log.Errorf("[bond %d] reorder overflow at %d frames", connID, have)
				cancel()
			},
			OnWriteError: func(err error) {
				if ctx.Err() == nil {
					log.Errorf("[bond %d] local write: %v", connID, err)
				}
			},
			OnCloseWrite: log.Debugf,
		})
		log.Debugf("[bond %d] download complete chunks=%d", connID, chunks)
		cancel()
	})
	copies.Wait()
}

func copyTCPToBond(ctx context.Context, cancel context.CancelFunc, log logx.Logger, conn net.Conn, lanes []*clientBondLane, connID uint64) {
	writerCtx, stopWriters := context.WithCancel(ctx)
	defer stopWriters()
	available := make(chan *clientBondLane, len(lanes))
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
						if ctx.Err() == nil {
							log.Errorf("[bond %d] session %d write: %v", connID, lane.ps.id, err)
							cancel()
						}
						return
					}
					if frame.Type == bondframe.FrameData && lane.ps.traffic != nil {
						lane.ps.traffic.AddTx(len(frame.Data))
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
		n, err := conn.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			select {
			case lane := <-available:
				select {
				case lane.jobs <- bondframe.Frame{Type: bondframe.FrameData, Seq: seq, Data: payload}:
					seq++
				case <-ctx.Done():
					workers.Wait()
					return
				}
			case <-ctx.Done():
				workers.Wait()
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				log.Debugf("[bond %d] local read: %v", connID, err)
			}
			break
		}
	}
	if ctx.Err() == nil {
		// Every idle writer advertises itself in available. Taking all tokens
		// therefore waits until every already-dispatched DATA frame is written.
		for range lanes {
			select {
			case <-available:
			case <-ctx.Done():
				stopWriters()
				workers.Wait()
				return
			}
		}
		// A FIN on every lane lets the receiver finish even if one lane closes
		// immediately after carrying the last data chunk.
		var fin sync.WaitGroup
		for _, lane := range lanes {
			if lane.dead.Load() {
				continue
			}
			fin.Go(func() {
				if err := bondframe.WriteFrame(lane.stream, bondframe.FrameFIN, seq, nil); err != nil && ctx.Err() == nil {
					log.Debugf("[bond %d] session %d FIN: %v", connID, lane.ps.id, err)
				}
			})
		}
		fin.Wait()
	}
	log.Debugf("[bond %d] upload complete chunks=%d", connID, seq)
	// Local EOF is only a half-close. Keep receiving the remote response until
	// its FIN arrives instead of tearing the whole bonded connection down.
	stopWriters()
	workers.Wait()
}
