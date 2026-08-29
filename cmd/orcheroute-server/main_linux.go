//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gooog1111/orcheroute/internal/serverruntime"
)

func main() {
	config := serverruntime.DefaultConfig()
	flag.StringVar(&config.Listen, "listen", config.Listen, "API listen address")
	flag.StringVar(&config.WebListen, "web-listen", config.WebListen, "WebUI listen address")
	flag.StringVar(&config.WebTLSListen, "web-tls-listen", config.WebTLSListen, "optional HTTPS WebUI listen address")
	flag.StringVar(&config.ProductionState, "production-state", config.ProductionState, "read-only production state")
	flag.StringVar(&config.StateDirectory, "state-dir", config.StateDirectory, "isolated Go state")
	flag.StringVar(&config.WebRoot, "web-root", config.WebRoot, "static WebUI root")
	flag.StringVar(&config.RuntimeEnv, "runtime-env", config.RuntimeEnv, "runtime secret environment")
	flag.StringVar(&config.ConfigDirectory, "config-dir", config.ConfigDirectory, "configuration directory")
	flag.StringVar(&config.MihomoAPI, "mihomo-api", config.MihomoAPI, "observed Mihomo controller URL")
	flag.StringVar(&config.MihomoBinary, "mihomo", config.MihomoBinary, "Mihomo binary")
	flag.StringVar(&config.UpdateBinary, "update-binary", config.UpdateBinary, "subscription updater")
	flag.StringVar(&config.NetworkBinary, "network-binary", config.NetworkBinary, "network transaction helper")
	flag.StringVar(&config.ComponentBinary, "component-binary", config.ComponentBinary, "component updater")
	flag.StringVar(&config.CoreService, "core-service", config.CoreService, "Mihomo systemd service")
	flag.DurationVar(&config.ControllerEvery, "controller-interval", config.ControllerEvery, "controller interval")
	flag.DurationVar(&config.ConnectivityEvery, "connectivity-interval", config.ConnectivityEvery, "physical-network monitor interval")
	flag.DurationVar(&config.ConnectivityTimeout, "connectivity-timeout", config.ConnectivityTimeout, "physical-network probe timeout")
	flag.BoolVar(&config.RequireAPIAuth, "api-auth", config.RequireAPIAuth, "require Bearer authentication for the control API")
	flag.Parse()
	runtime, err := serverruntime.New(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer runtime.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go runtime.RunController(ctx)
	go runtime.RunConnectivityMonitor(ctx)
	go runtime.RunIdentityMonitor(ctx)
	go runtime.RunAppUpdateMonitor(ctx)
	go runtime.ReconcileCallServer(ctx)
	api := &http.Server{Addr: config.Listen, Handler: runtime.APIHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	var web *http.Server
	errors := make(chan error, 3)
	go func() { errors <- api.ListenAndServe() }()
	if config.WebListen != "" {
		web = &http.Server{Addr: config.WebListen, Handler: runtime.WebHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
		go func() { errors <- web.ListenAndServe() }()
	}
	var webTLS *http.Server
	if config.WebTLSListen != "" {
		certificate, key, enabled := runtime.WebTLSSettings()
		if enabled {
			webTLS = &http.Server{Addr: config.WebTLSListen, Handler: runtime.WebHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
			go func() { errors <- webTLS.ListenAndServeTLS(certificate, key) }()
		}
	}
	select {
	case <-ctx.Done():
	case err := <-errors:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			stop()
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	if web != nil {
		_ = web.Shutdown(shutdown)
	}
	if webTLS != nil {
		_ = webTLS.Shutdown(shutdown)
	}
}
