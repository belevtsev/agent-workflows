package cli

import (
	"errors"
	"fmt"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskShowCommand returns cw task show. Without --field or --full it prints
// the show summary, whose size does not grow with every check run. It only
// reads.
func newTaskShowCommand(deps taskDeps) *cobra.Command {
	var directory string
	var fields []string
	var full bool
	show := &cobra.Command{Use: "show", Short: "Read a task and validate its immutable revision chain", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		if full && len(fields) > 0 {
			return usage(errors.New("--full and --field cannot be combined"))
		}
		for _, path := range fields {
			if !validFieldPath(path) {
				return usage(fmt.Errorf("--field %q must be a nonempty dotted path without empty segments", path))
			}
		}
		value, err := taskflow.Load(directory)
		if err != nil {
			return err
		}
		if len(fields) > 0 {
			selected, err := selectFields(value, fields)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), selected)
		}
		if full {
			return writeJSON(cmd.OutOrStdout(), value)
		}
		summary, err := showSummary(value)
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), summary)
	}}
	taskFlag(show, &directory)
	show.Flags().StringArrayVar(&fields, "field", nil, "Print only this dotted path of the record (repeatable); a numeric segment indexes an array, any other selects the latest element with that id, assignment id or check id")
	show.Flags().BoolVar(&full, "full", false, "Print the whole task record instead of the summary and its field list")
	return show
}
