package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gooog1111/orcheroute/internal/mihomo"
)

func main() {
	decoder := json.NewDecoder(os.Stdin)
	var input mihomo.Input
	if err := decoder.Decode(&input); err != nil {
		write(map[string]any{"ok": false, "error": map[string]string{"error": "invalid_request"}})
		return
	}
	config, err := mihomo.Build(input)
	if err != nil {
		write(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
		return
	}
	write(map[string]any{"ok": true, "result": config})
}

func write(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
