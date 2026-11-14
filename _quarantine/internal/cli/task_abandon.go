package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"

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
			"An abandoned record accepts no further revisions and never gates publication; start a new task to continue the work. " +
			"The task receipt (cw task receipt) follows on stderr.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.requireTask(&directory); err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return usage(errors.New("--reason TEXT is required"))
			}
			record, err := taskEnv(deps.config).Abandon(directory, reason)
			if err != nil {
				return err
			}
			printed := any(record)
			if summary {
				printed = map[string]string{"task_id": record.ID, "status": "abandoned"}
			}
			if err := writeJSON(cmd.OutOrStdout(), printed); err != nil {
				return err
			}
			// The receipt goes to stderr, so stdout keeps one JSON document.
			fmt.Fprint(cmd.ErrOrStderr(), taskflow.Receipt(record).Text())
			return nil
		},
	}
	taskFlag(abandon, &directory)
	abandon.Flags().StringVar(&reason, "reason", "", "Why the task was given up")
	abandon.Flags().BoolVar(&summary, "summary", false, "Print a one-line summary instead of the whole task record")
	return abandon
}
