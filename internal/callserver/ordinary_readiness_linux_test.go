//go:build linux

package callserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrdinaryReadyRequiresAuthenticatedControllerAndBothListeners(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(200)
	}))
	defer controller.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	snapshot := OrdinarySnapshot{ControllerAddress: strings.TrimPrefix(controller.URL, "http://"), ControllerSecret: "test-secret", VLESSListenAddress: listener.Addr().String(), TrojanListenAddress: "127.0.0.1:0"}
	if ordinaryReady(context.Background(), snapshot) {
		t.Fatal("unopened Trojan listener counted ready")
	}
	snapshot.TrojanListenAddress = other.Addr().String()
	if !ordinaryReady(context.Background(), snapshot) {
		t.Fatal("ready listeners rejected")
	}
	snapshot.ControllerSecret = "wrong"
	if ordinaryReady(context.Background(), snapshot) {
		t.Fatal("foreign controller counted ready")
	}
}
