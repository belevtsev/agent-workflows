// Package langpack holds the language-specific evidence rules that decide
// what a changed task must have executed before it completes. The task core
// stays language-neutral: it describes the changed source and the passed
// check runs, and asks Evaluate to apply the rule ids the task recorded.
package langpack

// Source describes the selected project at the current source. Paths are
// slash-separated and relative to the selected path.
type Source struct {
	// Current lists every eligible file now present.
	Current []string
	// Changed lists every file added, modified or deleted since the task
	// baseline; nil when the baseline manifest is unavailable.
	Changed []string
}

// Run is a check run that passed at the current source: the latest run of
// its check.
type Run struct {
	Argv []string
	// GOFLAGS is the GOFLAGS of the run's environment; empty when unset or
	// when the run was recorded before cw recorded it.
	GOFLAGS string
	// CheckID is the planned check the run belongs to; "" when unknown.
	CheckID string
	// Log is the run's stored output, possibly only its start (see
	// Truncated); nil when it was not read, could not be read, or does not
	// match the run's recorded digest.
	Log []byte
	// Truncated reports that the output was longer than the stored log.
	Truncated bool
	// Rerun reports that an earlier run of the same check failed with an
	// exit code at the same before-snapshot and the same recorded GOFLAGS.
	Rerun bool
}

// AnyPassedRun requires at least one passed check run at the current source.
// It is the only rule for a project no language pack recognises.
type AnyPassedRun struct{}

func (AnyPassedRun) Problems(_ Source, passed []Run) []string {
	if len(passed) == 0 {
		return []string{"changed substantial task has no passed check run at the current source"}
	}
	return nil
}
