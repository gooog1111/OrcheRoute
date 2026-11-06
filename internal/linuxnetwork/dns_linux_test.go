//go:build linux

package linuxnetwork

import (
	"context"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPhysicalDNSRefreshesAndRejectsStubAndTunnel(t *testing.T) {
	current := "127.0.0.53 198.18.0.2 ::ffff:127.0.0.1 8.8.8.8 77.88.8.8"
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "nmcli" || args[len(args)-1] != "eno1" {
			t.Fatalf("wrong interface/query: %s %v", name, args)
		}
		return []byte(current), nil
	}
	first := physicalNameServers(context.Background(), "eno1", []string{"1.1.1.1"}, run)
	if !reflect.DeepEqual(first, []string{"8.8.8.8:53", "77.88.8.8:53"}) {
		t.Fatal(first)
	}
	current = "9.9.9.9"
	if got := physicalNameServers(context.Background(), "eno1", nil, run); !reflect.DeepEqual(got, []string{"9.9.9.9:53"}) {
		t.Fatal("stale DHCP DNS", got)
	}
}

func TestPhysicalDNSResolvedAndConfiguredFallback(t *testing.T) {
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "nmcli" {
			return nil, errors.New("not installed")
		}
		return []byte("Link 2 (eno1): 192.168.1.1 2001:db8::53 fe80::1%eno1"), nil
	}
	got := physicalNameServers(context.Background(), "eno1", nil, run)
	if !reflect.DeepEqual(got, []string{"192.168.1.1:53", "[2001:db8::53]:53", "[fe80::1%eno1]:53"}) {
		t.Fatal(got)
	}
	missing := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("absent") }
	got = physicalNameServers(context.Background(), "eno1", []string{"127.0.0.53", "198.18.0.2", "1.1.1.1", "1.1.1.1"}, missing)
	if !reflect.DeepEqual(got, []string{"1.1.1.1:53"}) {
		t.Fatal(got)
	}
}

func TestBoundResolverNeverDialsStubOnPhysicalInterface(t *testing.T) {
	var called []string
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		called = append(called, address)
		return nil, errors.New("sentinel")
	}
	resolver := BoundResolver(dial, []string{"8.8.8.8:53", "77.88.8.8:53"}, true)
	_, _ = resolver.Dial(context.Background(), "udp", "127.0.0.53:53")
	if !reflect.DeepEqual(called, []string{"8.8.8.8:53", "77.88.8.8:53"}) {
		t.Fatal(called)
	}
	called = nil
	resolver = BoundResolver(dial, nil, true)
	_, err := resolver.Dial(context.Background(), "udp", "127.0.0.53:53")
	if err == nil || !strings.Contains(err.Error(), "physical_dns_unavailable") || len(called) != 0 {
		t.Fatalf("stub leak: %v %v", called, err)
	}
}

func TestPhysicalDNSLookupCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = physicalNameServers(ctx, "eno1", nil, func(ctx context.Context, _ string, _ ...string) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() })
	if time.Since(start) > time.Second {
		t.Fatal("DNS discovery ignored cancellation")
	}
}

func TestBoundResolverActuallyResolvesWithoutLocalStub(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, peer, err := server.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil || len(query.Questions) == 0 {
				continue
			}
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionDesired: true, RecursionAvailable: true}, Questions: query.Questions}
			reply.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 42}}}}
			payload, _ := reply.Pack()
			server.WriteTo(payload, peer)
		}
	}()
	resolver := BoundResolver(func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != server.LocalAddr().String() {
			return nil, errors.New("unexpected local stub")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}, []string{server.LocalAddr().String()}, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	addresses, err := resolver.LookupNetIP(ctx, "ip4", "qualification.test")
	if err != nil || len(addresses) != 1 || addresses[0].String() != "192.0.2.42" {
		t.Fatalf("physical resolution failed: %v %v", addresses, err)
	}
}
