//go:build darwin || linux

package usagelog_test

// Independent tests for contract metrics-history revision 2 (requirements
// R16-R20), written from requirements-r2.md, requirements-r1.md and
// contract-usagelog-r2.json only. Helpers such as cleanSession, recordJSON,
// historyLines, wantCounts and padRecord come from the revision 1 tests in
// history_test.go (same package). Every expected value is computed by hand in
// the comments next to the fixture or the assertion.
//
// The file is limited to darwin and linux because the lock tests (R17) hold
// flock(2) with syscall.Flock and the size-bound tests (R19) rely on sparse
// files and syscall.Stat_t.Blocks.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
)

// ---------------------------------------------------------------------------
// Codex rollout fixture builders (format from requirements-r2.md).

const (
	cxSol  = "gpt-6.1-sol"
	cxMini = "gpt-6.1-mini"
)

// cxUsage is one token_usage_record's usage; reasoning nil omits
// reasoning_output_tokens.
type cxUsage struct {
	in, cached, write, out int64
	reasoning              *int64
}

func cxMeta(t *testing.T, id, session, parent string, extra map[string]any) string {
	t.Helper()
	payload := map[string]any{"id": id, "session_id": session, "cwd": "/x"}
	if parent != "" {
		payload["parent_thread_id"] = parent
	}
	for k, v := range extra {
		payload[k] = v
	}
	return hjson(t, map[string]any{"type": "session_meta", "payload": payload})
}

func cxTurn(t *testing.T, model, effort string) string {
	t.Helper()
	return hjson(t, map[string]any{"type": "turn_context", "payload": map[string]any{"model": model, "effort": effort}})
}

// cxRecord is a token_usage_record line; ts "" omits the top-level timestamp
// and response "" omits response_id (an unkeyed request).
func cxRecord(t *testing.T, ts, response, thread string, u cxUsage) string {
	t.Helper()
	usage := map[string]any{
		"input_tokens": u.in, "cached_input_tokens": u.cached,
		"cache_write_input_tokens": u.write, "output_tokens": u.out,
	}
	if u.reasoning != nil {
		usage["reasoning_output_tokens"] = *u.reasoning
	}
	payload := map[string]any{"thread_id": thread, "usage": usage}
	if response != "" {
		payload["response_id"] = response
	}
	line := map[string]any{"type": "token_usage_record", "payload": payload}
	if ts != "" {
		line["timestamp"] = ts
	}
	return hjson(t, line)
}

type cxTree struct {
	sessions, root, child, grandchild string
}

// cxSession writes a Codex session T1 under a sessions root that is not named
// "sessions" (so Options.CodexSessionsRoot is required):
//
//	root T1 (session T1), turn sol/max:
//	  r1  2026-11-06T10:00:00.123Z  in 100 cached 60 write 10 out 20 reasoning 5   (line repeated: one request)
//	      -> uncached 100-60-10 = 30, read 60, unsplit 10, out 20, thinking 5
//	  --  2026-11-06T11:00:00Z      no response_id: counted, unkeyed, never written
//	  turn mini/low:
//	  r2  2026-11-07T02:30:00+05:00 in 50 out 8, no reasoning field
//	      -> 2026-11-06T21:30:00Z, uncached 50, thinking null
//	  r5  2026-11-07T23:30:00-02:00 in 9 cached 4 write 2 out 3 reasoning 0
//	      -> 2026-11-08T01:30:00Z, uncached 3, read 4, unsplit 2, out 3, thinking 0
//	child T2 (parent T1, agent_role explorer), turn sol/high:
//	  r3  (no timestamp)            in 40 cached 30 out 10 reasoning 2 -> uncached 10, read 30, thinking 2
//	grandchild T3 (parent T2, no agent_role), turn sol/medium:
//	  r4  "not a time"              in 20 write 5 out 4 reasoning 1 -> uncached 15, unsplit 5, thinking 1
//	unrelated T9 and its child T10: never imported.
//
// Import: requests r1, unkeyed, r2, r5, r3, r4 = 6; keyed 5.
func cxSession(t *testing.T) cxTree {
	t.Helper()
	sessions := filepath.Join(t.TempDir(), "codex-archive")
	tree := cxTree{
		sessions:   sessions,
		root:       filepath.Join(sessions, "2026", "11", "06", "rollout-2026-11-06T10-00-00-T1.jsonl"),
		child:      filepath.Join(sessions, "2026", "11", "06", "rollout-2026-11-06T10-05-00-T2.jsonl"),
		grandchild: filepath.Join(sessions, "2026", "11", "07", "rollout-2026-11-07T09-00-00-T3.jsonl"),
	}
	r1 := cxRecord(t, "2026-11-06T10:00:00.123Z", "r1", "T1", cxUsage{100, 60, 10, 20, i64(5)})
	writeFileLines(t, tree.root,
		cxMeta(t, "T1", "T1", "", nil),
		cxTurn(t, cxSol, "max"),
		r1,
		r1,
		cxRecord(t, "2026-11-06T11:00:00Z", "", "T1", cxUsage{7, 0, 0, 1, i64(0)}),
		cxTurn(t, cxMini, "low"),
		cxRecord(t, "2026-11-07T02:30:00+05:00", "r2", "T1", cxUsage{50, 0, 0, 8, nil}),
		cxRecord(t, "2026-11-07T23:30:00-02:00", "r5", "T1", cxUsage{9, 4, 2, 3, i64(0)}),
	)
	writeFileLines(t, tree.child,
		cxMeta(t, "T2", "T1", "T1", map[string]any{"agent_role": "explorer", "agent_nickname": "scout"}),
		cxTurn(t, cxSol, "high"),
		cxRecord(t, "", "r3", "T2", cxUsage{40, 30, 0, 10, i64(2)}),
	)
	writeFileLines(t, tree.grandchild,
		cxMeta(t, "T3", "T1", "T2", nil),
		cxTurn(t, cxSol, "medium"),
		cxRecord(t, "not a time", "r4", "T3", cxUsage{20, 0, 5, 4, i64(1)}),
	)
	writeFileLines(t, filepath.Join(sessions, "2026", "11", "06", "rollout-2026-11-06T12-00-00-T9.jsonl"),
		cxMeta(t, "T9", "T9", "", nil),
		cxTurn(t, cxSol, "max"),
		cxRecord(t, "2026-11-06T12:00:00Z", "r9", "T9", cxUsage{9999, 0, 0, 9999, i64(0)}),
	)
	writeFileLines(t, filepath.Join(sessions, "2026", "11", "06", "rollout-2026-11-06T12-05-00-T10.jsonl"),
		cxMeta(t, "T10", "T9", "T9", map[string]any{"agent_role": "explorer"}),
		cxTurn(t, cxSol, "max"),
		cxRecord(t, "2026-11-06T12:05:00Z", "r10", "T10", cxUsage{8888, 0, 0, 8888, i64(0)}),
	)
	return tree
}

