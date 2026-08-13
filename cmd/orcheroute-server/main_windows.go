//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/gooog1111/orcheroute/internal/serverruntime"
	"golang.org/x/sys/windows/svc"
)

const windowsServiceName = "OrcheRoute"

func main() {
	config := serverruntime.DefaultConfig()
	flag.StringVar(&config.Listen, "listen", config.Listen, "API listen address")
	flag.StringVar(&config.WebListen, "web-listen", config.WebListen, "WebUI listen address")
	flag.StringVar(&config.WebTLSListen, "web-tls-listen", config.WebTLSListen, "optional HTTPS WebUI listen address")
	flag.StringVar(&config.ProductionState, "production-state", config.ProductionState, "production state")
	flag.StringVar(&config.StateDirectory, "state-dir", config.StateDirectory, "state directory")
	flag.StringVar(&config.WebRoot, "web-root", config.WebRoot, "static WebUI root")
	flag.StringVar(&config.RuntimeEnv, "runtime-env", config.RuntimeEnv, "runtime secret environment")
	flag.StringVar(&config.ConfigDirectory, "config-dir", config.ConfigDirectory, "configuration directory")
	flag.StringVar(&config.MihomoAPI, "mihomo-api", config.MihomoAPI, "Mihomo controller URL")
	flag.StringVar(&config.MihomoBinary, "mihomo", config.MihomoBinary, "Mihomo binary")
	flag.StringVar(&config.UpdateBinary, "update-binary", config.UpdateBinary, "subscription updater")
	flag.StringVar(&config.NetworkBinary, "network-binary", config.NetworkBinary, "network transaction helper")
	flag.StringVar(&config.ComponentBinary, "component-binary", config.ComponentBinary, "component updater")
	flag.StringVar(&config.CoreService, "core-service", config.CoreService, "Mihomo Windows service name")
	flag.DurationVar(&config.ControllerEvery, "controller-interval", config.ControllerEvery, "controller interval")
	flag.BoolVar(&config.RequireAPIAuth, "api-auth", config.RequireAPIAuth, "require Bearer authentication for the control API")
	flag.Parse()

	isService, err := svc.IsWindowsService()
	if err == nil && isService {
		if err := svc.Run(windowsServiceName, &serviceHandler{config: config}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := runServer(ctx, config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type serviceHandler struct {
	config serverruntime.Config
}

func (handler *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServer(ctx, handler.config) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	status <- current
	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- current
			case svc.Stop, svc.Shutdown:
				current = svc.Status{State: svc.StopPending}
				status <- current
				cancel()
				if err := <-done; err != nil {
					return true, 1
				}
				return false, 0
			}
		case err := <-done:
			cancel()
			if err != nil {
				return true, 1
			}
			return false, 0
		}
	}
}

func runServer(ctx context.Context, config serverruntime.Config) error {
	runtime, err := serverruntime.New(config)
	if err != nil {
		return err
	}
	defer runtime.Close()
	go runtime.RunController(ctx)

	api := newHTTPServer(config.Listen, runtime.APIHandler())
	var web *http.Server
	errors := make(chan error, 3)
	go func() { errors <- api.ListenAndServe() }()
	if config.WebListen != "" {
		web = newHTTPServer(config.WebListen, runtime.WebHandler())
		go func() { errors <- web.ListenAndServe() }()
	}
	var webTLS *http.Server
	if config.WebTLSListen != "" {
		certificate, key, enabled := runtime.WebTLSSettings()
		if enabled {
			webTLS = newHTTPServer(config.WebTLSListen, runtime.WebHandler())
			go func() { errors <- webTLS.ListenAndServeTLS(certificate, key) }()
		}
	}
	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-errors:
		if err != nil && err != http.ErrServerClosed {
			serveErr = err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var shutdownErr error
	var shutdownOnce sync.Once
	stopServer := func(server *http.Server) {
		if err := server.Shutdown(shutdown); err != nil {
			shutdownOnce.Do(func() { shutdownErr = err })
		}
	}
	stopServer(api)
	if web != nil {
		stopServer(web)
	}
	if webTLS != nil {
		stopServer(webTLS)
	}
	if serveErr != nil {
		return serveErr
	}
	return shutdownErr
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
}
