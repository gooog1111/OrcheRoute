package controller

import (
	"sort"
	"time"

	"github.com/gooog1111/orcheroute/internal/core/noderank"
)

const (
	Primary   = "primary"
	Emergency = "emergency"
)

type Node struct {
	Name            string  `json:"name"`
	Pool            string  `json:"pool"`
	Alive           bool    `json:"alive"`
	Delay           int     `json:"delay_ms"`
	SpeedMbps       float64 `json:"speed_mbps,omitempty"`
	StabilityRatio  float64 `json:"stability_ratio,omitempty"`
	HealthSuccesses int     `json:"health_successes,omitempty"`
	HealthFailures  int     `json:"health_failures,omitempty"`
	LastTestedAt    int64   `json:"last_tested_at,omitempty"`
}

type Control struct {
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`
	ManualNode  string `json:"manual_node,omitempty"`
	ManualUntil int64  `json:"manual_until,omitempty"`
}

type Observation struct {
	Now        int64   `json:"now"`
	WAN        bool    `json:"wan_available"`
	ActiveOK   bool    `json:"active_ok"`
	Active     string  `json:"active"`
	ActivePool string  `json:"active_pool,omitempty"`
	Nodes      []Node  `json:"nodes"`
	Control    Control `json:"control"`
}

type State struct {
	Status              string           `json:"status"`
	Mode                string           `json:"mode"`
	Active              string           `json:"active"`
	ActivePool          string           `json:"active_pool,omitempty"`
	FailureStreak       int              `json:"failure_streak"`
	LastSwitch          int64            `json:"last_switch"`
	LastCycle           int64            `json:"last_cycle"`
	InternetDownSince   int64            `json:"internet_down_since"`
	WANAvailable        bool             `json:"wan_available"`
	RecoveryGraceUntil  int64            `json:"recovery_grace_until"`
	RecoveryStreak      int              `json:"recovery_streak"`
	RecoveryTarget      string           `json:"recovery_target"`
	RecoveryRequestedAt int64            `json:"recovery_requested_at"`
	NextSwitchAttempt   int64            `json:"next_switch_attempt"`
	NextRecoveryCheck   int64            `json:"next_recovery_check"`
	NextPoolRefresh     int64            `json:"next_pool_refresh"`
	FailedNodes         int              `json:"failed_nodes"`
	FailureWindowStart  int64            `json:"failure_window_start"`
	QualificationSince  int64            `json:"qualification_since"`
	Cooldowns           map[string]int64 `json:"cooldowns"`
}

type Decision struct {
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Pool   string `json:"pool,omitempty"`
	Reason string `json:"reason"`
}

type Policy struct {
	FailureLimit         int
	RetryAfter           time.Duration
	Cooldown             time.Duration
	RecoveryInterval     time.Duration
	RecoveryStableChecks int
	RecoveryGrace        time.Duration
	PoolRefreshCooldown  time.Duration
	PoolFailureLimit     int
	FailureWindow        time.Duration
	QualificationRetry   time.Duration
	SuspendOnExhaustion  bool
}

func DefaultPolicy() Policy {
	return Policy{FailureLimit: 2, RetryAfter: 30 * time.Second, Cooldown: 15 * time.Minute,
		RecoveryInterval: 20 * time.Second, RecoveryStableChecks: 2,
		RecoveryGrace: 10 * time.Second, PoolRefreshCooldown: 15 * time.Minute,
		FailureWindow: 5 * time.Minute, QualificationRetry: 5 * time.Minute}
}

// ManagedPolicy is used by adapters that can stop and restart their transport
// independently while qualification continues in the background.
func ManagedPolicy() Policy {
	policy := DefaultPolicy()
	policy.PoolFailureLimit = 3
	policy.SuspendOnExhaustion = true
	return policy
}

func Step(previous State, observation Observation, policy Policy) (State, Decision) {
	state := previous
	now := observation.Now
	if now == 0 {
		now = time.Now().Unix()
	}
	state.LastCycle = now
	if state.Cooldowns == nil {
		state.Cooldowns = map[string]int64{}
	}
	for name, until := range state.Cooldowns {
		if until <= now {
			delete(state.Cooldowns, name)
		}
	}
	mode := observation.Control.Mode
	if mode != "manual" && mode != "emergency" {
		mode = "auto"
	}
	if mode == "manual" && observation.Control.ManualUntil > 0 && now >= observation.Control.ManualUntil {
		mode = "auto"
	}
	state.Mode = mode

	if !observation.WAN {
		state.Status = "internet_down"
		state.WANAvailable = false
		state.FailureStreak = 0
		if state.InternetDownSince == 0 {
			state.InternetDownSince = now
		}
		return state, Decision{Action: "freeze", Reason: "direct_egress_unreachable"}
	}
	state.WANAvailable = true
	if state.Status == "internet_down" {
		state.Status, state.InternetDownSince, state.FailureStreak = "recovery_grace", 0, 0
		state.RecoveryGraceUntil = now + int64(policy.RecoveryGrace.Seconds())
		return state, Decision{Action: "keep", Reason: "internet_restored"}
	}
	if now < state.RecoveryGraceUntil {
		return state, Decision{Action: "keep", Reason: "recovery_grace"}
	}

	if !observation.Control.Enabled {
		state.Status, state.Active, state.ActivePool, state.FailureStreak = "disabled", observation.Active, "", 0
		state.FailedNodes, state.FailureWindowStart, state.QualificationSince = 0, 0, 0
		if observation.Active != "DIRECT-EGRESS" {
			return state, Decision{Action: "select", Target: "DIRECT-EGRESS", Reason: "service_disabled"}
		}
		return state, Decision{Action: "keep", Reason: "service_disabled"}
	}
	if state.QualificationSince > 0 {
		fresh := candidatesTestedSince(observation.Nodes, mode, state.QualificationSince)
		if len(fresh) > 0 {
			state.Status = "qualified_servers_ready"
			state.FailedNodes, state.FailureWindowStart, state.QualificationSince = 0, 0, 0
			state.Cooldowns = map[string]int64{}
			return state, Decision{Action: "resume", Target: fresh[0].Name, Pool: fresh[0].Pool, Reason: "qualified_servers_available"}
		}
		state.Status = "servers_unavailable"
		if now >= state.NextPoolRefresh {
			state.QualificationSince = now
			state.NextPoolRefresh = now + int64(policy.QualificationRetry.Seconds())
			return state, Decision{Action: "requalify", Pool: qualificationPool(mode), Reason: "retry_server_qualification"}
		}
		return state, Decision{Action: "suspend", Reason: "awaiting_qualified_servers"}
	}

	if mode == "manual" {
		target, ok := nodeByName(observation.Nodes, observation.Control.ManualNode)
		if !ok || !target.Alive {
			state.Status = "manual_target_unavailable"
			state.FailureStreak++
			return state, Decision{Action: "keep", Reason: "manual_target_unavailable"}
		}
		state.Active, state.ActivePool = observation.Active, target.Pool
		if observation.Active != target.Name {
			return state, Decision{Action: "select", Target: target.Name, Pool: target.Pool, Reason: "manual_control"}
		}
		state.FailureStreak = 0
		state.Status = "manual_proxy_ok"
		if !observation.ActiveOK {
			state.Status = "manual_proxy_degraded"
			state.FailureStreak = previous.FailureStreak + 1
		}
		return state, Decision{Action: "keep", Reason: state.Status}
	}
	if mode == "emergency" {
		return stepEmergency(state, observation, policy, now)
	}

	currentPool := observation.ActivePool
	if currentPool == "" {
		if node, ok := nodeByName(observation.Nodes, observation.Active); ok {
			currentPool = node.Pool
		}
	}
	state.Active, state.ActivePool = observation.Active, currentPool
	if observation.Active != "" && observation.Active != "DIRECT-EGRESS" && observation.ActiveOK {
		state.Status, state.FailureStreak = "proxy_ok", 0
		if state.FailedNodes > 0 && state.FailureWindowStart > 0 && now-state.FailureWindowStart >= int64(policy.FailureWindow.Seconds()) {
			state.FailedNodes, state.FailureWindowStart = 0, 0
		}
		if currentPool == Emergency && now >= state.NextRecoveryCheck {
			state.NextRecoveryCheck = now + int64(policy.RecoveryInterval.Seconds())
			better := candidates(observation.Nodes, Primary, state.Cooldowns, "")
			fresh := make([]Node, 0, len(better))
			for _, node := range better {
				if state.RecoveryRequestedAt > 0 && node.LastTestedAt >= state.RecoveryRequestedAt {
					fresh = append(fresh, node)
				}
			}
			if len(fresh) > 0 {
				target := fresh[0]
				if state.RecoveryTarget == target.Name {
					state.RecoveryStreak++
				} else {
					state.RecoveryTarget, state.RecoveryStreak = target.Name, 1
				}
				if state.RecoveryStreak >= policy.RecoveryStableChecks {
					state.RecoveryStreak, state.RecoveryTarget, state.RecoveryRequestedAt = 0, "", 0
					return state, Decision{Action: "select", Target: target.Name, Pool: target.Pool, Reason: "higher_priority_stable"}
				}
				return state, Decision{Action: "keep", Target: target.Name, Pool: target.Pool, Reason: "failback_probe"}
			}
			if state.RecoveryRequestedAt == 0 || now >= state.NextPoolRefresh {
				state.RecoveryRequestedAt = now
				state.NextPoolRefresh = now + int64(policy.PoolRefreshCooldown.Seconds())
				state.RecoveryStreak, state.RecoveryTarget = 0, ""
				return state, Decision{Action: "refresh", Pool: Primary, Reason: "refresh_primary_before_failback"}
			}
			if len(better) > 0 {
				return state, Decision{Action: "keep", Pool: Primary, Reason: "awaiting_fresh_primary"}
			}
		}
		return state, Decision{Action: "keep", Reason: "active_healthy"}
	}

	state.Status = "proxy_degraded"
	state.FailureStreak++
	if observation.Active == "" || observation.Active == "DIRECT-EGRESS" {
		state.FailureStreak = policy.FailureLimit
	}
	if state.FailureStreak < policy.FailureLimit || now < state.NextSwitchAttempt {
		return state, Decision{Action: "keep", Reason: "failure_threshold_pending"}
	}
	if observation.Active != "" {
		state.Cooldowns[observation.Active] = now + int64(policy.Cooldown.Seconds())
		state.FailedNodes++
		if state.FailureWindowStart == 0 {
			state.FailureWindowStart = now
		}
		if policy.PoolFailureLimit > 0 && state.FailedNodes >= policy.PoolFailureLimit {
			return beginRequalification(state, observation, policy, now, "failure_batch_requalification", "")
		}
	}
	all := candidates(observation.Nodes, "", state.Cooldowns, observation.Active)
	if len(all) > 0 {
		target := all[0]
		return state, Decision{Action: "select", Target: target.Name, Pool: target.Pool, Reason: "active_failed"}
	}
	state.NextSwitchAttempt = now + int64(policy.RetryAfter.Seconds())
	if now >= state.NextPoolRefresh {
		if policy.SuspendOnExhaustion {
			return beginRequalification(state, observation, policy, now, "no_working_candidate", "")
		}
		state.NextPoolRefresh = now + int64(policy.PoolRefreshCooldown.Seconds())
		return state, Decision{Action: "refresh", Reason: "no_working_candidate"}
	}
	return state, Decision{Action: "keep", Reason: "no_working_candidate"}
}

func stepEmergency(state State, observation Observation, policy Policy, now int64) (State, Decision) {
	currentPool := observation.ActivePool
	if currentPool == "" {
		if node, ok := nodeByName(observation.Nodes, observation.Active); ok {
			currentPool = node.Pool
		}
	}
	state.Active, state.ActivePool = observation.Active, currentPool
	if currentPool == Emergency && observation.ActiveOK {
		state.Status, state.FailureStreak = "emergency_proxy_ok", 0
		if state.FailedNodes > 0 && state.FailureWindowStart > 0 && now-state.FailureWindowStart >= int64(policy.FailureWindow.Seconds()) {
			state.FailedNodes, state.FailureWindowStart = 0, 0
		}
		return state, Decision{Action: "keep", Reason: "emergency_only_active_healthy"}
	}
	state.Status = "emergency_proxy_degraded"
	state.FailureStreak++
	if currentPool != Emergency || observation.Active == "" || observation.Active == "DIRECT-EGRESS" {
		state.FailureStreak = policy.FailureLimit
	}
	if state.FailureStreak < policy.FailureLimit || now < state.NextSwitchAttempt {
		return state, Decision{Action: "keep", Reason: "emergency_only_threshold_pending"}
	}
	if currentPool == Emergency && observation.Active != "" {
		state.Cooldowns[observation.Active] = now + int64(policy.Cooldown.Seconds())
		state.FailedNodes++
		if state.FailureWindowStart == 0 {
			state.FailureWindowStart = now
		}
		if policy.PoolFailureLimit > 0 && state.FailedNodes >= policy.PoolFailureLimit {
			return beginRequalification(state, observation, policy, now, "emergency_failure_batch_requalification", Emergency)
		}
	}
	available := candidates(observation.Nodes, Emergency, state.Cooldowns, observation.Active)
	if len(available) > 0 {
		target := available[0]
		return state, Decision{Action: "select", Target: target.Name, Pool: target.Pool, Reason: "emergency_only_select"}
	}
	state.NextSwitchAttempt = now + int64(policy.RetryAfter.Seconds())
	if now >= state.NextPoolRefresh {
		if policy.SuspendOnExhaustion {
			return beginRequalification(state, observation, policy, now, "emergency_only_no_working_candidate", Emergency)
		}
		state.NextPoolRefresh = now + int64(policy.PoolRefreshCooldown.Seconds())
		return state, Decision{Action: "refresh", Pool: Emergency, Reason: "emergency_only_no_working_candidate"}
	}
	return state, Decision{Action: "keep", Reason: "emergency_only_no_working_candidate"}
}

func beginRequalification(state State, observation Observation, policy Policy, now int64, reason, pool string) (State, Decision) {
	for _, node := range observation.Nodes {
		if pool == "" || node.Pool == pool {
			state.Cooldowns[node.Name] = now + int64(policy.Cooldown.Seconds())
		}
	}
	state.Status = "servers_unavailable"
	state.QualificationSince = now
	state.NextPoolRefresh = now + int64(policy.QualificationRetry.Seconds())
	return state, Decision{Action: "requalify", Pool: pool, Reason: reason}
}

func qualificationPool(mode string) string {
	if mode == "emergency" {
		return Emergency
	}
	return ""
}

func candidatesTestedSince(nodes []Node, mode string, since int64) []Node {
	pool := qualificationPool(mode)
	ranked := candidates(nodes, pool, map[string]int64{}, "")
	result := make([]Node, 0, len(ranked))
	for _, node := range ranked {
		if node.LastTestedAt >= since {
			result = append(result, node)
		}
	}
	return result
}

func nodeByName(nodes []Node, name string) (Node, bool) {
	for _, node := range nodes {
		if node.Name == name {
			return node, true
		}
	}
	return Node{}, false
}

func candidates(nodes []Node, pool string, cooldowns map[string]int64, exclude string) []Node {
	result := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if !node.Alive || node.Name == exclude || (pool != "" && node.Pool != pool) {
			continue
		}
		if _, blocked := cooldowns[node.Name]; blocked {
			continue
		}
		result = append(result, node)
	}
	sort.SliceStable(result, func(i, j int) bool {
		pi, pj := poolPriority(result[i].Pool), poolPriority(result[j].Pool)
		if pi != pj {
			return pi < pj
		}
		si := noderank.Score(noderank.Node{ID: result[i].Name, Pool: result[i].Pool, Alive: result[i].Alive, DelayMS: result[i].Delay, SpeedMbps: result[i].SpeedMbps, StabilityRatio: result[i].StabilityRatio, HealthSuccesses: result[i].HealthSuccesses, HealthFailures: result[i].HealthFailures})
		sj := noderank.Score(noderank.Node{ID: result[j].Name, Pool: result[j].Pool, Alive: result[j].Alive, DelayMS: result[j].Delay, SpeedMbps: result[j].SpeedMbps, StabilityRatio: result[j].StabilityRatio, HealthSuccesses: result[j].HealthSuccesses, HealthFailures: result[j].HealthFailures})
		if si != sj {
			return si > sj
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func poolPriority(pool string) int {
	if pool == Primary {
		return 0
	}
	if pool == Emergency {
		return 1
	}
	return 2
}
