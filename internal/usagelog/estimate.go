package usagelog

import (
	"context"
	_ "embed"
	"fmt"
	"time"
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

// Estimate is the tokens and dollars of a session's requests at or after
// Options.Since, the dollars priced request by request from a price table:
// an estimate, never a recorded amount. A nil value is unknown, with
// UnknownReason.
type Estimate struct {
	Tokens        *Tokens
	USD           *float64
	UnknownReason string
}

// EstimateSession imports the session as Import does, without pricing its
// totals, and counts and prices each de-duplicated request (main transcript,
// sidechain lines and subagent transcripts alike) stamped at or after
// options.Since, from options.Prices. A recorded cost-state is never used:
// stale, mismatched or missing, it changes nothing. An incomplete import, or
// with Since set a request without a readable timestamp, leaves tokens and
// dollars unknown; a request the table cannot price leaves the dollars
// unknown.
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
	tokens, total, priceReason := Tokens{}, 0.0, ""
	for _, c := range collected {
		if !options.Since.IsZero() {
			at, err := time.Parse(time.RFC3339Nano, c.timestamp)
			if err != nil {
				return report, Estimate{UnknownReason: "a request has no readable timestamp, so whether it falls in the task is unknown"}, nil
			}
			if at.Before(options.Since) {
				continue
			}
		}
		tokens.add(c.request)
		if priceReason == "" {
			amount, reason := prices.priceRequest(c.model, report.Source, c.request)
			priceReason = reason
			total += amount
		}
	}
	if priceReason != "" {
		return report, Estimate{Tokens: &tokens, UnknownReason: priceReason}, nil
	}
	return report, Estimate{Tokens: &tokens, USD: &total}, nil
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
