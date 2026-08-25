// Package whitelist implements the portable derived pool used when only an
// operator allowlist is reachable. It contains no networking or UI code.
package whitelist

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/gooog1111/orcheroute/internal/core/noderank"
)

const Pool = "whitelist"

type Node struct {
	ID              string         `json:"id"`
	DisplayName     string         `json:"display_name"`
	Pool            string         `json:"pool"`
	OriginPool      string         `json:"origin_pool,omitempty"`
	Priority        int            `json:"priority"`
	Alive           bool           `json:"alive"`
	DelayMS         int            `json:"delay_ms,omitempty"`
	SpeedMbps       float64        `json:"speed_mbps,omitempty"`
	StabilityRatio  float64        `json:"stability_ratio,omitempty"`
	HealthSuccesses int            `json:"health_successes,omitempty"`
	HealthFailures  int            `json:"health_failures,omitempty"`
	Score           float64        `json:"score,omitempty"`
	SourceID        string         `json:"source_id"`
	SourceName      string         `json:"source_name,omitempty"`
	Proxy           map[string]any `json:"proxy"`
}

type State struct {
	Nodes        []Node `json:"nodes"`
	SelectedNode string `json:"selected_node,omitempty"`
	PendingNode  string `json:"pending_node,omitempty"`
	ScanActive   bool   `json:"scan_active,omitempty"`
	Generation   int64  `json:"generation,omitempty"`
}