func cxAppend(t *testing.T, history string, tree cxTree) usagelog.AppendResult {
	t.Helper()
	result, err := usagelog.AppendHistory(t.Context(), history, usagelog.Options{CodexSession: tree.root, CodexSessionsRoot: tree.sessions})
	if err != nil {
		t.Fatalf("AppendHistory(codex %s) failed: %v", tree.root, err)
	}
	checkCountsSum(t, result)
	return result
}

// cxWant lists the five keyed records of cxSession.
var cxWant = []wantRecord{
	{requestID: "response:r1", agentID: "T1", role: "main", model: cxSol, timestamp: "2026-11-06T10:00:00.123Z", uncached: 30, unsplit: 10, read: 60, out: 20, thinking: i64(5)},
	{requestID: "response:r2", agentID: "T1", role: "main", model: cxMini, timestamp: "2026-11-06T21:30:00Z", uncached: 50, out: 8},
	{requestID: "response:r5", agentID: "T1", role: "main", model: cxMini, timestamp: "2026-11-08T01:30:00Z", uncached: 3, unsplit: 2, read: 4, out: 3, thinking: i64(0)},
	{requestID: "response:r3", agentID: "T2", role: "subagent", agentType: "explorer", model: cxSol, uncached: 10, read: 30, out: 10, thinking: i64(2)},
	{requestID: "response:r4", agentID: "T3", role: "subagent", model: cxSol, uncached: 15, unsplit: 5, out: 4, thinking: i64(1)},
}

// cxCheckRecord is checkRecord for a Codex record of session T1.
func cxCheckRecord(t *testing.T, got storedLine, w wantRecord, before, after time.Time) {
	t.Helper()
	r := got.rec
	name := "R16 record " + w.requestID
	if r.V != 1 || r.Kind != usagelog.HistoryRecordKind || r.Source != usagelog.SourceCodex || r.SessionID != "T1" {
		t.Fatalf("%s: v %d kind %q source %q session %q, want 1 %q %q T1 (the root rollout's session id)",
			name, r.V, r.Kind, r.Source, r.SessionID, usagelog.HistoryRecordKind, usagelog.SourceCodex)
	}
	if r.RequestID != w.requestID || r.AgentID != w.agentID || r.AgentRole != w.role || r.Model != w.model {
		t.Fatalf("%s: request_id %q agent_id %q agent_role %q model %q, want %q %q %q %q",
			name, r.RequestID, r.AgentID, r.AgentRole, r.Model, w.requestID, w.agentID, w.role, w.model)
	}
	if w.agentType == "" {
		if v, ok := got.obj["agent_type"]; ok {
			t.Fatalf("%s: agent_type %v present, want omitted (no agent_role in the rollout)", name, v)
		}
	} else if r.AgentType != w.agentType {
		t.Fatalf("%s: agent_type %q, want the child's agent_role %q", name, r.AgentType, w.agentType)
	}
	if w.timestamp == "" {
		if v, ok := got.obj["timestamp"]; ok {
			t.Fatalf("%s: timestamp %v present, want omitted (absent or not RFC 3339)", name, v)
		}
	} else {
		gotTS, err := time.Parse(time.RFC3339, r.Timestamp)
		wantTS, _ := time.Parse(time.RFC3339, w.timestamp)
		if err != nil || !gotTS.Equal(wantTS) || !strings.HasSuffix(r.Timestamp, "Z") {
			t.Fatalf("%s: timestamp %q, want the instant %s in UTC RFC 3339", name, r.Timestamp, w.timestamp)
		}
	}
	imported, err := time.Parse(time.RFC3339, r.ImportedAt)
	if err != nil || !strings.HasSuffix(r.ImportedAt, "Z") || imported.Before(before) || imported.After(after) {
		t.Fatalf("%s: imported_at %q, want UTC RFC 3339 within [%s, %s]", name, r.ImportedAt, before, after)
	}
	if r.UncachedInputTokens != w.uncached || r.CacheWrite5mTokens != 0 || r.CacheWrite1hTokens != 0 ||
		r.CacheWriteUnsplitTokens != w.unsplit || r.CacheReadTokens != w.read || r.OutputTokens != w.out {
		t.Fatalf("%s: tokens uncached %d w5 %d w1 %d unsplit %d read %d out %d; want %d 0 0 %d %d %d", name,
			r.UncachedInputTokens, r.CacheWrite5mTokens, r.CacheWrite1hTokens, r.CacheWriteUnsplitTokens, r.CacheReadTokens, r.OutputTokens,
			w.uncached, w.unsplit, w.read, w.out)
	}
	if w.thinking == nil {
		if v, ok := got.obj["thinking_tokens"]; !ok || v != nil {
			t.Fatalf("%s: thinking_tokens = %v (present %v), want JSON null (reasoning_output_tokens absent)", name, v, ok)
		}
	} else if r.ThinkingTokens == nil || *r.ThinkingTokens != *w.thinking {
		t.Fatalf("%s: thinking_tokens %v, want %d (reasoning_output_tokens)", name, r.ThinkingTokens, *w.thinking)
	}
}

func identities(lines []storedLine) map[string]storedLine {
	m := map[string]storedLine{}
	for _, l := range lines {
		m[identity(l.rec)] = l
	}
	return m
}

// ---------------------------------------------------------------------------
// R16: Codex append.

