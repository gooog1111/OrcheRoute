//go:build linux

package callserver

import (
	"context"
	"net"
	"testing"
	"time"

	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
	xraystats "github.com/xtls/xray-core/features/stats"
)

func TestEmbeddedXrayBackendStartsVLESSListener(t *testing.T) {
	address := freeTCPAddress(t)
	backend := EmbeddedXrayBackend{}
	running, err := backend.Start(context.Background(), address, []callxray.Client{{ID: "b831381d-6324-4d53-ad4f-8cda48b30811", Email: "phone"}})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()
	embedded, ok := running.(*embeddedXrayRuntime)
	if !ok {
		t.Fatalf("unexpected runtime type %T", running)
	}
	uplink, err := xraystats.GetOrRegisterCounter(embedded.stats, "user>>>phone>>>traffic>>>uplink")
	if err != nil {
		t.Fatal(err)
	}
	downlink, err := xraystats.GetOrRegisterCounter(embedded.stats, "user>>>phone>>>traffic>>>downlink")
	if err != nil {
		t.Fatal(err)
	}
	uplink.Add(12)
	downlink.Add(34)
	traffic := embedded.DrainTraffic()["phone"]
	if traffic.RXBytes != 12 || traffic.TXBytes != 34 || len(embedded.DrainTraffic()) != 0 {
		t.Fatalf("unexpected drained traffic: %#v", traffic)
	}
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("embedded Xray did not listen on %s: %v", address, err)
	}
	_ = connection.Close()
}
