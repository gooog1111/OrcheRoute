package bondframe

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestHelloAndFramesRoundTrip(t *testing.T) {
	t.Parallel()
	var wire bytes.Buffer
	if err := WriteHello(&wire, 42, 1, 3); err != nil {
		t.Fatal(err)
	}
	var magic [4]byte
	copy(magic[:], wire.Next(4))
	hello, err := ReadHelloAfterMagic(&wire, magic)
	if err != nil || hello != (Hello{ConnID: 42, LaneIndex: 1, LaneCount: 3}) {
		t.Fatalf("hello=%+v err=%v", hello, err)
	}
	if err := WriteFrame(&wire, FrameData, 7, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	frame, err := ReadFrame(&wire)
	if err != nil || frame.Type != FrameData || frame.Seq != 7 || string(frame.Data) != "payload" {
		t.Fatalf("frame=%+v err=%v", frame, err)
	}
}

func TestRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()
	bad := make([]byte, 17)
	copy(bad, Magic)
	bad[4] = Version
	binary.BigEndian.PutUint16(bad[13:15], 2)
	binary.BigEndian.PutUint16(bad[15:17], 2)
	if _, err := ParseHelloHeader(bad); err == nil {
		t.Fatal("out-of-range lane accepted")
	}
	var frame bytes.Buffer
	var hdr [13]byte
	hdr[0] = FrameData
	binary.BigEndian.PutUint32(hdr[9:13], MaxChunk+1)
	frame.Write(hdr[:])
	if _, err := ReadFrame(&frame); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestReorderOutOfOrderAndDuplicate(t *testing.T) {
	t.Parallel()
	left, right := net.Pipe()
	defer left.Close()  //nolint:errcheck
	defer right.Close() //nolint:errcheck
	recv := make(chan Frame, 6)
	recv <- Frame{Type: FrameData, Seq: 2, Data: []byte("C")}
	recv <- Frame{Type: FrameData, Seq: 0, Data: []byte("A")}
	recv <- Frame{Type: FrameData, Seq: 0, Data: []byte("X")}
	recv <- Frame{Type: FrameFIN, Seq: 3}
	recv <- Frame{Type: FrameData, Seq: 1, Data: []byte("B")}
	close(recv)

	done := make(chan uint64, 1)
	go func() {
		done <- Reorder(context.Background(), left, recv, ReorderHooks{})
		_ = left.Close()
	}()
	if err := right.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(right)
	if err != nil || string(got) != "ABC" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if chunks := <-done; chunks != 3 {
		t.Fatalf("chunks=%d", chunks)
	}
}
