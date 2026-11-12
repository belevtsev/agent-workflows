package cli

import "github.com/spf13/cobra"

// newTaskRunCommand returns cw task run. It prints the check run whenever one
// was recorded, then returns the run's error.
func newTaskRunCommand(deps taskDeps) *cobra.Command {
	var directory, check string
	var summary bool
	run := &cobra.Command{Use: "run", Short: "Execute one planned argv check with bounded private logs", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).RunCheck(cmd.Context(), directory, check)
		if value.ID != "" {
			var printed any = value
			if summary {
				printed = runSummary(directory, value)
			}
			if outputErr := writeJSON(cmd.OutOrStdout(), printed); outputErr != nil {
				return outputErr
			}
		}
		return err
	}}
	run.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	run.Flags().StringVar(&check, "check", "", "Check ID from the immutable explicit plan or added by 'cw task add-checks'")
	summaryFlag(run, &summary)
	return run
}
