package usagelog

// Fixture tests for the Codex rollout import. Every line is written by hand
// from the fields codexMeta, codexTurnContext, codexUsageRecord and
// codexUsage read; none comes from a real rollout. The expected values are
// computed by hand next to each assertion.

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
)

func TestImportCodexSumsRootAndDescendantRollouts(t *testing.T) {
	sessions := filepath.Join(t.TempDir(), "sessions")
	day := filepath.Join(sessions, "2026", "11", "01")
	root := filepath.Join(day, "rollout-root.jsonl")
	resp1 := `{"type":"token_usage_record","payload":{"response_id":"resp-1","thread_id":"root","usage":{"input_tokens":100,"cached_input_tokens":60,"cache_write_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5}}}`
	writeLines(t, root,
		`{"type":"session_meta","payload":{"id":"root"}}`,
		`{"type":"turn_context","payload":{"model":"m-codex","effort":"high"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":null}}`,
		resp1,
		resp1, // the same response id again: a duplicate
		`{"type":"token_usage_record","payload":{"response_id":"resp-x","thread_id":"other","usage":{"input_tokens":9,"cached_input_tokens":0,"output_tokens":1}}}`,
		// 20 cache writes do not fit in input 50 beside 40 cached, so they
		// are kept outside input: uncached 50-40 = 10.
		`{"type":"token_usage_record","payload":{"response_id":"resp-2","usage":{"input_tokens":50,"cached_input_tokens":40,"cache_write_input_tokens":20,"output_tokens":5}}}`,
		`{"type":"token_usage_record","payload":{"response_id":"resp-3"}}`,
		`not json`,
	)
	writeLines(t, filepath.Join(day, "rollout-child.jsonl"),
		`{"type":"session_meta","payload":{"id":"child","parent_thread_id":"root","agent_nickname":"Ada","agent_role":"reviewer","agent_path":"/root/child"}}`,
		`{"type":"turn_context","payload":{"model":"m-codex-mini","effort":"low"}}`,
		`{"type":"token_usage_record","payload":{"response_id":"resp-c1","usage":{"input_tokens":30,"cached_input_tokens":0,"output_tokens":3}}}`,
	)
	writeLines(t, filepath.Join(day, "rollout-old.jsonl"), `{"id":"old","timestamp":"2025-01-01T00:00:00Z","instructions":""}`)
	writeLines(t, filepath.Join(day, "rollout-stranger.jsonl"), `{"type":"session_meta","payload":{"id":"stranger"}}`)
	prices, err := ParsePriceTable([]byte(`{"currency":"USD","evidence":"synthetic","models":{` +
		`"m-codex":{"input_per_million":1,"output_per_million":4,"cache_write_5m_per_million":1.25,"cache_read_per_million":0.1},` +
		`"m-codex-mini":{"input_per_million":0.5,"output_per_million":2}}}`))
	if err != nil {
		t.Fatal(err)
	}

	report, err := Import(t.Context(), Options{CodexSession: root, Prices: &prices})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(sessions)
	if err != nil {
		t.Fatal(err)
	}
	// Oracle: the scan reads four rollouts under the nearest "sessions"
	// directory: the root, its one child, a legacy header (other format)
	// and an unrelated session.
	if want := (ChildDiscovery{Status: "scanned", SessionsRoot: resolved, FilesScanned: 4, OtherFormatFiles: 1, RootFound: true, Descendants: 1}); report.ChildDiscovery == nil || *report.ChildDiscovery != want {
		t.Errorf("child discovery = %+v; want %+v", report.ChildDiscovery, want)
	}
	if report.Status != StatusComplete || report.SessionID != "root" || len(report.Agents) != 2 {
		t.Fatalf("status %q, session %q, %d agents; want complete, root, 2", report.Status, report.SessionID, len(report.Agents))
	}
	// Oracle: resp-1 is uncached 100-60-10 = 30, read 60, writes 10, out 20,
	// reasoning 5; resp-2 is uncached 10, read 40, writes 20, out 5. The
	// child's resp-c1 is uncached 30, out 3. Codex writes carry no TTL, so
	// they are unsplit.
	wantRoot := Tokens{Requests: 2, UncachedInputTokens: 40, CacheWriteUnsplitTokens: 30, CacheReadTokens: 100, OutputTokens: 25,
		ThinkingTokens: ptr(5), ThinkingReportedRequests: 1, TotalInputTokens: 40 + 30 + 100}
	wantChild := Tokens{Requests: 1, UncachedInputTokens: 30, OutputTokens: 3, TotalInputTokens: 30}
	main, child := report.Agents[0], report.Agents[1]
	if main.Role != "main" || !reflect.DeepEqual(main.Files, []string{"2026/11/01/rollout-root.jsonl"}) || !reflect.DeepEqual(main.Tokens, wantRoot) ||
		!reflect.DeepEqual(main.ModelsSeen, []string{"m-codex"}) || !reflect.DeepEqual(main.EffortsSeen, []string{"high"}) {
		t.Errorf("root agent = %+v; want main, its rollout, %+v, m-codex, high", main, wantRoot)
	}
	if child.AgentID != "child" || child.Role != "subagent" || child.Depth != 1 || child.ParentID != "root" || child.Nickname != "Ada" ||
		child.AgentRole != "reviewer" || !reflect.DeepEqual(child.Tokens, wantChild) || !reflect.DeepEqual(child.EffortsSeen, []string{"low"}) {
		t.Errorf("child agent = %+v; want child of root at depth 1, Ada the reviewer, %+v, low", child, wantChild)
	}
	if want := (Skipped{MalformedLines: 1, NoUsageEntries: 1, DuplicateEntries: 1, OtherSessionEntries: 1, IgnoredTokenCountEvents: 1}); report.Skipped != want {
		t.Errorf("skipped = %+v; want %+v", report.Skipped, want)
	}
	if want := (Anomalies{CodexCacheWriteOutsideInputRequests: 1}); report.Anomalies != want {
		t.Errorf("anomalies = %+v; want %+v", report.Anomalies, want)
	}
	// Oracle: no recorded dollars. At the table, per million: m-codex
	// 40x1 + 25x4 + 100x0.1 + 30x1.25 (writes at the 5m rate) = 187.5;
	// m-codex-mini 30x0.5 + 3x2 = 21; total 208.5, so 0.0002085.
	if report.Dollars.Recorded != nil || report.Dollars.RecordedUnknownReason != "codex rollouts record no dollar cost" {
		t.Errorf("recorded = %+v (%q); want none, no dollar cost", report.Dollars.Recorded, report.Dollars.RecordedUnknownReason)
	}
	if table := report.Dollars.PriceTable; table == nil || table.Total == nil || math.Abs(*table.Total-0.0002085) > 1e-12 {
		t.Errorf("price table = %+v; want total 0.0002085", table)
	}
	// Oracle: usage is the totals, 200 input of which 70 uncached and 100
	// read, 28 output, 3 requests; reasoning is known for 1 of 3 requests,
	// so thinking is unknown. No cost, since Codex records none.
	usage := report.Usage
	if usage == nil || *usage.InputTokens != 200 || *usage.UncachedInputTokens != 70 || *usage.CacheReadTokens != 100 || *usage.OutputTokens != 28 ||
		*usage.APIRequests != 3 || usage.ThinkingTokens != nil || report.Cost != nil {
		t.Errorf("usage = %+v, cost %+v; want input 200, uncached 70, read 100, output 28, requests 3, no thinking, no cost", usage, report.Cost)
	}
}

