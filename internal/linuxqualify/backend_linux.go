//go:build linux

package linuxqualify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/gooog1111/orcheroute/internal/core/qualification"
	"golang.org/x/net/proxy"
	"golang.org/x/sys/unix"
)

type Backend struct {
	Config     Config
	Diagnostic func(stage string, index int, err error)
	Progress   func(stage string, current, total int)
	baselineMu sync.Mutex
	baseline   qualification.Download
	baselineAt time.Time
	speedBytes int64
}

func (backend *Backend) SetProgress(progress func(stage string, current, total int)) {
	backend.Progress = progress
}

func (backend *Backend) SetSpeedBytes(value int64) {
	backend.baselineMu.Lock()
	backend.speedBytes = value
	backend.baselineMu.Unlock()
}

func (backend *Backend) currentSpeedBytes() int64 {
	backend.baselineMu.Lock()
	defer backend.baselineMu.Unlock()
	if backend.speedBytes <= 0 {
		return qualification.MinSpeedSampleBytes
	}
	return backend.speedBytes
}

func (backend *Backend) TCP(ctx context.Context, proxies []map[string]any) ([]qualification.Latency, error) {
	config := backend.normalized()
	return parallel(proxies, config.TCPWorkers, func(_ int, item map[string]any) qualification.Latency { return tcpLatency(ctx, item, config) }, backend.stage("tcp")), nil
}
func (backend *Backend) URL(ctx context.Context, proxies []map[string]any) ([]qualification.Latency, error) {
	config := backend.normalized()
	return parallel(proxies, config.URLWorkers, func(index int, item map[string]any) qualification.Latency {
		var result qualification.Latency
		err := withCore(ctx, item, config, func(client *http.Client, proxyAddress string) error {
			latency, probeErr := probeURLMajority(ctx, client, proxyAddress, config.URLTests)
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
	if backend.Progress != nil {
		backend.Progress("baseline", 0, 1)
		defer backend.Progress("baseline", 1, 1)
	}
	config := backend.normalized()
	client, closeClient := directClient(config)
	defer closeClient()
	download := downloadSpeed(client, config.SpeedURL, qualification.SpeedBytes)
	if !download.OK || (download.HTTPCode != http.StatusOK && download.HTTPCode != http.StatusPartialContent) || download.Bytes < qualification.SpeedBytes || download.BytesPerSecond <= 0 {
		return qualification.Download{}, fmt.Errorf("direct speed baseline failed")
	}
	backend.baseline, backend.baselineAt = download, time.Now()
	return download, nil
}
func (backend *Backend) Geo(ctx context.Context, proxies []map[string]any) ([]qualification.GeoEvidence, error) {
	config := backend.normalized()
	return parallel(proxies, config.SpeedWorkers, func(_ int, item map[string]any) qualification.GeoEvidence {
		evidence := qualification.GeoEvidence{}
		err := withCore(ctx, item, config, func(client *http.Client, proxyAddress string) error {
			country, status := traceCountry(client, config.TraceURL)
			if status != "" {
				country, status = curlCountry(ctx, proxyAddress, config.TraceURL)
			}
			evidence.Country = country
			evidence.Status = status
			evidence.OK = status == "" && country != ""
			return nil
		})
		if err != nil {
			evidence = qualification.GeoEvidence{Status: "country_unknown"}
		}
		return evidence
	}, backend.stage("geo")), nil
}

func (backend *Backend) Speed(ctx context.Context, proxies []map[string]any) ([]qualification.SpeedEvidence, error) {
	config := backend.normalized()
	speedBytes := backend.currentSpeedBytes()
	return parallel(proxies, config.SpeedWorkers, func(_ int, item map[string]any) qualification.SpeedEvidence {
		evidence := qualification.SpeedEvidence{}
		err := withCore(ctx, item, config, func(client *http.Client, proxyAddress string) error {
			evidence.CoreOK = true
			for index := 0; index < 2; index++ {
				download := downloadSpeed(client, config.SpeedURL, speedBytes)
				if !download.OK || (download.HTTPCode != http.StatusOK && download.HTTPCode != http.StatusPartialContent) || download.Bytes < speedBytes {
					if fallback, fallbackErr := curlSpeed(ctx, proxyAddress, config.SpeedURL, speedBytes); fallbackErr == nil {
						download = fallback
					}
				}
				evidence.Downloads = append(evidence.Downloads, download)
			}
			return nil
		})
		if err != nil {
			evidence = qualification.SpeedEvidence{CoreOK: false}
		}
		return evidence
	}, backend.stage("speed_test")), nil
}

func (backend *Backend) stage(stage string) func(current, total int) {
	if backend.Progress == nil {
		return nil
	}
	return func(current, total int) { backend.Progress(stage, current, total) }
}

func (backend *Backend) normalized() Config {
	config := backend.Config
	defaults := DefaultConfig()
	if config.MihomoBinary == "" {
		config.MihomoBinary = defaults.MihomoBinary
	}
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

func tcpLatency(ctx context.Context, item map[string]any, config Config) qualification.Latency {
	// QUIC/UDP transports are verified by the following URL stage through a
	// real temporary Mihomo instance; a TCP connect would reject valid nodes.
	switch strings.ToLower(fmt.Sprint(item["type"])) {
	case "hysteria2", "tuic", "wireguard":
		return qualification.Latency{Alive: true}
	}
	server := fmt.Sprint(item["server"])
	port, err := integer(item["port"])
	if err != nil {
		return qualification.Latency{}
	}
	ip := net.ParseIP(server)
	if ip == nil || ip.To4() == nil {
		return qualification.Latency{Alive: true, Seconds: 0}
	}
	dialer := net.Dialer{Timeout: 1800 * time.Millisecond, Control: func(_, _ string, connection syscall.RawConn) error {
		var controlErr error
		err := connection.Control(func(fd uintptr) {
			if current := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, config.Mark); current != nil {
				controlErr = current
				return
			}
			controlErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, config.Interface)
		})
		if err != nil {
			return err
		}
		return controlErr
	}}
	started := time.Now()
	connection, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(server, strconv.Itoa(port)))
	if err != nil {
		return qualification.Latency{}
	}
	_ = connection.Close()
	return qualification.Latency{Alive: true, Seconds: time.Since(started).Seconds()}
}

func withCore(ctx context.Context, item map[string]any, config Config, callback func(*http.Client, string) error) error {
	directory, err := os.MkdirTemp("", "orcheroute-qualify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	port, err := freePort()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(CoreConfig(config, item, port))
	if err != nil {
		return err
	}
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, config.MihomoBinary, "-d", directory, "-f", configPath)
	var stderr bytes.Buffer
	command.Stdout = io.Discard
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return err
	}
	defer stopProcess(command)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := waitReady(ctx, command, address); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 1000 {
			detail = detail[len(detail)-1000:]
		}
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	dialer, err := proxy.SOCKS5("tcp", address, nil, &net.Dialer{Timeout: 3 * time.Second})
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
		return dialer.Dial(network, address)
	}, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	defer transport.CloseIdleConnections()
	return callback(client, address)
}

