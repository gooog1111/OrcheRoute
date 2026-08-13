package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/gooog1111/orcheroute/internal/routes"
)

type request struct {
	Operation string         `json:"operation"`
	Entry     string         `json:"entry"`
	Lists     map[string]any `json:"lists"`
}

type response struct {
	OK     bool                    `json:"ok"`
	Result any                     `json:"result,omitempty"`
	Error  *routes.ValidationError `json:"error,omitempty"`
}

func main() {
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	var input request
	if err := decoder.Decode(&input); err != nil {
		write(response{OK: false, Error: &routes.ValidationError{Code: "invalid_request"}})
		return
	}
	var result any
	var err error
	switch input.Operation {
	case "parse":
		result, err = routes.ParseEntry(input.Entry)
	case "compile":
		var normalized map[string][]string
		normalized, err = normalizeLists(input.Lists)
		if err == nil {
			result, err = routes.CompileLists(normalized)
		}
	default:
		err = &routes.ValidationError{Code: "unknown_operation"}
	}
	if err != nil {
		var validation *routes.ValidationError
		if !errors.As(err, &validation) {
			validation = &routes.ValidationError{Code: err.Error()}
		}
		write(response{OK: false, Error: validation})
		return
	}
	write(response{OK: true, Result: result})
}

func normalizeLists(input map[string]any) (map[string][]string, error) {
	result := make(map[string][]string, len(input))
	for name, raw := range input {
		var entries []any
		switch value := raw.(type) {
		case string:
			for _, line := range strings.Split(value, "\n") {
				entries = append(entries, line)
			}
		case []any:
			entries = value
		default:
			return nil, &routes.ValidationError{Code: "invalid_route_list", List: name}
		}
		if len(entries) > routes.MaxEntriesPerList {
			return nil, &routes.ValidationError{Code: "route_list_too_large", List: name}
		}
		result[name] = []string{}
		for index, rawEntry := range entries {
			var entry string
			switch value := rawEntry.(type) {
			case string:
				entry = strings.TrimSpace(value)
			case float64:
				if value != math.Trunc(value) {
					position := index
					return nil, &routes.ValidationError{Code: "invalid_route_entry", List: name, Index: &position, Entry: fmt.Sprint(value)}
				}
				entry = strconv.FormatInt(int64(value), 10)
			default:
				position := index
				return nil, &routes.ValidationError{Code: "invalid_route_entry", List: name, Index: &position, Entry: fmt.Sprint(value)}
			}
			if entry == "" || strings.HasPrefix(entry, "#") {
				continue
			}
			if len([]rune(entry)) > 512 {
				position := index
				return nil, &routes.ValidationError{Code: "route_entry_too_long", List: name, Index: &position, Entry: string([]rune(entry)[:200])}
			}
			result[name] = append(result[name], entry)
		}
	}
	return result, nil
}

func write(value response) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
