package mobile_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Micro-layers may share lower-level internal packages, but must not grow
// hidden dependencies on sibling layers. Constructor is the sole exception:
// it consumes the immutable plan produced by routing.
func TestMicroLayerDependencyDirections(t *testing.T) {
	allowed := map[string]map[string]bool{
		"parser": {}, "validator": {}, "mapper": {}, "routing": {},
		"constructor": {"routing": true}, "transport": {}, "connectivity": {},
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
				const prefix = "github.com/gooog1111/orcheroute/internal/mobile/"
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
