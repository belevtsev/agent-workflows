package outcomes

// Contract contract-usage-import revision 1: OUT-COMPAT, OUT-BREAKDOWN, OUT-RATES.
// Expected values are computed by hand from the fixtures in each test.

import (
	"errors"
	"math"
	"testing"
)

var breakdownMetrics = []string{"uncached_input_tokens", "cache_write_5m_tokens", "cache_write_1h_tokens", "cache_read_tokens", "thinking_tokens"}

// fullBreakdown: 40 + 10 + 20 + 30 = 100 input; thinking 20 of output 50.
func fullBreakdown() *Usage {
	return &Usage{
		InputTokens: i64(100), OutputTokens: i64(50), APIRequests: i64(2),
		UncachedInputTokens: i64(40), CacheWrite5mTokens: i64(10), CacheWrite1hTokens: i64(20), CacheReadTokens: i64(30),
		ThinkingTokens: i64(20),
	}
}

func requireInvalid(t *testing.T, name string, observation Observation) {
	t.Helper()
	report, err := analyze(t, observation)
	if !errors.Is(err, ErrInvalidInput) || report.Counts.Accepted != 0 {
		t.Fatalf("%s accepted: counts %+v err %v", name, report.Counts, err)
	}
}

// OUT-COMPAT: observations without the new counters stay valid and count as unknown.
func TestUsageWithoutBreakdownCountsNewMetricsAsUnknown(t *testing.T) {
	legacy, noUsage := sample("legacy", "baseline", 1), sample("no-usage", "baseline", 2)
	legacy.Usage = &Usage{InputTokens: i64(10), OutputTokens: i64(5), APIRequests: i64(1)}
	report, err := analyze(t, legacy, noUsage)
	if err != nil || report.Counts.Accepted != 2 || report.Counts.Invalid != 0 || len(report.InvalidSamples) != 0 {
		t.Fatalf("legacy usage rejected: %+v %v", report.Counts, err)
	}
	summary := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	requireFloat(t, summary.Metrics["input_tokens"].Total, 10)
	for _, name := range breakdownMetrics {
		measure, ok := summary.Metrics[name]
		if !ok {
			t.Fatalf("summary lacks metric %s: %v", name, summary.Metrics)
		}
		if measure.Known != 0 || measure.Unknown != 2 || measure.Total != nil {
			t.Fatalf("%s = %+v, want 0 known, 2 unknown, null total", name, measure)
		}
	}
}

// OUT-BREAKDOWN: complete and partial breakdowns are valid and summarized.
func TestUsageBreakdownTotalsAndCoverage(t *testing.T) {
	full, partial, equalThinking := sample("full", "baseline", 1), sample("partial", "baseline", 1), sample("equal-thinking", "baseline", 1)
	full.Usage = fullBreakdown()
	// Partial: only uncached is known; 3 <= 7 but the rest is unknown.
	partial.Usage = &Usage{InputTokens: i64(7), UncachedInputTokens: i64(3)}
	// Boundary: thinking equal to output is valid.
	equalThinking.Usage = &Usage{OutputTokens: i64(9), ThinkingTokens: i64(9)}
	report, err := analyze(t, full, partial, equalThinking)
	if err != nil || report.Counts.Accepted != 3 || report.Counts.Invalid != 0 {
		t.Fatalf("valid breakdowns rejected: %+v %v %+v", report.Counts, err, report.InvalidSamples)
	}
	summary := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	for name, want := range map[string]struct {
		total          float64
		known, unknown int
	}{
		"uncached_input_tokens": {43, 2, 1},
		"cache_write_5m_tokens": {10, 1, 2},
		"cache_write_1h_tokens": {20, 1, 2},
		"cache_read_tokens":     {30, 1, 2},
		"thinking_tokens":       {29, 2, 1},
		"input_tokens":          {107, 2, 1},
	} {
		measure := summary.Metrics[name]
		if measure.Known != want.known || measure.Unknown != want.unknown {
			t.Fatalf("%s coverage = %+v, want %d known %d unknown", name, measure.Coverage, want.known, want.unknown)
		}
		requireFloat(t, measure.Total, want.total)
	}
}

