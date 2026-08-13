package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/gooog1111/orcheroute/internal/serverstate"
)

func main() {
	path := flag.String("db", "state.db", "path to state.db")
	includeSecrets := flag.Bool("include-secrets", false, "include subscription secrets in output")
	flag.Parse()
	store, err := serverstate.Open(*path)
	if err != nil {
		writeError(err)
		return
	}
	defer store.Close()
	ctx := context.Background()
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		writeError(err)
		return
	}
	control, err := store.Control(ctx)
	if err != nil {
		writeError(err)
		return
	}
	subscriptions, err := store.List(ctx, *includeSecrets)
	if err != nil {
		writeError(err)
		return
	}
	events, err := store.Events(ctx, 100)
	if err != nil {
		writeError(err)
		return
	}
	if err := store.Close(); err != nil {
		writeError(err)
		return
	}
	write(map[string]any{"ok": true, "result": map[string]any{"snapshot": snapshot, "control": control, "subscriptions": subscriptions, "events": events}})
}

func writeError(err error) {
	write(map[string]any{"ok": false, "error": map[string]string{"error": err.Error()}})
}
func write(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
