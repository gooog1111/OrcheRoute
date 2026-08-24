//go:build android

package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	mobileconnectivity "github.com/gooog1111/orcheroute/internal/core/connectivity"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/listener/sing_tun"
	"github.com/metacubex/mihomo/tunnel"
)

var androidEngine = struct {
	sync.Mutex
	protector   SocketProtector
	tun         io.Closer
	initialized bool
}{}

func embeddedEngineAvailable() bool { return true }

func engineInit(home string, protector SocketProtector) string {
	if strings.TrimSpace(home) == "" || (runtime.GOOS == "android" && protector == nil) {
		return engineError("invalid_engine_init")
	}
	androidEngine.Lock()
	defer androidEngine.Unlock()
	C.SetHomeDir(home)
	androidEngine.protector = protector
	androidEngine.initialized = true
	if protector == nil {
		// NetworkExtension provider processes are already outside the packet
		// flow they serve. Apple has no VpnService.protect equivalent.
		dialer.DefaultSocketHook = nil
	} else {
		dialer.DefaultSocketHook = func(_ string, _ string, conn syscall.RawConn) error {
			var protected bool
			if err := conn.Control(func(fd uintptr) {
				protected = protector.Protect(int(fd))
			}); err != nil {
				return err
			}
			if !protected {
				return errors.New("android_vpn_protect_failed")
			}
			return nil
		}
	}
	return encode(map[string]any{"ok": true})
}

func engineLoadConfig(configYAML string) string {
	androidEngine.Lock()
	defer androidEngine.Unlock()
	if !androidEngine.initialized {
		return engineError("engine_not_initialized")
	}
	cfg, err := config.Parse([]byte(configYAML))
	if err != nil {
		return engineError(err.Error())
	}
	hub.ApplyConfig(cfg)
	return encode(map[string]any{"ok": true})
}

func engineStartTun(fd int, stack, gateway, dns string) string {
	androidEngine.Lock()
	defer androidEngine.Unlock()
	if !androidEngine.initialized {
		return engineError("engine_not_initialized")
	}
	if fd <= 0 {
		return engineError("invalid_tun_fd")
	}
	if androidEngine.tun != nil {
		_ = androidEngine.tun.Close()
		androidEngine.tun = nil
	}

	prefix4, prefix6, err := parsePrefixes(gateway)
	if err != nil {
		return engineError(err.Error())
	}
	var dnsHijack []string
	for _, value := range strings.Split(dns, ",") {
		value = strings.TrimSpace(value)
		if value != "" {
			dnsHijack = append(dnsHijack, net.JoinHostPort(value, "53"))
		}
	}
	tunStack, ok := C.StackTypeMapping[strings.ToLower(stack)]
	if !ok {
		tunStack = C.TunSystem
	}
	options := LC.Tun{
		Enable:              true,
		Device:              sing_tun.InterfaceName,
		Stack:               tunStack,
		DNSHijack:           dnsHijack,
		AutoRoute:           false,
		AutoDetectInterface: false,
		Inet4Address:        prefix4,
		Inet6Address:        prefix6,
		MTU:                 9000,
		FileDescriptor:      fd,
	}
	listener, err := sing_tun.New(options, tunnel.Tunnel)
	if err != nil {
		_ = syscall.Close(fd)
		return engineError(err.Error())
	}
	androidEngine.tun = listener
	return encode(map[string]any{"ok": true})
}

func engineStopTun() string {
	androidEngine.Lock()
	defer androidEngine.Unlock()
	if androidEngine.tun != nil {
		_ = androidEngine.tun.Close()
		androidEngine.tun = nil
	}
	return encode(map[string]any{"ok": true})
}

func engineTestProxies(proxiesJSON, testURL string, timeoutMs, concurrency int) string {
	urls, _ := json.Marshal([]string{testURL})
	return engineTestProxiesMulti(proxiesJSON, string(urls), timeoutMs, concurrency)
}

type mobileProbeResult struct {
	Name           string  `json:"name"`
	Alive          bool    `json:"alive"`
	DelayMS        int     `json:"delay_ms,omitempty"`
	SpeedMbps      float64 `json:"speed_mbps,omitempty"`
	StabilityRatio float64 `json:"stability_ratio,omitempty"`
	Country        string  `json:"country,omitempty"`
	Error          string  `json:"error,omitempty"`
}

