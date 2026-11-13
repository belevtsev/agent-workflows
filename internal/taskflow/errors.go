package taskflow

import (
	"errors"
	"fmt"
)

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

// Refusals: cw wrote nothing because the record's state or the caller's input
// does not allow the operation. Callers tell them from an I/O or storage
// failure with errors.Is, however deeply they are wrapped. Each keeps the
// message it had before it was typed; cw exits with its failure status for
// them (docs/operations.md#exit-statuses).
var (
	// ErrTaskLocked: another writer holds the task, or a .lock directory of
	// cw v3.0.0-rc.1 stays, for longer than the lock wait.
	ErrTaskLocked = errors.New("task is locked; inspect an interrupted writer before removing its .lock directory")
	// ErrAbandoned: the task was abandoned, so no revision follows.
	ErrAbandoned = errors.New("task was abandoned; start a new task with 'cw task start'")
	// ErrRevisionLimit: the task has no revision left for the operation.
	ErrRevisionLimit = errors.New("task revision limit reached; start a new task")
	// ErrInvalidInput: the caller's input, such as a plan, an assessment, a
	// worker brief, result or acceptance, a check amendment, a phase or a
	// reason, is malformed or names what the task does not hold. The same
	// input is refused again; fix it. A stored record that fails the same
	// rules is damaged, not invalid input, and does not match.
	ErrInvalidInput = errors.New("invalid input")
)

// ErrOverBudget: the operation would go past a limit of the task's profile,
// such as a stage's author rounds (ErrRoundBudget), and carries no owner
// approval. cw wrote nothing; cw exits with its refused status for it.
var ErrOverBudget = errors.New("task budget exceeded; the operation needs the owner's approval")

// kindError is an error with its own message that matches kind with
// errors.Is, and cause too when it has one, so a refusal is typed without
// rewording it.
type kindError struct {
	kind  error
	msg   string
	cause error
}

func (e kindError) Error() string { return e.msg }
func (e kindError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

// invalid is ErrInvalidInput with message msg.
func invalid(msg string) error { return kindError{kind: ErrInvalidInput, msg: msg} }

// invalidf is ErrInvalidInput with the message fmt.Errorf formats; a %w
// operand stays matchable.
func invalidf(format string, args ...any) error {
	return asInvalid(fmt.Errorf(format, args...))
}

// asInvalid types err, the refusal of a validator of caller input, as
// ErrInvalidInput with its message unchanged; nil stays nil.
func asInvalid(err error) error {
	if err == nil || errors.Is(err, ErrInvalidInput) {
		return err
	}
	return kindError{kind: ErrInvalidInput, msg: err.Error(), cause: err}
}
