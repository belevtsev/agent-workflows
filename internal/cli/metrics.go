package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/internal/outcomes"
	"github.com/belevtsev/agent-workflows/internal/usagelog"
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
	metrics.AddCommand(importCommand(config))
	root.AddCommand(metrics)
}

// importCommand reports per-agent token usage and dollars from one session
// transcript. It reads only the named files (and, for Codex, rollout headers
// under the sessions root) and never writes.
func importCommand(config Config) *cobra.Command {
	var claudeTranscript, codexSession, codexRoot, pricesPath string
	command := &cobra.Command{
		Use:   "import (--claude-transcript FILE | --codex-session FILE) [--prices FILE] [--codex-sessions-root DIR]",
		Short: "Report per-agent token usage and dollars from a Claude transcript or Codex rollout as JSON",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usage(errors.New("metrics import accepts no positional arguments"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			if (claudeTranscript == "") == (codexSession == "") {
				return usage(errors.New("metrics import requires exactly one of --claude-transcript or --codex-session"))
			}
			if codexRoot != "" && codexSession == "" {
				return usage(errors.New("--codex-sessions-root applies only with --codex-session"))
			}
			// Relative paths resolve against the configured working directory.
			paths := []*string{&claudeTranscript, &codexSession, &codexRoot, &pricesPath}
			for _, path := range paths {
				if *path != "" && !filepath.IsAbs(*path) {
					dir, err := config.WorkingDir()
					if err != nil {
						return err
					}
					*path = filepath.Join(dir, *path)
				}
			}
			options := usagelog.Options{ClaudeTranscript: claudeTranscript, CodexSession: codexSession, CodexSessionsRoot: codexRoot}
			if pricesPath != "" {
				table, err := usagelog.LoadPriceTable(pricesPath)
				if err != nil {
					return err
				}
				options.Prices = &table
			}
			report, err := usagelog.Import(command.Context(), options)
			if err != nil {
				if errors.Is(err, usagelog.ErrInvalidOptions) {
					return usage(err)
				}
				return err
			}
			return writeJSON(command.OutOrStdout(), report)
		},
	}
	command.Flags().StringVar(&claudeTranscript, "claude-transcript", "", "Claude Code main session transcript (<session-id>.jsonl); subagents are read from <session-id>/subagents")
	command.Flags().StringVar(&codexSession, "codex-session", "", "Codex root rollout (rollout-*.jsonl); descendant agent rollouts are discovered under the sessions root")
	command.Flags().StringVar(&codexRoot, "codex-sessions-root", "", "Directory scanned for Codex child rollouts (defaults to the nearest ancestor named sessions)")
	command.Flags().StringVar(&pricesPath, "prices", "", "Price table JSON with per-million-token USD rates and evidence; dollars from it are reported as estimates")
	return command
}
