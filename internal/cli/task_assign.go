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
			writeOverBudget(cmd.ErrOrStderr(), err)
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
	assign.Flags().StringArrayVar(&ownerApprovals, "owner-approval", nil, "The owner's own words approving a limit the brief goes past: a round past the stage's cap (round-budget) or a third fix round on an open finding or requirement (escalation); "+
		"a person must give them, and cw records them on the assignment. Past several limits, give it once per limit as LIMIT=TEXT, such as round-budget=TEXT")
	fullFlag(assign, &full, "the whole task record")
	return assign
}

// writeOverBudget explains a brief refused at a limit of the profile: what a
// round past its cap would answer, each earlier fix round on a finding or
// requirement still open with the owner's two options, and how the owner
// approves.
func writeOverBudget(w io.Writer, err error) {
	fmt.Fprintf(w, "agent-workflows: %s; cw wrote nothing.\n", err)
	if over, ok := errors.AsType[taskflow.ErrRoundBudget](err); ok && len(over.Needs) > 0 {
		fmt.Fprintln(w, "The round would answer:")
		for _, need := range over.Needs {
			fmt.Fprintf(w, "  - %s\n", need)
		}
	}
	if escalation, ok := errors.AsType[taskflow.ErrEscalation](err); ok {
		fmt.Fprintln(w, "Earlier fix rounds:")
		for _, round := range escalation.History {
			fmt.Fprintf(w, "  - %s: %s\n", round.Assignment, round.Summary)
		}
		fmt.Fprintln(w, "The owner chooses:")
		fmt.Fprintln(w, "  - approve one more fix round, with the owner's words as below;")
		fmt.Fprintln(w, "  - or change the approach: revise the design or the plan instead of fixing the same thing again.")
	}
	fmt.Fprintln(w, `Ask the owner whether to go on. A person must give the words: only then pass them, unchanged, as --owner-approval "<the owner's words>".`)
}