// OUT-BREAKDOWN: inconsistent or negative counters invalidate the observation.
func TestUsageBreakdownInvalidObservations(t *testing.T) {
	for name, mutate := range map[string]func(*Usage){
		"breakdown sums above input": func(u *Usage) { u.InputTokens = i64(99) },
		"breakdown sums below input": func(u *Usage) { u.InputTokens = i64(101) },
		"thinking above output":      func(u *Usage) { u.ThinkingTokens = i64(51) },
		"negative uncached":          func(u *Usage) { u.UncachedInputTokens = i64(-1) },
		"negative cache write 5m":    func(u *Usage) { u.CacheWrite5mTokens = i64(-1) },
		"negative cache write 1h":    func(u *Usage) { u.CacheWrite1hTokens = i64(-1) },
		"negative cache read":        func(u *Usage) { u.CacheReadTokens = i64(-1) },
		"negative thinking":          func(u *Usage) { u.ThinkingTokens = i64(-1) },
	} {
		t.Run(name, func(t *testing.T) {
			observation := sample("bad", "baseline", 1)
			observation.Usage = fullBreakdown()
			mutate(observation.Usage)
			requireInvalid(t, name, observation)
		})
	}
	// A partial negative counter is still invalid (no sum check applies).
	partial := sample("partial-negative", "baseline", 1)
	partial.Usage = &Usage{CacheReadTokens: i64(-5)}
	requireInvalid(t, "partial negative", partial)
}

// OUT-PARTIAL: known breakdown counters must not exceed a known input_tokens.
func TestPartialUsageBreakdownBoundedByInput(t *testing.T) {
	for name, usage := range map[string]*Usage{
		"uncached+read above input":  {InputTokens: i64(10), UncachedInputTokens: i64(6), CacheReadTokens: i64(5)},
		"cache writes above input":   {InputTokens: i64(10), CacheWrite5mTokens: i64(7), CacheWrite1hTokens: i64(4)},
		"single counter above input": {InputTokens: i64(10), CacheReadTokens: i64(11)},
		"three counters above input": {InputTokens: i64(10), UncachedInputTokens: i64(3), CacheWrite5mTokens: i64(3), CacheReadTokens: i64(5)},
	} {
		t.Run(name, func(t *testing.T) {
			observation := sample("partial-above", "baseline", 1)
			observation.Usage = usage
			requireInvalid(t, name, observation)
		})
	}
	atTotal, below := sample("partial-at", "baseline", 1), sample("partial-below", "baseline", 1)
	atTotal.Usage = &Usage{InputTokens: i64(10), UncachedInputTokens: i64(6), CacheReadTokens: i64(4)}
	below.Usage = &Usage{InputTokens: i64(10), CacheWrite5mTokens: i64(2), CacheWrite1hTokens: i64(3), CacheReadTokens: i64(4)}
	report, err := analyze(t, atTotal, below)
	if err != nil || report.Counts.Accepted != 2 || report.Counts.Invalid != 0 {
		t.Fatalf("partial breakdown at or below input rejected: %+v %v %+v", report.Counts, err, report.InvalidSamples)
	}
	summary := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	requireFloat(t, summary.Metrics["cache_read_tokens"].Total, 8) // 4 + 4
}

func cacheRates() *Rates {
	return &Rates{
		InputPerMillion: f64(3), OutputPerMillion: f64(15), APIRequest: f64(.01),
		CacheWrite5mPerMillion: f64(3.75), CacheWrite1hPerMillion: f64(6), CacheReadPerMillion: f64(.3),
		Evidence: "local:explicit-cache-rates",
	}
}

// cacheUsage: uncached 1000 + 5m 2000 + 1h 3000 + read 4000 = 10000 input.
func cacheUsage() *Usage {
	return &Usage{
		InputTokens: i64(10000), OutputTokens: i64(500), APIRequests: i64(2),
		UncachedInputTokens: i64(1000), CacheWrite5mTokens: i64(2000), CacheWrite1hTokens: i64(3000), CacheReadTokens: i64(4000),
	}
}

func derivedTotal(t *testing.T, observation Observation) CostTotal {
	t.Helper()
	report, err := analyze(t, observation)
	if err != nil || report.Counts.Accepted != 1 {
		t.Fatalf("derived cost rejected: %+v %v %+v", report.Counts, err, report.InvalidSamples)
	}
	summary := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	if summary.Cost.Known != 1 || len(summary.Cost.Totals) != 1 {
		t.Fatalf("cost coverage = %+v", summary.Cost)
	}
	total := summary.Cost.Totals[0]
	if total.Currency != "USD" || total.DerivedSamples != 1 || total.ObservedSamples != 0 || total.Samples != 1 {
		t.Fatalf("cost total = %+v, want one derived USD sample", total)
	}
	return total
}