func engineTestTCP(proxiesJSON string, timeoutMs, concurrency int) string {
	var mappings []map[string]any
	if json.Unmarshal([]byte(proxiesJSON), &mappings) != nil || len(mappings) == 0 {
		return engineError("invalid_proxies")
	}
	if timeoutMs < 500 || timeoutMs > 10000 {
		timeoutMs = 1800
	}
	if concurrency < 1 || concurrency > 128 {
		concurrency = 64
	}
	results := make([]mobileProbeResult, len(mappings))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				mapping := mappings[index]
				current := mobileProbeResult{Name: fmt.Sprint(mapping["name"])}
				typeName := strings.ToLower(fmt.Sprint(mapping["type"]))
				if typeName == "hysteria2" || typeName == "tuic" || typeName == "wireguard" {
					current.Alive = true
					results[index] = current
					continue
				}
				server := fmt.Sprint(mapping["server"])
				port, err := numericPort(mapping["port"])
				if net.ParseIP(server) == nil {
					current.Alive = true // the proxy URL stage resolves and verifies domains
					results[index] = current
					continue
				}
				started := time.Now()
				dialer := net.Dialer{Timeout: time.Duration(timeoutMs) * time.Millisecond, Control: protectedSocketControl}
				connection, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(server, strconv.Itoa(port)))
				if err == nil {
					_ = connection.Close()
					current.Alive = true
					current.DelayMS = int(time.Since(started) / time.Millisecond)
				}
				if err != nil {
					current.Error = err.Error()
				}
				results[index] = current
			}
		}()
	}
	for index := range mappings {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return encode(map[string]any{"ok": true, "result": map[string]any{"nodes": results}})
}

func engineTestProxiesMulti(proxiesJSON, testURLsJSON string, timeoutMs, concurrency int) string {
	var mappings []map[string]any
	var testURLs []string
	if json.Unmarshal([]byte(proxiesJSON), &mappings) != nil || len(mappings) == 0 || json.Unmarshal([]byte(testURLsJSON), &testURLs) != nil {
		return engineError("invalid_proxy_url_test")
	}
	cleanURLs := make([]string, 0, len(testURLs))
	for _, current := range testURLs {
		if strings.HasPrefix(current, "https://") || strings.HasPrefix(current, "http://") {
			cleanURLs = append(cleanURLs, current)
		}
	}
	if len(cleanURLs) == 0 {
		return engineError("missing_test_urls")
	}
	if timeoutMs < 1000 || timeoutMs > 30000 {
		timeoutMs = 8000
	}
	if concurrency < 1 || concurrency > 96 {
		concurrency = 24
	}
	results := parallelMobile(mappings, concurrency, func(mapping map[string]any) mobileProbeResult {
		current := mobileProbeResult{Name: fmt.Sprint(mapping["name"])}
		proxy, err := adapter.ParseProxy(mapping)
		latencies := make([]int, 0, len(cleanURLs))
		if err == nil {
			type urlResult struct {
				delay int
				err   error
			}
			completed := make(chan urlResult, len(cleanURLs))
			for _, testURL := range cleanURLs {
				go func(target string) {
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
					defer cancel()
					delay, testErr := proxy.URLTest(ctx, target, utils.IntRanges[uint16](nil))
					completed <- urlResult{delay: int(delay), err: testErr}
				}(testURL)
			}
			for range cleanURLs {
				result := <-completed
				if result.err == nil && result.delay > 0 {
					latencies = append(latencies, result.delay)
				} else if result.err != nil {
					err = result.err
				}
			}
		}
		required := len(cleanURLs)/2 + 1
		if len(latencies) >= required {
			sortInts(latencies)
			current.Alive = true
			current.DelayMS = latencies[len(latencies)/2]
		} else if err != nil {
			current.Error = err.Error()
		} else {
			current.Error = fmt.Sprintf("only_%d_of_%d_url_tests_succeeded", len(latencies), len(cleanURLs))
		}
		return current
	})
	return encode(map[string]any{"ok": true, "result": map[string]any{"nodes": results}})
}

