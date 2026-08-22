//go:build windows

package windowsqualify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gooog1111/orcheroute/internal/qualification"
	"golang.org/x/net/proxy"
	"golang.org/x/sys/windows"
)

type URLTarget struct {
	URL        string
	StatusCode int
}

type DNSConfig struct {
	CacheAlgorithm string
	Bootstrap      []string
	Proxy          []string
	VPNUnderlay    []string
}

type Config struct {
	MihomoBinary string
	Interface    string
	DNS          DNSConfig
	URLTests     []URLTarget
	TraceURL     string
	SpeedURL     string
	TCPWorkers   int
	URLWorkers   int
	SpeedWorkers int
}

func DefaultConfig() Config {
	return Config{URLTests: []URLTarget{
		{URL: "https://www.gstatic.com/generate_204", StatusCode: 204},
		{URL: "https://cp.cloudflare.com/generate_204", StatusCode: 204},
		{URL: "https://www.msftconnecttest.com/connecttest.txt", StatusCode: 200},
	}, TraceURL: "https://www.cloudflare.com/cdn-cgi/trace", SpeedURL: "https://cachefly.cachefly.net/10mb.test",
		TCPWorkers: 128, URLWorkers: 80, SpeedWorkers: 6}
}

type Backend struct {
	Config     Config
	Diagnostic func(stage string, index int, err error)
	Progress   func(stage string, current, total int)
	baselineMu sync.Mutex
	baseline   qualification.Download
	baselineAt time.Time
}

func (backend *Backend) SetProgress(progress func(stage string, current, total int)) {
	backend.Progress = progress
}

func (backend *Backend) TCP(ctx context.Context, proxies []map[string]any) ([]qualification.Latency, error) {
	config := backend.normalized()
	return parallel(proxies, config.TCPWorkers, func(_ int, item map[string]any) qualification.Latency {
		return tcpLatency(ctx, item, config)
	}, backend.stage("tcp")), nil
}

func (backend *Backend) URL(ctx context.Context, proxies []map[string]any) ([]qualification.Latency, error) {
	config := backend.normalized()
	return parallel(proxies, config.URLWorkers, func(index int, item map[string]any) qualification.Latency {
		result := qualification.Latency{}
		err := withCore(ctx, item, config, func(client *http.Client) error {
			latency, probeErr := probeURLMajority(ctx, client, config.URLTests)
			if probeErr == nil {
				result = qualification.Latency{Alive: true, Seconds: latency}
			}
			return probeErr
		})
		if err != nil && backend.Diagnostic != nil {
			backend.Diagnostic("url", index, err)
		}
		return result
	}, backend.stage("url_test")), nil
}

func (backend *Backend) Baseline(ctx context.Context) (qualification.Download, error) {
	backend.baselineMu.Lock()
	defer backend.baselineMu.Unlock()
	if backend.baseline.OK && time.Since(backend.baselineAt) < 10*time.Minute {
		return backend.baseline, nil
	}
	config := backend.normalized()
	if backend.Progress != nil {
		backend.Progress("baseline", 0, 1)
		defer backend.Progress("baseline", 1, 1)
	}
	client, closeClient := directClient(config)
	defer closeClient()
	download := downloadSpeed(ctx, client, config.SpeedURL)
	if !download.OK || download.BytesPerSecond <= 0 {
		return qualification.Download{}, fmt.Errorf("direct_speed_baseline_failed")
	}
	backend.baseline, backend.baselineAt = download, time.Now()
	return download, nil
}

func (backend *Backend) Speed(ctx context.Context, proxies []map[string]any, geoEnabled bool) ([]qualification.SpeedEvidence, error) {
	config := backend.normalized()
	return parallel(proxies, config.SpeedWorkers, func(_ int, item map[string]any) qualification.SpeedEvidence {
		evidence := qualification.SpeedEvidence{}
		err := withCore(ctx, item, config, func(client *http.Client) error {
			evidence.CoreOK = true
			if geoEnabled {
				country, status := traceCountry(ctx, client, config.TraceURL)
				evidence.Country, evidence.GeoStatus = country, status
				if status != "" {
					return nil
				}
			}
			for index := 0; index < 2; index++ {
				evidence.Downloads = append(evidence.Downloads, downloadSpeed(ctx, client, config.SpeedURL))
			}
			return nil
		})
		if err != nil {
			return qualification.SpeedEvidence{CoreOK: false}
		}
		return evidence
	}, backend.stage("speed_test")), nil
}

