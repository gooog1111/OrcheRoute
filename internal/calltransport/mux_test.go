package calltransport

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestMuxCarriesConcurrentVLESSStreams(t *testing.T) {
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			connection, acceptErr := backend.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()

	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	clientCarrier, serverCarrier := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errors := make(chan error, 2)
	go func() { errors <- ServeServer(ctx, serverCarrier, backend.Addr().String(), nil) }()
	go func() { errors <- ServeClient(ctx, clientCarrier, local) }()

	const streams = 8
	var clients sync.WaitGroup
	clientErrors := make(chan error, streams)
	for index := 0; index < streams; index++ {
		clients.Add(1)
		go func(index int) {
			defer clients.Done()
			connection, dialErr := net.DialTimeout("tcp", local.Addr().String(), time.Second)
			if dialErr != nil {
				clientErrors <- dialErr
				return
			}
			defer connection.Close()
			payload := []byte(fmt.Sprintf("vless-stream-%d", index))
			if _, writeErr := connection.Write(payload); writeErr != nil {
				clientErrors <- writeErr
				return
			}
			response := make([]byte, len(payload))
			if _, readErr := io.ReadFull(connection, response); readErr != nil {
				clientErrors <- readErr
				return
			}
			if string(response) != string(payload) {
				clientErrors <- fmt.Errorf("stream %d returned %q", index, response)
			}
		}(index)
	}
	clients.Wait()
	close(clientErrors)
	for clientErr := range clientErrors {
		t.Error(clientErr)
	}
	cancel()
	for index := 0; index < 2; index++ {
		select {
		case serveErr := <-errors:
			if serveErr != nil {
				t.Errorf("serve: %v", serveErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("multiplexer did not stop after cancellation")
		}
	}
}

func TestMuxRejectsInvalidBackendBeforeStarting(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if err := ServeServer(context.Background(), left, "missing-port", nil); err == nil {
		t.Fatal("invalid backend was accepted")
	}
}
