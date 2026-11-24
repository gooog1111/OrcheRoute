package mihomo

import "fmt"

// ExcludeCallServerIngress prevents the host client from recapturing the
// VPN-server tunnel. It preserves all unrelated configuration and exclusions.
func ExcludeCallServerIngress(config map[string]any) (bool, error) {
	tun, ok := config["tun"].(map[string]any)
	if !ok || tun["device"] != "orcheroute0" {
		return false, nil
	}
	stringsList := func(value any) ([]string, error) {
		switch list := value.(type) {
		case nil:
			return nil, nil
		case []string:
			return append([]string(nil), list...), nil
		case []any:
			result := make([]string, 0, len(list))
			for _, raw := range list {
				item, ok := raw.(string)
				if !ok {
					return nil, fmt.Errorf("invalid_tun_interface_list")
				}
				result = append(result, item)
			}
			return result, nil
		default:
			return nil, fmt.Errorf("invalid_tun_interface_list")
		}
	}
	excluded, err := stringsList(tun["exclude-interface"])
	if err != nil {
		return false, err
	}
	included, err := stringsList(tun["include-interface"])
	if err != nil {
		return false, err
	}
	kept := make([]string, 0, len(included))
	for _, name := range included {
		if name != "orchecall0" {
			kept = append(kept, name)
		}
	}
	if len(included) > 0 && len(kept) == 0 {
		return false, fmt.Errorf("call_server_interface_cannot_be_host_capture")
	}
	changed := len(kept) != len(included)
	found := false
	for _, name := range excluded {
		found = found || name == "orchecall0"
	}
	if !found {
		excluded = append(excluded, "orchecall0")
		changed = true
	}
	if !changed {
		return false, nil
	}
	if len(kept) != len(included) {
		tun["include-interface"] = kept
	}
	tun["exclude-interface"] = excluded
	return true, nil
}