func (backend *Backend) normalized() Config {
	config, defaults := backend.Config, DefaultConfig()
	if len(config.URLTests) == 0 {
		config.URLTests = defaults.URLTests
	}
	if config.TraceURL == "" {
		config.TraceURL = defaults.TraceURL
	}
	if config.SpeedURL == "" {
		config.SpeedURL = defaults.SpeedURL
	}
	if config.TCPWorkers <= 0 {
		config.TCPWorkers = defaults.TCPWorkers
	}
	if config.URLWorkers <= 0 {
		config.URLWorkers = defaults.URLWorkers
	}
	if config.SpeedWorkers <= 0 {
		config.SpeedWorkers = defaults.SpeedWorkers
	}
	return config
}

func (backend *Backend) stage(name string) func(int, int) {
	if backend.Progress == nil {
		return nil
	}
	return func(current, total int) { backend.Progress(name, current, total) }
}

func tcpLatency(ctx context.Context, item map[string]any, config Config) qualification.Latency {
	switch strings.ToLower(fmt.Sprint(item["type"])) {
	case "hysteria2", "tuic", "wireguard":
		return qualification.Latency{Alive: true}
	}
	server := strings.TrimSpace(fmt.Sprint(item["server"]))
	port, err := integer(item["port"])
	if err != nil || server == "" {
		return qualification.Latency{}
	}
	dialer := boundDialer(config.Interface, 2*time.Second)
	started := time.Now()
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(server, strconv.Itoa(port)))
	if err != nil {
		return qualification.Latency{}
	}
	_ = connection.Close()
	return qualification.Latency{Alive: true, Seconds: time.Since(started).Seconds()}
}

func withCore(ctx context.Context, item map[string]any, config Config, callback func(*http.Client) error) error {
	directory, err := os.MkdirTemp("", "orcheroute-qualify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	payload, err := json.Marshal(coreConfig(config, item, port))
	if err != nil {
		return err
	}
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, config.MihomoBinary, "-d", directory, "-f", configPath)
	if err := command.Start(); err != nil {
		return err
	}
	defer stopProcess(command)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := waitReady(ctx, command, address); err != nil {
		return err
	}
	dialer, err := proxy.SOCKS5("tcp", address, nil, &net.Dialer{Timeout: 3 * time.Second})
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(_ context.Context, networkName, address string) (net.Conn, error) {
		return dialer.Dial(networkName, address)
	}, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	defer transport.CloseIdleConnections()
	return callback(client)
}

func probeURLMajority(ctx context.Context, client *http.Client, targets []URLTarget) (float64, error) {
	if len(targets) == 0 {
		return 0, fmt.Errorf("url_targets_empty")
	}
	probeContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		latency float64
		err     error
	}
	results := make(chan result, len(targets))
	for _, target := range targets {
		target := target
		go func() {
			started := time.Now()
			request, err := http.NewRequestWithContext(probeContext, http.MethodGet, target.URL, nil)
			if err == nil {
				response, requestErr := client.Do(request)
				if requestErr == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
					_ = response.Body.Close()
					if acceptedURLStatus(response.StatusCode, target.StatusCode) {
						results <- result{latency: time.Since(started).Seconds()}
						return
					}
				}
			}
			results <- result{err: fmt.Errorf("url_probe_failed")}
		}()
	}
	required := len(targets)/2 + 1
	latencies := make([]float64, 0, len(targets))
	failures := 0
	for range targets {
		current := <-results
		if current.err == nil {
			latencies = append(latencies, current.latency)
			if len(latencies) >= required {
				sort.Float64s(latencies)
				return latencies[len(latencies)/2], nil
			}
		} else {
			failures++
			if failures > len(targets)-required {
				return 0, fmt.Errorf("url_majority_failed")
			}
		}
	}
	return 0, fmt.Errorf("url_majority_failed")
}

func acceptedURLStatus(actual, expected int) bool {
	if expected > 0 {
		return actual == expected
	}
	return actual >= http.StatusOK && actual < http.StatusBadRequest
}

