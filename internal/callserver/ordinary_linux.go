//go:build linux

package callserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	ordinaryControllerAddress = "127.0.0.1:19092"
	ordinaryTrafficPoll       = 5 * time.Second
)

type ordinaryMihomoBackend struct {
	Binary         string
	StateDirectory string
}

type ordinaryMihomoRuntime struct {
	mu             sync.Mutex
	command        *exec.Cmd
	cancel         context.CancelFunc
	done           chan error
	exited         bool
	exitErr        error
	lastConnection map[string]Traffic
	pending        map[string]Traffic
}

func (backend ordinaryMihomoBackend) Start(parent context.Context, snapshot OrdinarySnapshot) (io.Closer, error) {
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("call_server_mihomo_secret: %w", err)
	}
	snapshot.ControllerAddress = ordinaryControllerAddress
	snapshot.ControllerSecret = hex.EncodeToString(secret)
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
		go running.pollTraffic(ctx, snapshot.ControllerAddress, snapshot.ControllerSecret)
		return running, nil
	case <-parent.Done():
		_ = running.Close()
		return nil, parent.Err()
	}
}

// pollTraffic periodically reads Mihomo's external-controller /connections
// endpoint and accumulates per-client byte deltas into pending. VLESS, Trojan
// and Hysteria2 authenticate each connection against the client list configured
// in ordinaryMihomoConfig, and Mihomo reports the matched username back as
// metadata.inboundUser, so it doubles as the client ID used elsewhere. Traffic
// from a connection that opens and fully closes between two polls is lost;
// ordinaryTrafficPoll keeps that window short.
func (runtime *ordinaryMihomoRuntime) pollTraffic(ctx context.Context, address, secret string) {
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(ordinaryTrafficPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime.pollTrafficOnce(ctx, client, address, secret)
		}
	}
}

type ordinaryConnectionSnapshot struct {
	Connections []struct {
		ID       string `json:"id"`
		Metadata struct {
			InboundUser string `json:"inboundUser"`
		} `json:"metadata"`
		Upload   uint64 `json:"upload"`
		Download uint64 `json:"download"`
	} `json:"connections"`
}

func (runtime *ordinaryMihomoRuntime) pollTrafficOnce(ctx context.Context, client *http.Client, address, secret string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/connections", nil)
	if err != nil {
		return
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	var snapshot ordinaryConnectionSnapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.lastConnection == nil {
		runtime.lastConnection = map[string]Traffic{}
	}
	if runtime.pending == nil {
		runtime.pending = map[string]Traffic{}
	}
	seen := make(map[string]bool, len(snapshot.Connections))
	for _, connection := range snapshot.Connections {
		if connection.Metadata.InboundUser == "" {
			continue
		}
		seen[connection.ID] = true
		// Mihomo's "upload" is bytes read from the inbound (client) connection and
		// relayed onward — i.e. what the client sent — matching RXBytes' meaning
		// in the WireGuard path (bytes the server received from the peer).
		current := Traffic{RXBytes: connection.Upload, TXBytes: connection.Download}
		previous := runtime.lastConnection[connection.ID]
		delta := Traffic{RXBytes: counterDelta(current.RXBytes, previous.RXBytes), TXBytes: counterDelta(current.TXBytes, previous.TXBytes)}
		if delta.RXBytes != 0 || delta.TXBytes != 0 {
			total := runtime.pending[connection.Metadata.InboundUser]
			total.RXBytes += delta.RXBytes
			total.TXBytes += delta.TXBytes
			runtime.pending[connection.Metadata.InboundUser] = total
		}
		runtime.lastConnection[connection.ID] = current
	}
	for id := range runtime.lastConnection {
		if !seen[id] {
			delete(runtime.lastConnection, id)
		}
	}
}

func (runtime *ordinaryMihomoRuntime) DrainTraffic() map[string]Traffic {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.pending) == 0 {
		return nil
	}
	result := runtime.pending
	runtime.pending = map[string]Traffic{}
	return result
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
