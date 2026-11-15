package cli

import (
	"errors"
	"fmt"
)

// The exit statuses of kitsu. Every command uses this one table.
const (
	ExitOK          = 0 // kitsu did what was asked and the answer is yes
	ExitFailure     = 1 // kitsu could not do it
	ExitUsage       = 2 // the command line is wrong
	ExitDenied      = 3 // a hook denies
	ExitForeignHook = 4 // a hook of another tool is left for the user to edit
	ExitNotVerified = 5 // a check did not pass, or the content is not verified
)

// exitError carries an exit status. An empty message means the command has
// already printed everything it had to say.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// failf is a failure: kitsu could not do what was asked.
func failf(format string, args ...any) error {
	return &exitError{code: ExitFailure, msg: fmt.Sprintf(format, args...)}
}

// usagef is a wrong command line.
func usagef(format string, args ...any) error {
	return &exitError{code: ExitUsage, msg: fmt.Sprintf(format, args...)}
}

// exit ends a command with a status after the command printed its own text.
func exit(code int) error {
	if code == ExitOK {
		return nil
	}
	return &exitError{code: code}
}

// ExitCode is the exit status for the error a command returned.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	return ExitFailure
}
