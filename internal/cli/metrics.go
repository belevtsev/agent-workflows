package cli

import (
	"errors"
	"fmt"

	"github.com/belevtsev/agent-workflows/internal/outcomes"
	"github.com/spf13/cobra"
)

func addMetricsCommand(root *cobra.Command, config Config, _ options) {
	metrics := &cobra.Command{Use: "metrics", Short: "Summarize explicit local workflow observations without writes", Args: func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usage(fmt.Errorf("unknown metrics command: %s", args[0]))
		}
		return nil
	}, RunE: func(command *cobra.Command, _ []string) error { return command.Help() }}
	metrics.AddCommand(&cobra.Command{Use: "schema", Short: "Print the versioned observation JSON Schema and example", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return usage(errors.New("metrics schema accepts no arguments"))
		}
		return nil
	}, RunE: func(command *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(command.OutOrStdout(), string(outcomes.Schema()))
		return err
	}})
	var inputRoot, baseline, candidate string
	summarize := &cobra.Command{Use: "summarize FILE...", Short: "Aggregate valid measurements and compare matching baseline/candidate cases as JSON", Args: func(_ *cobra.Command, args []string) error {
		if len(args) < 1 || len(args) > outcomes.MaxFiles {
			return usage(fmt.Errorf("metrics summarize requires between 1 and %d observation files", outcomes.MaxFiles))
		}
		return nil
	}, RunE: func(command *cobra.Command, args []string) error {
		path := inputRoot
		if path == "" {
			var err error
			path, err = config.WorkingDir()
			if err != nil {
				return err
			}
		}
		report, err := outcomes.Summarize(command.Context(), outcomes.Options{InputRoot: path, Files: args, Baseline: baseline, Candidate: candidate})
		if writeErr := writeJSON(command.OutOrStdout(), report); writeErr != nil {
			return errors.Join(err, writeErr)
		}
		return err
	}}
	summarize.Flags().StringVar(&inputRoot, "input-root", "", "Read-only input boundary (defaults to working directory)")
	summarize.Flags().StringVar(&baseline, "baseline", "baseline", "Baseline variant name for paired comparisons")
	summarize.Flags().StringVar(&candidate, "candidate", "candidate", "Candidate variant name for paired comparisons")
	metrics.AddCommand(summarize)
	root.AddCommand(metrics)
}
