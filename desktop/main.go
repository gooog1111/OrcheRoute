package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var frontend embed.FS

func main() {
	apiURL := flag.String("api-url", "", "OrcheRoute API URL")
	runtimeEnv := flag.String("runtime-env", "", "path to runtime.env")
	diagnose := flag.Bool("diagnose", false, "check the local desktop API without opening a window")
	flag.Parse()

	config, err := loadConfig(*apiURL, *runtimeEnv)
	if err != nil {
		panic(err)
	}
	if *diagnose {
		if err := diagnoseAPI(config); err != nil {
			fmt.Println("api=failed", err)
			return
		}
		fmt.Println("api=available")
		return
	}
	proxy, err := newAPIProxy(config)
	if err != nil {
		panic(err)
	}
	var appContext context.Context
	err = wails.Run(&options.App{
		Title:                    "OrcheRoute",
		Width:                    1440,
		Height:                   900,
		MinWidth:                 980,
		MinHeight:                680,
		BackgroundColour:         &options.RGBA{R: 2, G: 7, B: 6, A: 255},
		AssetServer:              &assetserver.Options{Assets: frontend, Handler: proxy},
		EnableDefaultContextMenu: false,
		HideWindowOnClose:        true,
		OnStartup: func(ctx context.Context) {
			appContext = ctx
			startDesktopTray(ctx)
		},
		OnShutdown: func(_ context.Context) { stopDesktopTray() },
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "orcheroute-desktop",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				if appContext != nil {
					wailsruntime.WindowShow(appContext)
					wailsruntime.WindowUnminimise(appContext)
				}
			},
		},
	})
	if err != nil {
		fmt.Println("desktop error:", err)
	}
}

func diagnoseAPI(config desktopConfig) error {
	for _, path := range []string{"/healthz", "/v1/status", "/v1/pools", "/v1/nodes", "/v1/subscriptions"} {
		request, err := http.NewRequest(http.MethodGet, strings.TrimRight(config.APIURL, "/")+path, nil)
		if err != nil {
			return err
		}
		if path != "/healthz" {
			request.Header.Set("Authorization", "Bearer "+config.APIToken)
		}
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		fmt.Printf("api_path=%s status=%d\n", path, response.StatusCode)
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("%s returned HTTP %d", path, response.StatusCode)
		}
	}
	return nil
}
