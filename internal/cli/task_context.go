package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskContextCommand returns cw task context.
func newTaskContextCommand(deps taskDeps) *cobra.Command {
	var directory, phase string
	command := &cobra.Command{Use: "context", Short: "Emit the exact before/final source context, with the task's adviser binding, for shared adviser state", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		value, err := taskflow.PrepareContext(cmd.Context(), directory, phase)
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	command.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	command.Flags().StringVar(&phase, "phase", "", "before or final")
	return command
}
