package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gooog1111/orcheroute/internal/nodes"
)

type request struct {
	Links  []string `json:"links"`
	Source string   `json:"source"`
}

func main() {
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	var input request
	if err := decoder.Decode(&input); err != nil || input.Source == "" {
		write(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
		return
	}
	write(map[string]any{"ok": true, "result": nodes.ConvertLinks(input.Links, input.Source)})
}

func write(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