type Command struct {
	Operation string `json:"operation"`
	SourceID  string `json:"source_id,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	Nodes     []Node `json:"nodes,omitempty"`
}

type Result struct {
	State     State `json:"state"`
	Candidate *Node `json:"candidate,omitempty"`
	Changed   bool  `json:"changed"`
}

func Transition(input State, command Command) (Result, error) {
	state := normalize(input)
	before, _ := json.Marshal(state)
	var candidate *Node
	operation := strings.ToLower(strings.TrimSpace(command.Operation))
	switch operation {
	case "begin":
		// Keep the last known-good restricted-network pool online while a new
		// generation is checked. Each source is replaced atomically after its
		// complete test; transient probe or process failures therefore cannot
		// erase every working node at once.
		state.ScanActive = true
		state.Generation++
	case "add_source", "replace_source":
		if command.SourceID == "" {
			return Result{}, errors.New("source_id_required")
		}
		if operation == "replace_source" {
			removeSource(&state, command.SourceID)
		}
		for _, node := range command.Nodes {
			if !node.Alive {
				continue
			}
			node.SourceID, node.Pool = command.SourceID, Pool
			if node.ID == "" {
				node.ID = StableID(command.SourceID, node.Proxy)
			}
			if !contains(state.Nodes, node.ID) {
				state.Nodes = append(state.Nodes, node)
			}
		}
		reindex(&state)
		validateSelection(&state)
	case "remove_source":
		if command.SourceID == "" {
			return Result{}, errors.New("source_id_required")
		}
		removeSource(&state, command.SourceID)
	case "remove_node":
		if command.NodeID == "" {
			return Result{}, errors.New("node_id_required")
		}
		removeNode(&state, command.NodeID)
	case "active":
		candidate = selectCandidate(&state, false)
	case "request":
		candidate = selectCandidate(&state, true)
	case "confirm":
		id := command.NodeID
		if id == "" {
			id = state.PendingNode
		}
		if id != "" && id == state.SelectedNode && (state.PendingNode == "" || id == state.PendingNode) {
			state.PendingNode = ""
			candidate = find(state.Nodes, id)
		}
	case "fail":
		id := command.NodeID
		if id == "" {
			id = state.PendingNode
		}
		if id == "" {
			id = state.SelectedNode
		}
		if id != "" {
			kept := state.Nodes[:0]
			for _, node := range state.Nodes {
				if node.ID != id {
					kept = append(kept, node)
				}
			}
			state.Nodes = kept
		}
		state.SelectedNode, state.PendingNode = "", ""
		reindex(&state)
		candidate = selectCandidate(&state, true)
	case "complete":
		state.ScanActive = false
	case "deactivate":
		state.SelectedNode, state.PendingNode, state.ScanActive = "", "", false
	case "clear":
		state.Nodes = nil
		state.SelectedNode, state.PendingNode, state.ScanActive = "", "", false
		state.Generation++
	default:
		return Result{}, errors.New("unknown_whitelist_operation")
	}
	after, _ := json.Marshal(state)
	return Result{State: state, Candidate: candidate, Changed: string(before) != string(after)}, nil
}

func StableID(sourceID string, proxy map[string]any) string {
	payload, _ := json.Marshal(proxy)
	digest := sha256.Sum256(append([]byte(sourceID+"\x00"), payload...))
	return Pool + ":" + sourceID + ":" + hex.EncodeToString(digest[:8])
}

func normalize(state State) State {
	if state.Nodes == nil {
		state.Nodes = []Node{}
	}
	seen := map[string]bool{}
	clean := make([]Node, 0, len(state.Nodes))
	for _, node := range state.Nodes {
		if node.ID == "" {
			node.ID = StableID(node.SourceID, node.Proxy)
		}
		if seen[node.ID] {
			continue
		}
		seen[node.ID] = true
		node.Pool = Pool
		clean = append(clean, node)
	}
	state.Nodes = clean
	reindex(&state)
	validateSelection(&state)
	return state
}

func selectCandidate(state *State, markPending bool) *Node {
	if state.PendingNode != "" {
		return find(state.Nodes, state.PendingNode)
	}
	selected := find(state.Nodes, state.SelectedNode)
	if selected == nil || !selected.Alive {
		state.SelectedNode = ""
		selected = nil
		for index := range state.Nodes {
			if state.Nodes[index].Alive {
				state.SelectedNode = state.Nodes[index].ID
				selected = &state.Nodes[index]
				break
			}
		}
	}
	if selected != nil && markPending {
		state.PendingNode = selected.ID
	}
	if selected == nil {
		return nil
	}
	copy := *selected
	return &copy
}

func validateSelection(state *State) {
	if find(state.Nodes, state.SelectedNode) == nil {
		state.SelectedNode = ""
	}
	if find(state.Nodes, state.PendingNode) == nil || (state.PendingNode != "" && state.PendingNode != state.SelectedNode) {
		state.PendingNode = ""
	}
}
func removeSource(state *State, sourceID string) {
	kept := state.Nodes[:0]
	for _, node := range state.Nodes {
		if node.SourceID != sourceID {
			kept = append(kept, node)
		}
	}
	state.Nodes = kept
	reindex(state)
	validateSelection(state)
}
func removeNode(state *State, nodeID string) {
	kept := state.Nodes[:0]
	for _, node := range state.Nodes {
		if node.ID != nodeID {
			kept = append(kept, node)
		}
	}
	state.Nodes = kept
	reindex(state)
	validateSelection(state)
}
func reindex(state *State) {
	for index := range state.Nodes {
		state.Nodes[index].Score = noderank.Score(noderank.Node{
			ID: state.Nodes[index].ID, Pool: Pool, Alive: state.Nodes[index].Alive,
			DelayMS: state.Nodes[index].DelayMS, SpeedMbps: state.Nodes[index].SpeedMbps,
			StabilityRatio:  state.Nodes[index].StabilityRatio,
			HealthSuccesses: state.Nodes[index].HealthSuccesses, HealthFailures: state.Nodes[index].HealthFailures,
		})
	}
	sort.SliceStable(state.Nodes, func(i, j int) bool {
		if state.Nodes[i].Score == state.Nodes[j].Score {
			return state.Nodes[i].ID < state.Nodes[j].ID
		}
		return state.Nodes[i].Score > state.Nodes[j].Score
	})
	for index := range state.Nodes {
		state.Nodes[index].Priority = index + 1
	}
}
func contains(nodes []Node, id string) bool { return find(nodes, id) != nil }
func find(nodes []Node, id string) *Node {
	if id == "" {
		return nil
	}
	for index := range nodes {
		if nodes[index].ID == id {
			copy := nodes[index]
			return &copy
		}
	}
	return nil
}
