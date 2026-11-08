package cli

import (
	"errors"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// addTaskTimelineCommand adds `cw task timeline` to the task command that
// addTaskCommands registered on root.
func addTaskTimelineCommand(root *cobra.Command) {
	var task *cobra.Command
	for _, command := range root.Commands() {
		if command.Name() == "task" {
			task = command
		}
	}
	if task == nil {
		return
	}
	var directory string
	var summary bool
	timeline := &cobra.Command{
		Use:   "timeline",
		Short: "Report the wall time of each worker assignment and check run of a task",
		Long: "Read a task record and print the time each worker assignment took from assignment to handoff and from handoff to acceptance, " +
			"the duration of each check run, and the task's elapsed time from creation to its last stored revision (updated_at), not to now. " +
			"A timestamp the record does not hold, and every duration computed from one, is n/a. The command is read-only.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return usage(err)
			}
			return nil
		},
		// The command only reads, so the global --dry-run changes nothing.
		RunE: func(cmd *cobra.Command, _ []string) error {
			if directory == "" {
				return usage(errors.New("--task DIR is required"))
			}
			record, err := taskflow.Load(directory)
			if err != nil {
				return err
			}
			report := taskflow.BuildTimeline(record)
			if !summary {
				return writeJSON(cmd.OutOrStdout(), report)
			}
			var lines strings.Builder
			for _, line := range report.SummaryLines() {
				lines.WriteString(line)
				lines.WriteByte('\n')
			}
			_, err = cmd.OutOrStdout().Write([]byte(lines.String()))
			return err
		},
	}
	timeline.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	timeline.Flags().BoolVar(&summary, "summary", false, "Print JSON Lines instead of one JSON object: one compact object for the task, then one per entry")
	task.AddCommand(timeline)
}
