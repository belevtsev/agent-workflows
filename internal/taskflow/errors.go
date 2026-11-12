package taskflow

import "errors"

// Outcomes that are answers, not failures: the command worked and the answer
// is no. Callers tell them from a failure with errors.Is; cw exits with its
// negative-result status for them.
var (
	// ErrIncomplete is Verify's error when the task's completion evidence is
	// incomplete. The returned Verification lists the problems.
	ErrIncomplete = errors.New("task completion evidence is incomplete; inspect problems")
	// ErrCheckFailed is RunCheck's error, wrapped with the run's status, when the
	// check ran and its recorded terminal status is not passed. A check that
	// passed but whose log or test results cw could not write is recorded as
	// failed too, yet RunCheck returns a plain error for it: cw failed, not the
	// check.
	ErrCheckFailed = errors.New("check did not pass")
)
