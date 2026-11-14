package cli

import "github.com/spf13/cobra"

// newTaskContextCommand returns cw task context.
func newTaskContextCommand(deps taskDeps) *cobra.Command {
	var directory, phase string
	command := &cobra.Command{Use: "context", Short: "Emit the exact before/final source context, with the task's adviser binding, for shared adviser state", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).PrepareContext(cmd.Context(), directory, phase)
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	taskFlag(command, &directory)
	command.Flags().StringVar(&phase, "phase", "", "before or final")
	return command
}
