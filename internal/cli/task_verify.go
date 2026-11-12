package cli

import "github.com/spf13/cobra"

// newTaskVerifyCommand returns cw task verify. It prints the verification
// whenever one was produced, then returns the verification's error.
func newTaskVerifyCommand(deps taskDeps) *cobra.Command {
	var directory string
	var summary bool
	verify := &cobra.Command{Use: "verify", Short: "Gate completion against current source, requirements, checks and Coordinator decisions", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).Verify(cmd.Context(), directory)
		if value.TaskID != "" {
			var printed any = value
			if summary {
				printed = summarizeVerification(value)
			}
			if outputErr := writeJSON(cmd.OutOrStdout(), printed); outputErr != nil {
				return outputErr
			}
		}
		return err
	}}
	verify.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	verify.Flags().BoolVar(&summary, "summary", false, "Print one line with every problem and only the task-specific limitations")
	return verify
}