// Fails when a Codex record has the wrong source, session, request_id prefix,
// agent fields, timestamp conversion or token mapping, when an unkeyed request
// is written, or when an unrelated rollout is imported.
func TestAppendHistoryCodexRecordsEachKeyedRequest(t *testing.T) {
	tree := cxSession(t)
	history := filepath.Join(t.TempDir(), "history.jsonl")
	before := time.Now().UTC().Truncate(time.Second)
	result := cxAppend(t, history, tree)
	after := time.Now().UTC().Add(time.Second)

	// R2: 6 requests; 5 keyed appended, 1 unkeyed.
	wantCounts(t, "R16 first codex append", result, 6, 5, 0, 0, 1, 0)
	if result.SchemaVersion != 1 || result.Kind != usagelog.HistoryAppendKind || result.Source != usagelog.SourceCodex || result.SessionID != "T1" {
		t.Fatalf("R16: result schema %d kind %q source %q session %q, want 1 %q %q T1",
			result.SchemaVersion, result.Kind, result.Source, result.SessionID, usagelog.HistoryAppendKind, usagelog.SourceCodex)
	}
	if !filepath.IsAbs(result.History) || !samePathH(result.History, history) {
		t.Fatalf("R16: result history %q, want absolute %q", result.History, history)
	}

	lines := historyLines(t, history)
	if len(lines) != len(cxWant) {
		t.Fatalf("R16: %d history lines, want %d (unkeyed and unrelated requests are never written)", len(lines), len(cxWant))
	}
	got := byRequest(t, lines)
	for _, w := range cxWant {
		line, ok := got[w.requestID]
		if !ok {
			t.Fatalf("R16: no record %s; have %v", w.requestID, slices.Sorted(maps.Keys(got)))
		}
		cxCheckRecord(t, line, w, before, after)
	}
	data := string(readBytes(t, history))
	for _, foreign := range []string{"r9", "r10", "T9", "T10"} {
		if strings.Contains(data, `"`+foreign+`"`) || strings.Contains(data, "response:"+foreign+`"`) {
			t.Fatalf("R16: history holds the unrelated rollout's %s:\n%s", foreign, data)
		}
	}
}

// R16 with R3: re-importing unchanged rollouts appends nothing.
// Fails when a Codex record is not recognised as already present.
func TestAppendHistoryCodexReimportIsIdempotent(t *testing.T) {
	tree := cxSession(t)
	history := filepath.Join(t.TempDir(), "history.jsonl")
	wantCounts(t, "R16 first codex append", cxAppend(t, history, tree), 6, 5, 0, 0, 1, 0)
	stored := readBytes(t, history)
	for i := range 2 {
		again := cxAppend(t, history, tree)
		wantCounts(t, fmt.Sprintf("R3 codex re-import %d", i+1), again, 6, 0, 5, 0, 1, 0)
		if now := readBytes(t, history); !bytes.Equal(now, stored) {
			t.Fatalf("R3: codex re-import %d changed the history:\nbefore %q\nafter  %q", i+1, stored, now)
		}
	}
}

// R16 with R4: a new line in the root rollout, a new line in an existing child
// and a new child rollout are appended once; stored lines stay byte-identical.
//
//	root  + turn sol/max, r6 2026-11-08T06:00:00Z in 5 out 1 reasoning 0 -> T1 main sol uncached 5 thinking 0
//	T2    + r8 (no timestamp) in 3 out 1, no reasoning -> T2 subagent explorer sol uncached 3 thinking null
//	T4    new child of T1 (agent_role worker), turn mini/low,
//	        r7 2026-11-08T05:00:00Z in 11 cached 1 out 2 reasoning 1 -> uncached 10 read 1 thinking 1
//
// Re-import: requests 6 + 3 = 9; appended 3, already_present 5, unkeyed 1.
func TestAppendHistoryCodexGrowthAppendsOnlyNewRequests(t *testing.T) {
	tree := cxSession(t)
	history := filepath.Join(t.TempDir(), "history.jsonl")
	cxAppend(t, history, tree)
	stored := readBytes(t, history)

	appendFileLines(t, tree.root,
		cxTurn(t, cxSol, "max"),
		cxRecord(t, "2026-11-08T06:00:00Z", "r6", "T1", cxUsage{5, 0, 0, 1, i64(0)}))
	appendFileLines(t, tree.child,
		cxRecord(t, "", "r8", "T2", cxUsage{3, 0, 0, 1, nil}))
	writeFileLines(t, filepath.Join(tree.sessions, "2026", "11", "08", "rollout-2026-11-08T05-00-00-T4.jsonl"),
		cxMeta(t, "T4", "T1", "T1", map[string]any{"agent_role": "worker"}),
		cxTurn(t, cxMini, "low"),
		cxRecord(t, "2026-11-08T05:00:00Z", "r7", "T4", cxUsage{11, 1, 0, 2, i64(1)}))

	before := time.Now().UTC().Truncate(time.Second)
	grown := cxAppend(t, history, tree)
	after := time.Now().UTC().Add(time.Second)
	wantCounts(t, "R4 codex grown re-import", grown, 9, 3, 5, 0, 1, 0)
	if now := readBytes(t, history); !bytes.HasPrefix(now, stored) {
		t.Fatalf("R4: stored codex lines changed:\nbefore %q\nafter  %q", stored, now)
	}
	lines := historyLines(t, history) // each identity exactly once
	if len(lines) != 8 {
		t.Fatalf("R4: %d lines, want 8", len(lines))
	}
	got := byRequest(t, lines)
	for _, w := range []wantRecord{
		{requestID: "response:r6", agentID: "T1", role: "main", model: cxSol, timestamp: "2026-11-08T06:00:00Z", uncached: 5, out: 1, thinking: i64(0)},
		{requestID: "response:r8", agentID: "T2", role: "subagent", agentType: "explorer", model: cxSol, uncached: 3, out: 1},
		{requestID: "response:r7", agentID: "T4", role: "subagent", agentType: "worker", model: cxMini, timestamp: "2026-11-08T05:00:00Z", uncached: 10, read: 1, out: 2, thinking: i64(1)},
	} {
		line, ok := got[w.requestID]
		if !ok {
			t.Fatalf("R4: grown request %s was not appended", w.requestID)
		}
		cxCheckRecord(t, line, w, before, after)
	}
}

