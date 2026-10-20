package serverruntime

import "fmt"

func optionalLANPermission(body map[string]any) (*bool, error) {
	value, exists := body["allow_lan"]
	if !exists || value == nil {
		return nil, nil
	}
	allowed, ok := value.(bool)
	if !ok {
		return nil, fmt.Errorf("invalid_client_allow_lan")
	}
	return &allowed, nil
}
