package cli

import (
	"errors"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
	"github.com/spf13/cobra"
)

// maxUsageReasonBytes bounds the import's reasons a sample keeps.
const maxUsageReasonBytes = 1024

// newTaskUsageCommand returns cw task usage.
func newTaskUsageCommand(deps taskDeps) *cobra.Command {
	var directory, claudeTranscript, codexSession, pricesPath string
	var full bool
	command := &cobra.Command{
		Use:   "usage --task DIR (--claude-transcript FILE | --codex-session FILE) [--prices FILE]",
		Short: "Sample the session's usage from its transcript into the task record",
		Long: "Read the session's transcript, read-only, and store its cumulative tokens and recorded dollars in the task record when they matter. " +
			"Task usage is, per session, the latest sample minus the first; unknown values stay unknown, never 0. See docs/operations.md \"Cost budget\".",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.guard(); err != nil {
				return err
			}
			if err := deps.requireTask(&directory); err != nil {
				return err
			}
			if (claudeTranscript == "") == (codexSession == "") {
				return usage(errors.New("supply exactly one of --claude-transcript FILE or --codex-session FILE"))
			}
			if err := absolutePaths(deps.config, &claudeTranscript, &codexSession, &pricesPath); err != nil {
				return err
			}
			options := usagelog.Options{ClaudeTranscript: claudeTranscript, CodexSession: codexSession}
			if pricesPath != "" {
				table, err := usagelog.LoadPriceTable(pricesPath)
				if err != nil {
					return err
				}
				options.Prices = &table
			}
			report, err := usagelog.Import(cmd.Context(), options)
			if err != nil {
				if errors.Is(err, usagelog.ErrInvalidOptions) {
					return usage(err)
				}
				return err
			}
			result, err := taskEnv(deps.config).RecordUsage(cmd.Context(), directory, usageSample(report))
			if err != nil {
				return err
			}
			if full {
				return writeJSON(cmd.OutOrStdout(), result.Record)
			}
			return writeJSON(cmd.OutOrStdout(), taskUsageSummary{TaskID: result.Record.ID, Stored: result.Stored, Sample: result.Sample, Usage: taskflow.UsageOf(result.Record)})
		},
	}
	taskFlag(command, &directory)
	command.Flags().StringVar(&claudeTranscript, "claude-transcript", "", "Claude Code session transcript (.jsonl) to sample")
	command.Flags().StringVar(&codexSession, "codex-session", "", "Codex rollout file to sample")
	command.Flags().StringVar(&pricesPath, "prices", "", "Price table JSON; used for dollars only when the transcript records no cost")
	fullFlag(command, &full, "the whole task record")
	return command
}

// taskUsageSummary is cw task usage's default output: whether the sample was
// stored, the sample and the task's usage after it.
type taskUsageSummary struct {
	TaskID string               `json:"task_id"`
	Stored bool                 `json:"stored"`
	Sample taskflow.UsageSample `json:"sample"`
	Usage  taskflow.TaskUsage   `json:"usage"`
}

// usageSample maps an import report to a usage sample. Tokens come from the
// report's whole-session usage, with cache writes being total input less
// uncached input and cache reads. Dollars are the transcript's recorded cost;
// only a transcript that records none takes a supplied price table's total.
// An incomplete import, withheld usage or a cost that covers a model of
// unknown price leaves the value nil with the reason, never 0.
func usageSample(report usagelog.Report) taskflow.UsageSample {
	sample := taskflow.UsageSample{Source: report.Source, SessionID: report.SessionID}
	if report.Status != usagelog.StatusComplete {
		sample.UnknownReason = boundReason("the transcript import is incomplete: " + strings.Join(report.IncompleteReasons, "; "))
		return sample
	}
	if report.Usage == nil {
		sample.UnknownReason = boundReason("the transcript usage is unknown: " + report.UsageUnknownReason)
		return sample
	}
	value := report.Usage
	if value.InputTokens != nil && value.UncachedInputTokens != nil && value.CacheReadTokens != nil && value.OutputTokens != nil {
		sample.Tokens = &taskflow.UsageTokens{UncachedInput: *value.UncachedInputTokens, CacheWrite: *value.InputTokens - *value.UncachedInputTokens - *value.CacheReadTokens,
			CacheRead: *value.CacheReadTokens, Output: *value.OutputTokens}
	}
	dollars := report.Dollars
	var reason string
	switch {
	case dollars.Recorded != nil && report.Cost != nil && report.Cost.Amount != nil:
		amount := *report.Cost.Amount
		sample.USD, sample.DollarSource = &amount, taskflow.DollarsRecorded
	case dollars.Recorded != nil:
		reason = "the recorded cost covers a model of unknown cost"
	case dollars.PriceTable != nil && dollars.PriceTable.Total != nil:
		amount := *dollars.PriceTable.Total
		sample.USD, sample.DollarSource = &amount, taskflow.DollarsPriceTable
	case dollars.PriceTable != nil:
		reason = "no recorded cost (" + dollars.RecordedUnknownReason + ") and the price-table total is unknown: " + dollars.PriceTable.TotalUnknownReason
	default:
		reason = "no recorded cost (" + dollars.RecordedUnknownReason + ") and no price table supplied"
	}
	if sample.Tokens == nil {
		reason = strings.TrimPrefix(reason+"; the token counts are incomplete", "; ")
	}
	sample.UnknownReason = boundReason(reason)
	return sample
}

// boundReason cuts reason to maxUsageReasonBytes on a rune boundary.
func boundReason(reason string) string {
	if len(reason) <= maxUsageReasonBytes {
		return reason
	}
	return strings.ToValidUTF8(reason[:maxUsageReasonBytes-3], "") + "..."
}
