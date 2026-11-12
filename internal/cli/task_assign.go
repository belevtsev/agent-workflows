package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskAssignCommand returns cw task assign.
func newTaskAssignCommand(deps taskDeps) *cobra.Command {
	var directory, inputFile string
	var summary bool
	assign := &cobra.Command{Use: "assign", Short: "Bind a worker brief and exclusive writer scope to the current source", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		var input taskflow.WorkerBrief
		if err := readTaskInput(cmd, inputFile, &input); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).AssignWorker(cmd.Context(), directory, input)
		if err != nil {
			return err
		}
		if summary {
			return writeJSON(cmd.OutOrStdout(), assignSummary(value, input.ID))
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	assign.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	assign.Flags().StringVar(&inputFile, "input", "", "Worker brief JSON file, or - for standard input")
	summaryFlag(assign, &summary)
	return assign
}