func probeURL(ctx context.Context, client *http.Client, proxyAddress string, target URLTarget) (float64, error) {
	probeContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	started := time.Now()
	request, err := http.NewRequestWithContext(probeContext, http.MethodGet, target.URL, nil)
	if err == nil {
		if response, requestErr := client.Do(request); requestErr == nil {
			defer response.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			if acceptedURLStatus(response.StatusCode, target.StatusCode) {
				return time.Since(started).Seconds(), nil
			}
		}
	}
	return curlURL(probeContext, proxyAddress, target)
}

func probeURLMajority(ctx context.Context, client *http.Client, proxyAddress string, targets []URLTarget) (float64, error) {
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
			latency, err := probeURL(probeContext, client, proxyAddress, target)
			results <- result{latency: latency, err: err}
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
				return 0, fmt.Errorf("only %d of %d URL probes succeeded", len(latencies), len(targets))
			}
		}
	}
	return 0, fmt.Errorf("only %d of %d URL probes succeeded", len(latencies), len(targets))
}

func curlURL(ctx context.Context, proxyAddress string, target URLTarget) (float64, error) {
	output, err := exec.CommandContext(ctx, "curl", "--silent", "--show-error", "--location", "--proxy", "socks5h://"+proxyAddress, "--connect-timeout", "3", "--max-time", "3", "--output", "/dev/null", "--write-out", "%{http_code} %{time_total}", target.URL).Output()
	if err != nil {
		return 0, err
	}
	parts := strings.Fields(string(output))
	if len(parts) != 2 {
		return 0, fmt.Errorf("unexpected status")
	}
	status, statusErr := strconv.Atoi(parts[0])
	if statusErr != nil || !acceptedURLStatus(status, target.StatusCode) {
		return 0, fmt.Errorf("unexpected status")
	}
	return strconv.ParseFloat(parts[1], 64)
}

func acceptedURLStatus(actual, expected int) bool {
	if expected > 0 {
		return actual == expected
	}
	return actual >= http.StatusOK && actual < http.StatusBadRequest
}

