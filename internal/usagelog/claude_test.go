package usagelog

// Fixture tests for the Claude transcript import. Every line is written by
// hand from the fields claudeHead, claudeEntry, claudeUsage, costState and
// claudeMeta read; none comes from a real session. The expected values are
// computed by hand next to each assertion.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeLines writes one line per argument to path, creating its directory.
func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// usageJSON is a Claude usage object: input, cache creation with its 5m and
// 1h split, cache read and output tokens.
func usageJSON(input, creation, w5m, w1h, read, output int64) string {
	return fmt.Sprintf(`{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d}}`,
		input, creation, read, output, w5m, w1h)
}

func assistantLine(session, requestID, messageID, model, usage string) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"requestId":%q,"message":{"id":%q,"model":%q,"usage":%s}}`,
		session, requestID, messageID, model, usage)
}

func costStateLine(session string, total float64, models string) string {
	return fmt.Sprintf(`{"type":"cost-state","sessionId":%q,"totalCostUSD":%v,"hasUnknownModelCost":false,"modelUsage":{%s}}`, session, total, models)
}

// claudeFixture writes session s1: a main transcript with one line of each
// kind the import tells apart, and one subagent with a meta file.
func claudeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "s1.jsonl")
	r1 := `{"type":"assistant","sessionId":"s1","requestId":"req-1","effort":"high","message":{"id":"msg-1","model":"m-opus","usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":0,"output_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":40,"ephemeral_1h_input_tokens":60},"output_tokens_details":{"thinking_tokens":2}}}}`
	writeLines(t, main,
		`{"type":"attachment","sessionId":"s1","attachment":{"type":"instructions"},"rendered":[{"content":"rules"}]}`,
		`{"type":"user","sessionId":"s1","message":{"role":"user","content":"hi"}}`,
		r1,
		r1, // the same request id again: a duplicate
		assistantLine("s1", "req-s", "msg-s", "<synthetic>", usageJSON(1, 0, 0, 0, 0, 1)),
		`{"type":"assistant","sessionId":"s1","requestId":"req-n","message":{"id":"msg-n","model":"m-opus"}}`,
		// No request id: the key is the message id. The split 1+1 is not
		// the 7 created, so all 7 are unsplit cache writes.
		assistantLine("s1", "", "msg-6", "m-opus", usageJSON(3, 7, 1, 1, 110, 7)),
		`{"type":`,
		assistantLine("s0", "old", "msg-old", "m-opus", usageJSON(500, 0, 0, 0, 0, 50)),
		`{"type":"system","sessionId":"s1","subtype":"compact_boundary"}`,
		costStateLine("s1", 0.5, `"m-opus":{"inputTokens":13,"outputTokens":12,"cacheReadInputTokens":110,"cacheCreationInputTokens":107},`+
			`"m-sonnet":{"inputTokens":20,"outputTokens":4,"cacheReadInputTokens":0,"cacheCreationInputTokens":0}`),
	)
	subagents := filepath.Join(dir, "s1", "subagents")
	writeLines(t, filepath.Join(subagents, "agent-a1.jsonl"), assistantLine("s1", "q-1", "n-1", "m-sonnet", usageJSON(20, 0, 0, 0, 0, 4)))
	writeLines(t, filepath.Join(subagents, "agent-a1.meta.json"),
		`{"agentType":"agent-workflows:reviewer","description":"Review the change","model":"sonnet","effort":"low"}`)
	writeLines(t, filepath.Join(subagents, "notes.txt"), "not an agent transcript")
	return main
}

func TestImportClaudeCountsEachRequestOnceAcrossAgents(t *testing.T) {
	report, err := Import(t.Context(), Options{ClaudeTranscript: claudeFixture(t)})
	if err != nil {
		t.Fatal(err)
	}
	// Oracle: main counts req-1 (10, 5m 40, 1h 60, read 0, out 5, thinking 2)
	// and msg-6 (3, unsplit 7, read 110, out 7); the subagent counts q-1
	// (20, out 4). The duplicate, synthetic, usage-less, malformed and
	// other-session lines count nothing.
	wantMain := Tokens{Requests: 2, UncachedInputTokens: 13, CacheWrite5mTokens: 40, CacheWrite1hTokens: 60, CacheWriteUnsplitTokens: 7,
		CacheReadTokens: 110, OutputTokens: 12, ThinkingTokens: ptr(2), ThinkingReportedRequests: 1, TotalInputTokens: 13 + 40 + 60 + 7 + 110}
	wantSub := Tokens{Requests: 1, UncachedInputTokens: 20, OutputTokens: 4, TotalInputTokens: 20}
	wantTotals := Tokens{Requests: 3, UncachedInputTokens: 33, CacheWrite5mTokens: 40, CacheWrite1hTokens: 60, CacheWriteUnsplitTokens: 7,
		CacheReadTokens: 110, OutputTokens: 16, ThinkingTokens: ptr(2), ThinkingReportedRequests: 1, TotalInputTokens: 250}
	if report.Status != StatusComplete || report.SessionID != "s1" || len(report.Agents) != 2 {
		t.Fatalf("status %q, session %q, %d agents; want complete, s1, 2", report.Status, report.SessionID, len(report.Agents))
	}
	main, sub := report.Agents[0], report.Agents[1]
	if main.AgentID != "main" || !reflect.DeepEqual(main.Tokens, wantMain) || !reflect.DeepEqual(main.EffortsSeen, []string{"high"}) {
		t.Errorf("main agent = %s %+v efforts %v; want main %+v efforts [high]", main.AgentID, main.Tokens, main.EffortsSeen, wantMain)
	}
	if sub.AgentID != "agent-a1" || sub.Depth != 1 || !reflect.DeepEqual(sub.Files, []string{"s1/subagents/agent-a1.jsonl"}) || !reflect.DeepEqual(sub.Tokens, wantSub) {
		t.Errorf("subagent = %s depth %d files %v %+v; want agent-a1 depth 1 %+v", sub.AgentID, sub.Depth, sub.Files, sub.Tokens, wantSub)
	}
	// Oracle: the meta file's four strings, as written.
	if sub.AgentType != "agent-workflows:reviewer" || sub.Description != "Review the change" || sub.ConfiguredModel != "sonnet" || sub.ConfiguredEffort != "low" {
		t.Errorf("subagent meta = %q %q %q %q", sub.AgentType, sub.Description, sub.ConfiguredModel, sub.ConfiguredEffort)
	}
	if !reflect.DeepEqual(report.Totals, wantTotals) {
		t.Errorf("totals = %+v; want %+v", report.Totals, wantTotals)
	}
	if want := (Skipped{MalformedLines: 1, SyntheticEntries: 1, NoUsageEntries: 1, DuplicateEntries: 1, OtherSessionEntries: 1}); report.Skipped != want {
		t.Errorf("skipped = %+v; want %+v", report.Skipped, want)
	}
	if want := (Anomalies{CacheSplitMismatchRequests: 1}); report.Anomalies != want {
		t.Errorf("anomalies = %+v; want %+v", report.Anomalies, want)
	}
	// Oracle: the cost-state agrees with both models, so usage is the totals
	// (the unsplit 7 hide the 5m/1h split; thinking is known for 1 of 3
	// requests only) and the cost is the recorded 0.5.
	usage := report.Usage
	if usage == nil || *usage.InputTokens != 250 || *usage.UncachedInputTokens != 33 || *usage.CacheReadTokens != 110 || *usage.OutputTokens != 16 ||
		*usage.APIRequests != 3 || usage.CacheWrite5mTokens != nil || usage.ThinkingTokens != nil {
		t.Errorf("usage = %+v (reason %q); want input 250, uncached 33, read 110, output 16, requests 3, no split, no thinking", usage, report.UsageUnknownReason)
	}
	if report.Cost == nil || *report.Cost.Amount != 0.5 {
		t.Errorf("cost = %+v; want the recorded 0.5", report.Cost)
	}
}

func TestImportClaudeReportsRecordedAndPriceTableDollarsApart(t *testing.T) {
	main := filepath.Join(t.TempDir(), "s2.jsonl")
	writeLines(t, main,
		assistantLine("s2", "req-1", "msg-1", "m-opus", usageJSON(1000, 2000, 0, 2000, 10000, 500)),
		costStateLine("s2", 0.03, `"m-opus":{"inputTokens":1000,"outputTokens":500,"cacheReadInputTokens":10000,"cacheCreationInputTokens":2000,`+
			`"cacheCreationEphemeral5mInputTokens":0,"cacheCreationEphemeral1hInputTokens":2000,"costUSD":0.029},`+
			`"m-haiku":{"inputTokens":100,"outputTokens":10,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0.001}`),
	)
	prices, err := ParsePriceTable([]byte(`{"currency":"USD","evidence":"synthetic","models":{` +
		`"m-opus":{"input_per_million":4,"output_per_million":20,"cache_write_5m_per_million":5,"cache_write_1h_per_million":8,"cache_read_per_million":0.2},` +
		`"m-haiku":{"input_per_million":1,"output_per_million":5}}}`))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Import(t.Context(), Options{ClaudeTranscript: main, Prices: &prices})
	if err != nil {
		t.Fatal(err)
	}
	near := func(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-12 }
	recorded, table := report.Dollars.Recorded, report.Dollars.PriceTable
	// Oracle: m-haiku is only in the cost-state, so it is unattributed with
	// its recorded 0.001; m-opus agrees field by field.
	if recorded == nil || recorded.Total != 0.03 || !reflect.DeepEqual(recorded.UnattributedModels, []string{"m-haiku"}) || !near(recorded.UnattributedTotal, 0.001) {
		t.Fatalf("recorded = %+v; want total 0.03, unattributed [m-haiku] 0.001", recorded)
	}
	if got := []string{recorded.Reconciliation[0].Model + " " + recorded.Reconciliation[0].Status, recorded.Reconciliation[1].Model + " " + recorded.Reconciliation[1].Status}; !reflect.DeepEqual(got, []string{"m-haiku recorded_only", "m-opus equal"}) {
		t.Errorf("reconciliation = %v; want [m-haiku recorded_only, m-opus equal]", got)
	}
	// Oracle: m-opus at the table, per million: 1000x4 + 500x20 + 10000x0.2
	// + 2000x8 (1h) = 32000, so 0.032. m-haiku from its recorded tokens:
	// 100x1 + 10x5 = 150, so 0.00015. Difference: 0.03 - 0.03215.
	if table == nil || !near(table.Total, 0.032) || !near(table.UnattributedRecordedTotal, 0.00015) || !near(report.Dollars.RecordedMinusPriceTable, 0.03-0.03215) {
		t.Errorf("price table = %+v, difference %v; want 0.032, unattributed 0.00015, difference -0.00215", table, report.Dollars.RecordedMinusPriceTable)
	}
	// Oracle: usage covers the session, recorded-only tokens included:
	// input 1000+2000+10000+100 = 13100, output 510; the request count is
	// unknown because m-haiku used tokens in requests the transcript lacks.
	usage := report.Usage
	if usage == nil || *usage.InputTokens != 13100 || *usage.OutputTokens != 510 || usage.APIRequests != nil || *usage.CacheWrite1hTokens != 2000 {
		t.Errorf("usage = %+v (reason %q); want input 13100, output 510, no request count, 1h writes 2000", usage, report.UsageUnknownReason)
	}
	if report.Cost == nil || *report.Cost.Amount != 0.03 || report.Cost.Evidence != "claude transcript cost-state s2" {
		t.Errorf("cost = %+v; want the recorded 0.03 with its evidence", report.Cost)
	}
}

func TestImportClaudeLeavesUsageUnknownRatherThanZero(t *testing.T) {
	request := assistantLine("s3", "req-1", "msg-1", "m-opus", usageJSON(10, 0, 0, 0, 0, 5))
	for _, test := range []struct {
		name, reason, status string
		write                func(t *testing.T, main string)
	}{
		{
			// Oracle: transcript output 5 against recorded 4 is "differs" by +1,
			// so neither usage nor cost is emitted.
			name: "cost-state differs", status: StatusComplete,
			reason: "transcript usage and the recorded cost-state differ (or cannot be compared) for model(s) m-opus (differs); usage and cost are withheld",
			write: func(t *testing.T, main string) {
				writeLines(t, main, request,
					costStateLine("s3", 0.1, `"m-opus":{"inputTokens":10,"outputTokens":4,"cacheReadInputTokens":0,"cacheCreationInputTokens":0}`))
			},
		},
		{
			// Oracle: a directory named like an agent transcript cannot be
			// read, so the import is incomplete and usage is unknown.
			name: "subagent unreadable", status: StatusIncomplete,
			reason: "the import is incomplete; see incomplete_reasons",
			write: func(t *testing.T, main string) {
				writeLines(t, main, request)
				if err := os.MkdirAll(filepath.Join(filepath.Dir(main), "s3", "subagents", "agent-a2.jsonl"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			main := filepath.Join(t.TempDir(), "s3.jsonl")
			test.write(t, main)
			report, err := Import(t.Context(), Options{ClaudeTranscript: main})
			if err != nil {
				t.Fatal(err)
			}
			if report.Status != test.status || report.Usage != nil || report.Cost != nil || report.UsageUnknownReason != test.reason {
				t.Errorf("status %q, usage %+v, cost %+v, reason %q; want %q, no usage, no cost, reason %q",
					report.Status, report.Usage, report.Cost, report.UsageUnknownReason, test.status, test.reason)
			}
			// Oracle: the one request is still counted in the totals.
			if report.Totals.Requests != 1 || report.Totals.OutputTokens != 5 {
				t.Errorf("totals = %+v; want 1 request, 5 output tokens", report.Totals)
			}
		})
	}
}

func TestImportClaudeRefusesARenamedCopy(t *testing.T) {
	// Oracle: every line names session s1 but the file says s9, so no line
	// matches its session and the import fails instead of reporting zero.
	main := filepath.Join(t.TempDir(), "s9.jsonl")
	writeLines(t, main, assistantLine("s1", "req-1", "msg-1", "m-opus", usageJSON(10, 0, 0, 0, 0, 5)))
	_, err := Import(t.Context(), Options{ClaudeTranscript: main})
	if err == nil || !strings.Contains(err.Error(), `has no lines for session "s9"`) {
		t.Fatalf("Import of a renamed copy: %v; want the no-lines-for-session error", err)
	}
}
