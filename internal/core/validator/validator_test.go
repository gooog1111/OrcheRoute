package validator

import "testing"

func TestQualificationPolicyRejectsInvalidInput(t *testing.T) {
	_, err := QualificationPolicy(map[string]any{"tcp_timeout_ms": -1})
	if err == nil {
		t.Fatal("invalid qualification policy was accepted")
	}
}

func TestSubscriptionValidationRemainsPure(t *testing.T) {
	payload := map[string]any{"name": "test", "group": "primary", "parser": "standard", "secret": "https://example.com/sub"}
	result, err := Subscription(payload, false)
	if err != nil {
		t.Fatal(err)
	}
	if result["name"] != "test" {
		t.Fatalf("validated payload = %#v", result)
	}
}

func TestQualificationPolicyLifecycleUsesOneContract(t *testing.T) {
	policy := DefaultQualificationPolicy()
	updated, err := UpdateQualificationPolicy(policy, map[string]any{"defaults": map[string]any{"tcp_timeout_ms": 1500}})
	if err != nil {
		t.Fatal(err)
	}
	validated, err := QualificationPolicy(MigrateQualificationPolicy(updated))
	if err != nil {
		t.Fatal(err)
	}
	defaults := validated["defaults"].(map[string]any)
	if defaults["tcp_timeout_ms"] != float64(1500) {
		t.Fatalf("tcp_timeout_ms=%v", defaults["tcp_timeout_ms"])
	}
	if _, err := QualificationURLTestURLs(validated); err != nil {
		t.Fatal(err)
	}
}
