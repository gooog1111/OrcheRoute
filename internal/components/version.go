package components

import (
	"strings"

	"golang.org/x/mod/semver"
)

// IsNewerVersion is shared by component discovery and every platform UI API.
// Equality and stale release metadata must never expose an update action.
func IsNewerVersion(installed, candidate string) bool {
	left := "v" + strings.TrimPrefix(strings.TrimSpace(installed), "v")
	right := "v" + strings.TrimPrefix(strings.TrimSpace(candidate), "v")
	return semver.IsValid(left) && semver.IsValid(right) && semver.Compare(right, left) > 0
}