func coreConfig(config Config, proxyValue map[string]any, port int) map[string]any {
	candidate := map[string]any{}
	for key, value := range proxyValue {
		candidate[key] = value
	}
	if config.Interface != "" {
		candidate["interface-name"] = config.Interface
	}
	underlay := map[string]any{"name": "QUALIFY-UNDERLAY-DNS", "type": "direct", "udp": true, "ip-version": "ipv4"}
	if config.Interface != "" {
		underlay["interface-name"] = config.Interface
	}
	name := fmt.Sprint(candidate["name"])
	return map[string]any{
		"mixed-port": port, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "silent", "ipv6": false,
		"dns": map[string]any{"enable": true, "ipv6": false, "cache-algorithm": cacheAlgorithm(config.DNS.CacheAlgorithm), "default-nameserver": config.DNS.Bootstrap,
			"nameserver": bindResolvers(config.DNS.Proxy, name), "proxy-server-nameserver": bindResolvers(config.DNS.VPNUnderlay, "QUALIFY-UNDERLAY-DNS")},
		"proxies": []map[string]any{underlay, candidate}, "rules": []string{"MATCH," + name},
	}
}

func directClient(config Config) (*http.Client, func()) {
	dialer := boundDialer(config.Interface, 8*time.Second)
	transport := &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, DisableKeepAlives: true}
	return &http.Client{Transport: transport, Timeout: 45 * time.Second}, transport.CloseIdleConnections
}

func boundDialer(name string, timeout time.Duration) *net.Dialer {
	dialer := &net.Dialer{Timeout: timeout}
	current, err := net.InterfaceByName(name)
	if err != nil {
		return dialer
	}
	if address := interfaceIPv4(name); address != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: address}
	}
	index := current.Index
	dialer.Control = func(_, _ string, connection syscall.RawConn) error {
		var optionErr error
		if err := connection.Control(func(fd uintptr) {
			// Winsock IP_UNICAST_IF (31) expects the interface index in
			// network byte order. This prevents baseline/TCP probes from
			// accidentally following another system-wide VPN default route.
			optionErr = windows.SetsockoptInt(windows.Handle(fd), 0, 31, int(bits.ReverseBytes32(uint32(index))))
		}); err != nil {
			return err
		}
		return optionErr
	}
	return dialer
}

func interfaceIPv4(name string) net.IP {
	current, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addresses, _ := current.Addrs()
	for _, raw := range addresses {
		ip, _, err := net.ParseCIDR(raw.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return ip
		}
	}
	return nil
}

func downloadSpeed(ctx context.Context, client *http.Client, address string) qualification.Download {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return qualification.Download{}
	}
	defer response.Body.Close()
	written, err := io.Copy(io.Discard, io.LimitReader(response.Body, qualification.SpeedBytes))
	elapsed := time.Since(started).Seconds()
	return qualification.Download{OK: err == nil && response.StatusCode == http.StatusOK && written > 0, HTTPCode: response.StatusCode, Bytes: written, BytesPerSecond: float64(written) / elapsed}
}

func traceCountry(ctx context.Context, client *http.Client, address string) (string, string) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	response, err := client.Do(request)
	if err != nil {
		return "", "geo_failed"
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	for _, line := range strings.Split(string(payload), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "loc" {
			country := strings.ToUpper(strings.TrimSpace(value))
			if country != "" {
				return country, ""
			}
		}
	}
	return "", "country_unknown"
}

func waitReady(ctx context.Context, command *exec.Cmd, address string) error {
	for count := 0; count < 50; count++ {
		if command.ProcessState != nil && command.ProcessState.Exited() {
			return fmt.Errorf("mihomo_exited")
		}
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
	return fmt.Errorf("mihomo_start_timeout")
}

func stopProcess(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	_ = command.Process.Kill()
	_, _ = command.Process.Wait()
}

func integer(value any) (int, error) {
	switch current := value.(type) {
	case float64:
		return int(current), nil
	case int:
		return current, nil
	case string:
		return strconv.Atoi(current)
	default:
		return 0, fmt.Errorf("invalid_port")
	}
}

func bindResolvers(values []string, outbound string) []string {
	result := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value+"#"+outbound)
		}
	}
	return result
}

func cacheAlgorithm(value string) string {
	if value == "" {
		return "arc"
	}
	return value
}

func parallel[T any](items []map[string]any, workers int, task func(int, map[string]any) T, progress func(int, int)) []T {
	result := make([]T, len(items))
	if progress != nil {
		progress(0, len(items))
	}
	jobs := make(chan int)
	var group sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	if workers > len(items) {
		workers = len(items)
	}
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				result[index] = task(index, items[index])
				if progress != nil {
					progressMu.Lock()
					completed++
					progress(completed, len(items))
					progressMu.Unlock()
				}
			}
		}()
	}
	for index := range items {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return result
}
