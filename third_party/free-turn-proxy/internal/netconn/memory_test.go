package netconn

import (
	"runtime"
	"testing"
)

func TestPacketPipeRepeatedCloseDoesNotRetainQueues(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 300; i++ {
		a, b := PacketPipe(1792, 256)
		packet := make([]byte, 1280)
		for j := 0; j < 256; j++ {
			if _, err := a.WriteTo(packet, nil); err != nil {
				t.Fatal(err)
			}
		}
		_ = a.Close()
		_ = b.Close()
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("300 filled packet pipe cycles: retained heap delta=%d bytes", retained)
	if retained > 8*1024*1024 {
		t.Fatalf("retained packet queues: %d bytes", retained)
	}
}
