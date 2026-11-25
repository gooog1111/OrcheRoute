package mihomo

import "fmt"

// CaptureCallServerIngress shares the host client's routing policy with inbound
// VPN clients. It also repairs the exclusion introduced by server 0.8.9.
func CaptureCallServerIngress(config map[string]any) (bool, error) {
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
	kept := make([]string, 0, len(excluded))
	for _, name := range excluded {
		if name != "orchecall0" {
			kept = append(kept, name)
		}
	}
	changed := len(kept) != len(excluded)
	found := false
	for _, name := range included {
		found = found || name == "orchecall0"
	}
	if len(included) > 0 && !found {
		included = append(included, "orchecall0")
		changed = true
	}
	if !changed {
		return false, nil
	}
	if len(kept) != len(excluded) {
		if len(kept) == 0 {
			delete(tun, "exclude-interface")
		} else {
			tun["exclude-interface"] = kept
		}
	}
	if len(included) > 0 {
		tun["include-interface"] = included
	}
	return true, nil
}