// R16 with R5: a stored Codex record with the same identity and different
// tokens is kept and counted as conflicting; a record with the same session
// and request id but source claude_transcript is a different identity.
func TestAppendHistoryCodexConflictKeepsStoredRecord(t *testing.T) {
	t.Run("rollout changed after import", func(t *testing.T) {
		tree := cxSession(t)
		history := filepath.Join(t.TempDir(), "history.jsonl")
		cxAppend(t, history, tree)
		stored := readBytes(t, history)
		data := string(readBytes(t, tree.child))
		changed := strings.Replace(data, `"output_tokens":10`, `"output_tokens":11`, 1)
		if changed == data {
			t.Fatal("fixture: r3 output_tokens not rewritten")
		}
		if err := os.WriteFile(tree.child, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		// r3 now conflicts; r1, r2, r5, r4 are present; 1 unkeyed.
		wantCounts(t, "R5 codex conflicting re-import", cxAppend(t, history, tree), 6, 0, 4, 1, 1, 0)
		if now := readBytes(t, history); !bytes.Equal(now, stored) {
			t.Fatalf("R5: history changed on a codex conflict:\nbefore %q\nafter  %q", stored, now)
		}
	})
	t.Run("pre-existing records", func(t *testing.T) {
		tree := cxSession(t)
		// Stored codex response:r1 has output 99 where the rollout says 20.
		codexR1 := recordJSON(t, hrec{session: "T1", requestID: "response:r1", model: cxSol, ts: "2026-11-06T10:00:00.123Z", uncached: 30, unsplit: 10, read: 60, out: 99, thinking: i64(5)},
			func(m map[string]any) { m["source"] = "codex_rollout"; m["agent_id"] = "T1" })
		// Same session and request id as r3 but a Claude source: not r3's identity.
		claudeR3 := recordJSON(t, hrec{session: "T1", requestID: "response:r3", model: cxSol, uncached: 10, read: 30, out: 10, thinking: i64(2)}, nil)
		seed := codexR1 + "\n" + claudeR3 + "\n"
		history := writeHistory(t, seed)
		result := cxAppend(t, history, tree)
		// r1 conflicts; r2, r5, r3, r4 appended; 1 unkeyed.
		wantCounts(t, "R5 codex append over seeds", result, 6, 4, 0, 1, 1, 0)
		if now := string(readBytes(t, history)); !strings.HasPrefix(now, seed) {
			t.Fatalf("R5: stored lines changed:\nbefore %q\nafter  %q", seed, now)
		}
		lines := historyLines(t, history)
		ids := identities(lines)
		if len(lines) != 6 {
			t.Fatalf("R5: %d lines, want 2 seeds + 4 appended", len(lines))
		}
		if r1, ok := ids["codex_rollout\x00T1\x00response:r1"]; !ok || r1.rec.OutputTokens != 99 {
			t.Fatalf("R5: codex response:r1 = %+v (present %v), want the stored output 99", r1.rec, ok)
		}
		if _, ok := ids["codex_rollout\x00T1\x00response:r3"]; !ok {
			t.Fatal("R16: codex response:r3 was not appended beside a claude_transcript record of the same request id")
		}
		report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
		if report.Records != 6 || report.DuplicateLines != 0 || report.DamagedLines != 0 {
			t.Fatalf("R16: records %d duplicates %d damaged %d, want 6 0 0", report.Records, report.DuplicateLines, report.DamagedLines)
		}
	})
}

// R16 with R12: the Codex records' UTC dates drive date filters and by_day.
// r2 (local 2026-11-07, UTC 2026-11-06) and r5 (local 2026-11-07, UTC
// 2026-11-08) fail these checks when the offset is not converted.
//
//	by_day 2026-11-06: r1 + r2  requests 2 uncached 80 unsplit 10 read 60 out 28 total 150 thinking 5 (1 reported)
//	by_day 2026-11-08: r5       requests 1 uncached 3  unsplit 2  read 4  out 3  total 9   thinking 0 (1 reported)
//	by_day unknown:    r3 + r4  requests 2 uncached 25 unsplit 5  read 30 out 14 total 60  thinking 3 (2 reported)
//	totals: requests 5 uncached 108 unsplit 17 read 94 out 45 total 219 thinking 8 (4 reported)
//	by_model mini (r2, r5): requests 2 uncached 53 unsplit 2 read 4 out 11 total 59 thinking 0 (1 reported)
//	by_model sol (r1, r3, r4): requests 3 uncached 55 unsplit 15 read 90 out 34 total 160 thinking 8 (3 reported)
func TestReadHistoryCodexRecordsUseUTCDates(t *testing.T) {
	tree := cxSession(t)
	history := filepath.Join(t.TempDir(), "history.jsonl")
	cxAppend(t, history, tree)

	report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
	if report.Records != 5 || report.Lines != 5 || report.DamagedLines != 0 || report.DuplicateLines != 0 {
		t.Fatalf("R16: records %d lines %d damaged %d duplicates %d, want 5 5 0 0", report.Records, report.Lines, report.DamagedLines, report.DuplicateLines)
	}
	checkGroup(t, "R16 totals", report.Totals, wantGroup{key: "", requests: 5, uncached: 108, unsplit: 17, read: 94, out: 45, tin: 219, thinking: i64(8), thinkingReported: 4})
	checkGroups(t, "R16 by_day", report.ByDay, []wantGroup{
		{key: "2026-11-06", requests: 2, uncached: 80, unsplit: 10, read: 60, out: 28, tin: 150, thinking: i64(5), thinkingReported: 1},
		{key: "2026-11-08", requests: 1, uncached: 3, unsplit: 2, read: 4, out: 3, tin: 9, thinking: i64(0), thinkingReported: 1},
		{key: "unknown", requests: 2, uncached: 25, unsplit: 5, read: 30, out: 14, tin: 60, thinking: i64(3), thinkingReported: 2},
	})
	checkGroups(t, "R16 by_model", report.ByModel, []wantGroup{
		{key: cxMini, requests: 2, uncached: 53, unsplit: 2, read: 4, out: 11, tin: 59, thinking: i64(0), thinkingReported: 1},
		{key: cxSol, requests: 3, uncached: 55, unsplit: 15, read: 90, out: 34, tin: 160, thinking: i64(8), thinkingReported: 3},
	})
	checkGroups(t, "R16 by_session", report.BySession, []wantGroup{
		{key: "T1", requests: 5, uncached: 108, unsplit: 17, read: 94, out: 45, tin: 219, thinking: i64(8), thinkingReported: 4},
	})

	for _, c := range []struct {
		name     string
		filter   usagelog.HistoryFilter
		records  int64
		uncached int64
		days     []string
	}{
		{"local day 2026-11-07 holds no UTC record", usagelog.HistoryFilter{Since: "2026-11-07", Until: "2026-11-07"}, 0, 0, nil},
		{"until 2026-11-06", usagelog.HistoryFilter{Until: "2026-11-06"}, 2, 80, []string{"2026-11-06"}},
		{"since 2026-11-08", usagelog.HistoryFilter{Since: "2026-11-08"}, 1, 3, []string{"2026-11-08"}},
		{"since drops undated", usagelog.HistoryFilter{Since: "2026-11-06"}, 3, 83, []string{"2026-11-06", "2026-11-08"}},
		{"model and until", usagelog.HistoryFilter{Model: cxMini, Until: "2026-11-07"}, 1, 50, []string{"2026-11-06"}},
		{"root session id", usagelog.HistoryFilter{SessionID: "T1"}, 5, 108, []string{"2026-11-06", "2026-11-08", "unknown"}},
		{"child thread id is not a session", usagelog.HistoryFilter{SessionID: "T2"}, 0, 0, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := readHistory(t, history, c.filter, nil)
			if got.Records != c.records || got.Totals.Requests != c.records || got.Totals.UncachedInputTokens != c.uncached {
				t.Fatalf("R16: %+v: records %d totals requests %d uncached %d, want %d %d %d",
					c.filter, got.Records, got.Totals.Requests, got.Totals.UncachedInputTokens, c.records, c.records, c.uncached)
			}
			var days []string
			for _, g := range got.ByDay {
				days = append(days, g.Key)
			}
			if !slices.Equal(days, c.days) {
				t.Fatalf("R16: %+v: by_day keys %q, want %q", c.filter, days, c.days)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// R17: the lock file H + ".lock".

const lockWaitBound = 30 * time.Second

type appendOutcome struct {
	result usagelog.AppendResult
	err    error
}

type readOutcome struct {
	report usagelog.HistoryReport
	err    error
}

// holdLock takes flock(how) on path through a descriptor of the test's own,
// without blocking; the returned release is idempotent and also runs at
// cleanup so a blocked call under test always finishes.
func holdLock(t *testing.T, path string, how int) func() {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB); err != nil {
		f.Close()
		t.Fatalf("test could not take flock on %s: %v", path, err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
		})
	}
	t.Cleanup(release)
	return release
}

// observation is how long the test watches a call that must stay blocked: 20
// times the same call's unlocked duration, between 300ms and 3s. It bounds
// the observation of "has not returned"; it never synchronises the work.
func observation(baseline time.Duration) time.Duration {
	return min(max(20*baseline, 300*time.Millisecond), 3*time.Second)
}

func stillBlocked[T any](t *testing.T, ch <-chan T, window time.Duration, what string) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("R17: %s returned while another descriptor holds the lock: %+v", what, v)
	case <-time.After(window):
	}
}

func await[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(lockWaitBound):
		t.Fatalf("R17: %s did not return within %s", what, lockWaitBound)
		panic("unreachable")
	}
}

