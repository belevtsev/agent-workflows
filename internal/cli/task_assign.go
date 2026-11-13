package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskAssignCommand returns cw task assign.
func newTaskAssignCommand(deps taskDeps) *cobra.Command {
	var directory, inputFile, ownerApproval string
	var full bool
	assign := &cobra.Command{Use: "assign", Short: "Bind a worker brief and exclusive writer scope to the current source", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		var input taskflow.WorkerBrief
		if err := readTaskInput(cmd, inputFile, &input); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).AssignApprovedWorker(cmd.Context(), directory, input, ownerApproval)
		if over, ok := errors.AsType[taskflow.ErrRoundBudget](err); ok {
			writeRoundBudget(cmd.ErrOrStderr(), over)
			return reportedExit(ExitRefused)
		}
		if writeBudgetStop(cmd.ErrOrStderr(), directory, err) {
			return reportedExit(ExitRefused)
		}
		if err != nil {
			return err
		}
		if !full {
			return writeJSON(cmd.OutOrStdout(), assignSummary(value, input.ID))
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	taskFlag(assign, &directory)
	assign.Flags().StringVar(&inputFile, "input", "", "Worker brief JSON file, or - for standard input")
	assign.Flags().StringVar(&ownerApproval, "owner-approval", "", "The owner's own words approving a round past the stage's cap; a person must give them, and cw records them on the assignment")
	fullFlag(assign, &full, "the whole task record")
	return assign
}

// writeRoundBudget explains a round refused at its stage's cap: what the
// round would answer and how the owner approves it.
func writeRoundBudget(w io.Writer, over taskflow.ErrRoundBudget) {
	fmt.Fprintf(w, "agent-workflows: %s; cw wrote nothing.\n", over)
	if len(over.Needs) > 0 {
		fmt.Fprintln(w, "The round would answer:")
		for _, need := range over.Needs {
			fmt.Fprintf(w, "  - %s\n", need)
		}
	}
	fmt.Fprintln(w, `Ask the owner whether to spend another round. A person must give the words: only then pass them, unchanged, as --owner-approval "<the owner's words>".`)
}