// OUT-RATES: cache rates price the breakdown.
// (1000*3 + 2000*3.75 + 3000*6 + 4000*0.3 + 500*15)/1e6 + 2*0.01
// = (3000 + 7500 + 18000 + 1200 + 7500)/1e6 + 0.02 = 0.0572.
// The original formula would give (10000*3 + 500*15)/1e6 + 0.02 = 0.0575.
func TestCacheRatesDeriveCostFromBreakdown(t *testing.T) {
	observation := sample("cache-priced", "baseline", 1)
	observation.Usage, observation.Cost = cacheUsage(), &Cost{Currency: "USD", Rates: cacheRates()}
	if total := derivedTotal(t, observation); math.Abs(total.Total-0.0572) > 1e-12 {
		t.Fatalf("cache-rate amount = %.12g, want 0.0572", total.Total)
	}
	// input_tokens is not among the counters the cache formula requires.
	withoutInput := sample("cache-priced-no-input", "baseline", 1)
	withoutInput.Usage, withoutInput.Cost = cacheUsage(), &Cost{Currency: "USD", Rates: cacheRates()}
	withoutInput.Usage.InputTokens = nil
	if total := derivedTotal(t, withoutInput); math.Abs(total.Total-0.0572) > 1e-12 {
		t.Fatalf("cache-rate amount without input_tokens = %.12g, want 0.0572", total.Total)
	}
}

// OUT-RATES: without cache rates the original formula applies even when a
// breakdown is present: (2000*2 + 1000*3)/1e6 + 2*0.01 = 0.027.
func TestRatesWithoutCacheRatesKeepOriginalFormula(t *testing.T) {
	observation := sample("plain-priced", "baseline", 1)
	observation.Usage = &Usage{
		InputTokens: i64(2000), OutputTokens: i64(1000), APIRequests: i64(2),
		UncachedInputTokens: i64(500), CacheWrite5mTokens: i64(0), CacheWrite1hTokens: i64(0), CacheReadTokens: i64(1500),
	}
	observation.Cost = &Cost{Currency: "USD", Rates: &Rates{InputPerMillion: f64(2), OutputPerMillion: f64(3), APIRequest: f64(.01), Evidence: "local:plain-rates"}}
	if total := derivedTotal(t, observation); math.Abs(total.Total-0.027) > 1e-12 {
		t.Fatalf("plain amount = %.12g, want 0.027", total.Total)
	}
}

// OUT-RATES: partial cache rates or unknown required counters are invalid.
func TestCacheRatesRequireAllRatesAndCounters(t *testing.T) {
	for name, mutate := range map[string]func(*Observation){
		"only 5m rate":      func(o *Observation) { o.Cost.Rates.CacheWrite1hPerMillion, o.Cost.Rates.CacheReadPerMillion = nil, nil },
		"missing read rate": func(o *Observation) { o.Cost.Rates.CacheReadPerMillion = nil },
		"missing 1h rate":   func(o *Observation) { o.Cost.Rates.CacheWrite1hPerMillion = nil },
		"only read rate": func(o *Observation) {
			o.Cost.Rates.CacheWrite5mPerMillion, o.Cost.Rates.CacheWrite1hPerMillion = nil, nil
		},
		"unknown uncached":      func(o *Observation) { o.Usage.UncachedInputTokens = nil },
		"unknown cache 5m":      func(o *Observation) { o.Usage.CacheWrite5mTokens = nil },
		"unknown cache 1h":      func(o *Observation) { o.Usage.CacheWrite1hTokens = nil },
		"unknown cache read":    func(o *Observation) { o.Usage.CacheReadTokens = nil },
		"unknown output":        func(o *Observation) { o.Usage.OutputTokens = nil },
		"unknown api requests":  func(o *Observation) { o.Usage.APIRequests = nil },
		"unknown api req price": func(o *Observation) { o.Cost.Rates.APIRequest = nil },
	} {
		t.Run(name, func(t *testing.T) {
			observation := sample("cache-invalid", "baseline", 1)
			observation.Usage, observation.Cost = cacheUsage(), &Cost{Currency: "USD", Rates: cacheRates()}
			mutate(&observation)
			requireInvalid(t, name, observation)
		})
	}
}