func startAppend(ctx context.Context, history, transcript string) <-chan appendOutcome {
	ch := make(chan appendOutcome, 1)
	go func() {
		r, err := usagelog.AppendHistory(ctx, history, usagelog.Options{ClaudeTranscript: transcript})
		ch <- appendOutcome{r, err}
	}()
	return ch
}

func startRead(ctx context.Context, history string) <-chan readOutcome {
	ch := make(chan readOutcome, 1)
	go func() {
		r, err := usagelog.ReadHistory(ctx, history, usagelog.HistoryFilter{}, nil)
		ch <- readOutcome{r, err}
	}()
	return ch
}

func lockSeed(t *testing.T) string {
	t.Helper()
	return recordJSON(t, hrec{session: "s0", requestID: "old_1", model: hOpus, ts: "2026-11-01T00:00:00Z", uncached: 7, out: 1}, nil) + "\n" +
		recordJSON(t, hrec{session: "s0", requestID: "old_2", model: hOpus, ts: "2026-11-02T00:00:00Z", uncached: 5, out: 1}, nil) + "\n"
}

// Fails when ReadHistory or AppendHistory returns, or AppendHistory writes,
// while another descriptor holds LOCK_EX on H + ".lock", or when either does
// not complete normally after the release.
func TestHistoryLockExclusiveHolderBlocksReadAndAppend(t *testing.T) {
	t.Run("ReadHistory", func(t *testing.T) {
		history := writeHistory(t, lockSeed(t))
		start := time.Now()
		readHistory(t, history, usagelog.HistoryFilter{}, nil)
		window := observation(time.Since(start))

		release := holdLock(t, history+".lock", syscall.LOCK_EX)
		ch := startRead(context.Background(), history)
		stillBlocked(t, ch, window, "ReadHistory")
		release()
		got := await(t, ch, "ReadHistory after the lock was released")
		// old_1 (7) + old_2 (5): 2 records, uncached 12.
		if got.err != nil || got.report.Records != 2 || got.report.Totals.UncachedInputTokens != 12 {
			t.Fatalf("R17: ReadHistory after release: err %v records %d uncached %d, want nil 2 12",
				got.err, got.report.Records, got.report.Totals.UncachedInputTokens)
		}
	})
	t.Run("AppendHistory", func(t *testing.T) {
		main := cleanSession(t, false)
		seed := lockSeed(t)
		baseline := writeHistory(t, seed)
		start := time.Now()
		appendHistory(t, baseline, main)
		window := observation(time.Since(start))

		history := writeHistory(t, seed)
		release := holdLock(t, history+".lock", syscall.LOCK_EX)
		ch := startAppend(context.Background(), history, main)
		stillBlocked(t, ch, window, "AppendHistory")
		if now := readBytes(t, history); !bytes.Equal(now, []byte(seed)) {
			t.Fatalf("R17: AppendHistory wrote while another descriptor held LOCK_EX:\n%q", now)
		}
		release()
		got := await(t, ch, "AppendHistory after the lock was released")
		if got.err != nil {
			t.Fatalf("R17: AppendHistory after release failed: %v", got.err)
		}
		wantCounts(t, "R17 append after release", got.result, 4, 4, 0, 0, 0, 0)
		if n := len(historyLines(t, history)); n != 6 {
			t.Fatalf("R17: %d lines after the append, want 2 seeds + 4", n)
		}
	})
}

