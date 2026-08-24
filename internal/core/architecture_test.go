package core_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Micro-layers may share lower-level domain packages, but must not grow hidden
// sibling dependencies. Explicit edges form the small shared pipeline.
func TestMicroLayerDependencyDirections(t *testing.T) {
	allowed := map[string]map[string]bool{
		"parser": {}, "validator": {"qualification": true}, "mapper": {}, "routing": {},
		"constructor": {"routing": true}, "transport": {}, "connectivity": {},
		"qualification": {}, "noderank": {}, "whitelist": {"noderank": true}, "orchestrator": {"whitelist": true},
	}
	for layer, layerAllowed := range allowed {
		files, err := filepath.Glob(filepath.Join(layer, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imported := range parsed.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				const prefix = "github.com/gooog1111/orcheroute/internal/core/"
				if !strings.HasPrefix(path, prefix) {
					continue
				}
				dependency := strings.TrimPrefix(path, prefix)
				if !layerAllowed[dependency] {
					position := parsed.Pos()
					if imported.Pos().IsValid() {
						position = imported.Pos()
					}
					t.Errorf("%s: layer %s must not import sibling %s at %v", file, layer, dependency, position)
				}
			}
		}
	}
}

// Linux Server and Android must enter shared behavior through the same layer
// facades. This prevents a later platform fix from bypassing validation or
// mapping that the other platform still relies on.
func TestSharedConsumersDoNotBypassLayerEntrypoints(t *testing.T) {
	forbidden := []string{
		"subscriptions.Decode(", "subscriptions.NormalizeInline(", "subscriptions.ValidateFields(",
		"subscriptions.Aggregate(", "subscriptions.RetainSources(", "routes.CompileLists(",
		"qualification.Validate(", "qualification.Effective(", "qualification.Update(",
		"qualification.DefaultPolicy(", "qualification.MigrateLegacyPools(",
		"network.PreviewProfile(", "network.ValidateDNS(", "network.PreviewDNS(",
	}
	for _, pattern := range []string{"../serverruntime/*.go", "../updater/*.go"} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, call := range forbidden {
				if strings.Contains(string(content), call) {
					t.Errorf("%s bypasses a core layer with %s", file, call)
				}
			}
		}
	}
}
