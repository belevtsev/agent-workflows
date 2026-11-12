package cli

import (
	"errors"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskAuthorshipCommand returns cw task authorship.
func newTaskAuthorshipCommand(taskDeps) *cobra.Command {
	var directory string
	authorship := &cobra.Command{
		Use:   "authorship",
		Short: "List each changed test file with its authorship status",
		Long: "Read the task and report each Go test file changed since the task's baseline, with its authorship status " +
			"(independent, source_aware, lost or unowned), the reason and the worker assignment the record names for it. " +
			"It changes nothing, works on an abandoned task and exits 0 whatever the statuses.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if directory == "" {
				return usage(errors.New("authorship needs --task"))
			}
			report, err := taskflow.Authorship(cmd.Context(), directory)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), report)
		},
	}
	authorship.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	return authorship
}
