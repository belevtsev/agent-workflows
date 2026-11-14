package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// writeBudgetStop explains new work refused at the budget's stop, when err is
// that refusal, and reports whether it was: the refusal, the task receipt,
// the question for the owner and the way on.
func writeBudgetStop(w io.Writer, directory string, err error) bool {
	over, ok := errors.AsType[taskflow.ErrBudgetStop](err)
	if !ok {
		return false
	}
	// err names every limit the brief goes past, the round cap too.
	fmt.Fprintf(w, "agent-workflows: %s; cw wrote nothing.\n", err)
	if !strings.Contains(err.Error(), over.Error()) {
		// Past several limits the first line only names them.
		fmt.Fprintf(w, "The cost budget: %s\n", over)
	}
	writeRoundNeeds(w, err)
	writeReceipt(w, directory)
	fmt.Fprintln(w, "Ask the owner: continue with a higher budget, or stop with the partial result?")
	if over.Final() {
		fmt.Fprintln(w, "The task has used its whole budget: to continue, start a new task with a higher budget.")
	} else {
		fmt.Fprintln(w, `To continue in this task, a person must give the words: only then pass them, unchanged, on this cw task assign or cw task run as --owner-approval "cost-budget=<the owner's words>"; cw records them on the task, which raises the stop to 100% of the budget once.`)
	}
	fmt.Fprintln(w, "To stop, cw task verify, handoff, accept, show and abandon still run; no check runs again, so verify credits only check runs made before the stop or reused on unchanged source.")
	return true
}
