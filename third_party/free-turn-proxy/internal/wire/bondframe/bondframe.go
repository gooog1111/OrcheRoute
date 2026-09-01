// Package bondframe defines the wire format used to stripe one TCP connection
// across several independent smux sessions.
package bondframe

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"time"
)

const (
	Version uint8 = 1
	Magic         = "ORB1"

	FrameData byte = 1
	FrameFIN  byte = 2

	MaxChunk          = 16 * 1024
	PendingCap        = 2048
	LaneAttachTimeout = 2 * time.Second
)

type Hello struct {
	ConnID    uint64
	LaneIndex uint16
	LaneCount uint16
}

type Frame struct {
	Type byte
	Seq  uint64
	Data []byte
}

func WriteHello(w io.Writer, connID uint64, laneIndex, laneCount uint16) error {
	var hdr [17]byte
	copy(hdr[:4], Magic)
	hdr[4] = Version
	binary.BigEndian.PutUint64(hdr[5:13], connID)
	binary.BigEndian.PutUint16(hdr[13:15], laneIndex)
	binary.BigEndian.PutUint16(hdr[15:17], laneCount)
	_, err := w.Write(hdr[:])
	return err
}

func ReadHelloAfterMagic(r io.Reader, magic [4]byte) (Hello, error) {
	var hdr [17]byte
	copy(hdr[:4], magic[:])
	if _, err := io.ReadFull(r, hdr[4:]); err != nil {
		return Hello{}, err
	}
	return ParseHelloHeader(hdr[:])
}

func ParseHelloHeader(hdr []byte) (Hello, error) {
	if len(hdr) != 17 {
		return Hello{}, fmt.Errorf("bad bond hello size: %d", len(hdr))
	}
	if string(hdr[:4]) != Magic {
		return Hello{}, fmt.Errorf("bad bond magic")
	}
	if hdr[4] != Version {
		return Hello{}, fmt.Errorf("unsupported bond version: %d", hdr[4])
	}
	count := binary.BigEndian.Uint16(hdr[15:17])
	index := binary.BigEndian.Uint16(hdr[13:15])
	if count == 0 || index >= count {
		return Hello{}, fmt.Errorf("invalid bond lane %d/%d", index, count)
	}
	return Hello{ConnID: binary.BigEndian.Uint64(hdr[5:13]), LaneIndex: index, LaneCount: count}, nil
}

func WriteFrame(w io.Writer, typ byte, seq uint64, data []byte) error {
	if typ != FrameData && typ != FrameFIN {
		return fmt.Errorf("unknown bond frame type: %d", typ)
	}
	if len(data) > MaxChunk || uint64(len(data)) > math.MaxUint32 {
		return fmt.Errorf("bond frame too large: %d", len(data))
	}
	if typ == FrameFIN && len(data) != 0 {
		return fmt.Errorf("bond FIN contains data")
	}
	var hdr [13]byte
	hdr[0] = typ
	binary.BigEndian.PutUint64(hdr[1:9], seq)
	binary.BigEndian.PutUint32(hdr[9:13], uint32(len(data))) //nolint:gosec // MaxChunk bounds the value.
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [13]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	typ := hdr[0]
	size := binary.BigEndian.Uint32(hdr[9:13])
	if typ != FrameData && typ != FrameFIN {
		return Frame{}, fmt.Errorf("unknown bond frame type: %d", typ)
	}
	if size > MaxChunk || (typ == FrameFIN && size != 0) {
		return Frame{}, fmt.Errorf("invalid bond frame size: type=%d size=%d", typ, size)
	}
	f := Frame{Type: typ, Seq: binary.BigEndian.Uint64(hdr[1:9])}
	if size > 0 {
		f.Data = make([]byte, size)
		if _, err := io.ReadFull(r, f.Data); err != nil {
			return Frame{}, err
		}
	}
	return f, nil
}

type ReorderHooks struct {
	OnOverflow   func(have int)
	OnWriteError func(err error)
	OnCloseWrite func(format string, v ...any)
}

// Reorder writes frames in Seq order. Duplicate frames are ignored and a
// permanently missing sequence cannot grow memory beyond PendingCap.
func Reorder(ctx context.Context, dst net.Conn, recv <-chan Frame, hooks ReorderHooks) uint64 {
	pending := make(map[uint64][]byte)
	var expect uint64
	var finSeq *uint64
	for {
		if finSeq != nil && expect == *finSeq {
			CloseWrite(dst, hooks.OnCloseWrite)
			return expect
		}
		select {
		case <-ctx.Done():
			return expect
		case frame, ok := <-recv:
			if !ok {
				return expect
			}
			if frame.Type == FrameFIN {
				value := frame.Seq
				if finSeq == nil || value < *finSeq {
					finSeq = &value
				}
				continue
			}
			if frame.Seq < expect {
				continue
			}
			if _, exists := pending[frame.Seq]; !exists {
				if len(pending) >= PendingCap {
					if hooks.OnOverflow != nil {
						hooks.OnOverflow(len(pending))
					}
					return expect
				}
				pending[frame.Seq] = frame.Data
			}
			for {
				data, exists := pending[expect]
				if !exists {
					break
				}
				delete(pending, expect)
				if len(data) > 0 {
					if _, err := dst.Write(data); err != nil {
						if hooks.OnWriteError != nil {
							hooks.OnWriteError(err)
						}
						return expect
					}
				}
				expect++
			}
		}
	}
}

func CloseWrite(conn net.Conn, errf func(format string, v ...any)) {
	type closeWriter interface{ CloseWrite() error }
	if writer, ok := conn.(closeWriter); ok {
		if err := writer.CloseWrite(); err != nil && errf != nil {
			errf("CloseWrite failed: %v", err)
		}
	}
}