func TestImportCodexWithoutSessionsRootIsIncomplete(t *testing.T) {
	// Oracle: no ancestor is named "sessions" and no root is given, so
	// children cannot be found: the request is counted, but the import is
	// incomplete and usage is unknown rather than a partial number.
	root := filepath.Join(t.TempDir(), "rollouts", "rollout-root.jsonl")
	writeLines(t, root,
		`{"type":"session_meta","payload":{"id":"root"}}`,
		`{"type":"turn_context","payload":{"model":"m-codex"}}`,
		`{"type":"token_usage_record","payload":{"response_id":"resp-1","usage":{"input_tokens":100,"cached_input_tokens":60,"output_tokens":20}}}`,
	)
	report, err := Import(t.Context(), Options{CodexSession: root})
	if err != nil {
		t.Fatal(err)
	}
	if report.ChildDiscovery == nil || report.ChildDiscovery.Status != "not_attempted" || report.Status != StatusIncomplete ||
		!reflect.DeepEqual(report.IncompleteReasons, []string{"child_discovery_not_attempted"}) {
		t.Fatalf("discovery %+v, status %q, reasons %v; want not_attempted, incomplete, [child_discovery_not_attempted]",
			report.ChildDiscovery, report.Status, report.IncompleteReasons)
	}
	if report.Totals.Requests != 1 || report.Totals.TotalInputTokens != 100 || report.Usage != nil || report.UsageUnknownReason != "the import is incomplete; see incomplete_reasons" {
		t.Errorf("totals %+v, usage %+v (%q); want 1 request of 100 input, usage unknown", report.Totals, report.Usage, report.UsageUnknownReason)
	}
}
