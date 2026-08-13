// Package orchestrator owns platform-neutral connectivity transitions. A
// platform adapter probes the underlay, tests subscriptions and executes VPN
// start/stop; this package decides what should happen next.
package orchestrator

import (
	"errors"
	"strings"

	"github.com/gooog1111/orcheroute/internal/whitelist"
)

const (
	Normal    = "normal"
	Allowlist = "allowlist"
	Offline   = "offline"
	Unknown   = "unknown"
)

type State struct {
	Mode         string          `json:"mode"`
	PreviousMode string          `json:"previous_mode,omitempty"`
	Enabled      bool            `json:"enabled"`
	Connected    bool            `json:"connected"`
	Whitelist    whitelist.State `json:"whitelist"`
	LastError    string          `json:"last_error,omitempty"`
}

type Event struct {
	Type     string           `json:"type"`
	Mode     string           `json:"mode,omitempty"`
	NodeID   string           `json:"node_id,omitempty"`
	SourceID string           `json:"source_id,omitempty"`
	Nodes    []whitelist.Node `json:"nodes,omitempty"`
}

type Action struct {
	Type      string          `json:"type"`
	Candidate *whitelist.Node `json:"candidate,omitempty"`
	Message   string          `json:"message,omitempty"`
}

type Result struct {
	State  State  `json:"state"`
	Action Action `json:"action"`
}

func Transition(input State, event Event) (Result, error) {
	state := input
	if state.Mode == "" {
		state.Mode = Unknown
	}
	action := Action{Type: "none"}
	switch strings.ToLower(strings.TrimSpace(event.Type)) {
	case "enable":
		state.Enabled = true
		action = Action{Type: "probe", Message: "Проверяем доступность сети"}
	case "disable":
		state.Enabled, state.Connected, state.LastError = false, false, ""
		pool, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "deactivate"})
		state.Whitelist = pool.State
		action = Action{Type: "stop"}
	case "probe":
		if event.Mode != Normal && event.Mode != Allowlist && event.Mode != Offline {
			return Result{}, errors.New("invalid_connectivity_mode")
		}
		old := state.Mode
		state.PreviousMode, state.Mode = old, event.Mode
		switch event.Mode {
		case Offline:
			action = Action{Type: "pause", Message: "Интернет недоступен; серверы и статусы сохранены"}
		case Allowlist:
			if old != Allowlist {
				pool, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "begin"})
				state.Whitelist = pool.State
				action = Action{Type: "scan_all_cached", Message: "Проверяем все сохранённые серверы без регионального фильтра"}
			}
		case Normal:
			if old == Allowlist {
				pool, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "deactivate"})
				state.Whitelist = pool.State
				action = Action{Type: "restore_normal", Message: "Возвращаем пользовательские маршруты и основной пул"}
			}
		}
	case "add_source", "replace_source":
		pool, err := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: event.Type, SourceID: event.SourceID, Nodes: event.Nodes})
		if err != nil {
			return Result{}, err
		}
		state.Whitelist = pool.State
		if state.Mode == Allowlist && !state.Connected {
			requested, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "request"})
			state.Whitelist = requested.State
			if requested.Candidate != nil {
				action = Action{Type: "connect", Candidate: requested.Candidate, Message: "Подключаем первый подтверждённый сервер"}
			}
		}
	case "connected":
		pool, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "confirm", NodeID: event.NodeID})
		state.Whitelist = pool.State
		state.Connected, state.LastError = true, ""
		action = Action{Type: "continue_scan", Message: "Соединение работает; продолжаем формировать устойчивый пул"}
	case "failed":
		state.Connected = false
		pool, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "fail", NodeID: event.NodeID})
		state.Whitelist = pool.State
		if pool.Candidate != nil {
			action = Action{Type: "connect", Candidate: pool.Candidate, Message: "Переключаемся на следующий сервер"}
		} else {
			action = Action{Type: "scan_all_cached", Message: "Рабочий whitelist-пул исчерпан; повторяем полную проверку"}
		}
	case "scan_complete":
		pool, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "complete"})
		state.Whitelist = pool.State
		if len(pool.State.Nodes) == 0 {
			state.Enabled, state.Connected = false, false
			state.LastError = "whitelist_pool_empty"
			action = Action{Type: "stop", Message: "В сети с белыми списками доступных VPN-серверов нет"}
		} else if state.Connected {
			action = Action{Type: "refresh_subscriptions_sequentially", Message: "Обновляем подписки по одной и проверяем только изменившиеся наборы"}
		} else {
			requested, _ := whitelist.Transition(state.Whitelist, whitelist.Command{Operation: "request"})
			state.Whitelist = requested.State
			action = Action{Type: "connect", Candidate: requested.Candidate}
		}
	default:
		return Result{}, errors.New("unknown_orchestrator_event")
	}
	return Result{State: state, Action: action}, nil
}
