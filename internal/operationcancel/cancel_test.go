package operationcancel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchCancelsWithoutConsumingServerRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.request")
	ctx, stop := Watch(context.Background(), path, 5*time.Millisecond)
	if err := os.WriteFile(path, []byte("cancel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel request was not observed")
	}
	stop()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("server cancel request was consumed: %v", err)
	}
}
