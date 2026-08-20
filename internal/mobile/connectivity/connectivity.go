// Package connectivity classifies the physical network independently from the
// VPN lifecycle. Platform adapters perform the actual HTTP probes (and bind or
// protect their sockets); this package owns targets, policy and the three
// portable states returned to the controller.
package connectivity

import (
	"context"
	"errors"
	"strings"
	"sync"
)

type State string

const (
	Normal    State = "normal"
	Allowlist State = "allowlist"
	Offline   State = "offline"
)

type Config struct {
	AllowlistURL    string
	OpenInternetURL string
}

type Target struct {
	Name            string
	URL             string
	OpenInternet    bool
	ExpectNoContent bool
}

type Observation struct {
	AllowlistAvailable         bool `json:"allowlist_available"`
	ConfiguredOpenAvailable    bool `json:"configured_open_available"`
	OpenAnchorGitHubAvailable  bool `json:"open_anchor_github_available"`
	OpenAnchorMozillaAvailable bool `json:"open_anchor_mozilla_available"`
}

type Result struct {
	State                      State `json:"state"`
	AllowlistAvailable         bool  `json:"allowlist_available"`
	OpenInternetAvailable      bool  `json:"open_internet_available"`
	ConfiguredOpenAvailable    bool  `json:"configured_open_available"`
	OpenAnchorAvailable        bool  `json:"open_anchor_available"`
	OpenAnchorGitHubAvailable  bool  `json:"open_anchor_github_available"`
	OpenAnchorMozillaAvailable bool  `json:"open_anchor_mozilla_available"`
}

type Probe func(context.Context, Target) bool

func Targets(config Config) ([]Target, error) {
	allowlistURL := strings.TrimSpace(config.AllowlistURL)
	openURL := normalizeOpenURL(config.OpenInternetURL)
	if allowlistURL == "" || openURL == "" {
		return nil, errors.New("connectivity_probe_url_empty")
	}
	return []Target{
		{Name: "allowlist", URL: allowlistURL},
		{Name: "open_internet", URL: openURL, OpenInternet: true, ExpectNoContent: isGenerate204(openURL)},
		{Name: "open_anchor_github", URL: "https://api.github.com/zen", OpenInternet: true},
		{Name: "open_anchor_mozilla", URL: "https://www.mozilla.org/robots.txt", OpenInternet: true},
	}, nil
}

// Diagnose runs independent targets concurrently. The supplied probe decides
// how sockets leave the device, so Android can bind them to a non-VPN Network
// or protect them through VpnService without changing classification policy.
func Diagnose(ctx context.Context, config Config, probe Probe) (Result, error) {
	if probe == nil {
		return Result{}, errors.New("connectivity_probe_missing")
	}
	targets, err := Targets(config)
	if err != nil {
		return Result{}, err
	}
	type outcome struct {
		name string
		ok   bool
	}
	results := make(chan outcome, len(targets))
	var workers sync.WaitGroup
	workers.Add(len(targets))
	for _, target := range targets {
		target := target
		go func() {
			defer workers.Done()
			results <- outcome{name: target.Name, ok: probe(ctx, target)}
		}()
	}
	workers.Wait()
	close(results)
	available := map[string]bool{}
	for item := range results {
		available[item.name] = item.ok
	}
	return Classify(Observation{
		AllowlistAvailable:         available["allowlist"],
		ConfiguredOpenAvailable:    available["open_internet"],
		OpenAnchorGitHubAvailable:  available["open_anchor_github"],
		OpenAnchorMozillaAvailable: available["open_anchor_mozilla"],
	}), nil
}

func Classify(observation Observation) Result {
	anchors := observation.OpenAnchorGitHubAvailable && observation.OpenAnchorMozillaAvailable
	open := observation.ConfiguredOpenAvailable && anchors
	anyReachable := observation.AllowlistAvailable || observation.ConfiguredOpenAvailable ||
		observation.OpenAnchorGitHubAvailable || observation.OpenAnchorMozillaAvailable
	state := Offline
	if open {
		state = Normal
	} else if anyReachable {
		state = Allowlist
	}
	return Result{
		State:                      state,
		AllowlistAvailable:         observation.AllowlistAvailable,
		OpenInternetAvailable:      open,
		ConfiguredOpenAvailable:    observation.ConfiguredOpenAvailable,
		OpenAnchorAvailable:        anchors,
		OpenAnchorGitHubAvailable:  observation.OpenAnchorGitHubAvailable,
		OpenAnchorMozillaAvailable: observation.OpenAnchorMozillaAvailable,
	}
}

func normalizeOpenURL(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.Contains(lower, "gstatic.com/generate_204") ||
		strings.Contains(lower, "connectivitycheck.gstatic.com") ||
		strings.Contains(lower, "clients3.google.com/generate_204") {
		return "https://www.cloudflare.com/cdn-cgi/trace"
	}
	return value
}

func isGenerate204(value string) bool {
	return strings.Contains(strings.ToLower(value), "generate_204")
}
