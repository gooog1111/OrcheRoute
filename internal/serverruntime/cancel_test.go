package serverruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCancelSubscriptionUpdateIsCooperativeAndIdempotent(t *testing.T) {
	runtime := cleanTestRuntime(t)
	operationPath := filepath.Join(runtime.Config.StateDirectory, "update-operation.json")
	if err := atomicJSON(operationPath, map[string]any{
		"kind": "subscription_update", "status": "running", "phase": "url_test", "current": 80, "total": 500,
	}); err != nil {
		t.Fatal(err)
	}
	status, payload := runtime.cancelSubscriptionUpdate()
	if status != 202 || payload.(map[string]any)["accepted"] != true {
		t.Fatalf("status=%d payload=%#v", status, payload)
	}
	if _, err := os.Stat(filepath.Join(runtime.Config.StateDirectory, "update-cancel.request")); err != nil {
		t.Fatalf("cancel request missing: %v", err)
	}
	operation := runtime.operations()["subscription_update"].(map[string]any)
	if operation["status"] != "cancelling" || operation["phase"] != "cancelling" {
		t.Fatalf("operation=%#v", operation)
	}
	status, payload = runtime.cancelSubscriptionUpdate()
	if status != 202 || payload.(map[string]any)["already_cancelling"] != true {
		t.Fatalf("second status=%d payload=%#v", status, payload)
	}
}

func TestCancelSubscriptionUpdateWhenIdle(t *testing.T) {
	runtime := cleanTestRuntime(t)
	status, payload := runtime.cancelSubscriptionUpdate()
	if status != 200 || payload.(map[string]any)["accepted"] != false {
		t.Fatalf("status=%d payload=%#v", status, payload)
	}
}
