package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gooog1111/orcheroute/internal/qualification"
)

type request struct {
	Operation string         `json:"operation"`
	Policy    map[string]any `json:"policy"`
	Changes   map[string]any `json:"changes"`
	Pool      string         `json:"pool"`
}

func main() {
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	var input request
	if decoder.Decode(&input) != nil {
		writeError("invalid_request")
		return
	}
	var result map[string]any
	var err error
	switch input.Operation {
	case "default":
		result = qualification.DefaultPolicy()
	case "validate":
		result, err = qualification.Validate(input.Policy)
	case "update":
		result, err = qualification.Update(input.Policy, input.Changes)
	case "effective":
		result, err = qualification.Effective(input.Policy, input.Pool)
	case "migrate_legacy":
		result = qualification.MigrateLegacyPools(input.Policy)
	default:
		writeError("unsupported_operation")
		return
	}
	if err != nil {
		writeError(err.Error())
		return
	}
	write(map[string]any{"ok": true, "result": result})
}

func writeError(code string) {
	write(map[string]any{"ok": false, "error": map[string]string{"error": code}})
}
func write(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
