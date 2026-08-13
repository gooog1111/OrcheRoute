package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopProxyKeepsTokenOutOfFrontend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("missing token")
		}
		if request.Header.Get("X-OrcheRoute-UI") != "1" {
			t.Errorf("missing mutation marker")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"ok":true}`)
	}))
	defer server.Close()
	proxy, err := newAPIProxy(desktopConfig{APIURL: server.URL, APIToken: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://desktop/api/v1/control/auto", strings.NewReader("{}"))
	request.Header.Set("X-OrcheRoute-UI", "1")
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestDesktopProxyAllowsLocalAPIWithoutTokenAndRejectsUnknownRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			t.Errorf("unexpected authorization header")
		}
		_, _ = io.WriteString(response, `{"local":true}`)
	}))
	defer server.Close()
	proxy, err := newAPIProxy(desktopConfig{APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://desktop/api/v1/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"local":true`) {
		t.Fatalf("local API status: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	proxy.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://desktop/private/file", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown route status: %d", response.Code)
	}
}