// Fails when ReadHistory takes an exclusive lock (a shared holder must not
// block it) or when AppendHistory proceeds while a reader holds LOCK_SH.
func TestHistoryLockSharedHolderAdmitsReadersOnly(t *testing.T) {
	main := cleanSession(t, false)
	seed := lockSeed(t)
	baseline := writeHistory(t, seed)
	start := time.Now()
	appendHistory(t, baseline, main)
	window := observation(time.Since(start))

	history := writeHistory(t, seed)
	release := holdLock(t, history+".lock", syscall.LOCK_SH)
	read := await(t, startRead(context.Background(), history), "ReadHistory while another reader holds LOCK_SH")
	if read.err != nil || read.report.Records != 2 {
		t.Fatalf("R17: ReadHistory beside a shared holder: err %v records %d, want nil 2", read.err, read.report.Records)
	}
	ch := startAppend(context.Background(), history, main)
	stillBlocked(t, ch, window, "AppendHistory beside a LOCK_SH holder")
	if now := readBytes(t, history); !bytes.Equal(now, []byte(seed)) {
		t.Fatalf("R17: AppendHistory wrote while a reader held LOCK_SH:\n%q", now)
	}
	release()
	got := await(t, ch, "AppendHistory after the shared lock was released")
	if got.err != nil {
		t.Fatalf("R17: AppendHistory after release failed: %v", got.err)
	}
	wantCounts(t, "R17 append after shared release", got.result, 4, 4, 0, 0, 0, 0)
}

// Fails when a call waiting for the lock ignores cancellation, returns an
// error that does not match context.Canceled, or writes the history.
func TestHistoryLockWaitHonoursCancellation(t *testing.T) {
	t.Run("AppendHistory", func(t *testing.T) {
		main := cleanSession(t, false)
		seed := lockSeed(t)
		baseline := writeHistory(t, seed)
		start := time.Now()
		appendHistory(t, baseline, main)
		window := observation(time.Since(start))

		history := writeHistory(t, seed)
		release := holdLock(t, history+".lock", syscall.LOCK_EX)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ch := startAppend(ctx, history, main)
		stillBlocked(t, ch, window, "AppendHistory")
		cancel()
		got := await(t, ch, "AppendHistory after cancellation (lock still held)")
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("R17: cancelled AppendHistory returned %v (%+v), want an error matching context.Canceled", got.err, got.result)
		}
		release()
		if now := readBytes(t, history); !bytes.Equal(now, []byte(seed)) {
			t.Fatalf("R17: a cancelled AppendHistory wrote the history:\n%q", now)
		}
	})
	t.Run("ReadHistory", func(t *testing.T) {
		history := writeHistory(t, lockSeed(t))
		start := time.Now()
		readHistory(t, history, usagelog.HistoryFilter{}, nil)
		window := observation(time.Since(start))

		release := holdLock(t, history+".lock", syscall.LOCK_EX)
		defer release()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ch := startRead(ctx, history)
		stillBlocked(t, ch, window, "ReadHistory")
		cancel()
		got := await(t, ch, "ReadHistory after cancellation (lock still held)")
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("R17: cancelled ReadHistory returned %v, want an error matching context.Canceled", got.err)
		}
	})
}

// Fails when the lock file is not created with mode 0600 or is deleted after
// an append or a read.
func TestHistoryLockFileIsPrivateAndKept(t *testing.T) {
	main := cleanSession(t, false)
	history := filepath.Join(t.TempDir(), "metrics", "history.jsonl")
	lock := history + ".lock"
	check := func(after string) {
		t.Helper()
		info, err := os.Lstat(lock)
		if err != nil {
			t.Fatalf("R17: %s: lock file %s missing (%v), want it kept", after, lock, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("R17: %s: lock file mode %v, want regular 0600", after, info.Mode())
		}
	}
	appendHistory(t, history, main)
	check("first append")
	appendHistory(t, history, main)
	check("second append")
	readHistory(t, history, usagelog.HistoryFilter{}, nil)
	check("read")
}

// Fails when AppendHistory follows a symlinked H + ".lock": it must fail,
// leave H unchanged, and neither create nor modify the link target.
func TestAppendHistoryRefusesSymlinkedLockFile(t *testing.T) {
	main := cleanSession(t, false)
	// Control: with a regular lock file the same append succeeds, so a failure
	// below is the symlink refusal and not a general failure.
	wantCounts(t, "R17 control append", appendHistory(t, writeHistory(t, lockSeed(t)), main), 4, 4, 0, 0, 0, 0)
	refuse := func(t *testing.T, history string) {
		t.Helper()
		before := readBytes(t, history)
		got := await(t, startAppend(context.Background(), history, main), "AppendHistory with a symlinked lock file")
		if got.err == nil {
			t.Fatalf("R17: AppendHistory with a symlinked lock file succeeded: %+v", got.result)
		}
		if now := readBytes(t, history); !bytes.Equal(now, before) {
			t.Fatalf("R17: AppendHistory wrote H despite the symlinked lock file:\n%q", now)
		}
		if info, err := os.Lstat(history + ".lock"); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("R17: the lock symlink was replaced or removed: %v %v", info, err)
		}
	}
	t.Run("link to a regular file", func(t *testing.T) {
		history := writeHistory(t, lockSeed(t))
		target := filepath.Join(t.TempDir(), "elsewhere.lock")
		if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, history+".lock"); err != nil {
			t.Fatal(err)
		}
		refuse(t, history)
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0o644 || !bytes.Equal(readBytes(t, target), []byte("keep\n")) {
			t.Fatalf("R17: the link target was modified: %v %v", info, err)
		}
	})
	t.Run("dangling link", func(t *testing.T) {
		history := writeHistory(t, lockSeed(t))
		target := filepath.Join(t.TempDir(), "absent.lock")
		if err := os.Symlink(target, history+".lock"); err != nil {
			t.Fatal(err)
		}
		refuse(t, history)
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("R17: the dangling link was followed and %s created (%v)", target, err)
		}
	})
	t.Run("link to the history itself", func(t *testing.T) {
		history := writeHistory(t, lockSeed(t))
		if err := os.Symlink(history, history+".lock"); err != nil {
			t.Fatal(err)
		}
		refuse(t, history)
	})
	t.Run("link to a directory", func(t *testing.T) {
		history := writeHistory(t, lockSeed(t))
		target := t.TempDir()
		if err := os.Symlink(target, history+".lock"); err != nil {
			t.Fatal(err)
		}
		refuse(t, history)
		if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
			t.Fatalf("R17: the linked directory was written: %v %v", entries, err)
		}
	})
}

