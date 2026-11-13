package usagelog

import (
	"context"
	_ "embed"
	"fmt"
)

// builtInPrices is the price table cw ships, dated and sourced in its date and
// evidence members. Its bytes' SHA-256 names it as a dollar source.
//
//go:embed builtin_prices.json
var builtInPrices []byte

// BuiltInPrices returns the shipped price table and its bytes, whose SHA-256
// identifies it.
func BuiltInPrices() (PriceTable, []byte, error) {
	table, err := ParsePriceTable(builtInPrices)
	return table, append([]byte(nil), builtInPrices...), err
}

// Estimate is a session's dollars priced request by request from a price
// table, or the reason they are unknown. It is an estimate, never a
// recorded amount.
type Estimate struct {
	USD           *float64
	UnknownReason string
}

// EstimateSession imports the session as Import does, without pricing its
// totals, and prices each de-duplicated request from options.Prices. A
// recorded cost-state is never used: stale, mismatched or missing, it changes
// nothing. One request that the table cannot price makes the estimate
// unknown, as does an incomplete import.
func EstimateSession(ctx context.Context, options Options) (Report, Estimate, error) {
	prices := options.Prices
	if prices == nil {
		return Report{}, Estimate{}, fmt.Errorf("%w: an estimate needs a price table", ErrInvalidOptions)
	}
	options.Prices = nil
	var collected []collectedRequest
	report, err := importSession(ctx, options, &collected, nil)
	if err != nil {
		return Report{}, Estimate{}, err
	}
	if report.Status != StatusComplete {
		return report, Estimate{UnknownReason: "the import is incomplete; see incomplete_reasons"}, nil
	}
	total := 0.0
	for _, c := range collected {
		amount, reason := prices.priceRequest(c.model, report.Source, c.request)
		if reason != "" {
			return report, Estimate{UnknownReason: reason}, nil
		}
		total += amount
	}
	return report, Estimate{USD: &total}, nil
}

// priceRequest prices one request. A model's long-prompt rates apply when the
// request's prompt (uncached input, cache reads and cache writes) exceeds
// their threshold. Claude cache writes without a 5m/1h split are priced at
// the 1h rate, the higher one, so a budget never under-counts; Codex writes
// carry no TTL and take the 5m rate, as the per-model totals do.
func (table *PriceTable) priceRequest(model, source string, r request) (float64, string) {
	if r.unpriced != "" {
		return 0, r.unpriced
	}
	rates, ok := table.Models[model]
	if !ok {
		return 0, fmt.Sprintf("the price table has no rates for model %q", model)
	}
	if long := rates.LongPrompt; long != nil && r.uncached+r.read+r.write5m+r.write1h+r.writeUnsplit > long.AboveTokens {
		rates = long.Rates
	}
	unsplitRate, unsplitName := rates.CacheWrite1hPerMillion, "cache_write_1h_per_million"
	if source == SourceCodex {
		unsplitRate, unsplitName = rates.CacheWrite5mPerMillion, "cache_write_5m_per_million"
	}
	parts := []struct {
		name   string
		tokens int64
		rate   *float64
	}{
		{"input_per_million", r.uncached, rates.InputPerMillion},
		{"output_per_million", r.output, rates.OutputPerMillion},
		{"cache_read_per_million", r.read, rates.CacheReadPerMillion},
		{"cache_write_5m_per_million", r.write5m, rates.CacheWrite5mPerMillion},
		{"cache_write_1h_per_million", r.write1h, rates.CacheWrite1hPerMillion},
		{unsplitName, r.writeUnsplit, unsplitRate},
	}
	total := 0.0
	for _, p := range parts {
		if p.tokens == 0 {
			continue
		}
		if p.rate == nil {
			return 0, fmt.Sprintf("the price table lacks %s for model %q, which has tokens in that category", p.name, model)
		}
		total += float64(p.tokens) / 1e6 * *p.rate
	}
	return total, ""
}
