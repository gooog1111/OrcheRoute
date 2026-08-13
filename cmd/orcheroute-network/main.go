package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/gooog1111/orcheroute/internal/network"
)

type request struct {
	Operation string               `json:"operation"`
	Profile   network.ProfileInput `json:"profile"`
	Topology  network.Topology     `json:"topology"`
	DNS       *network.DNSInput    `json:"dns"`
}

func main() {
	var input request
	if json.NewDecoder(os.Stdin).Decode(&input) != nil {
		write(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
		return
	}
	var result any
	var err error
	switch input.Operation {
	case "preview":
		result, err = network.PreviewProfile(input.Profile, input.Topology)
	case "validate":
		result, err = network.ValidateProfile(input.Profile, nil)
	case "dns":
		var config network.DNSConfig
		config, err = network.ValidateDNS(input.DNS)
		if err == nil {
			result = network.PreviewDNS(config)
		}
	default:
		err = &network.ValidationError{Code: "unknown_operation"}
	}
	if err != nil {
		var validation *network.ValidationError
		if errors.As(err, &validation) {
			write(map[string]any{"ok": false, "error": validation})
		} else {
			write(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
		}
		return
	}
	write(map[string]any{"ok": true, "result": result})
}

func write(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
