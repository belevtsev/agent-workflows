package taskflow

import (
	"maps"
	"slices"
)

// BuiltInProfileNames returns the names of the built-in profiles, sorted,
// so a test can compare the whole built-in set with the documented one.
func BuiltInProfileNames() []string {
	return slices.Sorted(maps.Keys(builtInProfiles))
}
