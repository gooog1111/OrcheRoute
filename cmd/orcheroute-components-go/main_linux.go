//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gooog1111/orcheroute/internal/components"
)

func main() {
	config := components.Config{}
	flag.StringVar(&config.Component, "component", "all", "check, geo, core or all")
	flag.StringVar(&config.StateDirectory, "state-dir", "/var/lib/orcheroute", "operation and staging state")
	flag.StringVar(&config.ProductionState, "production-state", "/var/lib/orcheroute", "production data directory")
	flag.StringVar(&config.ConfigDirectory, "config-dir", "/etc/orcheroute", "configuration directory")
	flag.StringVar(&config.Mihomo, "mihomo", "/opt/orcheroute/bin/mihomo", "Mihomo binary")
	flag.StringVar(&config.CoreService, "core-service", "orcheroute-core.service", "Mihomo service")
	flag.StringVar(&config.ControllerService, "controller-service", "orcheroute-go.service", "controller service")
	flag.Parse()
	if err := components.Run(context.Background(), config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
