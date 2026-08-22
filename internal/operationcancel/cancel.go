// Package operationcancel provides a small cross-process cooperative cancel
// signal. The server creates and removes the request file; a helper process
// only maps its presence to context cancellation.
package operationcancel

import (
	"context"
	"os"
	"time"
)

func Watch(parent context.Context, path string, interval time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	if path == "" {
		return ctx, cancel
	}
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if _, err := os.Stat(path); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() {
		select {
		case <-done:
		default:
			close(done)
		}
		cancel()
	}
}
