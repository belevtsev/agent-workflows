package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
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
		Long: "Read the session's transcript, read-only, count and price each request stamped at or after the task's start from cw's dated price table " +
			"(or --prices), and store those tokens and estimated dollars in the task record when they matter. A recorded cost-state is not used. " +
			"Task usage is the sum of each session's latest sample, so one sample of a session is enough; unknown values stay unknown, never 0. " +
			"See docs/operations.md \"Cost budget\".",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.requireTask(&directory); err != nil {
				return err
			}
			if (claudeTranscript == "") == (codexSession == "") {
				return usage(errors.New("supply exactly one of --claude-transcript FILE or --codex-session FILE"))
			}
			if err := absolutePaths(deps.config, &claudeTranscript, &codexSession, &pricesPath); err != nil {
				return err
			}
			table, source, err := usagePrices(pricesPath)
			if err != nil {
				return err
			}
			// Only the requests stamped at or after the task's start
			// belong to it; created_at never changes, so reading it before
			// the locked update is safe.
			task, err := taskflow.Load(directory)
			if err != nil {
				return err
			}
			options := usagelog.Options{ClaudeTranscript: claudeTranscript, CodexSession: codexSession, Prices: &table, Since: task.CreatedAt}
			report, estimate, err := usagelog.EstimateSession(cmd.Context(), options)
			if err != nil {
				if errors.Is(err, usagelog.ErrInvalidOptions) {
					return usage(err)
				}
				return err
			}
			result, err := taskEnv(deps.config).RecordUsage(cmd.Context(), directory, usageSample(report, estimate, source))
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
	command.Example = helpText("task-usage-example")
	command.Flags().StringVar(&claudeTranscript, "claude-transcript", "", "Claude Code session transcript (.jsonl) to sample")
	command.Flags().StringVar(&codexSession, "codex-session", "", "Codex rollout file to sample")
	command.Flags().StringVar(&pricesPath, "prices", "", "Price table JSON that replaces cw's built-in table; its SHA-256 becomes the dollar source")
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

// usageSample maps an import report and its estimate to a usage sample: the
// session's tokens and dollars since the task started, cache writes being
// the 5m, 1h and unsplit writes together. A recorded cost-state changes
// neither. An incomplete import, a request without a readable timestamp, or
// one the table cannot price leaves the value nil with the reason, never 0.
func usageSample(report usagelog.Report, estimate usagelog.Estimate, source taskflow.DollarSource) taskflow.UsageSample {
	sample := taskflow.UsageSample{Source: report.Source, SessionID: report.SessionID}
	if report.Status != usagelog.StatusComplete {
		sample.UnknownReason = boundReason("the transcript import is incomplete: " + strings.Join(report.IncompleteReasons, "; "))
		return sample
	}
	if totals := estimate.Tokens; totals != nil {
		sample.Tokens = &taskflow.UsageTokens{UncachedInput: totals.UncachedInputTokens, CacheWrite: totals.CacheWrite5mTokens + totals.CacheWrite1hTokens + totals.CacheWriteUnsplitTokens,
			CacheRead: totals.CacheReadTokens, Output: totals.OutputTokens}
	}
	if estimate.USD == nil {
		sample.UnknownReason = boundReason("the dollars cannot be estimated: " + estimate.UnknownReason)
		return sample
	}
	amount := *estimate.USD
	sample.USD, sample.USDEstimated, sample.DollarSource = &amount, true, &source
	return sample
}

// boundReason cuts reason to maxUsageReasonBytes on a rune boundary.
func boundReason(reason string) string {
	if len(reason) <= maxUsageReasonBytes {
		return reason
	}
	return strings.ToValidUTF8(reason[:maxUsageReasonBytes-3], "") + "..."
}

// usagePrices returns the price table that prices a sample, with the dollar
// source that names it: the price table at path, read once, or cw's built-in
// table when path is empty. Either is named by the SHA-256 of its bytes.
func usagePrices(path string) (usagelog.PriceTable, taskflow.DollarSource, error) {
	if path == "" {
		table, data, err := usagelog.BuiltInPrices()
		return table, priceSource(taskflow.PriceTableBuiltIn, table, data), err
	}
	data, err := readUsagePrices(path)
	if err != nil {
		return usagelog.PriceTable{}, taskflow.DollarSource{}, fmt.Errorf("read price table: %w", err)
	}
	table, err := usagelog.ParsePriceTable(data)
	return table, priceSource(taskflow.PriceTableFile, table, data), err
}

func priceSource(kind string, table usagelog.PriceTable, data []byte) taskflow.DollarSource {
	sum := sha256.Sum256(data)
	return taskflow.DollarSource{Kind: kind, Date: table.Date, SHA256: hex.EncodeToString(sum[:])}
}

// readUsagePrices reads the regular file at path, at most
// usagelog.MaxSmallFileBytes.
func readUsagePrices(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, usagelog.MaxSmallFileBytes+1))
	if err == nil && len(data) > usagelog.MaxSmallFileBytes {
		err = fmt.Errorf("file exceeds %d bytes", usagelog.MaxSmallFileBytes)
	}
	return data, err
}