func directClient(config Config) (*http.Client, func()) {
	dialer := &net.Dialer{Timeout: 8 * time.Second, Control: func(_, _ string, connection syscall.RawConn) error {
		var controlErr error
		err := connection.Control(func(fd uintptr) {
			if config.Mark != 0 {
				controlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, config.Mark)
				if controlErr != nil {
					return
				}
			}
			if config.Interface != "" {
				controlErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, config.Interface)
			}
		})
		if err != nil {
			return err
		}
		return controlErr
	}}
	transport := &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, DisableKeepAlives: true}
	return &http.Client{Transport: transport, Timeout: 60 * time.Second}, transport.CloseIdleConnections
}

func curlCountry(ctx context.Context, proxyAddress, target string) (string, string) {
	output, err := exec.CommandContext(ctx, "curl", "--silent", "--show-error", "--location", "--proxy", "socks5h://"+proxyAddress, "--connect-timeout", "4", "--max-time", "12", target).Output()
	if err != nil {
		return "", "geo_failed"
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "loc" {
			country := strings.ToUpper(strings.TrimSpace(value))
			if country == "" {
				return "", "country_unknown"
			}
			return country, ""
		}
	}
	return "", "country_unknown"
}

func curlSpeed(ctx context.Context, proxyAddress, target string, limit int64) (qualification.Download, error) {
	byteRange := fmt.Sprintf("0-%d", limit-1)
	output, err := exec.CommandContext(ctx, "curl", "--silent", "--show-error", "--location", "--range", byteRange, "--proxy", "socks5h://"+proxyAddress, "--connect-timeout", "4", "--max-time", "15", "--output", "/dev/null", "--write-out", "%{http_code} %{size_download} %{speed_download}", target).Output()
	if err != nil {
		return qualification.Download{}, err
	}
	parts := strings.Fields(string(output))
	if len(parts) != 3 {
		return qualification.Download{}, fmt.Errorf("invalid curl speed result")
	}
	code, err := strconv.Atoi(parts[0])
	if err != nil {
		return qualification.Download{}, err
	}
	bytesValue, err := strconv.ParseInt(strings.Split(parts[1], ".")[0], 10, 64)
	if err != nil {
		return qualification.Download{}, err
	}
	speed, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return qualification.Download{}, err
	}
	return qualification.Download{OK: true, HTTPCode: code, Bytes: bytesValue, BytesPerSecond: speed, ExpectedBytes: limit}, nil
}

func waitReady(ctx context.Context, command *exec.Cmd, address string) error {
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("mihomo probe did not start")
		case <-ticker.C:
			if command.ProcessState != nil {
				return fmt.Errorf("mihomo rejected proxy")
			}
			connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				connection.Close()
				return nil
			}
		}
	}
}
func stopProcess(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}
func freePort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func traceCountry(client *http.Client, url string) (string, string) {
	response, err := client.Get(url)
	if err != nil {
		return "", "geo_failed"
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "geo_failed"
	}
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 1<<20))
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found && strings.TrimSpace(key) == "loc" {
			country := strings.ToUpper(strings.TrimSpace(value))
			if country == "" {
				return "", "country_unknown"
			}
			return country, ""
		}
	}
	if scanner.Err() != nil {
		return "", "geo_failed"
	}
	return "", "country_unknown"
}
func downloadSpeed(client *http.Client, url string, limit int64) qualification.Download {
	request, _ := http.NewRequest(http.MethodGet, url, nil)
	request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return qualification.Download{}
	}
	defer response.Body.Close()
	written, copyErr := io.Copy(io.Discard, io.LimitReader(response.Body, limit))
	elapsed := time.Since(started).Seconds()
	speed := float64(0)
	if elapsed > 0 {
		speed = float64(written) / elapsed
	}
	return qualification.Download{OK: copyErr == nil, HTTPCode: response.StatusCode, Bytes: written, BytesPerSecond: speed, ExpectedBytes: limit}
}

func parallel[T any](items []map[string]any, workers int, run func(int, map[string]any) T, progress func(current, total int)) []T {
	if workers < 1 {
		workers = 1
	}
	result := make([]T, len(items))
	jobs := make(chan int)
	var group sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	if progress != nil {
		progress(0, len(items))
	}
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				result[index] = run(index, items[index])
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
func integer(value any) (int, error) {
	switch current := value.(type) {
	case int:
		return current, nil
	case float64:
		return int(current), nil
	case json.Number:
		return strconv.Atoi(current.String())
	case string:
		return strconv.Atoi(current)
	default:
		return 0, fmt.Errorf("invalid port")
	}
}
