package cli

import (
	"errors"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// addTaskAbandonCommand adds `cw task abandon` to the task command that
// addTaskCommands registered on root.
func addTaskAbandonCommand(root *cobra.Command) {
	var task *cobra.Command
	for _, command := range root.Commands() {
		if command.Name() == "task" {
			task = command
		}
	}
	if task == nil {
		return
	}
	var directory, reason string
	var summary bool
	abandon := &cobra.Command{
		Use:   "abandon",
		Short: "Mark a task given up with a terminal revision, so it no longer gates git commit and push",
		Long: "Append the record's last revision, which stores the reason and time it was abandoned. " +
			"An abandoned record accepts no further revisions and never gates publication; start a new task to continue the work.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return usage(err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun, _ := root.PersistentFlags().GetBool("dry-run"); dryRun {
				return usage(errors.New("task state commands reject --dry-run; no files or checks were changed"))
			}
			if directory == "" {
				return usage(errors.New("--task DIR is required"))
			}
			if strings.TrimSpace(reason) == "" {
				return usage(errors.New("--reason TEXT is required"))
			}
			record, err := taskflow.Abandon(directory, reason)
			if err != nil {
				return err
			}
			if summary {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"task": record.Directory, "task_id": record.ID, "status": "abandoned"})
			}
			return writeJSON(cmd.OutOrStdout(), record)
		},
	}
	abandon.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	abandon.Flags().StringVar(&reason, "reason", "", "Why the task was given up")
	abandon.Flags().BoolVar(&summary, "summary", false, "Print a one-line summary instead of the whole task record")
	task.AddCommand(abandon)
}
