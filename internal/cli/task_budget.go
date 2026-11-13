package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// writeBudgetStop explains new work refused at the budget's stop, when err is
// that refusal, and reports whether it was: the refusal, the task's usage,
// the question for the owner and the way on. A failure to read the usage
// only leaves it out.
func writeBudgetStop(w io.Writer, directory string, err error) bool {
	over, ok := errors.AsType[taskflow.ErrBudgetStop](err)
	if !ok {
		return false
	}
	// err names every limit the brief goes past, the round cap too.
	fmt.Fprintf(w, "agent-workflows: %s; cw wrote nothing.\n", err)
	writeRoundNeeds(w, err)
	// TODO(T1): print the task receipt here once cw task receipt exists;
	// until then the usage summary stands in for it.
	if record, loadErr := taskflow.Load(directory); loadErr == nil {
		fmt.Fprint(w, "Task usage (dollars are estimates): ")
		_ = writeJSON(w, taskflow.UsageOf(record))
	}
	fmt.Fprintln(w, "Ask the owner: continue with a higher budget, or stop with the partial result?")
	if over.Final() {
		fmt.Fprintln(w, "The task has used its whole budget: to continue, start a new task with a higher budget.")
	} else {
		fmt.Fprintln(w, `To continue in this task, a person must give the words: only then pass them, unchanged, on the next cw task assign as --owner-approval "cost-budget=<the owner's words>", which raises the stop to 100% of the budget once.`)
	}
	fmt.Fprintln(w, "To stop, cw task verify, handoff, accept, show and abandon still run.")
	return true
}
