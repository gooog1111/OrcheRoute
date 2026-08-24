// Package routing compiles the user-facing direct/proxy/block model into an
// ordered, platform-neutral routing plan. It never constructs or starts Mihomo.
package routing

import (
	"fmt"

	baseroutes "github.com/gooog1111/orcheroute/internal/routes"
)

type Input struct {
	Default string              `json:"default"`
	Lists   map[string][]string `json:"lists"`
}

type Plan struct {
	Default  string                   `json:"default"`
	Rules    []string                 `json:"rules"`
	Compiled baseroutes.CompileResult `json:"compiled"`
}

func DefaultInput() Input {
	return Input{Default: "proxy", Lists: map[string][]string{"direct": {}, "proxy": {}, "block": {}}}
}

func Compile(input Input) (Plan, error) {
	actions := map[string]string{"proxy": "ACTIVE", "direct": "DIRECT", "block": "REJECT"}
	defaultTarget := actions[input.Default]
	if defaultTarget == "" {
		return Plan{}, fmt.Errorf("invalid_route_default")
	}
	compiled, err := baseroutes.CompileLists(input.Lists)
	if err != nil {
		return Plan{}, err
	}
	rules := []string{}
	for _, listName := range []string{"block", "direct", "proxy"} {
		for _, rule := range compiled.Compiled[listName] {
			rules = append(rules, rule+","+actions[listName])
		}
	}
	rules = append(rules, "MATCH,"+defaultTarget)
	return Plan{Default: input.Default, Rules: rules, Compiled: compiled}, nil
}

func CompileLists(lists map[string][]string) (baseroutes.CompileResult, error) {
	return baseroutes.CompileLists(lists)
}
