package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskHandoffCommand returns cw task handoff.
func newTaskHandoffCommand(deps taskDeps) *cobra.Command {
	var directory, inputFile string
	var summary bool
	handoff := &cobra.Command{Use: "handoff", Short: "Validate a worker result against its assignment, source and actual checks", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := requireTask(directory); err != nil {
			return err
		}
		var input taskflow.WorkerResult
		if err := readTaskInput(cmd, inputFile, &input); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).SubmitWorkerResult(cmd.Context(), directory, input)
		if err != nil {
			return err
		}
		if summary {
			return writeJSON(cmd.OutOrStdout(), handoffSummary(value, input.AssignmentID))
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	taskFlag(handoff, &directory)
	handoff.Flags().StringVar(&inputFile, "input", "", "Worker result JSON file, or - for standard input")
	summaryFlag(handoff, &summary)
	return handoff
}
