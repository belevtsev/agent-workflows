package cli

import (
	"errors"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskTimelineCommand returns cw task timeline.
func newTaskTimelineCommand(taskDeps) *cobra.Command {
	var directory string
	var summary bool
	var last int
	timeline := &cobra.Command{
		Use:   "timeline",
		Short: "Report the wall time of each worker assignment and check run of a task",
		Long: "Read a task record and print the time each worker assignment took from assignment to handoff and from handoff to acceptance, " +
			"the duration of each check run, and the task's elapsed time from creation to its last stored revision (updated_at), not to now. " +
			"A timestamp the record does not hold, and every duration computed from one, is n/a. " +
			"It lists the last 20 entries by start time and counts the earlier ones as omitted_entries; --last 0 lists every entry. The command is read-only.",
		Args: noPositional,
		// The command only reads, so the global --dry-run changes nothing.
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireTask(directory); err != nil {
				return err
			}
			if last < 0 {
				return usage(errors.New("--last N must be 0 or more"))
			}
			record, err := taskflow.Load(directory)
			if err != nil {
				return err
			}
			report := taskflow.BuildTimeline(record).Last(last)
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
	taskFlag(timeline, &directory)
	timeline.Flags().BoolVar(&summary, "summary", false, "Print JSON Lines instead of one JSON object: one compact object for the task, then one per entry")
	timeline.Flags().IntVar(&last, "last", defaultTimelineEntries, "List only the last N entries by start time; 0 lists every entry")
	return timeline
}

// defaultTimelineEntries bounds the entries cw task timeline lists by default.
const defaultTimelineEntries = 20
