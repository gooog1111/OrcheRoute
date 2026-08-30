//go:build linux

package callserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/calltransport"
	"github.com/pion/dtls/v3"
)

func TestCallProfileCarriesVLESSThroughManagedRuntime(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		connection, acceptErr := echo.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	manager, _ := configuredManager(t)
	config := manager.data
	config.ListenAddress = freeUDPAddress(t)
	config.BackendAddress = freeTCPAddress(t)
	config.OrdinaryEnabled = false
	if _, err := manager.UpdateConfig(config); err != nil {
		t.Fatal(err)
	}
	client, err := manager.CreateClient(CreateClientInput{Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(EmbeddedXrayBackend{})
	defer runtime.Close()
	if err := runtime.Apply(manager); err != nil {
		t.Fatal(err)
	}

	psk, err := client.Profile.PSKBytes()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := net.ResolveUDPAddr("udp", runtime.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := dtls.DialWithOptions("udp", remote,
		dtls.WithPSK(func([]byte) ([]byte, error) { return psk, nil }),
		dtls.WithPSKIdentityHint([]byte(client.Profile.VLESSUUID)),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	if err != nil {
		t.Fatal(err)
	}
	reliable, err := calltransport.NewReliableClient(carrier)
	if err != nil {
		t.Fatal(err)
	}
	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- calltransport.ServeClient(ctx, reliable, local) }()

	connection, err := net.DialTimeout("tcp", local.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	payload := []byte("orcheroute-call-e2e")
	request := vlessTCPRequest(t, client.Profile.VLESSUUID, echo.Addr().(*net.TCPAddr), payload)
	if _, err := connection.Write(request); err != nil {
		t.Fatal(err)
	}
	responseHeader := make([]byte, 2)
	if _, err := io.ReadFull(connection, responseHeader); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(responseHeader, []byte{0, 0}) {
		t.Fatalf("unexpected VLESS response header %x", responseHeader)
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, response); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response, payload) {
		t.Fatalf("unexpected response %q", response)
	}
	_ = connection.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := runtime.Apply(manager); err != nil {
			t.Fatal(err)
		}
		if manager.PublicConfig().Clients[0].TrafficUsedBytes > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Xray traffic counters were not persisted")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client carrier did not stop")
	}
}

func vlessTCPRequest(t *testing.T, id string, target *net.TCPAddr, payload []byte) []byte {
	t.Helper()
	rawID, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil || len(rawID) != 16 || target.IP.To4() == nil {
		t.Fatalf("invalid VLESS test endpoint id=%q target=%v", id, target)
	}
	request := make([]byte, 0, 26+len(payload))
	request = append(request, 0)
	request = append(request, rawID...)
	request = append(request, 0, 1)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(target.Port))
	request = append(request, port...)
	request = append(request, 1)
	request = append(request, target.IP.To4()...)
	return append(request, payload...)
}
