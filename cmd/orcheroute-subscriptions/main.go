package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
)

type request struct {
	Operation      string                       `json:"operation"`
	Payload        map[string]any               `json:"payload"`
	Partial        bool                         `json:"partial"`
	Body           string                       `json:"body"`
	BodyBase64     string                       `json:"body_base64"`
	Sources        []subscriptions.SourceLinks  `json:"sources"`
	Existing       []subscriptions.Subscription `json:"existing"`
	Environment    map[string]string            `json:"environment"`
	DefaultEnabled *bool                        `json:"default_enabled"`
	LastSuccess    int64                        `json:"last_success"`
	Interval       int                          `json:"interval_seconds"`
	Force          bool                         `json:"force"`
	CachedLinks    []string                     `json:"cached_links"`
	Now            int64                        `json:"now"`
}

func main() {
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	var input request
	if decoder.Decode(&input) != nil {
		writeError("invalid_request")
		return
	}
	defaultEnabled := true
	if input.DefaultEnabled != nil {
		defaultEnabled = *input.DefaultEnabled
	}
	switch input.Operation {
	case "validate":
		result, err := subscriptions.ValidateFields(input.Payload, input.Partial)
		if err != nil {
			writeError(err.Error())
			return
		}
		writeResult(result)
	case "decode":
		body := []byte(input.Body)
		if input.BodyBase64 != "" {
			var err error
			body, err = base64.StdEncoding.DecodeString(input.BodyBase64)
			if err != nil {
				writeError("invalid_body_base64")
				return
			}
		}
		writeResult(subscriptions.Decode(body))
	case "aggregate":
		writeResult(subscriptions.Aggregate(input.Sources))
	case "defaults":
		writeResult(subscriptions.MissingDefaults(input.Existing, defaultEnabled))
	case "migrate_legacy":
		writeResult(subscriptions.LegacyMigrationPlan(input.Existing, input.Environment, defaultEnabled))
	case "refresh_due":
		now := time.Unix(input.Now, 0)
		if input.Now == 0 {
			now = time.Now()
		}
		writeResult(map[string]bool{"due": subscriptions.RefreshDue(now, input.LastSuccess, input.Interval, input.Force, input.CachedLinks)})
	default:
		writeError("unsupported_operation")
	}
}

func writeResult(value any) { write(map[string]any{"ok": true, "result": value}) }
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