func engineFilterCountries(proxiesJSON, excludedJSON string, timeoutMs, concurrency int) string {
	var mappings []map[string]any
	var excludedValues []string
	if json.Unmarshal([]byte(proxiesJSON), &mappings) != nil || len(mappings) == 0 || json.Unmarshal([]byte(excludedJSON), &excludedValues) != nil {
		return engineError("invalid_country_filter")
	}
	excluded := map[string]bool{}
	for _, value := range excludedValues {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" {
			excluded[value] = true
		}
	}
	if timeoutMs < 1000 || timeoutMs > 15000 {
		timeoutMs = 5000
	}
	if concurrency < 1 || concurrency > 32 {
		concurrency = 12
	}
	results := parallelMobile(mappings, concurrency, func(mapping map[string]any) mobileProbeResult {
		current := mobileProbeResult{Name: fmt.Sprint(mapping["name"])}
		proxy, err := adapter.ParseProxy(mapping)
		if err == nil {
			current.Country, err = countryThroughProxy(proxy, time.Duration(timeoutMs)*time.Millisecond)
		}
		if err != nil {
			current.Error = "country_unknown"
		} else if excluded[current.Country] {
			current.Error = "country_excluded"
		} else {
			current.Alive = true
		}
		return current
	})
	return encode(map[string]any{"ok": true, "result": map[string]any{"nodes": results}})
}

func countryThroughProxy(proxy C.Proxy, timeout time.Duration) (string, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		metadata := C.Metadata{}
		if err := metadata.SetRemoteAddress(address); err != nil {
			return nil, err
		}
		return proxy.DialContext(ctx, &metadata)
	}}
	client := &http.Client{Timeout: timeout, Transport: transport}
	response, err := client.Get("https://www.cloudflare.com/cdn-cgi/trace")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errors.New("geo_http_failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "loc=") {
			country := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "loc=")))
			if len(country) == 2 {
				return country, nil
			}
		}
	}
	return "", errors.New("country_unknown")
}

func engineSpeedAvailable(testURL string, timeoutMs int) string {
	if timeoutMs < 1000 || timeoutMs > 30000 {
		timeoutMs = 8000
	}
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: time.Duration(timeoutMs) * time.Millisecond, Control: protectedSocketControl}).DialContext}}
	req, err := http.NewRequest(http.MethodGet, testURL, nil)
	if err == nil {
		req.Header.Set("Range", "bytes=0-1048575")
	}
	var count int64
	started := time.Now()
	if err == nil {
		var response *http.Response
		response, err = client.Do(req)
		if err == nil {
			count, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1048576))
			_ = response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 400 || count < 65536 {
				err = errors.New("speed_endpoint_incomplete")
			}
		}
	}
	if err != nil {
		return encode(map[string]any{"ok": true, "result": map[string]any{"available": false, "error": err.Error()}})
	}
	seconds := time.Since(started).Seconds()
	mbps := float64(count) * 8 / seconds / 1_000_000
	recommended := int64(mbps * 1_000_000 / 8 * .5)
	if recommended < 262144 {
		recommended = 262144
	}
	if recommended > 2097152 {
		recommended = 2097152
	}
	return encode(map[string]any{"ok": true, "result": map[string]any{"available": true, "baseline_mbps": math.Round(mbps*100) / 100, "recommended_bytes": recommended}})
}

func platformProbeConnectivity(allowlistURL, openInternetURL string, timeoutMs int) string {
	if timeoutMs < 1000 || timeoutMs > 15000 {
		timeoutMs = 5000
	}
	result, err := mobileconnectivity.Diagnose(context.Background(), mobileconnectivity.Config{
		AllowlistURL: allowlistURL, OpenInternetURL: openInternetURL,
	}, func(ctx context.Context, target mobileconnectivity.Target) bool {
		client := &http.Client{
			Timeout:   time.Duration(timeoutMs) * time.Millisecond,
			Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: time.Duration(timeoutMs) * time.Millisecond, Control: protectedSocketControl}).DialContext},
		}
		if target.OpenInternet {
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
		if err != nil {
			return false
		}
		request.Header.Set("Cache-Control", "no-cache, no-store")
		request.Header.Set("Pragma", "no-cache")
		response, err := client.Do(request)
		ok := false
		if err == nil && response != nil {
			if target.ExpectNoContent {
				ok = response.StatusCode == http.StatusNoContent
			} else {
				ok = response.StatusCode >= 200 && response.StatusCode < 300
			}
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
			_ = response.Body.Close()
		}
		return ok
	})
	if err != nil {
		return engineError(err.Error())
	}
	return encode(map[string]any{"ok": true, "result": result})
}

func engineTestSpeed(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64) string {
	return engineTestSpeedAdaptive(proxiesJSON, testURL, timeoutMs, concurrency, minimumMbps, stabilityRatio, 10_485_760)
}

