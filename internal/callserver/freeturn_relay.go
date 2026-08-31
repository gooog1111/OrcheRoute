package callserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type FreeTURNRelay struct {
	Binary         string
	StateDirectory string
}

type freeTURNClientInfo struct {
	Comment string `json:"comment,omitempty"`
}

type freeTURNClients struct {
	Clients map[string]freeTURNClientInfo `json:"clients"`
}

func (relay FreeTURNRelay) Start(parent context.Context, snapshot RuntimeSnapshot) (io.Closer, error) {
	if relay.Binary == "" || relay.StateDirectory == "" {
		return nil, fmt.Errorf("freeturn_runtime_unavailable")
	}
	if info, err := os.Stat(relay.Binary); err != nil || info.IsDir() {
		return nil, fmt.Errorf("freeturn_binary_unavailable")
	}
	directory := filepath.Join(relay.StateDirectory, "freeturn")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	clientsPath := filepath.Join(directory, "clients.json")
	if err := writeFreeTURNClients(clientsPath, snapshot); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(directory, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	command := exec.CommandContext(ctx, relay.Binary,
		"-listen", snapshot.ListenAddress,
		"-connect", snapshot.BackendAddress,
		"-mode", "tcp",
		"-clients-file", clientsPath,
	)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return nil, err
	}
	running := &freeTURNProcess{cancel: cancel, command: command, logFile: logFile, done: make(chan struct{})}
	go func() {
		err := command.Wait()
		running.mu.Lock()
		running.exitErr = err
		running.mu.Unlock()
		close(running.done)
	}()
	select {
	case <-running.done:
		_ = logFile.Close()
		running.mu.Lock()
		err := running.exitErr
		running.mu.Unlock()
		if err == nil {
			err = fmt.Errorf("freeturn_process_exited")
		}
		return nil, err
	case <-time.After(250 * time.Millisecond):
		return running, nil
	}
}

func writeFreeTURNClients(path string, snapshot RuntimeSnapshot) error {
	clients := freeTURNClients{Clients: make(map[string]freeTURNClientInfo, len(snapshot.Clients))}
	for _, client := range snapshot.Clients {
		clients.Clients[client.ID] = freeTURNClientInfo{Comment: client.Email}
	}
	payload, err := json.MarshalIndent(clients, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".clients-*.json")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

type freeTURNProcess struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	command *exec.Cmd
	logFile *os.File
	done    chan struct{}
	exitErr error
	closed  bool
}

func (process *freeTURNProcess) Alive() error {
	select {
	case <-process.done:
		process.mu.Lock()
		err := process.exitErr
		process.mu.Unlock()
		if err == nil {
			return fmt.Errorf("freeturn_process_exited")
		}
		return err
	default:
		return nil
	}
}

func (process *freeTURNProcess) Close() error {
	process.mu.Lock()
	if process.closed {
		process.mu.Unlock()
		return nil
	}
	process.closed = true
	process.cancel()
	process.mu.Unlock()
	select {
	case <-process.done:
	case <-time.After(3 * time.Second):
		if process.command.Process != nil {
			_ = process.command.Process.Kill()
		}
	}
	return process.logFile.Close()
}
