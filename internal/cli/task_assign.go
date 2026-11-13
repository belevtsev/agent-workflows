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
	var directory, inputFile string
	var ownerApprovals []string
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
		value, err := taskEnv(deps.config).AssignApprovedWorker(cmd.Context(), directory, input, ownerApprovals)
		if errors.Is(err, taskflow.ErrOverBudget) {
			if !writeBudgetStop(cmd.ErrOrStderr(), directory, err) {
				writeOverBudget(cmd.ErrOrStderr(), err)
			}
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
	assign.Flags().StringArrayVar(&ownerApprovals, "owner-approval", nil, "The owner's own words approving a limit the brief goes past, such as a round past the stage's cap; "+
		"a person must give them, and cw records them on the assignment. Past several limits, give it once per limit as LIMIT=TEXT, such as round-budget=TEXT")
	fullFlag(assign, &full, "the whole task record")
	return assign
}

// writeOverBudget explains a brief refused at a limit of the profile: what a
// round past its cap would answer and how the owner approves it.
func writeOverBudget(w io.Writer, err error) {
	fmt.Fprintf(w, "agent-workflows: %s; cw wrote nothing.\n", err)
	writeRoundNeeds(w, err)
	fmt.Fprintln(w, `Ask the owner whether to go on. A person must give the words: only then pass them, unchanged, as --owner-approval "<the owner's words>".`)
}

// writeRoundNeeds lists what a round refused past its cap would answer.
func writeRoundNeeds(w io.Writer, err error) {
	if over, ok := errors.AsType[taskflow.ErrRoundBudget](err); ok && len(over.Needs) > 0 {
		fmt.Fprintln(w, "The round would answer:")
		for _, need := range over.Needs {
			fmt.Fprintf(w, "  - %s\n", need)
		}
	}
}