func engineTestSpeedAdaptive(proxiesJSON, testURL string, timeoutMs, concurrency int, minimumMbps, stabilityRatio float64, sampleBytes int64) string {
	var mappings []map[string]any
	if json.Unmarshal([]byte(proxiesJSON), &mappings) != nil || len(mappings) == 0 {
		return engineError("invalid_proxies")
	}
	if timeoutMs < 5000 || timeoutMs > 120000 {
		timeoutMs = 45000
	}
	if concurrency < 1 || concurrency > 6 {
		concurrency = 6
	}
	if minimumMbps <= 0 {
		minimumMbps = 10
	}
	if stabilityRatio <= 0 || stabilityRatio > 1 {
		stabilityRatio = .65
	}
	if sampleBytes < 262144 {
		sampleBytes = 262144
	}
	if sampleBytes > 2097152 {
		sampleBytes = 2097152
	}
	results := parallelMobile(mappings, concurrency, func(mapping map[string]any) mobileProbeResult {
		current := mobileProbeResult{Name: fmt.Sprint(mapping["name"])}
		proxy, err := adapter.ParseProxy(mapping)
		speeds := make([]float64, 0, 2)
		if err == nil {
			for run := 0; run < 2; run++ {
				var speed float64
				speed, err = downloadThroughProxy(proxy, testURL, time.Duration(timeoutMs)*time.Millisecond, sampleBytes)
				if err != nil {
					break
				}
				speeds = append(speeds, speed)
			}
		}
		if err == nil && len(speeds) == 2 {
			slowest, fastest := math.Min(speeds[0], speeds[1]), math.Max(speeds[0], speeds[1])
			current.SpeedMbps = math.Round(slowest*100) / 100
			if fastest > 0 {
				current.StabilityRatio = math.Round((slowest/fastest)*1000) / 1000
			}
			switch {
			case slowest < minimumMbps:
				current.Error = "speed_below_threshold"
			case fastest > 0 && slowest/fastest < stabilityRatio:
				current.Error = "speed_unstable"
			default:
				current.Alive = true
			}
		} else if err != nil {
			current.Error = err.Error()
		}
		return current
	})
	return encode(map[string]any{"ok": true, "result": map[string]any{"nodes": results}})
}

func downloadThroughProxy(proxy C.Proxy, testURL string, timeout time.Duration, required int64) (float64, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		metadata := C.Metadata{}
		if err := metadata.SetRemoteAddress(address); err != nil {
			return nil, err
		}
		return proxy.DialContext(ctx, &metadata)
	}}
	client := &http.Client{Timeout: timeout, Transport: transport}
	started := time.Now()
	request, err := http.NewRequest(http.MethodGet, testURL, nil)
	if err == nil {
		request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", required-1))
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("speed_http_%d", response.StatusCode)
	}
	written, err := io.Copy(io.Discard, io.LimitReader(response.Body, required))
	if err != nil || written < required {
		return 0, errors.New("speed_incomplete")
	}
	seconds := time.Since(started).Seconds()
	if seconds <= 0 {
		return 0, errors.New("speed_invalid")
	}
	return float64(written) * 8 / seconds / 1_000_000, nil
}

func parallelMobile[T any](mappings []map[string]any, concurrency int, run func(map[string]any) T) []T {
	results := make([]T, len(mappings))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = run(mappings[index])
			}
		}()
	}
	for index := range mappings {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func numericPort(value any) (int, error) {
	switch current := value.(type) {
	case float64:
		return int(current), nil
	case int:
		return current, nil
	case string:
		return strconv.Atoi(current)
	default:
		return 0, errors.New("invalid_port")
	}
}

func protectedSocketControl(_, _ string, connection syscall.RawConn) error {
	androidEngine.Lock()
	protector := androidEngine.protector
	androidEngine.Unlock()
	if protector == nil {
		// Apple NetworkExtension has no VpnService.protect API. Provider-owned
		// outbound sockets are scoped outside the virtual interface.
		return nil
	}
	var protected bool
	if err := connection.Control(func(fd uintptr) { protected = protector.Protect(int(fd)) }); err != nil {
		return err
	}
	if !protected {
		return errors.New("android_vpn_protect_failed")
	}
	return nil
}

func sortInts(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func parsePrefixes(value string) ([]netip.Prefix, []netip.Prefix, error) {
	var prefix4 []netip.Prefix
	var prefix6 []netip.Prefix
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, nil, err
		}
		if prefix.Addr().Is4() {
			prefix4 = append(prefix4, prefix)
		} else {
			prefix6 = append(prefix6, prefix)
		}
	}
	if len(prefix4) == 0 && len(prefix6) == 0 {
		return nil, nil, errors.New("missing_tun_gateway")
	}
	return prefix4, prefix6, nil
}

func engineError(message string) string {
	return encode(map[string]any{"ok": false, "error": map[string]string{"error": message}})
}
