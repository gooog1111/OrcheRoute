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
