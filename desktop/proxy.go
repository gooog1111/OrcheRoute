package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type apiProxy struct {
	base   *url.URL
	token  string
	client *http.Client
}

func logDesktopProxy(format string, values ...any) {
	state, err := os.UserConfigDir()
	if err != nil {
		return
	}
	directory := filepath.Join(state, "OrcheRoute")
	if os.MkdirAll(directory, 0o700) != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(directory, "desktop.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, time.Now().Format(time.RFC3339)+" "+format+"\n", values...)
}

func newAPIProxy(config desktopConfig) (*apiProxy, error) {
	base, err := url.Parse(strings.TrimSpace(config.APIURL))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return nil, fmt.Errorf("invalid OrcheRoute API URL")
	}
	base.RawQuery, base.Fragment = "", ""
	return &apiProxy{base: base, token: strings.TrimSpace(config.APIToken), client: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (proxy *apiProxy) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	logDesktopProxy("request method=%s path=%s", request.Method, request.URL.Path)
	path := strings.TrimPrefix(request.URL.Path, "/api")
	if !strings.HasPrefix(path, "/v1/") && path != "/healthz" {
		writeProxyError(response, http.StatusNotFound, "desktop_route_not_found")
		return
	}
	target := *proxy.base
	target.Path = strings.TrimRight(proxy.base.Path, "/") + path
	target.RawQuery = request.URL.RawQuery
	body := io.Reader(nil)
	if request.Body != nil {
		body = io.LimitReader(request.Body, 1<<20)
	}
	upstream, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), body)
	if err != nil {
		writeProxyError(response, http.StatusBadGateway, "desktop_request_failed")
		return
	}
	for _, header := range []string{"Accept", "Content-Type", "X-OrcheRoute-UI"} {
		if value := request.Header.Get(header); value != "" {
			upstream.Header.Set(header, value)
		}
	}
	if proxy.token != "" {
		upstream.Header.Set("Authorization", "Bearer "+proxy.token)
	}
	upstreamResponse, err := proxy.client.Do(upstream)
	if err != nil {
		logDesktopProxy("upstream_error path=%s error=%s", path, err)
		writeProxyError(response, http.StatusBadGateway, "desktop_api_unavailable")
		return
	}
	logDesktopProxy("response path=%s status=%d", path, upstreamResponse.StatusCode)
	defer upstreamResponse.Body.Close()
	for _, header := range []string{"Content-Type", "Cache-Control"} {
		if value := upstreamResponse.Header.Get(header); value != "" {
			response.Header().Set(header, value)
		}
	}
	response.WriteHeader(upstreamResponse.StatusCode)
	_, _ = io.Copy(response, io.LimitReader(upstreamResponse.Body, 64<<20))
}

func writeProxyError(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": code})
}
