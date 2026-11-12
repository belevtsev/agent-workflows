package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskAcceptCommand returns cw task accept.
func newTaskAcceptCommand(deps taskDeps) *cobra.Command {
	var directory, inputFile string
	var full bool
	accept := &cobra.Command{Use: "accept", Short: "Record the coordinator's source-backed disposition of an exact worker result", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := requireTask(directory); err != nil {
			return err
		}
		var input taskflow.WorkerAcceptance
		if err := readTaskInput(cmd, inputFile, &input); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).AcceptWorkerResult(cmd.Context(), directory, input)
		if err != nil {
			return err
		}
		if !full {
			return writeJSON(cmd.OutOrStdout(), acceptSummary(value, input.AssignmentID))
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	taskFlag(accept, &directory)
	accept.Flags().StringVar(&inputFile, "input", "", "Coordinator acceptance JSON file, or - for standard input")
	fullFlag(accept, &full, "the whole task record")
	return accept
}
