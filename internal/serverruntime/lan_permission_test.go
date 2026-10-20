package serverruntime

import "testing"

func TestOptionalLANPermissionRejectsAmbiguousValues(t *testing.T) {
	for _, value := range []any{true, false, nil} {
		permission, err := optionalLANPermission(map[string]any{"allow_lan": value})
		if err != nil {
			t.Fatal(err)
		}
		if value == nil && permission != nil {
			t.Fatal("inherit was lost")
		}
		if expected, ok := value.(bool); ok && (permission == nil || *permission != expected) {
			t.Fatal("explicit permission was lost")
		}
	}
	if _, err := optionalLANPermission(map[string]any{"allow_lan": "false"}); err == nil {
		t.Fatal("string boolean accepted")
	}
}
