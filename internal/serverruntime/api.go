package serverruntime

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/components"
	"github.com/gooog1111/orcheroute/internal/network"
	"github.com/gooog1111/orcheroute/internal/qualification"
	"github.com/gooog1111/orcheroute/internal/routes"
	"github.com/gooog1111/orcheroute/internal/subscriptions"
	"github.com/gooog1111/orcheroute/internal/whitelist"
)

func (runtime *Runtime) APIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", runtime.health)
	mux.HandleFunc("/v1/", runtime.api)
	return securityHeaders(mux)
}

func (runtime *Runtime) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, 200, map[string]any{"ok": true, "runtime": "go", "uptime_seconds": time.Now().Unix() - runtime.startedAt})
}

func (runtime *Runtime) api(writer http.ResponseWriter, request *http.Request) {
	if runtime.Config.RequireAPIAuth && subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+runtime.apiToken)) != 1 {
		writeJSON(writer, 401, map[string]any{"error": "unauthorized"})
		return
	}
	var body map[string]any
	if request.Method != http.MethodGet {
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 8<<20))
		if err := decoder.Decode(&body); err != nil {
			writeJSON(writer, 400, map[string]any{"error": "invalid_json"})
			return
		}
		if body == nil {
			body = map[string]any{}
		}
	}
	status, payload := runtime.dispatch(request.Context(), request.Method, request.URL, body)
	writeJSON(writer, status, payload)
}

func (runtime *Runtime) dispatch(ctx context.Context, method string, parsed *url.URL, body map[string]any) (int, any) {
	path := parsed.Path
	if method == http.MethodGet {
		switch path {
		case "/v1/status":
			return runtime.getStatus(ctx)
		case "/v1/pools":
			return runtime.getPools(ctx)
		case "/v1/nodes":
			nodes, _, err := runtime.liveNodes(ctx)
			if err != nil {
				return 200, map[string]any{"nodes": []PublicNode{}, "transport_available": false, "transport_error": "mihomo_api_unavailable"}
			}
			return 200, map[string]any{"nodes": nodes, "transport_available": true}
		case "/v1/events":
			limit, _ := strconv.Atoi(parsed.Query().Get("limit"))
			if limit == 0 {
				limit = 100
			}
			events, err := runtime.Store.Events(ctx, limit)
			return result(200, map[string]any{"events": events}, err)
		case "/v1/policy":
			return 200, map[string]any{"pool_order": []string{"primary", "emergency"}, "pool_priority": map[string]int{"primary": 0, "emergency": 1}, "sticky": true, "failure_limit": 2, "retry_after_seconds": 30, "switch_retry_seconds": 30, "cooldown_seconds": 900, "health_interval_seconds": 10, "max_candidates_per_incident": 3, "failback_probe_seconds": 20, "failback_stable_checks": 2, "runtime": "go"}
		case "/v1/qualification":
			return runtime.getQualification()
		case "/v1/subscriptions":
			return runtime.getSubscriptions(ctx)
		case "/v1/routes":
			return runtime.getRoutes()
		case "/v1/network/interfaces":
			topology, err := discoverTopology(ctx)
			return result(200, topology, err)
		case "/v1/network/profile":
			return runtime.getNetwork()
		case "/v1/dns":
			return runtime.getDNS()
		case "/v1/operations":
			return 200, runtime.operations()
		case "/v1/components":
			return runtime.getComponents(ctx)
		case "/v1/app-update":
			return runtime.getAppUpdate()
		}
	}
	if method == http.MethodPost {
		switch {
		case path == "/v1/control/auto":
			err := runtime.Store.SetAuto(ctx)
			return result(202, map[string]any{"accepted": true, "mode": "auto"}, err)
		case path == "/v1/control/emergency":
			err := runtime.Store.SetEmergency(ctx)
			if err != nil {
				return backendError(err)
			}
			_, _ = runtime.mihomo(ctx, http.MethodPut, "/providers/proxies/emergency/healthcheck", nil)
			checkStatus, _ := runtime.startUpdate(nil, "check", "emergency")
			return 202, map[string]any{"accepted": true, "mode": "emergency", "check_scheduled": checkStatus == 202}
		case path == "/v1/service/enable":
			if err := runtime.Store.SetEnabled(ctx, true); err != nil {
				return backendError(err)
			}
			if _, err := os.Stat(filepath.Join(runtime.Config.ConfigDirectory, "config.json")); os.IsNotExist(err) {
				desired, _, profileErr := runtime.networkProfiles()
				if profileErr != nil {
					return backendError(profileErr)
				}
				status, value := runtime.applyNetwork(ctx, map[string]any{"revision": float64(desired.Revision), "confirm_system_capture": true})
				if status != http.StatusAccepted {
					_ = runtime.Store.SetEnabled(ctx, false)
					return status, value
				}
				return 202, map[string]any{"accepted": true, "enabled": true, "network_apply_started": true, "system_mutated": true}
			}
			if err := platformSetTransportEnabled(ctx, runtime.Config.CoreService, true); err != nil {
				_ = runtime.Store.SetEnabled(ctx, false)
				return backendError(err)
			}
			return 202, map[string]any{"accepted": true, "enabled": true, "system_mutated": true}
		case path == "/v1/service/disable":
			if err := runtime.Store.SetEnabled(ctx, false); err != nil {
				return backendError(err)
			}
			if err := platformSetTransportEnabled(ctx, runtime.Config.CoreService, false); err != nil {
				return backendError(err)
			}
			return 202, map[string]any{"accepted": true, "enabled": false, "system_mutated": true}
		case path == "/v1/control/manual":
			return runtime.setManual(ctx, body)
		case path == "/v1/subscriptions":
			return runtime.createSubscription(ctx, body)
		case path == "/v1/subscriptions/import":
			return runtime.importSubscriptions(ctx, body)
		case path == "/v1/subscriptions/refresh":
			return runtime.startUpdate(nil, "fetch")
		case path == "/v1/subscriptions/check":
			if runtime.connectivitySnapshot().State == "allowlist" {
				return runtime.startWhitelistScan(nil)
			}
			return runtime.startUpdate(nil, "check")
		case path == "/v1/whitelist/scan":
			ids := []string{}
			if raw, ok := body["subscription_ids"].([]any); ok {
				for _, value := range raw {
					if id := strings.TrimSpace(stringValue(value)); id != "" {
						ids = append(ids, id)
					}
				}
			}
			return runtime.startWhitelistScan(ids)
		case path == "/v1/operations/subscription-update/cancel":
			return runtime.cancelSubscriptionUpdate()
		case strings.HasPrefix(path, "/v1/subscriptions/") && strings.HasSuffix(path, "/refresh"):
			id, _ := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(path, "/v1/subscriptions/"), "/refresh"))
			return runtime.startUpdate([]string{id}, "fetch")
		case strings.HasPrefix(path, "/v1/subscriptions/") && strings.HasSuffix(path, "/check"):
			id, _ := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(path, "/v1/subscriptions/"), "/check"))
			if runtime.connectivitySnapshot().State == "allowlist" {
				return runtime.startWhitelistScan([]string{id})
			}
			return runtime.startUpdate([]string{id}, "check")
		case strings.HasPrefix(path, "/v1/subscriptions/") && strings.HasSuffix(path, "/secret"):
			id, _ := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(path, "/v1/subscriptions/"), "/secret"))
			item, err := runtime.Store.Get(ctx, id, true)
			if err != nil {
				return backendError(err)
			}
			if item == nil {
				return 404, map[string]any{"error": "subscription_not_found"}
			}
			return 200, map[string]any{"id": id, "secret": item.Secret, "warning": "transport_must_be_trusted"}
		case strings.HasPrefix(path, "/v1/subscriptions/") && strings.HasSuffix(path, "/export"):
			id, _ := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(path, "/v1/subscriptions/"), "/export"))
			item, err := runtime.Store.Get(ctx, id, true)
			if err != nil {
				return backendError(err)
			}
			if item == nil {
				return 404, map[string]any{"error": "subscription_not_found"}
			}
			links, err := (subscriptions.FileCache{Directory: filepath.Join(runtime.Config.StateDirectory, "subscription-cache")}).Read(ctx, id)
			if err != nil {
				return backendError(err)
			}
			if len(links) == 0 && (item.Parser == subscriptions.Inline || item.Parser == subscriptions.WireGuard) {
				links = subscriptions.Decode([]byte(item.Secret))
			}
			return 200, map[string]any{"id": id, "name": item.Name, "parser": item.Parser, "secret": item.Secret, "links": links, "warning": "transport_must_be_trusted"}
		case path == "/v1/routes/validate":
			return validateRoutes(body)
		case path == "/v1/network/validate":
			return runtime.validateNetwork(ctx, body)
		case path == "/v1/dns/validate":
			return validateDNS(body)
		case path == "/v1/network/apply":
			return runtime.applyNetwork(ctx, body)
		case path == "/v1/components/update":
			return runtime.componentUpdate(body)
		case path == "/v1/app-update/check":
			return runtime.startAppUpdate("check", body)
		case path == "/v1/app-update/install":
			return runtime.startAppUpdate("install", body)
		case path == "/v1/whitelist/transition":
			payload, err := json.Marshal(body)
			if err != nil {
				return backendError(err)
			}
			var command whitelist.Command
			if err := json.Unmarshal(payload, &command); err != nil {
				return 400, map[string]any{"error": "invalid_whitelist_transition"}
			}
			transition, err := runtime.whitelistTransition(command)
			return result(200, transition, err)
		}
	}
	if method == http.MethodPut {
		switch path {
		case "/v1/components/settings":
			return runtime.saveComponentSettings(body)
		case "/v1/subscriptions/default-emergency":
			return runtime.saveDefaultEmergency(ctx, body)
		case "/v1/qualification/policy":
			return runtime.saveQualification(body)
		case "/v1/routes":
			return runtime.saveRoutes(body)
		case "/v1/network/profile":
			return runtime.saveNetwork(ctx, body)
		case "/v1/dns":
			return runtime.saveDNS(body)
		}
	}
	if method == http.MethodPatch && strings.HasPrefix(path, "/v1/subscriptions/") {
		id, _ := url.PathUnescape(strings.TrimPrefix(path, "/v1/subscriptions/"))
		return runtime.updateSubscription(ctx, id, body)
	}
	if method == http.MethodDelete && strings.HasPrefix(path, "/v1/subscriptions/") {
		id, _ := url.PathUnescape(strings.TrimPrefix(path, "/v1/subscriptions/"))
		deleted, err := runtime.Store.Delete(ctx, id)
		if err != nil {
			return backendError(err)
		}
		if !deleted {
			return 404, map[string]any{"error": "subscription_not_found"}
		}
		return 200, map[string]any{"deleted": true, "id": id}
	}
	if method == http.MethodDelete && strings.HasPrefix(path, "/v1/nodes/") {
		id, _ := url.PathUnescape(strings.TrimPrefix(path, "/v1/nodes/"))
		return runtime.deletePoolNode(ctx, id)
	}
	return 404, map[string]any{"error": "not_found"}
}

