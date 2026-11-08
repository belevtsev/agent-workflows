package usagelog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/outcomes"
)

// ErrInvalidOptions reports an option combination Import does not accept.
var ErrInvalidOptions = errors.New("invalid usage import options")

// Options selects exactly one source. CodexSessionsRoot applies to Codex
// only; when empty the nearest ancestor directory named "sessions" is used.
type Options struct {
	ClaudeTranscript  string
	CodexSession      string
	CodexSessionsRoot string
	Prices            *PriceTable
	// Composition adds the context-composition report; it needs Prices.
	Composition bool
}

// Import reads the selected transcript read-only and returns the report. It
// returns an error, and no report, only for unusable options or an unreadable
// or unrecognised source; bounded reads that stop early yield a report with
// status incomplete instead.
func Import(ctx context.Context, options Options) (Report, error) {
	return importSession(ctx, options, nil)
}

// importSession is Import that also appends every recorded request to
// collected when it is non-nil.
func importSession(ctx context.Context, options Options, collected *[]collectedRequest) (Report, error) {
	switch {
	case (options.ClaudeTranscript == "") == (options.CodexSession == ""):
		return Report{}, fmt.Errorf("%w: supply exactly one of a Claude transcript or a Codex session", ErrInvalidOptions)
	case options.ClaudeTranscript != "" && options.CodexSessionsRoot != "":
		return Report{}, fmt.Errorf("%w: a Codex sessions root applies only to a Codex session", ErrInvalidOptions)
	case options.Composition && options.Prices == nil:
		return Report{}, fmt.Errorf("%w: a composition needs a price table to reconcile with the recorded cost", ErrInvalidOptions)
	case options.ClaudeTranscript != "":
		return importClaude(ctx, options.ClaudeTranscript, options.Prices, options.Composition, collected)
	default:
		report, err := importCodex(ctx, options.CodexSession, options.CodexSessionsRoot, options.Prices, collected)
		if err == nil && options.Composition {
			report.Composition = unsupportedComposition(report)
		}
		return report, err
	}
}

func newReport(source string) Report {
	return Report{SchemaVersion: ReportSchemaVersion, Kind: ReportKind, Source: source, Limits: limits(),
		Status: StatusComplete, Dollars: Dollars{Currency: "USD"}}
}

// applyPriceTable fills price-table totals. With a recorded cost, models only
// in the cost-state are priced from their recorded tokens so the recorded and
// price-table totals cover the same requests.
func applyPriceTable(report *Report, prices *PriceTable, cost *costState) {
	if prices == nil {
		report.Dollars.PriceTableUnknownReason = "no --prices table supplied"
		report.Dollars.DifferenceUnknownReason = "needs both a recorded cost and a price table"
		return
	}
	table := &PriceTableDollars{Basis: BasisPriceTable, Currency: prices.Currency, Evidence: prices.Evidence}
	var amounts []Amount
	for _, model := range report.Models {
		table.Models = append(table.Models, ModelAmount{Model: model.Model, Amount: model.Dollars})
		amounts = append(amounts, model.Dollars)
	}
	total := sumAmounts(amounts, BasisPriceTable)
	table.Total, table.TotalUnknownReason = total.Amount, total.UnknownReason
	table.UnattributedRecorded = []ModelAmount{}
	report.Dollars.PriceTable = table
	if cost == nil {
		report.Dollars.DifferenceUnknownReason = "no recorded cost to compare"
		return
	}
	var unattributed []Amount
	for _, model := range report.Dollars.Recorded.UnattributedModels {
		amount := Amount{UnknownReason: fmt.Sprintf("recorded-only model %q lacks token totals in cost-state", model)}
		if tokens, ok := recordedTokens(cost.ModelUsage[model]); ok {
			amount = prices.price(model, SourceClaude, tokens)
		}
		table.UnattributedRecorded = append(table.UnattributedRecorded, ModelAmount{Model: model, Amount: amount})
		unattributed = append(unattributed, amount)
	}
	unattributedTotal := sumAmounts(unattributed, BasisPriceTable)
	table.UnattributedRecordedTotal, table.UnattributedRecordedUnknownReason = unattributedTotal.Amount, unattributedTotal.UnknownReason
	switch {
	case table.Total == nil:
		report.Dollars.DifferenceUnknownReason = "price-table total is unknown: " + table.TotalUnknownReason
	case unattributedTotal.Amount == nil:
		report.Dollars.DifferenceUnknownReason = "price-table amount for unattributed recorded models is unknown: " + unattributedTotal.UnknownReason
	default:
		difference := *cost.TotalCostUSD - (*table.Total + *unattributedTotal.Amount)
		report.Dollars.RecordedMinusPriceTable = &difference
	}
}

