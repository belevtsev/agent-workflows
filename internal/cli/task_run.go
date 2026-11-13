package cli

import "github.com/spf13/cobra"

// newTaskRunCommand returns cw task run. It prints the check run whenever one
// was recorded, then returns the run's error. When the check's latest passed
// run is reusable it runs nothing and prints that run's summary with status
// reused, also under --full; --force or CW_NO_REUSE=1 always runs. A task at
// its budget's stop runs nothing and exits 4 with the question for the owner
// (writeBudgetStop) unless --owner-approval carries the owner's cost-budget
// words.
func newTaskRunCommand(deps taskDeps) *cobra.Command {
	var directory, check string
	var full, force bool
	var ownerApprovals []string
	run := &cobra.Command{Use: "run", Short: "Execute one planned argv check with bounded private logs", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		env := taskEnv(deps.config)
		value, reused, err := env.RunApprovedCheck(cmd.Context(), directory, check, !force, ownerApprovals)
		if writeBudgetStop(cmd.ErrOrStderr(), directory, err) {
			return reportedExit(ExitRefused)
		}
		if value.ID != "" {
			var printed any = value
			if !full || reused != "" {
				summary := runSummary(directory, value)
				if reused != "" {
					summary.Status, summary.Reason = "reused", reused
				}
				printed = summary
			}
			if outputErr := writeJSON(cmd.OutOrStdout(), printed); outputErr != nil {
				return outputErr
			}
		}
		return err
	}}
	taskFlag(run, &directory)
	run.Flags().StringVar(&check, "check", "", "Check ID from the immutable explicit plan or added by 'cw task add-checks'")
	fullFlag(run, &full, "the whole check run record (a reused run prints its summary with status reused)")
	run.Flags().StringArrayVar(&ownerApprovals, "owner-approval", nil, "The owner's own words approving a check run past the budget's stop, as cost-budget=TEXT or TEXT; "+
		"a person must give them, and cw records them on the task. A run with them is never reused")
	run.Flags().BoolVar(&force, "force", false, "Run the check even when its latest passed run is reusable (CW_NO_REUSE=1 does the same)")
	return run
}