// ---------------------------------------------------------------------------
// R18: long lines at and around the 64 KiB and 128 KiB buffer boundaries.

var longLineLengths = []int{4097, 65535, 65536, 65537, 131071, 131072, 131073, 262145}

func longLine(t *testing.T, kind string, n int) string {
	t.Helper()
	if kind == "padded record" {
		// A valid record padded with JSON whitespace: damaged by length alone.
		return padRecord(t, recordJSON(t, hrec{session: "s0", requestID: "long", model: hOpus, uncached: 1000, out: 1000}, nil), n)
	}
	return strings.Repeat("x", n)
}

func r18Valid(t *testing.T, id string, uncached int64) string {
	t.Helper()
	return recordJSON(t, hrec{session: "s0", requestID: id, model: hOpus, ts: "2026-11-04T08:00:00Z", uncached: uncached, out: 1}, nil)
}

// Fails when a long line is accepted, split into several damaged lines, stops
// the read, or shifts the numbers of later lines.
func TestReadHistoryLongLineIsOneDamagedLine(t *testing.T) {
	for _, kind := range []string{"padded record", "garbage"} {
		for _, n := range longLineLengths {
			t.Run(fmt.Sprintf("%s of %d bytes", kind, n), func(t *testing.T) {
				// Lines: 1 ok_1 (1), 2 long, 3 ok_3 (2), 4 short damaged, 5 ok_5 (4).
				history := writeHistory(t, strings.Join([]string{
					r18Valid(t, "ok_1", 1), longLine(t, kind, n), r18Valid(t, "ok_3", 2), "not json", r18Valid(t, "ok_5", 4),
				}, "\n")+"\n")
				report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
				if report.Lines != 5 || report.Records != 3 || report.DamagedLines != 2 || len(report.Damaged) != 2 ||
					report.Damaged[0].Line != 2 || report.Damaged[1].Line != 4 {
					t.Fatalf("R18: lines %d records %d damaged %d %+v, want 5 lines, 3 records, lines 2 and 4 damaged",
						report.Lines, report.Records, report.DamagedLines, report.Damaged)
				}
				// ok_1 + ok_3 + ok_5 = 1 + 2 + 4.
				if report.Totals.UncachedInputTokens != 7 {
					t.Fatalf("R18: totals uncached %d, want 7 (valid lines only)", report.Totals.UncachedInputTokens)
				}
			})
		}
	}
}

// Fails when an unterminated oversized final line is dropped, accepted or
// breaks the read.
func TestReadHistoryUnterminatedLongFinalLine(t *testing.T) {
	for _, kind := range []string{"padded record", "garbage"} {
		for _, n := range []int{4097, 65536, 65537, 131072, 131073} {
			t.Run(fmt.Sprintf("%s of %d bytes", kind, n), func(t *testing.T) {
				history := writeHistory(t, r18Valid(t, "ok_1", 1)+"\n"+r18Valid(t, "ok_2", 2)+"\n"+longLine(t, kind, n))
				report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
				if report.Lines != 3 || report.Records != 2 || report.DamagedLines != 1 || len(report.Damaged) != 1 || report.Damaged[0].Line != 3 {
					t.Fatalf("R18: lines %d records %d damaged %d %+v, want 3 lines, 2 records, line 3 damaged",
						report.Lines, report.Records, report.DamagedLines, report.Damaged)
				}
			})
		}
	}
}

// Fails when an append after an unterminated oversized final line glues its
// first record onto that line or writes anything but whole valid lines.
func TestAppendHistoryAfterUnterminatedLongFinalLine(t *testing.T) {
	for _, kind := range []string{"padded record", "garbage"} {
		for _, n := range []int{65536, 131073} {
			t.Run(fmt.Sprintf("%s of %d bytes", kind, n), func(t *testing.T) {
				main := cleanSession(t, false)
				seed := r18Valid(t, "ok_1", 1) + "\n" + longLine(t, kind, n)
				history := writeHistory(t, seed)
				result := appendHistory(t, history, main)
				wantCounts(t, "R18 append after a long unterminated line", result, 4, 4, 0, 0, 0, 0)
				if result.HistoryDamagedLines != 1 {
					t.Fatalf("R18: history_damaged_lines %d, want 1 (the long line)", result.HistoryDamagedLines)
				}
				data := string(readBytes(t, history))
				if !strings.HasPrefix(data, seed+"\n") {
					t.Fatalf("R18: the long final line was not terminated before the new records (tail %q)", tail([]byte(data)))
				}
				rest := strings.TrimPrefix(data, seed+"\n")
				if !strings.HasSuffix(rest, "\n") || strings.Count(rest, "\n") != 4 {
					t.Fatalf("R18: new records are not 4 whole lines: %q", rest)
				}
				for _, line := range strings.Split(strings.TrimSuffix(rest, "\n"), "\n") {
					if _, _, err := validRecord(line); err != nil {
						t.Fatalf("R18: new line is not a valid record (%v): %q", err, line)
					}
				}
				report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
				if report.Lines != 6 || report.Records != 5 || report.DamagedLines != 1 || len(report.Damaged) != 1 || report.Damaged[0].Line != 2 {
					t.Fatalf("R18: lines %d records %d damaged %d %+v, want 6 lines, 5 records, line 2 damaged",
						report.Lines, report.Records, report.DamagedLines, report.Damaged)
				}
			})
		}
	}
}

