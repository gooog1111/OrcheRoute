// Package connectivity classifies the physical network independently from the
// VPN lifecycle. Platform adapters perform the actual HTTP probes (and bind or
// protect their sockets); this package owns targets, policy and the three
// portable states returned to the controller.
package connectivity

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"

	"golang.org/x/text/language"
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

// ConfirmationInput is the state carried by a platform monitor between raw
// probe observations. Keeping the thresholds here prevents Android and future
// clients from interpreting a single timeout as a network-mode transition.
type ConfirmationInput struct {
	ConfirmedState State `json:"confirmed_state"`
	CandidateState State `json:"candidate_state"`
	CandidateCount int   `json:"candidate_count"`
	ObservedState  State `json:"observed_state"`
}

type ConfirmationResult struct {
	State          State `json:"state"`
	CandidateState State `json:"candidate_state,omitempty"`
	CandidateCount int   `json:"candidate_count"`
	Changed        bool  `json:"changed"`
}

type Identity struct {
	IP          string `json:"ip"`
	CountryCode string `json:"country_code,omitempty"`
	Region      string `json:"region,omitempty"`
	Flag        string `json:"flag,omitempty"`
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
	anchors := observation.OpenAnchorGitHubAvailable || observation.OpenAnchorMozillaAvailable
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

// Confirm applies hysteresis to a raw observation. A working network is not
// declared offline until three consecutive observations agree. Switching
// between normal and allowlist requires two observations, while recovery from
// confirmed offline is immediate.
func Confirm(input ConfirmationInput) (ConfirmationResult, error) {
	if !validObserved(input.ObservedState) {
		return ConfirmationResult{}, errors.New("invalid_connectivity_observation")
	}
	if input.ConfirmedState != "" && input.ConfirmedState != "unknown" && !validObserved(input.ConfirmedState) {
		return ConfirmationResult{}, errors.New("invalid_confirmed_connectivity_state")
	}
	confirmed := input.ConfirmedState
	if confirmed == "" {
		confirmed = "unknown"
	}
	if input.ObservedState == confirmed {
		return ConfirmationResult{State: confirmed}, nil
	}
	candidate, count := input.ObservedState, 1
	if input.CandidateState == input.ObservedState && input.CandidateCount > 0 {
		count = input.CandidateCount + 1
	}
	required := 2
	if confirmed == Offline {
		required = 1
	} else if input.ObservedState == Offline && confirmed != "unknown" {
		required = 3
	} else if confirmed == "unknown" && input.ObservedState != Offline {
		required = 1
	}
	if count >= required {
		return ConfirmationResult{State: input.ObservedState, Changed: input.ObservedState != confirmed}, nil
	}
	return ConfirmationResult{State: confirmed, CandidateState: candidate, CandidateCount: count}, nil
}

func validObserved(state State) bool {
	return state == Normal || state == Allowlist || state == Offline
}

// ParseTraceIdentity parses Cloudflare's plain-text trace response. Network
// I/O remains platform-owned so Direct can be bound to a physical interface
// while Proxy follows the active TUN.
func ParseTraceIdentity(body string) (Identity, error) {
	values := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	ip := values["ip"]
	if net.ParseIP(ip) == nil {
		return Identity{}, errors.New("connection_identity_ip_missing")
	}
	result := Identity{IP: ip}
	code := strings.ToUpper(values["loc"])
	if len(code) != 2 {
		return result, nil
	}
	region, err := language.ParseRegion(code)
	if err != nil || !region.IsCountry() {
		return result, nil
	}
	result.CountryCode = code
	result.Region = region.ISO3()
	if result.Region == "ZZZ" {
		result.Region = code
	}
	result.Flag = countryFlag(code)
	return result, nil
}

func countryFlag(code string) string {
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return string([]rune{rune(0x1F1E6) + rune(code[0]-'A'), rune(0x1F1E6) + rune(code[1]-'A')})
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
