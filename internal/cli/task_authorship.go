package cli

import "github.com/spf13/cobra"

// newTaskAuthorshipCommand returns cw task authorship.
func newTaskAuthorshipCommand(deps taskDeps) *cobra.Command {
	var directory string
	authorship := &cobra.Command{
		Use:   "authorship",
		Short: "List each changed test file with its authorship status",
		Long: "Read the task and report each Go test file changed since the task's baseline, with its authorship status " +
			"(independent, source_aware, lost or unowned), the reason and the worker assignment the record names for it. " +
			"It changes nothing, works on an abandoned task and exits 0 whatever the statuses.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.requireTask(&directory); err != nil {
				return err
			}
			report, err := taskEnv(deps.config).Authorship(cmd.Context(), directory)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), report)
		},
	}
	taskFlag(authorship, &directory)
	return authorship
}
