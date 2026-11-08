package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/v3/internal/outcomes"
	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
	"github.com/spf13/cobra"
)

func addMetricsCommand(root *cobra.Command, config Config) {
	metrics := &cobra.Command{Use: "metrics", Short: "Summarize explicit local workflow observations; only import --append writes, to the cost history", Args: func(_ *cobra.Command, args []string) error {
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
	metrics.AddCommand(historyCommand(config))
	root.AddCommand(metrics)
}

// importCommand reports per-agent token usage and dollars from one session
// transcript. It reads only the named files (and, for Codex, rollout headers
// under the sessions root); with --append it also appends the session's
// per-request token counts to the cost history, and writes nothing else.
func importCommand(config Config) *cobra.Command {
	var claudeTranscript, codexSession, codexRoot, pricesPath, historyPath string
	var composition, summary, appendHistory bool
	command := &cobra.Command{
		Use:   "import (--claude-transcript FILE | --codex-session FILE) [--codex-sessions-root DIR] ([--prices FILE] [--composition [--summary]] | --append [--history FILE])",
		Short: "Report per-agent token usage and dollars from a Claude transcript or Codex rollout as JSON, or append it to the cost history",
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
			if historyPath != "" && !appendHistory {
				return usage(errors.New("--history applies only with --append"))
			}
			if appendHistory && (composition || summary || pricesPath != "") {
				return usage(errors.New("--append stores token counts only and takes no --prices, --composition or --summary; pass --prices to cw metrics history"))
			}
			if composition && pricesPath == "" {
				return usage(errors.New("--composition requires --prices: its totals are reconciled with the recorded cost"))
			}
			if summary && !composition {
				return usage(errors.New("--summary applies only with --composition"))
			}
			// Relative paths resolve against the configured working directory.
			if err := absolutePaths(config, &claudeTranscript, &codexSession, &codexRoot, &pricesPath, &historyPath); err != nil {
				return err
			}
			options := usagelog.Options{ClaudeTranscript: claudeTranscript, CodexSession: codexSession, CodexSessionsRoot: codexRoot, Composition: composition}
			if appendHistory {
				if historyPath == "" {
					var err error
					if historyPath, err = defaultHistoryPath(command, config); err != nil {
						return err
					}
				}
				result, err := usagelog.AppendHistory(command.Context(), historyPath, options)
				if err != nil {
					if errors.Is(err, usagelog.ErrInvalidOptions) {
						return usage(err)
					}
					return err
				}
				return writeJSON(command.OutOrStdout(), result)
			}
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
			if summary {
				return usagelog.WriteCompositionSummary(command.OutOrStdout(), report)
			}
			return writeJSON(command.OutOrStdout(), report)
		},
	}
	command.Flags().StringVar(&claudeTranscript, "claude-transcript", "", "Claude Code main session transcript (<session-id>.jsonl); subagents are read from <session-id>/subagents")
	command.Flags().StringVar(&codexSession, "codex-session", "", "Codex root rollout (rollout-*.jsonl); descendant agent rollouts are discovered under the sessions root")
	command.Flags().StringVar(&codexRoot, "codex-sessions-root", "", "Directory scanned for Codex child rollouts (defaults to the nearest ancestor named sessions)")
	command.Flags().StringVar(&pricesPath, "prices", "", "Price table JSON with per-million-token USD rates and evidence; dollars from it are reported as estimates")
	command.Flags().BoolVar(&composition, "composition", false, "Add per-agent context composition (input by source, re-read cost, repeated calls, stale-phase re-read); needs --prices and is withheld unless it reconciles with the recorded cost within 1%")
	command.Flags().BoolVar(&summary, "summary", false, "With --composition, print a plain-text table instead of JSON")
	command.Flags().BoolVar(&appendHistory, "append", false, "Append each request's token counts (no prompt text) to the cost history and print what was appended instead of the report; re-importing never double-counts")
	command.Flags().StringVar(&historyPath, "history", "", "Cost history file for --append (default: metrics/history.jsonl under the state directory)")
	return command
}

// historyCommand aggregates the cost history read-only.
func historyCommand(config Config) *cobra.Command {
	var historyPath, pricesPath string
	var filter usagelog.HistoryFilter
	command := &cobra.Command{
		Use:   "history [--history FILE] [--session ID] [--model MODEL] [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--prices FILE]",
		Short: "Aggregate the cost history written by import --append as JSON, by model, UTC day and session",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usage(errors.New("metrics history accepts no positional arguments"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			if err := absolutePaths(config, &historyPath, &pricesPath); err != nil {
				return err
			}
			if historyPath == "" {
				var err error
				if historyPath, err = defaultHistoryPath(command, config); err != nil {
					return err
				}
			}
			var prices *usagelog.PriceTable
			if pricesPath != "" {
				table, err := usagelog.LoadPriceTable(pricesPath)
				if err != nil {
					return err
				}
				prices = &table
			}
			report, err := usagelog.ReadHistory(command.Context(), historyPath, filter, prices)
			if err != nil {
				if errors.Is(err, usagelog.ErrInvalidOptions) {
					return usage(err)
				}
				return err
			}
			if report.DamagedLines > 0 {
				fmt.Fprintf(command.ErrOrStderr(), "warning: skipped %d damaged line(s) in %s; see \"damaged\" in the report\n", report.DamagedLines, report.History)
			}
			return writeJSON(command.OutOrStdout(), report)
		},
	}
	command.Flags().StringVar(&historyPath, "history", "", "Cost history file (default: metrics/history.jsonl under the state directory)")
	command.Flags().StringVar(&filter.SessionID, "session", "", "Only records of this session id")
	command.Flags().StringVar(&filter.Model, "model", "", "Only records of this model id")
	command.Flags().StringVar(&filter.Since, "since", "", "Only records on or after this UTC date (YYYY-MM-DD)")
	command.Flags().StringVar(&filter.Until, "until", "", "Only records on or before this UTC date (YYYY-MM-DD)")
	command.Flags().StringVar(&pricesPath, "prices", "", "Price table JSON; adds price-table dollar estimates to every group")
	return command
}

// absolutePaths resolves each nonempty relative path against the configured
// working directory.
func absolutePaths(config Config, paths ...*string) error {
	for _, path := range paths {
		if *path != "" && !filepath.IsAbs(*path) {
			dir, err := config.WorkingDir()
			if err != nil {
				return err
			}
			*path = filepath.Join(dir, *path)
		}
	}
	return nil
}

// defaultHistoryPath is metrics/history.jsonl under the resolved state
// directory (--state-dir, else $XDG_STATE_HOME/agent-workflows, else
// ~/.local/state/agent-workflows).
func defaultHistoryPath(command *cobra.Command, config Config) (string, error) {
	roots, err := resolveRoots(command.Root(), config)
	if err != nil {
		return "", err
	}
	return filepath.Join(roots.state, "metrics", "history.jsonl"), nil
}