// Fails when AppendHistory stops reading identities at a long line and so
// appends requests that are already stored after it.
func TestAppendHistoryReadsIdentitiesPastLongLine(t *testing.T) {
	for _, n := range []int{65536, 131072} {
		t.Run(fmt.Sprintf("%d bytes", n), func(t *testing.T) {
			main := cleanSession(t, false)
			first := filepath.Join(t.TempDir(), "history.jsonl")
			appendHistory(t, first, main)
			seed := longLine(t, "garbage", n) + "\n" + string(readBytes(t, first))
			history := writeHistory(t, seed)
			result := appendHistory(t, history, main)
			wantCounts(t, "R18 re-import past a long line", result, 4, 0, 4, 0, 0, 0)
			if result.HistoryDamagedLines != 1 {
				t.Fatalf("R18: history_damaged_lines %d, want 1", result.HistoryDamagedLines)
			}
			if !bytes.Equal(readBytes(t, history), []byte(seed)) {
				t.Fatal("R18: the re-import past a long line changed the history")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// R19: MaxHistoryBytes.

// sparseHistory creates a sparse file of size bytes whose last bytes are tail
// (the rest are NUL bytes, one damaged line); it skips when the filesystem
// does not keep the file sparse.
func sparseHistory(t *testing.T, size int64, tail []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(tail, size-int64(len(tail))); err != nil {
		t.Fatal(err)
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		t.Fatal(err)
	}
	if int64(st.Blocks)*512 > 64<<20 {
		t.Skipf("filesystem allocated %d bytes for a sparse %d-byte file", int64(st.Blocks)*512, size)
	}
	return path
}

func fileTail(t *testing.T, path string, n int64) (int64, []byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	n = min(n, info.Size())
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, info.Size()-n); err != nil {
		t.Fatal(err)
	}
	return info.Size(), buf
}

// Fails when AppendHistory writes to a history over the bound, grows a history
// past the bound, or refuses a re-import that appends nothing at the bound.
func TestAppendHistorySizeBound(t *testing.T) {
	const bound = int64(usagelog.MaxHistoryBytes)
	main := cleanSession(t, false) // 4 keyed requests, each line far longer than 64 bytes
	// Control: the same append succeeds on a small history, so a refusal below
	// is the size bound and not a general failure.
	wantCounts(t, "R19 control append", appendHistory(t, filepath.Join(t.TempDir(), "control.jsonl"), main), 4, 4, 0, 0, 0, 0)
	refuse := func(t *testing.T, history, why string) {
		t.Helper()
		size, tailBefore := fileTail(t, history, 4096)
		result, err := usagelog.AppendHistory(t.Context(), history, usagelog.Options{ClaudeTranscript: main})
		if err == nil {
			t.Fatalf("R19: %s: AppendHistory succeeded (%+v), want an error", why, result)
		}
		sizeAfter, tailAfter := fileTail(t, history, 4096)
		if sizeAfter != size || !bytes.Equal(tailAfter, tailBefore) {
			t.Fatalf("R19: %s: the history changed (size %d -> %d)", why, size, sizeAfter)
		}
	}
	t.Run("existing history over the bound", func(t *testing.T) {
		refuse(t, sparseHistory(t, bound+1, []byte("\n")), "history of MaxHistoryBytes+1")
	})
	t.Run("append would cross the bound", func(t *testing.T) {
		refuse(t, sparseHistory(t, bound-64, []byte("\n")), "64 bytes below the bound")
	})
	t.Run("history at the bound gains lines", func(t *testing.T) {
		refuse(t, sparseHistory(t, bound, []byte("\n")), "history of exactly MaxHistoryBytes")
	})
	t.Run("no-op re-import at the bound", func(t *testing.T) {
		first := filepath.Join(t.TempDir(), "history.jsonl")
		appendHistory(t, first, main)
		stored := append([]byte("\n"), readBytes(t, first)...)
		history := sparseHistory(t, bound, stored)
		result := appendHistory(t, history, main)
		wantCounts(t, "R19 no-op re-import at the bound", result, 4, 0, 4, 0, 0, 0)
		if result.HistoryDamagedLines != 1 {
			t.Fatalf("R19: history_damaged_lines %d, want 1 (the NUL-filled line)", result.HistoryDamagedLines)
		}
		size, tailAfter := fileTail(t, history, int64(len(stored)))
		if size != bound || !bytes.Equal(tailAfter, stored) {
			t.Fatalf("R19: size %d (want %d) or stored records changed by a no-op re-import", size, bound)
		}
	})
}

// ---------------------------------------------------------------------------
// R20: filter error text (Go error of ReadHistory).

// Fails when a date-filter error does not name since/until, mentions
// "import options", or stops matching ErrInvalidOptions (R14).
func TestReadHistoryFilterErrorNamesTheFlag(t *testing.T) {
	history := writeHistory(t, lockSeed(t))
	for _, c := range []struct {
		name   string
		filter usagelog.HistoryFilter
		names  []string // the error must contain one of these
	}{
		{"malformed since", usagelog.HistoryFilter{Since: "2026/11/01"}, []string{"since"}},
		{"malformed until", usagelog.HistoryFilter{Until: "yesterday"}, []string{"until"}},
		{"since with a valid until", usagelog.HistoryFilter{Since: "2026-11-1", Until: "2026-11-30"}, []string{"since"}},
		{"until with a valid since", usagelog.HistoryFilter{Since: "2026-11-01", Until: "30-11-2026"}, []string{"until"}},
		{"since after until", usagelog.HistoryFilter{Since: "2026-11-03", Until: "2026-11-02"}, []string{"since", "until"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := usagelog.ReadHistory(t.Context(), history, c.filter, nil)
			if !errors.Is(err, usagelog.ErrInvalidOptions) {
				t.Fatalf("R14: ReadHistory(%+v) error %v, want ErrInvalidOptions", c.filter, err)
			}
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "import options") {
				t.Fatalf("R20: ReadHistory(%+v) error %q mentions import options", c.filter, err)
			}
			if !slices.ContainsFunc(c.names, func(name string) bool { return strings.Contains(msg, name) }) {
				t.Fatalf("R20: ReadHistory(%+v) error %q names none of %q", c.filter, err, c.names)
			}
		})
	}
}
