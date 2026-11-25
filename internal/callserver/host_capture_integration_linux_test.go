//go:build linux

package callserver

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gooog1111/orcheroute/internal/mihomo"
)

func TestPacketCaptureHTTPHelper(t *testing.T) {
	address := os.Getenv("ORCHEROUTE_TEST_HTTP_ADDRESS")
	if address == "" {
		return
	}
	t.Fatal(http.ListenAndServe(address, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			if r.Host != "203.0.113.4:18080" {
				http.Error(w, "unexpected proxy target", 403)
				return
			}
			upstream, err := net.Dial("tcp4", "203.0.113.2:18080")
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			defer upstream.Close()
			client, buffered, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer client.Close()
			_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			_ = buffered.Flush()
			go func() { _, _ = io.Copy(upstream, buffered) }()
			_, _ = io.Copy(client, upstream)
			return
		}
		if r.Host == "203.0.113.4:18080" {
			_, _ = w.Write([]byte("proxy-policy"))
			return
		}
		_, _ = w.Write([]byte("ok"))
	})))
}

// Run only in disposable network AND mount namespaces, with a private /run.
// This uses the actual packaged Mihomo, not a model of its redirect rules.
func TestPacketHostCaptureCoexistence(t *testing.T) {
	if os.Getenv("ORCHEROUTE_PACKET_CAPTURE_INTEGRATION") != "1" {
		t.Skip("isolated privileged integration test")
	}
	for _, ns := range []string{"net", "mnt"} {
		self, _ := os.Readlink("/proc/self/ns/" + ns)
		init, _ := os.Readlink("/proc/1/ns/" + ns)
		if self == "" || self == init {
			t.Fatal("requires a disposable " + ns + " namespace")
		}
	}
	binary := os.Getenv("ORCHEROUTE_TEST_MIHOMO")
	if binary == "" {
		t.Fatal("missing packaged Mihomo path")
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return string(out)
	}
	for _, ns := range []string{"orche-client", "orche-wan"} {
		run("ip", "netns", "add", ns)
		defer exec.Command("ip", "netns", "delete", ns).Run()
		run("ip", "-n", ns, "link", "set", "lo", "up")
	}
	run("ip", "link", "set", "lo", "up")
	run("ip", "link", "add", "orchecall0", "type", "veth", "peer", "name", "client0")
	run("ip", "link", "set", "client0", "netns", "orche-client")
	run("ip", "-n", "orche-client", "address", "add", "10.77.0.2/24", "dev", "client0")
	run("ip", "-n", "orche-client", "address", "add", "10.77.0.3/24", "dev", "client0")
	run("ip", "-n", "orche-client", "link", "set", "client0", "up")
	run("ip", "-n", "orche-client", "route", "add", "default", "via", "10.77.0.1")
	run("ip", "link", "add", "wan0", "type", "veth", "peer", "name", "wanpeer0")
	run("ip", "link", "set", "wanpeer0", "netns", "orche-wan")
	run("ip", "address", "add", "198.51.100.1/24", "dev", "wan0")
	run("ip", "link", "set", "wan0", "up")
	run("ip", "-n", "orche-wan", "address", "add", "198.51.100.2/24", "dev", "wanpeer0")
	run("ip", "-n", "orche-wan", "address", "add", "203.0.113.2/32", "dev", "lo")
	run("ip", "-n", "orche-wan", "address", "add", "203.0.113.3/32", "dev", "lo")
	run("ip", "-n", "orche-wan", "link", "set", "wanpeer0", "up")
	run("ip", "-n", "orche-wan", "route", "add", "default", "via", "198.51.100.1")
	run("ip", "route", "add", "default", "via", "198.51.100.2")
	run("ip", "rule", "add", "priority", "90", "fwmark", "0x5351/0xffff", "lookup", "main")
	run("ip", "rule", "add", "priority", "91", "fwmark", "0x5352/0xffff", "lookup", "main")
	run("ip", "address", "add", "10.42.0.1/32", "dev", "lo")
	allow := true
	runtime := &embeddedPacketRuntime{interfaceName: "orchecall0"}
	if err := runtime.configureNetwork("10.77.0.1/16", false, []PacketPeer{{Address: netip.MustParseAddr("10.77.0.3"), AllowLAN: &allow}}); err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	listener, err := net.Listen("tcp4", "10.42.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	localServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })}
	go localServer.Serve(listener)
	defer localServer.Close()
	self, _ := os.Executable()
	wanServer := exec.Command("ip", "netns", "exec", "orche-wan", self, "-test.run=^TestPacketCaptureHTTPHelper$", "-test.timeout=90s")
	wanServer.Env = append(os.Environ(), "ORCHEROUTE_TEST_HTTP_ADDRESS=0.0.0.0:18080")
	if err := wanServer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wanServer.Process.Kill(); _ = wanServer.Wait() }()
	probe := func(source, target string) string {
		out, _ := exec.Command("ip", "netns", "exec", "orche-client", "curl", "--noproxy", "*", "--interface", source, "-s", "--max-time", "2", "-o", "/dev/null", "-w", "%{http_code}", target).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	publicURL := "http://203.0.113.2:18080/"
	deadline := time.Now().Add(5 * time.Second)
	for probe("10.77.0.2", publicURL) != "200" {
		if time.Now().After(deadline) {
			t.Fatal("isolated WAN baseline failed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	captureMode := os.Getenv("ORCHEROUTE_TEST_CAPTURE_MODE")
	if captureMode == "" {
		captureMode = "system"
	}
	config, err := mihomo.Build(mihomo.Input{StateDir: t.TempDir(), Secret: "isolated-test", RouteDefault: "direct", Network: mihomo.Network{
		Profile:              mihomo.Profile{Capture: mihomo.Capture{Mode: captureMode, Interfaces: []string{"unrelated0"}, StrictRoute: true}},
		ResolvedRoles:        map[string]mihomo.Role{"direct": {Interface: "wan0", Mark: 0x5351}, "vpn_underlay": {Interface: "wan0", Mark: 0x5352}},
		EffectiveBypassCIDRs: append(append([]string{}, privateIPv4CIDRs...), "198.18.0.0/30"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	blockedURL := "http://203.0.113.3:18080/"
	if got := probe("10.77.0.2", blockedURL); got != "200" {
		t.Fatalf("policy target baseline failed: %s", got)
	}
	proxies := append(config["proxies"].([]any), map[string]any{"name": "TEST-PROXY", "type": "http", "server": "198.51.100.2", "port": 18080, "interface-name": "wan0", "routing-mark": 0x5352})
	data, _ := json.Marshal(map[string]any{"mode": "rule", "log-level": "debug", "ipv6": false, "tun": config["tun"], "proxies": proxies, "rules": []string{"IP-CIDR,203.0.113.3/32,REJECT,no-resolve", "IP-CIDR,203.0.113.4/32,TEST-PROXY,no-resolve", "MATCH,DIRECT-EGRESS"}})
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(directory, "mihomo.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	core := exec.Command(binary, "-d", directory, "-f", path)
	core.Stdout, core.Stderr = logFile, logFile
	if err := core.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = core.Process.Signal(syscall.SIGTERM); _ = core.Wait() }()
	deadline = time.Now().Add(8 * time.Second)
	for {
		out, _ := exec.Command("nft", "list", "ruleset").CombinedOutput()
		if strings.Contains(string(out), "redirect to") {
			break
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logFile.Name())
			t.Fatalf("Mihomo redirect not ready: %s", logs)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Log(strings.TrimSpace(run("ip", "route", "get", "203.0.113.2", "from", "10.77.0.2", "iif", "orchecall0")))
	if got := probe("10.77.0.2", blockedURL); got == "200" {
		t.Fatal("client traffic bypassed the host client's routing policy")
	}
	if got := probe("10.77.0.2", publicURL); got != "200" {
		t.Log(run("ip", "rule"))
		out, _ := exec.Command("ip", "route", "get", "10.77.0.1", "from", "10.77.0.2", "iif", "orchecall0").CombinedOutput()
		t.Log(string(out))
		t.Log(run("sysctl", "net.ipv4.conf.all.rp_filter", "net.ipv4.conf.orchecall0.rp_filter", "net.ipv4.conf.all.forwarding"))
		t.Log(run("ss", "-lnt"))
		t.Log(run("iptables", "-nvL", "INPUT"))
		t.Log(run("iptables", "-nvL", "FORWARD"))
		t.Log(run("nft", "list", "ruleset"))
		logs, _ := os.ReadFile(logFile.Name())
		t.Log(string(logs))
		t.Fatalf("public HTTP blocked by host capture: %s", got)
	}
	out, err := exec.Command("ip", "netns", "exec", "orche-client", "curl", "--noproxy", "*", "--interface", "10.77.0.2", "-fsS", "--max-time", "3", "http://203.0.113.4:18080/").CombinedOutput()
	if err != nil || string(out) != "proxy-policy" {
		t.Fatalf("host proxy selection was not inherited: %v %s", err, out)
	}
	localURL := "http://" + listener.Addr().String() + "/"
	if got := probe("10.77.0.2", localURL); got == "200" {
		t.Fatal("LAN block was bypassed")
	}
	if got := probe("10.77.0.3", localURL); got != "200" {
		t.Fatalf("per-client LAN permission broken: %s", got)
	}
}
