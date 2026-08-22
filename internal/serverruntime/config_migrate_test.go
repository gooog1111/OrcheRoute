package serverruntime

import "testing"

func TestAddWhitelistProviderMigratesExistingConfigOnce(t *testing.T) {
	input := map[string]any{
		"proxy-providers": map[string]any{"emergency": map[string]any{"type": "file", "path": "/state/providers/emergency.json"}},
		"proxy-groups":    []any{map[string]any{"name": "ACTIVE", "use": []any{"primary", "emergency"}}},
	}
	result, changed, err := addWhitelistProvider(input)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	providers := result["proxy-providers"].(map[string]any)
	if stringValue(providers["whitelist"].(map[string]any)["path"]) != "/state/providers/whitelist.json" {
		t.Fatalf("provider=%#v", providers)
	}
	groups := result["proxy-groups"].([]any)
	uses := groups[0].(map[string]any)["use"].([]any)
	if len(uses) != 3 || stringValue(uses[2]) != "whitelist" || stringValue(groups[1].(map[string]any)["name"]) != "PROBE-WHITELIST" {
		t.Fatalf("groups=%#v", groups)
	}
	second, changedAgain, err := addWhitelistProvider(result)
	if err != nil || changedAgain || second == nil {
		t.Fatalf("second migration changed=%v err=%v", changedAgain, err)
	}
}
