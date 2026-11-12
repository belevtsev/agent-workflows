package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
)

// newTaskAbandonCommand returns cw task abandon.
func newTaskAbandonCommand(deps taskDeps) *cobra.Command {
	var directory, reason string
	var summary bool
	abandon := &cobra.Command{
		Use:   "abandon",
		Short: "Mark a task given up with a terminal revision, so it no longer gates git commit and push",
		Long: "Append the record's last revision, which stores the reason and time it was abandoned. " +
			"An abandoned record accepts no further revisions and never gates publication; start a new task to continue the work.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.guard(); err != nil {
				return err
			}
			if err := requireTask(directory); err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return usage(errors.New("--reason TEXT is required"))
			}
			record, err := taskEnv(deps.config).Abandon(directory, reason)
			if err != nil {
				return err
			}
			if summary {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"task": record.Directory, "task_id": record.ID, "status": "abandoned"})
			}
			return writeJSON(cmd.OutOrStdout(), record)
		},
	}
	taskFlag(abandon, &directory)
	abandon.Flags().StringVar(&reason, "reason", "", "Why the task was given up")
	summaryFlag(abandon, &summary)
	return abandon
}
