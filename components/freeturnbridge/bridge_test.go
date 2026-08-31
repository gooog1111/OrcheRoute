package freeturnbridge

import (
	"encoding/json"
	"testing"
)

func TestDefaultConfigAndIdleStateAreValidJSON(t *testing.T) {
	for name, value := range map[string]string{
		"config": DefaultConfigJSON(),
		"state":  StateJSON(),
	} {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(value), &decoded); err != nil {
			t.Fatalf("%s is not JSON: %v", name, err)
		}
	}
}

func TestInvalidConfigIsRejectedWithoutStartingTransport(t *testing.T) {
	if got := ValidateConfig(`{"clientId":""}`); got == "" {
		t.Fatal("invalid config was accepted")
	}
}
