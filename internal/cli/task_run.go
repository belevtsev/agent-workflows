package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskRunCommand returns cw task run. It prints the check run whenever one
// was recorded, then returns the run's error. When the check's latest passed
// run is reusable it runs nothing and prints that run's summary with status
// reused, also under --full; --force or CW_NO_REUSE=1 always runs. A task at
// its budget's stop runs nothing and exits 4 with the question for the owner
// (writeBudgetStop).
func newTaskRunCommand(deps taskDeps) *cobra.Command {
	var directory, check string
	var full, force bool
	run := &cobra.Command{Use: "run", Short: "Execute one planned argv check with bounded private logs", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		env := taskEnv(deps.config)
		var value taskflow.CheckRun
		var reused string
		var err error
		if force {
			value, err = env.RunCheck(cmd.Context(), directory, check)
		} else {
			value, reused, err = env.RunOrReuseCheck(cmd.Context(), directory, check)
		}
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
	run.Flags().BoolVar(&force, "force", false, "Run the check even when its latest passed run is reusable (CW_NO_REUSE=1 does the same)")
	return run
}
