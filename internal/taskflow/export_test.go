package taskflow

import (
	"maps"
	"slices"
	"testing"
)

// BuiltInProfileNames returns the names of the built-in profiles, sorted,
// so a test can compare the whole built-in set with the documented one.
func BuiltInProfileNames() []string {
	return slices.Sorted(maps.Keys(builtInProfiles))
}

// DropProfile rewrites the only revision of the task in directory without
// its profile, as a record started before profiles existed.
func DropProfile(t *testing.T, directory string) {
	t.Helper()
	record := mustLoad(t, directory)
	if record.Revision != 1 {
		t.Fatalf("DropProfile needs a task with one revision, got %d", record.Revision)
	}
	rewriteInitialRevision(t, record, func(r *Record) { r.Profile, r.Plan.Profile = nil, "" })
}
