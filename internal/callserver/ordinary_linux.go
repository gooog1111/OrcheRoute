//go:build linux

package callserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type ordinaryMihomoBackend struct {
	Binary         string
	StateDirectory string
}

type ordinaryMihomoRuntime struct {
	mu      sync.Mutex
	command *exec.Cmd
	cancel  context.CancelFunc
	done    chan error
	exited  bool
	exitErr error
}

func (backend ordinaryMihomoBackend) Start(parent context.Context, snapshot OrdinarySnapshot) (io.Closer, error) {
	config, err := ordinaryMihomoConfig(snapshot)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(backend.StateDirectory, "call-protocols")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		return nil, err
	}
	if output, err := exec.CommandContext(parent, backend.Binary, "-t", "-d", directory, "-f", configPath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("call_server_mihomo_config_invalid: %w: %s", err, tailOutput(output))
	}
	ctx, cancel := context.WithCancel(parent)
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, backend.Binary, "-d", directory, "-f", configPath)
	command.Stdout, command.Stderr = io.Discard, &stderr
	if err := command.Start(); err != nil {
		cancel()
		return nil, err
	}
	running := &ordinaryMihomoRuntime{command: command, cancel: cancel, done: make(chan error, 1)}
	go func() {
		err := command.Wait()
		running.mu.Lock()
		running.exited, running.exitErr = true, err
		running.mu.Unlock()
		running.done <- err
	}()
	select {
	case err := <-running.done:
		cancel()
		return nil, fmt.Errorf("call_server_mihomo_start: %w: %s", err, tailOutput(stderr.Bytes()))
	case <-time.After(500 * time.Millisecond):
		return running, nil
	case <-parent.Done():
		_ = running.Close()
		return nil, parent.Err()
	}
}

func (runtime *ordinaryMihomoRuntime) Alive() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if !runtime.exited {
		return nil
	}
	if runtime.exitErr != nil {
		return runtime.exitErr
	}
	return fmt.Errorf("call_server_mihomo_stopped")
}

func (runtime *ordinaryMihomoRuntime) Close() error {
	runtime.mu.Lock()
	command := runtime.command
	cancel := runtime.cancel
	runtime.command = nil
	runtime.mu.Unlock()
	if command == nil {
		return nil
	}
	if command.Process != nil {
		_ = command.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-runtime.done:
	case <-time.After(2 * time.Second):
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		<-runtime.done
	}
	cancel()
	return nil
}

func tailOutput(value []byte) string {
	const limit = 1200
	if len(value) > limit {
		value = value[len(value)-limit:]
	}
	return string(bytes.TrimSpace(value))
}
