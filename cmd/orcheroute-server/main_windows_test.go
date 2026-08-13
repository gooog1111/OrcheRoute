//go:build windows

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/serverruntime"
)

func TestRunServerWithoutWebListener(t *testing.T) {
	directory := t.TempDir()
	runtimeEnv := filepath.Join(directory, "runtime.env")
	if err := os.WriteFile(runtimeEnv, []byte("api_token=test-token\ncontroller_secret=test-secret\nwebui_username=admin\nwebui_password_hash=unused\nwebui_tls_mode=disabled\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	state := filepath.Join(directory, "state")
	config := serverruntime.DefaultConfig()
	config.Listen, config.WebListen, config.WebTLSListen = address, "", ""
	config.ProductionState, config.StateDirectory = state, state
	config.RuntimeEnv, config.ConfigDirectory = runtimeEnv, directory
	config.MihomoAPI = "http://127.0.0.1:1"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServer(ctx, config) }()
	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	for attempt := 0; attempt < 40; attempt++ {
		response, err = client.Get("http://" + address + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		cancel()
		<-done
		t.Fatalf("health request failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status: %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal(fmt.Errorf("server shutdown timed out"))
	}
}