// finishReport sets status, the observation-ready usage and recorded cost.
func finishReport(report *Report, cost *costState) {
	if len(report.IncompleteReasons) > 0 {
		report.Status = StatusIncomplete
	}
	report.Notes = append(report.Notes,
		"Requests are de-duplicated by request id (Claude requestId, falling back to message id; Codex response_id). Codex token_count events are not counted.",
		"total_input_tokens = uncached input + cache writes + cache reads. Thinking tokens are a subset of output tokens and are not priced separately.",
		"Recorded dollars come only from the transcript's last cost-state; price-table dollars come only from --prices and are estimates under that table's evidence.",
	)
	if report.Source == SourceCodex {
		report.Notes = append(report.Notes, "Codex input_tokens includes cached and cache-write tokens; uncached = input - cached - cache_write. Codex cache writes have no TTL and are priced at cache_write_5m_per_million.")
	}
	if report.Status != StatusComplete {
		report.UsageUnknownReason = "the import is incomplete; see incomplete_reasons"
		return
	}
	// An observation needs transcript usage and the recorded cost to describe
	// the same requests; any per-model disagreement withholds both.
	if recorded := report.Dollars.Recorded; cost != nil && recorded != nil {
		var mismatched []string
		for _, item := range recorded.Reconciliation {
			if item.Status != "equal" && item.Status != "recorded_only" {
				mismatched = append(mismatched, item.Model+" ("+item.Status+")")
			}
		}
		if len(mismatched) > 0 {
			report.UsageUnknownReason = "transcript usage and the recorded cost-state differ (or cannot be compared) for model(s) " + strings.Join(mismatched, ", ") + "; usage and cost are withheld"
			report.Notes = append(report.Notes, "Transcript usage and the recorded cost-state differ (see dollars.recorded.reconciliation), so no observation usage or cost is emitted; per-agent and per-model counts are still reported.")
			return
		}
	}
	totals := report.Totals
	requestsKnown := true
	// Thinking is known only when every request (and recorded-only model) reported it.
	thinkingKnown := totals.ThinkingReportedRequests == totals.Requests
	thinking := int64(0)
	if totals.ThinkingTokens != nil {
		thinking = *totals.ThinkingTokens
	}
	if cost != nil && report.Dollars.Recorded != nil && len(report.Dollars.Recorded.UnattributedModels) > 0 {
		for _, model := range report.Dollars.Recorded.UnattributedModels {
			tokens, ok := recordedTokens(cost.ModelUsage[model])
			if !ok {
				report.UsageUnknownReason = fmt.Sprintf("recorded-only model %q lacks token totals in cost-state", model)
				return
			}
			if tokens.TotalInputTokens > 0 || tokens.OutputTokens > 0 {
				requestsKnown = false
			}
			if tokens.ThinkingTokens == nil {
				thinkingKnown = false
			} else {
				thinking += *tokens.ThinkingTokens
			}
			tokens.ThinkingTokens = nil
			totals.merge(tokens)
		}
		report.Notes = append(report.Notes, "usage covers the whole session: transcript requests plus recorded-only (unattributed) models from cost-state; api_requests is unknown when an unattributed model used tokens.")
	}
	usage := &outcomes.Usage{
		InputTokens: ptr(totals.TotalInputTokens), UncachedInputTokens: ptr(totals.UncachedInputTokens),
		CacheReadTokens: ptr(totals.CacheReadTokens), OutputTokens: ptr(totals.OutputTokens),
	}
	if requestsKnown {
		usage.APIRequests = ptr(totals.Requests)
	}
	if totals.CacheWriteUnsplitTokens == 0 {
		usage.CacheWrite5mTokens, usage.CacheWrite1hTokens = ptr(totals.CacheWrite5mTokens), ptr(totals.CacheWrite1hTokens)
	}
	if thinkingKnown {
		usage.ThinkingTokens = ptr(thinking)
	}
	report.Usage = usage
	if cost != nil && cost.HasUnknownModelCost != nil && !*cost.HasUnknownModelCost {
		amount := *cost.TotalCostUSD
		report.Cost = &outcomes.Cost{Currency: "USD", Amount: &amount, Evidence: "claude transcript cost-state " + report.SessionID}
	}
}

func ptr(value int64) *int64 { return &value }
