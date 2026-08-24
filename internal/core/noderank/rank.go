// Package noderank provides a platform-neutral server rating.
package noderank

import (
	"math"
	"sort"
)

type Node struct {
	ID               string  `json:"id"`
	SourceID         string  `json:"source_id,omitempty"`
	Pool             string  `json:"pool,omitempty"`
	Alive            bool    `json:"alive"`
	DelayMS          int     `json:"delay_ms,omitempty"`
	SpeedMbps        float64 `json:"speed_mbps,omitempty"`
	StabilityRatio   float64 `json:"stability_ratio,omitempty"`
	HealthSuccesses  int     `json:"health_successes,omitempty"`
	HealthFailures   int     `json:"health_failures,omitempty"`
	Score            float64 `json:"score"`
	OriginalPosition int     `json:"-"`
}

// Score combines current latency and throughput evidence with historical
// connection health. Missing speed evidence is neutral rather than zero, so a
// node tested while a speed-test endpoint was unavailable is not discarded.
func Score(node Node) float64 {
	latency := 0.5
	if node.DelayMS > 0 {
		latency = 1 / (1 + float64(node.DelayMS)/150)
	}
	speed := 0.5
	if node.SpeedMbps > 0 {
		speed = math.Min(math.Log1p(node.SpeedMbps)/math.Log1p(100), 1)
	}
	healthStability := float64(node.HealthSuccesses+2) / float64(node.HealthSuccesses+node.HealthFailures+4)
	stability := healthStability
	if node.StabilityRatio > 0 && node.StabilityRatio <= 1 {
		stability = (node.StabilityRatio + healthStability) / 2
	}
	value := latency*400 + speed*350 + stability*250
	return math.Round(value*100) / 100
}

func Rank(nodes []Node) []Node {
	result := append([]Node(nil), nodes...)
	for index := range result {
		result[index].OriginalPosition = index
		result[index].Score = Score(result[index])
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Alive != result[j].Alive {
			return result[i].Alive
		}
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].OriginalPosition < result[j].OriginalPosition
	})
	return result
}

// Select returns the highest-ranked node allowed by the operating mode. Auto
// prefers primary and only then falls back to emergency. Emergency is strict:
// it can never return a primary node.
func Select(nodes []Node, mode string) *Node {
	ranked := Rank(nodes)
	if mode == "emergency" {
		for index := range ranked {
			if ranked[index].Alive && ranked[index].Pool == "emergency" {
				result := ranked[index]
				return &result
			}
		}
		return nil
	}
	for _, pool := range []string{"primary", "emergency"} {
		for index := range ranked {
			if ranked[index].Alive && ranked[index].Pool == pool {
				result := ranked[index]
				return &result
			}
		}
	}
	return nil
}