func (runtime *Runtime) deletePoolNode(ctx context.Context, id string) (int, any) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 400, map[string]any{"error": "node_id_required"}
	}
	nodes, _, err := runtime.liveNodes(ctx)
	if err != nil {
		return 503, map[string]any{"error": "mihomo_api_unavailable"}
	}
	var target *PublicNode
	for index := range nodes {
		if nodes[index].ID == id {
			copy := nodes[index]
			target = &copy
			break
		}
	}
	if target == nil {
		return 404, map[string]any{"error": "node_not_found"}
	}
	if target.Pool == whitelist.Pool {
		result, transitionErr := runtime.whitelistTransition(whitelist.Command{Operation: "remove_node", NodeID: id})
		if transitionErr != nil {
			return backendError(transitionErr)
		}
		return 200, map[string]any{"deleted": result.Changed, "id": id, "pool": target.Pool, "temporary": true, "remaining": len(result.State.Nodes)}
	}
	if target.Pool != "primary" && target.Pool != "emergency" {
		return 400, map[string]any{"error": "invalid_node_pool"}
	}
	providerPath := filepath.Join(runtime.Config.StateDirectory, "providers", target.Pool+".json")
	provider := map[string]any{}
	if err := readJSON(providerPath, &provider); err != nil {
		return backendError(err)
	}
	raw, _ := provider["proxies"].([]any)
	filtered := make([]any, 0, len(raw))
	deleted := false
	for _, item := range raw {
		proxy, _ := item.(map[string]any)
		if !deleted && stringValue(proxy["name"]) == target.FullName {
			deleted = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !deleted {
		return 404, map[string]any{"error": "node_not_found"}
	}
	provider["proxies"] = filtered
	if err := atomicJSON(providerPath, provider); err != nil {
		return backendError(err)
	}
	metadataPath := filepath.Join(runtime.Config.StateDirectory, "providers", target.Pool+".sources.json")
	metadata := map[string]any{}
	if readJSON(metadataPath, &metadata) == nil {
		if values, ok := metadata["nodes"].(map[string]any); ok {
			delete(values, target.FullName)
			_ = atomicJSON(metadataPath, metadata)
		}
	}
	response := map[string]any{"deleted": true, "id": id, "pool": target.Pool, "temporary": true, "remaining": len(filtered)}
	if _, reloadErr := runtime.mihomo(ctx, http.MethodPut, "/providers/proxies/"+target.Pool, nil); reloadErr != nil {
		response["applied"] = false
		response["apply_pending"] = true
		response["reload_error"] = reloadErr.Error()
	} else {
		response["applied"] = true
	}
	return 200, response
}

func (runtime *Runtime) getStatus(ctx context.Context) (int, any) {
	snapshot, err := runtime.Store.Snapshot(ctx)
	if err != nil {
		return backendError(err)
	}
	control, err := runtime.Store.Control(ctx)
	if err != nil {
		return backendError(err)
	}
	state := snapshot.State
	physical := runtime.connectivitySnapshot()
	identities := runtime.identitySnapshot()
	profile := network.Profile{}
	_ = readJSON(filepath.Join(runtime.Config.ProductionState, "network-active.json"), &profile)
	connectivity := stringValue(state["status"])
	if !control.Enabled {
		connectivity = "disabled"
	} else if connectivity == "" {
		connectivity = "starting"
	}
	active := stringValue(state["active"])
	var activeValue any = active
	if active == "" {
		activeValue = nil
	}
	pool := stringValue(state["active_pool"])
	var poolValue any = pool
	if pool == "" {
		poolValue = nil
	}
	var physicalAvailable any
	if physical.State == "normal" || physical.State == "allowlist" {
		physicalAvailable = true
	} else if physical.State == "offline" {
		physicalAvailable = false
	}
	lastSwitch := int64Value(state["last_switch"])
	if physical.ConfirmedAt > lastSwitch {
		lastSwitch = physical.ConfirmedAt
	}
	return 200, map[string]any{"version": 1, "timestamp": time.Now().Unix(), "updated_at": snapshot.UpdatedAt, "stale": time.Now().Unix()-snapshot.UpdatedAt > 35, "connectivity": connectivity, "runtime": "go", "service": map[string]any{"enabled": control.Enabled}, "wan": map[string]any{"interface": profile.Roles["direct"].Interface, "available": physicalAvailable, "mode": physical.State, "updated_at": physical.UpdatedAt, "error": physical.Error, "identity": identities.Direct}, "network": map[string]any{"capture_mode": profile.Capture.Mode, "direct_interface": profile.Roles["direct"].Interface, "vpn_underlay_interface": profile.Roles["vpn_underlay"].Interface}, "proxy": map[string]any{"mode": control.Mode, "active_node": activeValue, "active_pool": poolValue, "failure_streak": intValue(state["failure_streak"]), "last_switch": lastSwitch, "manual_until": control.ManualUntil, "identity": identities.Proxy}}
}

func (runtime *Runtime) getPools(ctx context.Context) (int, any) {
	nodes, _, err := runtime.liveNodes(ctx)
	if err != nil {
		nodes = []PublicNode{}
	}
	resultPools := []map[string]any{}
	for index, pool := range []string{"primary", "emergency", "whitelist"} {
		total, alive, selected := 0, 0, false
		for _, node := range nodes {
			if node.Pool == pool {
				total++
				if node.Alive {
					alive++
				}
				if node.Selected {
					selected = true
				}
			}
		}
		resultPools = append(resultPools, map[string]any{"id": pool, "priority": index, "total": total, "alive": alive, "selected": selected})
	}
	return 200, map[string]any{"pools": resultPools, "transport_available": err == nil}
}

func (runtime *Runtime) getSubscriptions(ctx context.Context) (int, any) {
	items, err := runtime.Store.List(ctx, false)
	if err != nil {
		return backendError(err)
	}
	output := make([]map[string]any, 0, len(items))
	builtins := builtinMap()
	for _, item := range items {
		value := subscriptionPublic(item)
		if builtin, ok := builtins[item.ID]; ok {
			value["builtin_default"] = true
			value["description"] = builtin.Description
			value["repository"] = builtin.Repository
		}
		output = append(output, value)
	}
	return 200, map[string]any{"subscriptions": output}
}

func subscriptionPublic(item subscriptions.Subscription) map[string]any {
	return map[string]any{"id": item.ID, "name": item.Name, "group": string(item.GroupName), "parser": string(item.Parser), "enabled": item.Enabled, "interval_seconds": item.IntervalSeconds, "last_attempt": item.LastAttempt, "last_success": item.LastSuccess, "last_status": item.LastStatus, "last_error": item.LastError, "last_links": item.LastLinks, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt, "secret_configured": true}
}
func builtinMap() map[string]subscriptions.BuiltinSource {
	result := map[string]subscriptions.BuiltinSource{}
	for _, item := range subscriptions.DefaultEmergencySources() {
		result[item.ID] = item
	}
	return result
}

func (runtime *Runtime) getRoutes() (int, any) {
	var state map[string]any
	path := filepath.Join(runtime.Config.StateDirectory, "routes.json")
	if err := readJSON(path, &state); err != nil {
		state = map[string]any{"revision": 0, "default": "proxy", "lists": map[string]any{"direct": []string{}, "proxy": []string{}, "block": []string{}}, "stats": map[string]any{}}
	}
	return 200, state
}

func routeLists(body map[string]any) (map[string][]string, error) {
	raw, ok := body["lists"].(map[string]any)
	if !ok {
		return nil, &routes.ValidationError{Code: "invalid_routes_payload"}
	}
	result := map[string][]string{}
	for _, name := range []string{"direct", "proxy", "block"} {
		result[name] = []string{}
		values, ok := raw[name].([]any)
		if !ok {
			return nil, &routes.ValidationError{Code: "invalid_route_list", List: name}
		}
		for _, value := range values {
			entry := strings.TrimSpace(stringValue(value))
			if entry != "" && !strings.HasPrefix(entry, "#") {
				result[name] = append(result[name], entry)
			}
		}
	}
	return result, nil
}
func validateRoutes(body map[string]any) (int, any) {
	lists, err := routeLists(body)
	if err != nil {
		return validationError(err)
	}
	compiled, err := routes.CompileLists(lists)
	if err != nil {
		return validationError(err)
	}
	action := strings.ToLower(stringValue(body["default"]))
	if action == "" {
		action = "proxy"
	}
	if action != "proxy" && action != "direct" && action != "block" {
		return 400, map[string]any{"error": "invalid_default_action"}
	}
	return 200, map[string]any{"valid": true, "default": action, "lists": compiled.Normalized, "stats": compiled.Stats, "compiled": compiled.Compiled, "items": compiled.Items}
}

func (runtime *Runtime) saveRoutes(body map[string]any) (int, any) {
	status, preview := validateRoutes(body)
	if status != 200 {
		return status, preview
	}
	currentStatus, currentValue := runtime.getRoutes()
	_ = currentStatus
	current, _ := currentValue.(map[string]any)
	expected := intValue(body["revision"])
	if expected != intValue(current["revision"]) {
		return 409, map[string]any{"error": "route_revision_conflict"}
	}
	result := preview.(map[string]any)
	state := map[string]any{"revision": expected + 1, "updated_at": time.Now().Unix(), "default": result["default"], "lists": result["lists"], "stats": result["stats"], "items": result["items"]}
	compiled := result["compiled"].(map[string][]string)
	if err := runtime.writeRouteProviders(compiled); err != nil {
		return backendError(err)
	}
	if err := atomicJSON(filepath.Join(runtime.Config.StateDirectory, "routes.json"), state); err != nil {
		return backendError(err)
	}
	reloadErrors := []string{}
	for _, name := range []string{"block", "direct", "proxy"} {
		if _, err := runtime.mihomo(context.Background(), http.MethodPut, "/providers/rules/routes-"+name, nil); err != nil {
			reloadErrors = append(reloadErrors, fmt.Sprintf("route_provider_reload_%s: %v", name, err))
		}
	}
	// Saving routes is durable independently of the transport lifecycle.  A
	// stopped core cannot reload providers, but the generated config will use
	// the files at the next start.  Returning an error after the atomic write
	// made the UI claim that nothing was saved and encouraged duplicate edits.
	response := map[string]any{"updated": true, "routes": state, "system_mutated": len(reloadErrors) == 0, "applied": len(reloadErrors) == 0}
	if len(reloadErrors) != 0 {
		response["apply_pending"] = true
		response["reload_errors"] = reloadErrors
	}
	return 200, response
}

func (runtime *Runtime) writeRouteProviders(compiled map[string][]string) error {
	directory := filepath.Join(runtime.Config.StateDirectory, "rules")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"block", "direct", "proxy"} {
		lines := compiled[name]
		payload := strings.Join(lines, "\n")
		if payload != "" {
			payload += "\n"
		}
		if err := atomicWrite(filepath.Join(directory, name+".txt"), []byte(payload), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func profileInput(value any) (network.ProfileInput, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return network.ProfileInput{}, err
	}
	var profile network.ProfileInput
	err = json.Unmarshal(payload, &profile)
	return profile, err
}
func (runtime *Runtime) validateNetwork(ctx context.Context, body map[string]any) (int, any) {
	profile, err := profileInput(body["profile"])
	if err != nil {
		return 400, map[string]any{"error": "invalid_network_profile"}
	}
	topology, err := discoverTopology(ctx)
	if err != nil {
		return backendError(err)
	}
	preview, err := network.PreviewProfile(profile, topology)
	if err != nil {
		return validationError(err)
	}
	return 200, preview
}

func (runtime *Runtime) networkProfiles() (network.Profile, network.Profile, error) {
	desired, active := network.Profile{}, network.Profile{}
	if err := readJSON(filepath.Join(runtime.Config.StateDirectory, "network-profile.json"), &desired); err != nil {
		if err := readJSON(filepath.Join(runtime.Config.ProductionState, "network-profile.json"), &desired); err != nil {
			return desired, active, err
		}
	}
	if err := readJSON(filepath.Join(runtime.Config.StateDirectory, "network-active.json"), &active); err != nil {
		if err := readJSON(filepath.Join(runtime.Config.ProductionState, "network-active.json"), &active); err != nil {
			return desired, active, err
		}
	}
	return desired, active, nil
}
func (runtime *Runtime) getNetwork() (int, any) {
	desired, active, err := runtime.networkProfiles()
	if err != nil {
		return backendError(err)
	}
	apply := map[string]any{"status": "never", "updated_at": 0}
	_ = readJSON(filepath.Join(runtime.Config.StateDirectory, "network-apply-status.json"), &apply)
	if _, ok := apply["revision"]; !ok {
		apply["revision"] = active.Revision
	}
	if _, ok := apply["capture_mode"]; !ok {
		apply["capture_mode"] = active.Capture.Mode
	}
	if _, ok := apply["direct_interface"]; !ok {
		apply["direct_interface"] = active.Roles["direct"].Interface
	}
	if _, ok := apply["vpn_underlay_interface"]; !ok {
		apply["vpn_underlay_interface"] = active.Roles["vpn_underlay"].Interface
	}
	if _, ok := apply["digest"]; !ok {
		apply["digest"] = network.ProfileDigest(active)
	}
	desiredJSON, _ := json.Marshal(desired)
	activeJSON, _ := json.Marshal(active)
	return 200, map[string]any{"desired": desired, "active": active, "in_sync": string(desiredJSON) == string(activeJSON), "apply": apply}
}
func (runtime *Runtime) saveNetwork(ctx context.Context, body map[string]any) (int, any) {
	desired, _, err := runtime.networkProfiles()
	if err != nil {
		return backendError(err)
	}
	if intValue(body["revision"]) != int(desired.Revision) {
		return 409, map[string]any{"error": "network_revision_conflict"}
	}
	status, preview := runtime.validateNetwork(ctx, body)
	if status != 200 {
		return status, preview
	}
	profile := preview.(network.Preview).Profile
	profile.Revision = desired.Revision + 1
	profile.UpdatedAt = time.Now().Unix()
	if err := atomicJSON(filepath.Join(runtime.Config.StateDirectory, "network-profile.json"), profile); err != nil {
		return backendError(err)
	}
	return 200, map[string]any{"updated": true, "applied": false, "desired": profile, "preview": preview}
}

func validateDNS(body map[string]any) (int, any) {
	payload, err := json.Marshal(body["dns"])
	if err != nil {
		return 400, map[string]any{"error": "invalid_dns_config"}
	}
	var input network.DNSInput
	if err := json.Unmarshal(payload, &input); err != nil {
		return 400, map[string]any{"error": "invalid_dns_config"}
	}
	config, err := network.ValidateDNS(&input)
	if err != nil {
		return validationError(err)
	}
	preview := network.PreviewDNS(config)
	return 200, map[string]any{"config": config, "effective": preview.Effective}
}
func (runtime *Runtime) getDNS() (int, any) {
	desired, active, err := runtime.networkProfiles()
	if err != nil {
		return backendError(err)
	}
	desiredJSON, _ := json.Marshal(desired.DNS)
	activeJSON, _ := json.Marshal(active.DNS)
	_, networkState := runtime.getNetwork()
	apply := networkState.(map[string]any)["apply"]
	return 200, map[string]any{"desired": desired.DNS, "active": active.DNS, "in_sync": string(desiredJSON) == string(activeJSON), "preview": network.PreviewDNS(active.DNS), "network_revision": desired.Revision, "apply": apply}
}
func (runtime *Runtime) saveDNS(body map[string]any) (int, any) {
	desired, _, err := runtime.networkProfiles()
	if err != nil {
		return backendError(err)
	}
	if intValue(body["revision"]) != int(desired.Revision) {
		return 409, map[string]any{"error": "network_revision_conflict"}
	}
	status, value := validateDNS(body)
	if status != 200 {
		return status, value
	}
	config := value.(map[string]any)["config"].(network.DNSConfig)
	desired.DNS = config
	desired.Revision++
	desired.UpdatedAt = time.Now().Unix()
	if err := atomicJSON(filepath.Join(runtime.Config.StateDirectory, "network-profile.json"), desired); err != nil {
		return backendError(err)
	}
	return 200, map[string]any{"updated": true, "applied": false, "network_revision": desired.Revision, "dns": config}
}
func (runtime *Runtime) applyNetwork(ctx context.Context, body map[string]any) (int, any) {
	desired, _, err := runtime.networkProfiles()
	if err != nil {
		return backendError(err)
	}
	revision := intValue(body["revision"])
	if revision != int(desired.Revision) {
		return 409, map[string]any{"error": "network_revision_conflict"}
	}
	input := network.ProfileInput{}
	payload, _ := json.Marshal(desired)
	_ = json.Unmarshal(payload, &input)
	topology, err := discoverTopology(ctx)
	if err != nil {
		return backendError(err)
	}
	if _, err := network.PreviewProfile(input, topology); err != nil {
		return validationError(err)
	}
	requestPath := filepath.Join(runtime.Config.StateDirectory, "network-apply-request.json")
	statusPath := filepath.Join(runtime.Config.StateDirectory, "network-apply-status.json")
	var current map[string]any
	if readJSON(statusPath, &current) == nil && stringValue(current["status"]) == "applying" {
		updatedAt := intValue(current["updated_at"])
		if updatedAt > 0 && time.Now().Unix()-int64(updatedAt) <= 240 {
			return 409, map[string]any{"error": "network_apply_in_progress"}
		}
		_ = atomicJSON(statusPath, map[string]any{"status": "failed", "revision": intValue(current["revision"]), "updated_at": time.Now().Unix(), "error": "network_apply_interrupted"})
	}
	confirmed, _ := body["confirm_system_capture"].(bool)
	if !confirmed {
		return 409, map[string]any{"error": "system_capture_confirmation_required"}
	}
	if err := atomicJSON(requestPath, map[string]any{"revision": revision, "confirm_system_capture": confirmed}); err != nil {
		return backendError(err)
	}
	_ = atomicJSON(statusPath, map[string]any{"status": "applying", "revision": revision, "updated_at": time.Now().Unix()})
	go func() {
		arguments := []string{"--action", "apply-staged", "--state-dir", runtime.Config.StateDirectory, "--config", filepath.Join(runtime.Config.ConfigDirectory, "config.json"), "--runtime-env", runtime.Config.RuntimeEnv, "--mihomo", runtime.Config.MihomoBinary, "--core-service", runtime.Config.CoreService}
		if confirmed {
			arguments = append(arguments, "--confirm-system-capture")
		}
		output, err := exec.Command(runtime.Config.NetworkBinary, arguments...).CombinedOutput()
		if err != nil {
			// An asynchronous first start must not leave the persistent control
			// flag enabled while no VPN transport exists.  Otherwise the next UI
			// action looks like a running VPN even though the core rolled back.
			_ = runtime.Store.SetEnabled(context.Background(), false)
			_ = atomicJSON(statusPath, map[string]any{"status": "failed", "revision": revision, "updated_at": time.Now().Unix(), "error": err.Error(), "output": truncate(string(output), 4000)})
		}
	}()
	return 202, map[string]any{"accepted": true, "revision": revision, "system_mutated": true}
}

func (runtime *Runtime) getQualification() (int, any) {
	policy := qualification.DefaultPolicy()
	_ = readJSON(filepath.Join(runtime.Config.StateDirectory, "qualification-policy.json"), &policy)
	validated, err := qualification.Validate(qualification.MigrateLegacyPools(policy))
	if err != nil {
		return validationError(err)
	}
	effective := map[string]any{}
	reports := map[string]any{}
	for _, pool := range qualification.Pools {
		effective[pool], _ = qualification.Effective(validated, pool)
		var report any
		if readJSON(filepath.Join(runtime.Config.ProductionState, "qualification", pool+".json"), &report) != nil {
			report = nil
		}
		reports[pool] = report
	}
	return 200, map[string]any{"policy": validated, "effective": effective, "reports": reports}
}
func (runtime *Runtime) saveQualification(body map[string]any) (int, any) {
	_, current := runtime.getQualification()
	policy := current.(map[string]any)["policy"].(map[string]any)
	updated, err := qualification.Update(policy, body)
	if err != nil {
		return validationError(err)
	}
	if err := atomicJSON(filepath.Join(runtime.Config.StateDirectory, "qualification-policy.json"), updated); err != nil {
		return backendError(err)
	}
	return 200, map[string]any{"updated": true, "policy": updated, "effective_next_update": true}
}

func (runtime *Runtime) setManual(ctx context.Context, body map[string]any) (int, any) {
	nodes, mapping, err := runtime.liveNodes(ctx)
	if err != nil {
		return backendError(err)
	}
	_ = nodes
	target, ok := mapping[stringValue(body["node_id"])]
	if !ok {
		return 400, map[string]any{"error": "unknown_node"}
	}
	seconds := intValue(body["lock_seconds"])
	if seconds != 0 && (seconds < 60 || seconds > 86400) {
		return 400, map[string]any{"error": "lock_seconds_out_of_range"}
	}
	until := int64(0)
	if seconds > 0 {
		until = time.Now().Unix() + int64(seconds)
	}
	if err := runtime.Store.SetManual(ctx, target, until); err != nil {
		return backendError(err)
	}
	return 202, map[string]any{"accepted": true, "mode": "manual", "node_id": body["node_id"], "manual_until": until, "system_mutated": true}
}

func (runtime *Runtime) createSubscription(ctx context.Context, body map[string]any) (int, any) {
	values, err := subscriptions.ValidateFields(body, false)
	if err != nil {
		return validationError(err)
	}
	item := subscriptions.Subscription{Name: stringValue(values["name"]), GroupName: subscriptions.Group(stringValue(values["group_name"])), Parser: subscriptions.Parser(stringValue(values["parser"])), Secret: stringValue(values["secret"]), Enabled: true, IntervalSeconds: 3600}
	duplicatesRemoved, duplicateCode, err := runtime.normalizeSubscription(ctx, "", item.Parser, &item.Secret)
	if err != nil {
		return backendError(err)
	}
	if duplicateCode != "" {
		return 409, map[string]any{"error": duplicateCode}
	}
	if value, ok := values["enabled"].(bool); ok {
		item.Enabled = value
	}
	if value, ok := values["interval_seconds"].(int); ok {
		item.IntervalSeconds = value
	}
	created, err := runtime.Store.Create(ctx, item)
	if err != nil {
		return backendError(err)
	}
	return 201, map[string]any{"subscription": subscriptionPublic(*created), "duplicates_removed": duplicatesRemoved}
}

func (runtime *Runtime) importSubscriptions(ctx context.Context, body map[string]any) (int, any) {
	raw, ok := body["subscriptions"].([]any)
	if !ok || len(raw) == 0 || len(raw) > 100 {
		return 400, map[string]any{"error": "invalid_subscription_batch"}
	}
	for _, value := range raw {
		payload, ok := value.(map[string]any)
		if !ok {
			return 400, map[string]any{"error": "invalid_subscription_batch"}
		}
		if _, err := subscriptions.ValidateFields(payload, false); err != nil {
			return validationError(err)
		}
	}
	created := []any{}
	skipped := []any{}
	ids := []string{}
	for _, value := range raw {
		status, result := runtime.createSubscription(ctx, value.(map[string]any))
		payload, _ := result.(map[string]any)
		if status == 409 && (stringValue(payload["error"]) == "duplicate_subscription" || stringValue(payload["error"]) == "duplicate_servers") {
			skipped = append(skipped, map[string]any{"name": stringValue(value.(map[string]any)["name"]), "reason": payload["error"]})
			continue
		}
		if status != 201 {
			return status, result
		}
		subscription, _ := payload["subscription"].(map[string]any)
		created = append(created, subscription)
		ids = append(ids, stringValue(subscription["id"]))
	}
	return 201, map[string]any{"created": created, "skipped": skipped, "refresh_scheduled": false, "refresh_required": len(ids) > 0}
}
func (runtime *Runtime) updateSubscription(ctx context.Context, id string, body map[string]any) (int, any) {
	values, err := subscriptions.ValidateFields(body, true)
	if err != nil {
		return validationError(err)
	}
	current, err := runtime.Store.Get(ctx, id, true)
	if err != nil {
		return backendError(err)
	}
	if current == nil {
		return 404, map[string]any{"error": "subscription_not_found"}
	}
	parser := current.Parser
	if value, ok := values["parser"]; ok {
		parser = subscriptions.Parser(stringValue(value))
	}
	if secret, ok := values["secret"]; ok {
		normalized := stringValue(secret)
		_, duplicateCode, normalizeErr := runtime.normalizeSubscription(ctx, id, parser, &normalized)
		if normalizeErr != nil {
			return backendError(normalizeErr)
		}
		if duplicateCode != "" {
			return 409, map[string]any{"error": duplicateCode}
		}
		values["secret"] = normalized
	}
	updated, err := runtime.Store.Update(ctx, id, values)
	if err != nil {
		return backendError(err)
	}
	if updated == nil {
		return 404, map[string]any{"error": "subscription_not_found"}
	}
	return 200, map[string]any{"subscription": subscriptionPublic(*updated), "refresh_scheduled": false}
}

func (runtime *Runtime) normalizeSubscription(ctx context.Context, excludeID string, parser subscriptions.Parser, secret *string) (int, string, error) {
	items, err := runtime.Store.List(ctx, true)
	if err != nil {
		return 0, "", err
	}
	if parser != subscriptions.Inline && parser != subscriptions.WireGuard {
		for _, item := range items {
			if item.ID != excludeID && item.Parser == parser && strings.TrimSpace(item.Secret) == strings.TrimSpace(*secret) {
				return 0, "duplicate_subscription", nil
			}
		}
		return 0, "", nil
	}
	normalized, duplicates := subscriptions.NormalizeInline(*secret)
	links := subscriptions.Decode([]byte(normalized))
	keepOriginal := false
	if parser == subscriptions.WireGuard {
		links = subscriptions.Decode([]byte(*secret))
		keepOriginal = true
	}
	if len(links) == 0 {
		return 0, "subscription_returned_no_supported_links", nil
	}
	known := map[string]bool{}
	cache := subscriptions.FileCache{Directory: filepath.Join(runtime.Config.StateDirectory, "subscription-cache")}
	for _, item := range items {
		if item.ID == excludeID {
			continue
		}
		links, cacheErr := cache.Read(ctx, item.ID)
		if cacheErr != nil {
			return duplicates, "", cacheErr
		}
		if len(links) == 0 && (item.Parser == subscriptions.Inline || item.Parser == subscriptions.WireGuard) {
			links = subscriptions.Decode([]byte(item.Secret))
		}
		for _, link := range links {
			known[link] = true
		}
	}
	filtered := make([]string, 0, len(links))
	for _, link := range links {
		if known[link] {
			duplicates++
			continue
		}
		filtered = append(filtered, link)
	}
	if len(filtered) == 0 {
		return duplicates, "duplicate_servers", nil
	}
	if !keepOriginal || len(filtered) != len(links) {
		*secret = strings.Join(filtered, "\n")
	}
	return duplicates, "", nil
}
func (runtime *Runtime) saveDefaultEmergency(ctx context.Context, body map[string]any) (int, any) {
	raw, ok := body["enabled_ids"].([]any)
	if !ok {
		return 400, map[string]any{"error": "invalid_default_emergency_selection"}
	}
	enabled := map[string]bool{}
	builtins := builtinMap()
	for _, value := range raw {
		id := stringValue(value)
		if _, ok := builtins[id]; !ok {
			return 400, map[string]any{"error": "unknown_default_emergency_source"}
		}
		enabled[id] = true
	}
	for id := range builtins {
		_, err := runtime.Store.Update(ctx, id, map[string]any{"enabled": enabled[id]})
		if err != nil {
			return backendError(err)
		}
	}
	ids := make([]string, 0, len(enabled))
	for id := range enabled {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return 200, map[string]any{"updated": true, "enabled_ids": ids, "refresh_scheduled": false}
}

func (runtime *Runtime) startUpdate(ids []string, updateMode string, groups ...string) (int, any) {
	operation := filepath.Join(runtime.Config.StateDirectory, "update-operation.json")
	cancelPath := filepath.Join(runtime.Config.StateDirectory, "update-cancel.request")
	var current map[string]any
	if readJSON(operation, &current) == nil && stringValue(current["status"]) == "running" {
		age := time.Now().Unix() - int64(intValue(current["updated_at"]))
		if age < 10 || runtime.subscriptionUpdateProcessActive() {
			return 409, map[string]any{"error": "subscription_update_in_progress"}
		}
		_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": "error", "phase": "interrupted", "message": "Предыдущая проверка была прервана", "error": "update_interrupted", "updated_at": time.Now().Unix()})
	}
	message := "Обновление подписок запущено"
	if updateMode == "check" {
		message = "Проверка сохранённых серверов запущена"
	}
	_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": "running", "phase": "starting", "message": message, "updated_at": time.Now().Unix()})
	_ = os.Remove(cancelPath)
	go func() {
		arguments := []string{
			"--state-dir", runtime.Config.ProductionState,
			"--output-state-dir", runtime.Config.StateDirectory,
			"--operation-path", operation,
			"--cancel-path", cancelPath,
			"--network-profile", filepath.Join(runtime.Config.StateDirectory, "network-active.json"),
			"--policy", filepath.Join(runtime.Config.StateDirectory, "qualification-policy.json"),
			"--mihomo", runtime.Config.MihomoBinary,
		}
		arguments = append(arguments, "--force")
		if updateMode == "check" {
			arguments = append(arguments, "--cached-only")
		} else {
			arguments = append(arguments, "--fetch-only")
		}
		for _, id := range ids {
			arguments = append(arguments, "--subscription-id", id)
		}
		for _, group := range groups {
			arguments = append(arguments, "--group", group)
		}
		command := exec.Command(runtime.Config.UpdateBinary, arguments...)
		output, err := command.CombinedOutput()
		if _, cancelErr := os.Stat(cancelPath); cancelErr == nil {
			_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": "cancelled", "phase": "cancelled", "message": "Операция остановлена пользователем. Завершённые результаты сохранены.", "error": "", "output": truncate(string(output), 4000), "updated_at": time.Now().Unix()})
			_ = os.Remove(cancelPath)
			return
		}
		// The helper owns the detailed terminal state (including a warning when
		// every checked server is unavailable). Do not replace that result with
		// a generic success merely because the helper process exited with code 0.
		var terminal map[string]any
		if readJSON(operation, &terminal) == nil && stringValue(terminal["phase"]) == "complete" &&
			containsString([]string{"success", "warning", "error"}, stringValue(terminal["status"])) {
			terminal["output"] = truncate(string(output), 4000)
			if err != nil && stringValue(terminal["status"]) != "error" {
				terminal["status"] = "error"
				terminal["error"] = err.Error()
			}
			_ = atomicJSON(operation, terminal)
			return
		}
		status, message, errorText := "success", "Подписки обновлены, сохранённые серверы не удалены", ""
		if updateMode == "check" {
			message = "Проверка сохранённых серверов завершена"
		}
		if err != nil {
			status, message, errorText = "error", "Обновление завершилось ошибкой", err.Error()
		}
		_ = atomicJSON(operation, map[string]any{"kind": "subscription_update", "status": status, "phase": "complete", "message": message, "error": errorText, "output": truncate(string(output), 4000), "updated_at": time.Now().Unix()})
	}()
	return 202, map[string]any{"accepted": true, "system_mutated": true}
}

func (runtime *Runtime) cancelSubscriptionUpdate() (int, any) {
	operationPath := filepath.Join(runtime.Config.StateDirectory, "update-operation.json")
	operation := map[string]any{}
	if readJSON(operationPath, &operation) != nil ||
		!containsString([]string{"running", "cancelling"}, stringValue(operation["status"])) {
		return 200, map[string]any{"accepted": false, "active": false}
	}
	if stringValue(operation["status"]) == "cancelling" {
		return 202, map[string]any{"accepted": false, "already_cancelling": true, "active": true}
	}
	cancelPath := filepath.Join(runtime.Config.StateDirectory, "update-cancel.request")
	if err := atomicWrite(cancelPath, []byte("cancel\n"), 0o600); err != nil {
		return backendError(err)
	}
	operation["status"] = "cancelling"
	operation["phase"] = "cancelling"
	operation["message"] = "Останавливаем после завершения текущей группы тестов"
	operation["updated_at"] = time.Now().Unix()
	if err := atomicJSON(operationPath, operation); err != nil {
		return backendError(err)
	}
	return 202, map[string]any{"accepted": true, "active": true}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (runtime *Runtime) subscriptionUpdateProcessActive() bool {
	locked, release, err := tryOperationLock(filepath.Join(runtime.Config.StateDirectory, "update-go.lock"))
	if err != nil {
		return true
	}
	if !locked {
		return true
	}
	release()
	return false
}

func (runtime *Runtime) operations() map[string]any {
	read := func(name, kind string) map[string]any {
		value := map[string]any{"kind": kind, "status": "idle", "phase": "idle", "message": "", "failures": []string{}, "skipped": []string{}, "updated_at": 0, "active": false}
		_ = readJSON(filepath.Join(runtime.Config.StateDirectory, name), &value)
		value["active"] = value["status"] == "running"
		return value
	}
	networkValue := map[string]any{"status": "never", "updated_at": 0, "active": false}
	_ = readJSON(filepath.Join(runtime.Config.StateDirectory, "network-apply-status.json"), &networkValue)
	return map[string]any{"subscription_update": read("update-operation.json", "subscription_update"), "network_apply": networkValue, "component_update": read("component-operation.json", "component_update")}
}

var versionPattern = regexp.MustCompile(`\bv?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\b`)

func (runtime *Runtime) getComponents(ctx context.Context) (int, any) {
	version := "unavailable"
	versionError := ""
	if output, err := exec.CommandContext(ctx, runtime.Config.MihomoBinary, "-v").CombinedOutput(); err == nil {
		version = strings.TrimSpace(strings.Split(string(output), "\n")[0])
	} else {
		versionError = err.Error()
	}
	installed := ""
	if match := versionPattern.FindStringSubmatch(version); len(match) > 1 {
		installed = match[1]
	}
	fileStatus := func(name string) map[string]any {
		info, err := os.Stat(filepath.Join(runtime.Config.ProductionState, name))
		if err != nil {
			return map[string]any{"installed": false, "updated_at": 0, "size": 0}
		}
		return map[string]any{"installed": true, "updated_at": info.ModTime().Unix(), "size": info.Size()}
	}
	settings := map[string]any{"geo_auto_update": true, "geo_interval_hours": 24, "geo_source": "metacubex", "geoip_url": "", "geosite_url": ""}
	_ = readJSON(filepath.Join(runtime.Config.StateDirectory, "component-settings.json"), &settings)
	latest := map[string]any{}
	_ = readJSON(filepath.Join(runtime.Config.StateDirectory, "component-latest.json"), &latest)
	geoip, geosite := fileStatus("GeoIP.dat"), fileStatus("GeoSite.dat")
	interval := intValue(settings["geo_interval_hours"])
	if interval == 0 {
		interval = 24
	}
	next := time.Now().Unix()
	if intValue(geoip["updated_at"]) > 0 && intValue(geosite["updated_at"]) > 0 {
		oldest := intValue(geoip["updated_at"])
		if value := intValue(geosite["updated_at"]); value < oldest {
			oldest = value
		}
		next = int64(oldest + interval*3600)
	}
	operations := runtime.operations()
	catalog := map[string]any{}
	if readJSON(filepath.Join(runtime.Config.StateDirectory, "geo-catalog.json"), &catalog) != nil {
		geoIPCatalog, _ := components.GeoCatalog(filepath.Join(runtime.Config.ProductionState, "GeoIP.dat"))
		geoSiteCatalog, _ := components.GeoCatalog(filepath.Join(runtime.Config.ProductionState, "GeoSite.dat"))
		catalog = map[string]any{"geoip": geoIPCatalog, "geosite": geoSiteCatalog}
		_ = atomicJSON(filepath.Join(runtime.Config.StateDirectory, "geo-catalog.json"), catalog)
	}
	installedSource := map[string]any{}
	geoIPInstalled, _ := geoip["installed"].(bool)
	geoSiteInstalled, _ := geosite["installed"].(bool)
	if readJSON(filepath.Join(runtime.Config.StateDirectory, "geo-installed-source.json"), &installedSource) != nil && geoIPInstalled && geoSiteInstalled {
		installedSource = map[string]any{"id": "metacubex", "name": "MetaCubeX", "inferred": true}
	}
	geoSource := stringValue(settings["geo_source"])
	if geoSource == "" {
		geoSource = "metacubex"
	}
	latestVersion := stringValue(latest["latest_version"])
	return 200, map[string]any{"mihomo": map[string]any{"installed": installed != "", "version": version, "installed_version": installed, "error": versionError, "binary": runtime.Config.MihomoBinary, "latest_version": latest["latest_version"], "update_available": components.IsNewerVersion(installed, latestVersion), "checked_at": intValue(latest["checked_at"]), "release_url": latest["release_url"]}, "geoip": geoip, "geosite": geosite, "auto_update": settings["geo_auto_update"], "interval_hours": interval, "next_geo_update": next, "geo_source": geoSource, "geoip_url": settings["geoip_url"], "geosite_url": settings["geosite_url"], "geo_sources": components.GeoSources, "installed_geo_source": installedSource, "catalog": catalog, "operation": operations["component_update"]}
}
func (runtime *Runtime) saveComponentSettings(body map[string]any) (int, any) {
	enabled, ok := body["geo_auto_update"].(bool)
	if !ok {
		return 400, map[string]any{"error": "invalid_geo_auto_update"}
	}
	interval := intValue(body["geo_interval_hours"])
	if interval < 1 || interval > 720 {
		return 400, map[string]any{"error": "geo_interval_out_of_range"}
	}
	sourceID := stringValue(body["geo_source"])
	geoIPURL := stringValue(body["geoip_url"])
	geoSiteURL := stringValue(body["geosite_url"])
	source, err := components.ResolveGeoSource(sourceID, geoIPURL, geoSiteURL)
	if err != nil {
		return 400, map[string]any{"error": err.Error()}
	}
	settings := map[string]any{"geo_auto_update": enabled, "geo_interval_hours": interval, "geo_source": source.ID, "geoip_url": "", "geosite_url": ""}
	if source.ID == "custom" {
		settings["geoip_url"], settings["geosite_url"] = source.GeoIPURL, source.GeoSiteURL
	}
	if err := atomicJSON(filepath.Join(runtime.Config.StateDirectory, "component-settings.json"), settings); err != nil {
		return backendError(err)
	}
	return 200, map[string]any{"updated": true, "settings": settings, "system_mutated": true}
}
func (runtime *Runtime) componentUpdate(body map[string]any) (int, any) {
	component := stringValue(body["component"])
	if component == "" {
		component = "all"
	}
	if component != "check" && component != "geo" && component != "core" && component != "all" {
		return 400, map[string]any{"error": "unknown_component"}
	}
	operationPath := filepath.Join(runtime.Config.StateDirectory, "component-operation.json")
	var existing map[string]any
	if readJSON(operationPath, &existing) == nil && stringValue(existing["status"]) == "running" {
		return 409, map[string]any{"error": "component_update_in_progress"}
	}
	operation := map[string]any{"kind": "component_update", "status": "running", "phase": "starting", "component": component, "message": "Запускаем Go-проверку компонентов", "updated_at": time.Now().Unix(), "system_mutated": true}
	_ = atomicJSON(operationPath, operation)
	go func() {
		arguments := []string{"--component", component, "--state-dir", runtime.Config.StateDirectory, "--production-state", runtime.Config.ProductionState, "--config-dir", runtime.Config.ConfigDirectory, "--mihomo", runtime.Config.MihomoBinary, "--core-service", runtime.Config.CoreService}
		command := exec.Command(runtime.Config.ComponentBinary, arguments...)
		if output, err := command.CombinedOutput(); err != nil {
			var detailed map[string]any
			if readJSON(operationPath, &detailed) == nil && stringValue(detailed["status"]) == "error" && stringValue(detailed["error"]) != "" {
				return
			}
			_ = atomicJSON(operationPath, map[string]any{"kind": "component_update", "status": "error", "phase": "failed", "component": component, "message": "Go-проверка компонентов не выполнена", "error": err.Error(), "output": truncate(string(output), 4000), "updated_at": time.Now().Unix(), "system_mutated": true})
		}
	}()
	return 202, map[string]any{"accepted": true, "component": component, "system_mutated": true}
}

func validationError(err error) (int, any) {
	var routeError *routes.ValidationError
	if errors.As(err, &routeError) {
		return 400, routeError
	}
	var networkError *network.ValidationError
	if errors.As(err, &networkError) {
		return 400, networkError
	}
	var subscriptionError *subscriptions.ValidationError
	if errors.As(err, &subscriptionError) {
		return 400, map[string]any{"error": subscriptionError.Code}
	}
	var qualificationError *qualification.ValidationError
	if errors.As(err, &qualificationError) {
		return 400, map[string]any{"error": qualificationError.Code}
	}
	return 400, map[string]any{"error": err.Error()}
}
func backendError(err error) (int, any) {
	return 503, map[string]any{"error": "backend_unavailable", "type": fmt.Sprintf("%T", err), "message": err.Error()}
}
func result(status int, value any, err error) (int, any) {
	if err != nil {
		return backendError(err)
	}
	return status, value
}
func writeJSON(writer http.ResponseWriter, status int, value any) {
	payload, _ := json.Marshal(value)
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(writer, request)
	})
}
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
