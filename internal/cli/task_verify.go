package cli

import "github.com/spf13/cobra"

// newTaskVerifyCommand returns cw task verify. It prints the verification
// whenever one was produced, then returns the verification's error.
func newTaskVerifyCommand(deps taskDeps) *cobra.Command {
	var directory string
	var full bool
	verify := &cobra.Command{Use: "verify", Short: "Gate completion against current source, requirements, checks and Coordinator decisions", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).Verify(cmd.Context(), directory)
		if value.TaskID != "" {
			var printed any = value
			if !full {
				printed = summarizeVerification(value)
			}
			if outputErr := writeJSON(cmd.OutOrStdout(), printed); outputErr != nil {
				return outputErr
			}
		}
		return err
	}}
	taskFlag(verify, &directory)
	fullFlag(verify, &full, "the whole verification, with every fixed limitation,")
	return verify
}
