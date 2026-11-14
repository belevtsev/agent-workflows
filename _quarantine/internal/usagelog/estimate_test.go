package usagelog

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// estimateLine is one assistant request of session s1 with a raw usage object.
func estimateLine(id int, model, usage string) string {
	return assistantLine("s1", "r"+string(rune('a'+id)), "m"+string(rune('a'+id)), model, usage)
}

// Oracles: each amount is hand-computed from builtin_prices.json, USD per
// million tokens (input / 5m write / 1h write / cache read / output):
// claude-opus-5-5 4 / 5 / 8 / 0.20 / 20; claude-haiku-5-5 0.10 / 0.125 /
// 0.20 / 0.01 / 0.50 up to a 100,000-token prompt and 0.50 / 0.625 / 1 /
// 0.05 / 2.50 above it, the prompt being one request's input, cache reads and
// cache writes. A cost-state line never changes the estimate.
func TestEstimateSessionPricesEachRequestFromTheBuiltInTable(t *testing.T) {
	split := func(input, w5m, w1h, read, output int64) string {
		return usageJSON(input, w5m+w1h, w5m, w1h, read, output)
	}
	for _, c := range []struct {
		name   string
		lines  []string
		want   float64
		reason string
	}{
		{name: "split cache writes at their own rates", lines: []string{estimateLine(0, "claude-opus-5-5", split(10, 400, 600, 200, 5))},
			want: 10*4 + 400*5 + 600*8 + 200*0.20 + 5*20},
		{name: "a missing split prices every write at the 1h rate", lines: []string{estimateLine(0, "claude-opus-5-5",
			`{"input_tokens":0,"cache_creation_input_tokens":1000,"cache_read_input_tokens":0,"output_tokens":0}`)}, want: 1000 * 8},
		{name: "haiku at a 100,000-token prompt keeps the base rates", lines: []string{estimateLine(0, "claude-haiku-5-5", split(1000, 0, 0, 99000, 100))},
			want: 1000*0.10 + 99000*0.01 + 100*0.50},
		{name: "haiku above a 100,000-token prompt takes the long rates", lines: []string{estimateLine(0, "claude-haiku-5-5", split(1001, 0, 0, 99000, 100))},
			want: 1001*0.50 + 99000*0.05 + 100*2.50},
		{name: "a stale cost-state is ignored", lines: []string{estimateLine(0, "claude-opus-5-5", split(10, 0, 0, 0, 5)),
			costStateLine("s1", 99, `"claude-opus-5-5":{"inputTokens":1,"outputTokens":1,"cacheReadInputTokens":0,"cacheCreationInputTokens":0}`),
			estimateLine(1, "claude-opus-5-5", split(20, 0, 0, 0, 0))}, want: 10*4 + 5*20 + 20*4},
		{name: "an unknown model", lines: []string{estimateLine(0, "claude-opus-5-5", split(10, 0, 0, 0, 5)), estimateLine(1, "claude-mystery-1", split(10, 0, 0, 0, 5))},
			reason: `the price table has no rates for model "claude-mystery-1"`},
		{name: "fast speed", lines: []string{estimateLine(0, "claude-opus-5-5", strings.TrimSuffix(split(10, 0, 0, 0, 5), "}")+`,"speed":"fast"}`)},
			reason: `a request ran at speed "fast"`},
		{name: "a regional inference", lines: []string{estimateLine(0, "claude-opus-5-5", strings.TrimSuffix(split(10, 0, 0, 0, 5), "}")+`,"speed":"standard","inference_geo":"us"}`)},
			reason: `a request ran with inference_geo "us"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s1.jsonl")
			writeLines(t, path, c.lines...)
			table, _, err := BuiltInPrices()
			if err != nil {
				t.Fatal(err)
			}
			_, estimate, err := EstimateSession(t.Context(), Options{ClaudeTranscript: path, Prices: &table})
			if err != nil {
				t.Fatal(err)
			}
			if c.reason != "" {
				if estimate.USD != nil || !strings.Contains(estimate.UnknownReason, c.reason) {
					t.Fatalf("estimate %v (%q), want unknown with %q", estimate.USD, estimate.UnknownReason, c.reason)
				}
				return
			}
			if estimate.USD == nil || math.Abs(*estimate.USD-c.want/1e6) > 1e-12 {
				t.Fatalf("estimate %v (%q), want %v USD", estimate.USD, estimate.UnknownReason, c.want/1e6)
			}
		})
	}
}

// Oracle: the shipped table is the one dated 2026-11-13 with nine models (ten rows: claude-haiku-5-5 has two prompt tiers).
func TestBuiltInPricesAreDated(t *testing.T) {
	table, data, err := BuiltInPrices()
	if err != nil || table.Date != "2026-11-13" || len(table.Models) != 9 || len(data) == 0 {
		t.Fatalf("built-in table date %q, %d models, %d bytes, %v; want 2026-11-13 and 9 models", table.Date, len(table.Models), len(data), err)
	}
}
