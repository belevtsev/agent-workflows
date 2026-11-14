package usagelog_test

// Independent tests for contract metrics-history revision 1 (requirements
// R1-R15), written from requirements-r1.md and contract-usagelog-r1.json only.
// Every expected value is computed by hand from the fixtures in this file; the
// arithmetic is in the comments next to each assertion.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
)

const (
	hOpus   = "claude-opus-5-5"
	hSonnet = "claude-sonnet-5"
	hHaiku  = "claude-haiku-5"
)

// ---------------------------------------------------------------------------
// Transcript fixture builders (Claude transcript format from the contract).

// tok is one request's usage. creation overrides cache_creation_input_tokens
// (default w5+w1); noSplit omits the cache_creation object.
type tok struct {
	in, w5, w1, read, out int64
	creation              *int64
	noSplit               bool
	thinking              *int64
}

type areq struct {
	session, requestID, messageID, model, timestamp string
	tok                                             tok
	content                                         []any
	extra                                           map[string]any
}

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }
func hjson(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assistantLine(t *testing.T, r areq) string {
	t.Helper()
	creation := r.tok.w5 + r.tok.w1
	if r.tok.creation != nil {
		creation = *r.tok.creation
	}
	usage := map[string]any{
		"input_tokens":                r.tok.in,
		"cache_creation_input_tokens": creation,
		"cache_read_input_tokens":     r.tok.read,
		"output_tokens":               r.tok.out,
	}
	if !r.tok.noSplit {
		usage["cache_creation"] = map[string]any{"ephemeral_5m_input_tokens": r.tok.w5, "ephemeral_1h_input_tokens": r.tok.w1}
	}
	if r.tok.thinking != nil {
		usage["output_tokens_details"] = map[string]any{"thinking_tokens": *r.tok.thinking}
	}
	content := r.content
	if content == nil {
		content = []any{}
	}
	message := map[string]any{"role": "assistant", "content": content, "usage": usage}
	if r.messageID != "" {
		message["id"] = r.messageID
	}
	if r.model != "" {
		message["model"] = r.model
	}
	line := map[string]any{"type": "assistant", "sessionId": r.session, "message": message}
	if r.requestID != "" {
		line["requestId"] = r.requestID
	}
	if r.timestamp != "" {
		line["timestamp"] = r.timestamp
	}
	for k, v := range r.extra {
		line[k] = v
	}
	return hjson(t, line)
}

func userLine(t *testing.T, session string, content any) string {
	t.Helper()
	return hjson(t, map[string]any{"type": "user", "sessionId": session, "message": map[string]any{"role": "user", "content": content}})
}

func writeFileLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendFileLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

func subagentFile(main, session, id string) string {
	return filepath.Join(filepath.Dir(main), session, "subagents", "agent-"+id+".jsonl")
}

func writeMeta(t *testing.T, main, session, id string, meta map[string]any) {
	t.Helper()
	path := filepath.Join(filepath.Dir(main), session, "subagents", "agent-"+id+".meta.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(hjson(t, meta)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func appendHistory(t *testing.T, history, transcript string) usagelog.AppendResult {
	t.Helper()
	result, err := usagelog.AppendHistory(t.Context(), history, usagelog.Options{ClaudeTranscript: transcript})
	if err != nil {
		t.Fatalf("AppendHistory(%s) failed: %v", transcript, err)
	}
	checkCountsSum(t, result)
	return result
}

// R2: every counted request lands in exactly one bucket.
func checkCountsSum(t *testing.T, r usagelog.AppendResult) {
	t.Helper()
	if sum := r.Appended + r.AlreadyPresent + r.Conflicting + r.Unkeyed + r.Rejected; sum != r.Requests {
		t.Fatalf("R2: appended %d + already_present %d + conflicting %d + unkeyed %d + rejected %d = %d, want requests %d",
			r.Appended, r.AlreadyPresent, r.Conflicting, r.Unkeyed, r.Rejected, sum, r.Requests)
	}
}

func wantCounts(t *testing.T, name string, r usagelog.AppendResult, requests, appended, present, conflicting, unkeyed, rejected int64) {
	t.Helper()
	if r.Requests != requests || r.Appended != appended || r.AlreadyPresent != present || r.Conflicting != conflicting || r.Unkeyed != unkeyed || r.Rejected != rejected {
		t.Fatalf("%s: counts requests %d appended %d already_present %d conflicting %d unkeyed %d rejected %d; want %d %d %d %d %d %d",
			name, r.Requests, r.Appended, r.AlreadyPresent, r.Conflicting, r.Unkeyed, r.Rejected,
			requests, appended, present, conflicting, unkeyed, rejected)
	}
}

// ---------------------------------------------------------------------------
// History line parsing with an independent validity oracle (R7's rules).

type storedLine struct {
	raw string
	rec usagelog.HistoryRecord
	obj map[string]any
}

func identity(r usagelog.HistoryRecord) string {
	return r.Source + "\x00" + r.SessionID + "\x00" + r.RequestID
}

func isRFC3339(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

// validRecord applies the R7 damage rules to a stored line.
func validRecord(line string) (usagelog.HistoryRecord, map[string]any, error) {
	var rec usagelog.HistoryRecord
	if len(line) > usagelog.MaxHistoryLineBytes {
		return rec, nil, fmt.Errorf("line of %d bytes", len(line))
	}
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return rec, nil, err
	}
	if dec.More() {
		return rec, nil, errors.New("trailing data")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return rec, nil, err
	}
	switch {
	case rec.V != 1:
		return rec, obj, fmt.Errorf("v %d", rec.V)
	case rec.Kind != "cw.metrics_request":
		return rec, obj, fmt.Errorf("kind %q", rec.Kind)
	case rec.Source != "claude_transcript" && rec.Source != "codex_rollout":
		return rec, obj, fmt.Errorf("source %q", rec.Source)
	case rec.SessionID == "" || rec.RequestID == "" || rec.AgentID == "" || rec.Model == "":
		return rec, obj, errors.New("empty identity field")
	case rec.AgentRole != "main" && rec.AgentRole != "subagent":
		return rec, obj, fmt.Errorf("agent_role %q", rec.AgentRole)
	case rec.Timestamp != "" && !isRFC3339(rec.Timestamp):
		return rec, obj, fmt.Errorf("timestamp %q", rec.Timestamp)
	case rec.ImportedAt != "" && !isRFC3339(rec.ImportedAt):
		return rec, obj, fmt.Errorf("imported_at %q", rec.ImportedAt)
	}
	for _, s := range []string{rec.Source, rec.SessionID, rec.RequestID, rec.AgentID, rec.AgentRole, rec.AgentType, rec.Model, rec.Timestamp, rec.ImportedAt} {
		if len(s) > usagelog.MaxHistoryLabelBytes {
			return rec, obj, fmt.Errorf("text field of %d bytes", len(s))
		}
	}
	for _, n := range []int64{rec.UncachedInputTokens, rec.CacheWrite5mTokens, rec.CacheWrite1hTokens, rec.CacheWriteUnsplitTokens, rec.CacheReadTokens, rec.OutputTokens} {
		if n < 0 {
			return rec, obj, errors.New("negative count")
		}
	}
	if rec.ThinkingTokens != nil && (*rec.ThinkingTokens < 0 || *rec.ThinkingTokens > rec.OutputTokens) {
		return rec, obj, errors.New("thinking out of range")
	}
	return rec, obj, nil
}

// historyLines requires every line of the file to be a whole, valid record and
// no identity to appear twice (R1, R6).
func historyLines(t *testing.T, path string) []storedLine {
	t.Helper()
	data := readBytes(t, path)
	if len(data) == 0 {
		return nil
	}
	if data[len(data)-1] != '\n' {
		t.Fatalf("history does not end with a newline: %q", tail(data))
	}
	var lines []storedLine
	seen := map[string]int{}
	for i, raw := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		rec, obj, err := validRecord(raw)
		if err != nil {
			t.Fatalf("history line %d is not a valid record (%v): %q", i+1, err, raw)
		}
		if prev, dup := seen[identity(rec)]; dup {
			t.Fatalf("history lines %d and %d share identity %q", prev, i+1, identity(rec))
		}
		seen[identity(rec)] = i + 1
		lines = append(lines, storedLine{raw: raw, rec: rec, obj: obj})
	}
	return lines
}

func tail(data []byte) []byte {
	if len(data) > 200 {
		return data[len(data)-200:]
	}
	return data
}

func byRequest(t *testing.T, lines []storedLine) map[string]storedLine {
	t.Helper()
	m := map[string]storedLine{}
	for _, l := range lines {
		m[l.rec.RequestID] = l
	}
	return m
}

type wantRecord struct {
	requestID, agentID, role, agentType, model, timestamp string
	uncached, w5, w1, unsplit, read, out                  int64
	thinking                                              *int64
}

func checkRecord(t *testing.T, got storedLine, session string, w wantRecord, before, after time.Time) {
	t.Helper()
	r := got.rec
	name := "record " + w.requestID
	if r.V != 1 || r.Kind != usagelog.HistoryRecordKind || r.Source != usagelog.SourceClaude || r.SessionID != session {
		t.Fatalf("%s: v %d kind %q source %q session %q", name, r.V, r.Kind, r.Source, r.SessionID)
	}
	if r.RequestID != w.requestID || r.AgentID != w.agentID || r.AgentRole != w.role || r.Model != w.model {
		t.Fatalf("%s: request_id %q agent_id %q agent_role %q model %q, want %q %q %q %q",
			name, r.RequestID, r.AgentID, r.AgentRole, r.Model, w.requestID, w.agentID, w.role, w.model)
	}
	if w.agentType == "" {
		if _, ok := got.obj["agent_type"]; ok {
			t.Fatalf("%s: agent_type %v present, want omitted", name, got.obj["agent_type"])
		}
	} else if r.AgentType != w.agentType {
		t.Fatalf("%s: agent_type %q, want %q", name, r.AgentType, w.agentType)
	}
	if w.timestamp == "" {
		if _, ok := got.obj["timestamp"]; ok {
			t.Fatalf("%s: timestamp %v present, want omitted", name, got.obj["timestamp"])
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
	if r.UncachedInputTokens != w.uncached || r.CacheWrite5mTokens != w.w5 || r.CacheWrite1hTokens != w.w1 ||
		r.CacheWriteUnsplitTokens != w.unsplit || r.CacheReadTokens != w.read || r.OutputTokens != w.out {
		t.Fatalf("%s: tokens uncached %d w5 %d w1 %d unsplit %d read %d out %d; want %d %d %d %d %d %d", name,
			r.UncachedInputTokens, r.CacheWrite5mTokens, r.CacheWrite1hTokens, r.CacheWriteUnsplitTokens, r.CacheReadTokens, r.OutputTokens,
			w.uncached, w.w5, w.w1, w.unsplit, w.read, w.out)
	}
	if w.thinking == nil {
		if v, ok := got.obj["thinking_tokens"]; !ok || v != nil {
			t.Fatalf("%s: thinking_tokens = %v (present %v), want JSON null", name, v, ok)
		}
	} else if r.ThinkingTokens == nil || *r.ThinkingTokens != *w.thinking {
		t.Fatalf("%s: thinking_tokens %v, want %d", name, r.ThinkingTokens, *w.thinking)
	}
}

// ---------------------------------------------------------------------------
// Hand-written history records for ReadHistory tests.

type hrec struct {
	session, requestID, model, ts        string
	uncached, w5, w1, unsplit, read, out int64
	thinking                             *int64
}

func recordMap(r hrec) map[string]any {
	m := map[string]any{
		"v": 1, "kind": "cw.metrics_request", "source": "claude_transcript",
		"session_id": r.session, "request_id": r.requestID, "agent_id": "main", "agent_role": "main",
		"model": r.model, "imported_at": "2026-11-06T12:00:00Z",
		"uncached_input_tokens": r.uncached, "cache_write_5m_tokens": r.w5, "cache_write_1h_tokens": r.w1,
		"cache_write_unsplit_tokens": r.unsplit, "cache_read_tokens": r.read, "output_tokens": r.out,
		"thinking_tokens": nil,
	}
	if r.thinking != nil {
		m["thinking_tokens"] = *r.thinking
	}
	if r.ts != "" {
		m["timestamp"] = r.ts
	}
	return m
}

func recordJSON(t *testing.T, r hrec, mutate func(map[string]any)) string {
	t.Helper()
	m := recordMap(r)
	if mutate != nil {
		mutate(m)
	}
	return hjson(t, m)
}

// padRecord inserts JSON whitespace after the opening brace so the line is
// exactly n bytes long while staying a valid record.
func padRecord(t *testing.T, line string, n int) string {
	t.Helper()
	if len(line) > n {
		t.Fatalf("record of %d bytes cannot be padded to %d", len(line), n)
	}
	return "{" + strings.Repeat(" ", n-len(line)) + line[1:]
}

func writeHistory(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readHistory(t *testing.T, path string, filter usagelog.HistoryFilter, prices *usagelog.PriceTable) usagelog.HistoryReport {
	t.Helper()
	report, err := usagelog.ReadHistory(t.Context(), path, filter, prices)
	if err != nil {
		t.Fatalf("ReadHistory(%+v) failed: %v", filter, err)
	}
	return report
}

type wantGroup struct {
	key                                                 string
	requests, uncached, w5, w1, unsplit, read, out, tin int64
	thinking                                            *int64
	thinkingReported                                    int64
}

func checkGroup(t *testing.T, name string, got usagelog.HistoryGroup, w wantGroup) {
	t.Helper()
	g := got.Tokens
	if got.Key != w.key || g.Requests != w.requests || g.UncachedInputTokens != w.uncached || g.CacheWrite5mTokens != w.w5 ||
		g.CacheWrite1hTokens != w.w1 || g.CacheWriteUnsplitTokens != w.unsplit || g.CacheReadTokens != w.read ||
		g.OutputTokens != w.out || g.TotalInputTokens != w.tin {
		t.Fatalf("%s = key %q %+v, want %+v", name, got.Key, g, w)
	}
	if (g.ThinkingTokens == nil) != (w.thinking == nil) || (w.thinking != nil && *g.ThinkingTokens != *w.thinking) || g.ThinkingReportedRequests != w.thinkingReported {
		t.Fatalf("%s thinking = %v (%d reported), want %v (%d reported)", name, g.ThinkingTokens, g.ThinkingReportedRequests, w.thinking, w.thinkingReported)
	}
}

func checkGroups(t *testing.T, name string, got []usagelog.HistoryGroup, want []wantGroup) {
	t.Helper()
	keys := make([]string, len(got))
	for i, g := range got {
		keys[i] = g.Key
	}
	wantKeys := make([]string, len(want))
	for i, w := range want {
		wantKeys[i] = w.key
	}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("%s keys = %q, want %q (sorted by key)", name, keys, wantKeys)
	}
	for i := range want {
		checkGroup(t, fmt.Sprintf("%s[%s]", name, want[i].key), got[i], want[i])
	}
}

// checkDollars: want nil means a null amount with a nonempty reason; otherwise
// a price-table amount equal to want.
func checkDollars(t *testing.T, name string, got usagelog.Amount, want *float64) {
	t.Helper()
	if want == nil {
		if got.Amount != nil || strings.TrimSpace(got.UnknownReason) == "" {
			t.Fatalf("%s dollars = %v (reason %q), want null with a reason", name, deref(got.Amount), got.UnknownReason)
		}
		return
	}
	if got.Amount == nil || math.Abs(*got.Amount-*want) > 1e-12 {
		t.Fatalf("%s dollars = %v (reason %q), want %.10g", name, deref(got.Amount), got.UnknownReason, *want)
	}
	if got.Basis != usagelog.BasisPriceTable {
		t.Fatalf("%s dollars basis = %q, want %q", name, got.Basis, usagelog.BasisPriceTable)
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func groupByKey(t *testing.T, groups []usagelog.HistoryGroup, key string) usagelog.HistoryGroup {
	t.Helper()
	for _, g := range groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("no group %q in %+v", key, groups)
	return usagelog.HistoryGroup{}
}

func samePathH(got, want string) bool {
	if r, err := filepath.EvalSymlinks(got); err == nil {
		got = r
	}
	if r, err := filepath.EvalSymlinks(want); err == nil {
		want = r
	}
	return got == want
}

// ---------------------------------------------------------------------------
// R1, R2: one record per keyed request with the contracted fields.

func TestAppendHistoryRecordsEachKeyedRequestOnce(t *testing.T) {
	main := filepath.Join(t.TempDir(), "project", "s1.jsonl")
	req1 := assistantLine(t, areq{session: "s1", requestID: "req_1", messageID: "msg_1", model: hOpus, timestamp: "2026-11-04T08:21:32.970Z",
		tok: tok{in: 10, w5: 20, w1: 10, read: 40, out: 50, thinking: i64(5)}})
	writeFileLines(t, main,
		userLine(t, "s1", "hello"),
		req1,
		req1, // same request key: one request (first wins)
		// message id only: key "message:msg_2"; +02:00 offset is 08:00:00Z.
		assistantLine(t, areq{session: "s1", messageID: "msg_2", model: hSonnet, timestamp: "2026-11-04T10:00:00+02:00", tok: tok{in: 3, read: 7, out: 9}}),
		// split 2+3=5 does not sum to creation 7: both 0, unsplit 7; no timestamp.
		assistantLine(t, areq{session: "s1", requestID: "req_3", messageID: "msg_3", model: hOpus, tok: tok{in: 1, w5: 2, w1: 3, creation: i64(7), out: 4}}),
		// no cache_creation object: unsplit 9; unparseable timestamp omitted.
		assistantLine(t, areq{session: "s1", requestID: "req_4", messageID: "msg_4", model: hOpus, timestamp: "not a time", tok: tok{in: 2, creation: i64(9), noSplit: true, out: 1}}),
		// no model: "unknown".
		assistantLine(t, areq{session: "s1", requestID: "req_5", messageID: "msg_5", timestamp: "2026-11-05T00:00:00Z", tok: tok{in: 1, out: 1}}),
		// neither requestId nor message id: counted but unkeyed, never written.
		assistantLine(t, areq{session: "s1", model: hOpus, tok: tok{in: 100, out: 100}}),
		// request id longer than MaxHistoryLabelBytes: outside the record bounds.
		assistantLine(t, areq{session: "s1", requestID: strings.Repeat("r", 300), messageID: "msg_long", model: hOpus, tok: tok{in: 1, out: 1}}),
		// not requests.
		assistantLine(t, areq{session: "s1", requestID: "req_syn", messageID: "msg_syn", model: "<synthetic>", tok: tok{in: 1000, out: 1000}}),
		assistantLine(t, areq{session: "other", requestID: "req_other", messageID: "msg_other", model: hOpus, tok: tok{in: 1000, out: 1000}}),
	)
	writeFileLines(t, subagentFile(main, "s1", "a1"),
		assistantLine(t, areq{session: "s1", requestID: "req_a1", messageID: "msg_a1", model: hOpus, timestamp: "2026-11-04T09:00:00Z", tok: tok{in: 4, w5: 8, read: 40, out: 6, thinking: i64(2)}}))
	writeMeta(t, main, "s1", "a1", map[string]any{"agentType": "general-purpose", "description": "Review"})
	writeFileLines(t, subagentFile(main, "s1", "b2"),
		assistantLine(t, areq{session: "s1", requestID: "req_b1", messageID: "msg_b1", model: hHaiku, tok: tok{in: 1, read: 10, out: 3}}))
	longType := strings.Repeat("t", 300)
	writeFileLines(t, subagentFile(main, "s1", "c3"),
		assistantLine(t, areq{session: "s1", requestID: "req_c1", messageID: "msg_c1", model: hSonnet, tok: tok{in: 2, out: 2}}))
	writeMeta(t, main, "s1", "c3", map[string]any{"agentType": longType, "description": "Long"})

	history := filepath.Join(t.TempDir(), "history.jsonl")
	before := time.Now().UTC().Truncate(time.Second)
	result := appendHistory(t, history, main)
	after := time.Now().UTC().Add(time.Second)

	// R2: requests req_1, msg_2, req_3, req_4, req_5, unkeyed, long id, req_a1, req_b1, req_c1 = 10;
	// appended 8, unkeyed 1, rejected 1.
	wantCounts(t, "first append", result, 10, 8, 0, 0, 1, 1)
	if result.SchemaVersion != 1 || result.Kind != usagelog.HistoryAppendKind || result.Source != usagelog.SourceClaude || result.SessionID != "s1" {
		t.Fatalf("R1: result identity schema %d kind %q source %q session %q", result.SchemaVersion, result.Kind, result.Source, result.SessionID)
	}
	if !filepath.IsAbs(result.History) || !samePathH(result.History, history) {
		t.Fatalf("R1: result history %q, want absolute %q", result.History, history)
	}
	if result.ImportStatus != usagelog.StatusComplete && result.ImportStatus != usagelog.StatusIncomplete {
		t.Fatalf("R1: import_status %q", result.ImportStatus)
	}

	lines := historyLines(t, history)
	if len(lines) != 8 {
		t.Fatalf("R1: %d history lines, want 8", len(lines))
	}
	got := byRequest(t, lines)
	for _, w := range []wantRecord{
		{requestID: "req_1", agentID: "main", role: "main", model: hOpus, timestamp: "2026-11-04T08:21:32.970Z", uncached: 10, w5: 20, w1: 10, read: 40, out: 50, thinking: i64(5)},
		{requestID: "message:msg_2", agentID: "main", role: "main", model: hSonnet, timestamp: "2026-11-04T08:00:00Z", uncached: 3, read: 7, out: 9},
		{requestID: "req_3", agentID: "main", role: "main", model: hOpus, uncached: 1, unsplit: 7, out: 4},
		{requestID: "req_4", agentID: "main", role: "main", model: hOpus, uncached: 2, unsplit: 9, out: 1},
		{requestID: "req_5", agentID: "main", role: "main", model: "unknown", timestamp: "2026-11-05T00:00:00Z", uncached: 1, out: 1},
		{requestID: "req_a1", agentID: "agent-a1", role: "subagent", agentType: "general-purpose", model: hOpus, timestamp: "2026-11-04T09:00:00Z", uncached: 4, w5: 8, read: 40, out: 6, thinking: i64(2)},
		{requestID: "req_b1", agentID: "agent-b2", role: "subagent", model: hHaiku, uncached: 1, read: 10, out: 3},
	} {
		line, ok := got[w.requestID]
		if !ok {
			t.Fatalf("R1: no record for %s; have %v", w.requestID, slices.Collect(maps.Keys(got)))
		}
		checkRecord(t, line, "s1", w, before, after)
	}
	// AgentType is truncated to MaxHistoryLabelBytes, never dropping the record.
	c1, ok := got["req_c1"]
	if !ok {
		t.Fatalf("R1: no record for req_c1 (long agent type)")
	}
	if c1.rec.AgentID != "agent-c3" || c1.rec.AgentRole != "subagent" || c1.rec.AgentType == "" ||
		len(c1.rec.AgentType) > usagelog.MaxHistoryLabelBytes || !strings.HasPrefix(longType, c1.rec.AgentType) {
		t.Fatalf("R1: req_c1 agent %q/%q type of %d bytes, want agent-c3 subagent with a truncated prefix of the meta agentType",
			c1.rec.AgentID, c1.rec.AgentRole, len(c1.rec.AgentType))
	}
}

// cleanSession writes s1 with four keyed requests (one in a subagent) and one
// unkeyed request.
func cleanSession(t *testing.T, withUnkeyed bool) string {
	t.Helper()
	main := filepath.Join(t.TempDir(), "project", "s1.jsonl")
	lines := []string{
		userLine(t, "s1", "start"),
		assistantLine(t, areq{session: "s1", requestID: "req_1", messageID: "msg_1", model: hOpus, timestamp: "2026-11-04T08:00:00Z", tok: tok{in: 2, w5: 10, w1: 20, read: 100, out: 7, thinking: i64(3)}}),
		assistantLine(t, areq{session: "s1", requestID: "req_2", messageID: "msg_2", model: hSonnet, timestamp: "2026-11-04T08:01:00Z", tok: tok{in: 5, read: 200, out: 11}}),
		assistantLine(t, areq{session: "s1", messageID: "msg_3", model: hOpus, timestamp: "2026-11-04T08:02:00Z", tok: tok{in: 1, w5: 4, out: 2}}),
	}
	if withUnkeyed {
		lines = append(lines, assistantLine(t, areq{session: "s1", model: hOpus, tok: tok{in: 9, out: 9}}))
	}
	writeFileLines(t, main, lines...)
	writeFileLines(t, subagentFile(main, "s1", "a1"),
		assistantLine(t, areq{session: "s1", requestID: "req_a1", messageID: "msg_a1", model: hOpus, timestamp: "2026-11-04T08:03:00Z", tok: tok{in: 4, w5: 8, read: 40, out: 6, thinking: i64(2)}}))
	writeMeta(t, main, "s1", "a1", map[string]any{"agentType": "general-purpose", "description": "Check"})
	return main
}

// R3: re-importing an unchanged transcript appends nothing and leaves the
// history byte-identical.
func TestAppendHistoryReimportUnchangedTranscriptAppendsNothing(t *testing.T) {
	main := cleanSession(t, true)
	history := filepath.Join(t.TempDir(), "history.jsonl")
	first := appendHistory(t, history, main)
	wantCounts(t, "first append", first, 5, 4, 0, 0, 1, 0)
	stored := readBytes(t, history)
	for i := range 2 {
		again := appendHistory(t, history, main)
		wantCounts(t, fmt.Sprintf("re-import %d", i+1), again, 5, 0, 4, 0, 1, 0)
		if now := readBytes(t, history); !bytes.Equal(now, stored) {
			t.Fatalf("R3: re-import %d changed the history:\nbefore %q\nafter  %q", i+1, stored, now)
		}
	}
	if n := len(historyLines(t, history)); n != 4 {
		t.Fatalf("R3: %d lines, want 4", n)
	}
}

// R4: new requests (main lines, an existing subagent's lines and a new
// subagent file) are appended once; stored lines stay byte-identical.
func TestAppendHistoryGrowthAppendsOnlyNewRequests(t *testing.T) {
	main := cleanSession(t, false)
	history := filepath.Join(t.TempDir(), "history.jsonl")
	wantCounts(t, "first append", appendHistory(t, history, main), 4, 4, 0, 0, 0, 0)
	stored := readBytes(t, history)

	appendFileLines(t, main,
		assistantLine(t, areq{session: "s1", requestID: "req_1", messageID: "msg_1", model: hOpus, timestamp: "2026-11-04T08:00:00Z", tok: tok{in: 2, w5: 10, w1: 20, read: 100, out: 7, thinking: i64(3)}}),
		assistantLine(t, areq{session: "s1", requestID: "req_4", messageID: "msg_4", model: hOpus, timestamp: "2026-11-04T09:00:00Z", tok: tok{in: 6, read: 300, out: 13}}))
	appendFileLines(t, subagentFile(main, "s1", "a1"),
		assistantLine(t, areq{session: "s1", requestID: "req_a2", messageID: "msg_a2", model: hOpus, timestamp: "2026-11-04T09:01:00Z", tok: tok{in: 3, out: 3}}))
	writeFileLines(t, subagentFile(main, "s1", "d4"),
		assistantLine(t, areq{session: "s1", requestID: "req_d1", messageID: "msg_d1", model: hHaiku, timestamp: "2026-11-04T09:02:00Z", tok: tok{in: 7, out: 5}}))
	writeMeta(t, main, "s1", "d4", map[string]any{"agentType": "explorer", "description": "Look"})

	before := time.Now().UTC().Truncate(time.Second)
	grown := appendHistory(t, history, main)
	after := time.Now().UTC().Add(time.Second)
	// 7 distinct requests: req_1, req_2, message:msg_3, req_a1 present; req_4, req_a2, req_d1 new.
	wantCounts(t, "grown re-import", grown, 7, 3, 4, 0, 0, 0)
	now := readBytes(t, history)
	if !bytes.HasPrefix(now, stored) {
		t.Fatalf("R4: stored lines changed:\nbefore %q\nafter  %q", stored, now)
	}
	lines := historyLines(t, history) // also: each identity exactly once
	if len(lines) != 7 {
		t.Fatalf("R4: %d lines, want 7", len(lines))
	}
	got := byRequest(t, lines)
	checkRecord(t, got["req_4"], "s1", wantRecord{requestID: "req_4", agentID: "main", role: "main", model: hOpus, timestamp: "2026-11-04T09:00:00Z", uncached: 6, read: 300, out: 13}, before, after)
	checkRecord(t, got["req_a2"], "s1", wantRecord{requestID: "req_a2", agentID: "agent-a1", role: "subagent", agentType: "general-purpose", model: hOpus, timestamp: "2026-11-04T09:01:00Z", uncached: 3, out: 3}, before, after)
	checkRecord(t, got["req_d1"], "s1", wantRecord{requestID: "req_d1", agentID: "agent-d4", role: "subagent", agentType: "explorer", model: hHaiku, timestamp: "2026-11-04T09:02:00Z", uncached: 7, out: 5}, before, after)
}

// R5: a stored record with the same identity but different token counts is
// kept; nothing is written for the request and it counts as conflicting.
func TestAppendHistoryConflictKeepsStoredRecord(t *testing.T) {
	t.Run("transcript changed after import", func(t *testing.T) {
		main := cleanSession(t, false)
		history := filepath.Join(t.TempDir(), "history.jsonl")
		appendHistory(t, history, main)
		stored := readBytes(t, history)
		data := strings.Replace(string(readBytes(t, main)), `"output_tokens":11`, `"output_tokens":999`, 1)
		if !strings.Contains(data, `"output_tokens":999`) {
			t.Fatal("fixture: req_2 output_tokens not rewritten")
		}
		if err := os.WriteFile(main, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		result := appendHistory(t, history, main)
		wantCounts(t, "conflicting re-import", result, 4, 0, 3, 1, 0, 0)
		if now := readBytes(t, history); !bytes.Equal(now, stored) {
			t.Fatalf("R5: history changed on conflict:\nbefore %q\nafter  %q", stored, now)
		}
	})
	t.Run("pre-existing record", func(t *testing.T) {
		main := cleanSession(t, false)
		// Stored req_1 has output 70 where the transcript says 7.
		seed := recordJSON(t, hrec{session: "s1", requestID: "req_1", model: hOpus, ts: "2026-11-04T08:00:00Z", uncached: 2, w5: 10, w1: 20, read: 100, out: 70, thinking: i64(3)}, nil) + "\n"
		history := writeHistory(t, seed)
		result := appendHistory(t, history, main)
		wantCounts(t, "append over conflicting seed", result, 4, 3, 0, 1, 0, 0)
		now := string(readBytes(t, history))
		if !strings.HasPrefix(now, seed) {
			t.Fatalf("R5: stored line changed:\nbefore %q\nafter  %q", seed, now)
		}
		lines := historyLines(t, history)
		if len(lines) != 4 || byRequest(t, lines)["req_1"].rec.OutputTokens != 70 {
			t.Fatalf("R5: %d lines, req_1 output %d; want 4 lines and the stored output 70", len(lines), byRequest(t, lines)["req_1"].rec.OutputTokens)
		}
	})
}

// R6: concurrent appends with overlapping and disjoint transcripts. Without an
// exclusive writer lock, two goroutines read the same identities and both
// append, so a request appears twice and the appended sum exceeds the
// distinct count (or lines interleave and tear).
func TestAppendHistoryConcurrentOverlappingAppendsStoreEachRequestOnce(t *testing.T) {
	const (
		goroutines = 24
		rounds     = 8
	)
	transcript := func(session string, from, to int) string {
		path := filepath.Join(t.TempDir(), "project", session+".jsonl")
		var lines []string
		for i := from; i < to; i++ {
			lines = append(lines, assistantLine(t, areq{session: session, requestID: fmt.Sprintf("req_%04d", i), messageID: fmt.Sprintf("msg_%04d", i),
				model: hOpus, timestamp: "2026-11-04T08:00:00Z", tok: tok{in: int64(i + 1), w5: 1, read: 3, out: int64(2*i + 1)}}))
		}
		writeFileLines(t, path, lines...)
		return path
	}
	// A and B are two copies of session "conc" that overlap on req_0075..req_0149;
	// C is a disjoint session. Distinct keyed requests: 225 + 75 = 300.
	paths := []string{transcript("conc", 0, 150), transcript("conc", 75, 225), transcript("solo", 0, 75)}
	want := map[string]bool{}
	for i := range 225 {
		want["claude_transcript\x00conc\x00"+fmt.Sprintf("req_%04d", i)] = true
	}
	for i := range 75 {
		want["claude_transcript\x00solo\x00"+fmt.Sprintf("req_%04d", i)] = true
	}

	for round := range rounds {
		history := filepath.Join(t.TempDir(), "history.jsonl")
		results := make([]usagelog.AppendResult, goroutines)
		errs := make([]error, goroutines)
		var ready, done sync.WaitGroup
		start := make(chan struct{})
		for g := range goroutines {
			ready.Add(1)
			done.Add(1)
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				results[g], errs[g] = usagelog.AppendHistory(context.Background(), history, usagelog.Options{ClaudeTranscript: paths[g%len(paths)]})
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		var appended int64
		for g := range goroutines {
			if errs[g] != nil {
				t.Fatalf("round %d goroutine %d: %v", round, g, errs[g])
			}
			checkCountsSum(t, results[g])
			if results[g].Conflicting != 0 || results[g].Rejected != 0 || results[g].Unkeyed != 0 {
				t.Fatalf("round %d goroutine %d: %+v, want no conflicting, rejected or unkeyed", round, g, results[g])
			}
			appended += results[g].Appended
		}
		lines := historyLines(t, history) // every line valid, no identity twice
		got := map[string]bool{}
		for _, l := range lines {
			got[identity(l.rec)] = true
		}
		if len(lines) != len(want) || len(got) != len(want) {
			t.Fatalf("round %d: %d lines, %d identities, want %d", round, len(lines), len(got), len(want))
		}
		for id := range want {
			if !got[id] {
				t.Fatalf("round %d: request %q missing from the history", round, id)
			}
		}
		if appended != int64(len(want)) {
			t.Fatalf("round %d: sum of appended = %d, want %d distinct requests", round, appended, len(want))
		}
	}
}

// R7: damaged lines are skipped, counted and listed with their 1-based line
// number; valid lines around them still count.
func TestReadHistoryDamagedLinesAreCountedAndListed(t *testing.T) {
	base := func(id string) hrec {
		return hrec{session: "s1", requestID: id, model: hOpus, ts: "2026-11-04T08:00:00Z", uncached: 1000, out: 1000}
	}
	set := func(k string, v any) func(map[string]any) { return func(m map[string]any) { m[k] = v } }
	del := func(k string) func(map[string]any) { return func(m map[string]any) { delete(m, k) } }
	valid := func(id string, uncached int64) string {
		return recordJSON(t, hrec{session: "s1", requestID: id, model: hOpus, ts: "2026-11-04T08:00:00Z", uncached: uncached, out: 5, thinking: i64(5)}, nil)
	}
	exact4096 := padRecord(t, valid("ok_4096", 4), usagelog.MaxHistoryLineBytes)
	over4096 := padRecord(t, recordJSON(t, base("bad_long_line"), nil), usagelog.MaxHistoryLineBytes+1)
	label256 := recordJSON(t, hrec{session: "s1", requestID: "ok_label", model: strings.Repeat("m", 256), uncached: 8, out: 0}, nil)
	damaged := []struct{ name, line string }{
		{"not json", `not json at all`},
		{"json array", `[1,2,3]`},
		{"line over 4096 bytes", over4096},
		{"v 2", recordJSON(t, base("bad_v"), set("v", 2))},
		{"missing v", recordJSON(t, base("bad_no_v"), del("v"))},
		{"other kind", recordJSON(t, base("bad_kind"), set("kind", "cw.other"))},
		{"unknown field", recordJSON(t, base("bad_unknown"), set("prompt", "hello"))},
		{"missing source", recordJSON(t, base("bad_no_source"), del("source"))},
		{"empty session_id", recordJSON(t, base("bad_session"), set("session_id", ""))},
		{"missing request_id", recordJSON(t, base("unused"), del("request_id"))},
		{"empty agent_id", recordJSON(t, base("bad_agent_id"), set("agent_id", ""))},
		{"missing agent_role", recordJSON(t, base("bad_no_role"), del("agent_role"))},
		{"empty model", recordJSON(t, base("bad_model"), set("model", ""))},
		{"unknown source", recordJSON(t, base("bad_source"), set("source", "gemini_log"))},
		{"unknown agent_role", recordJSON(t, base("bad_role"), set("agent_role", "boss"))},
		{"text field of 257 bytes", recordJSON(t, base("bad_label"), set("model", strings.Repeat("m", 257)))},
		{"negative count", recordJSON(t, base("bad_negative"), set("cache_read_tokens", -1))},
		{"thinking above output", recordJSON(t, base("bad_thinking"), set("thinking_tokens", 1001))},
		{"timestamp not RFC 3339", recordJSON(t, base("bad_ts"), set("timestamp", "2026-11-04 08:00:00"))},
		{"imported_at not RFC 3339", recordJSON(t, base("bad_imported"), set("imported_at", "yesterday"))},
	}
	// Layout: valid, damaged..., valid (exactly 4096 bytes), valid (256-byte
	// label), blank line, valid. Damaged lines are lines 2..21.
	lines := []string{valid("ok_first", 1)}
	for _, d := range damaged {
		lines = append(lines, d.line)
	}
	lines = append(lines, exact4096, label256, "", valid("ok_last", 2))
	history := writeHistory(t, strings.Join(lines, "\n")+"\n")

	report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
	if report.DamagedLines != int64(len(damaged)) || len(report.Damaged) != len(damaged) {
		reported := map[int64]bool{}
		for _, d := range report.Damaged {
			reported[d.Line] = true
		}
		var missed []string
		for i, d := range damaged {
			if !reported[int64(i+2)] {
				missed = append(missed, d.name)
			}
		}
		t.Fatalf("R7: damaged_lines %d (listed %d), want %d; not reported: %q", report.DamagedLines, len(report.Damaged), len(damaged), missed)
	}
	for i, d := range report.Damaged {
		if d.Line != int64(i+2) || strings.TrimSpace(d.Reason) == "" {
			t.Fatalf("R7: damaged[%d] = %+v, want line %d (%s) with a reason", i, d, i+2, damaged[i].name)
		}
	}
	// Valid: ok_first (1), ok_4096 (4), ok_label (8), ok_last (2): 4 records, uncached 15.
	// Nonblank lines: 1 + 20 + 3 = 24.
	if report.Records != 4 || report.Lines != 24 || report.DuplicateLines != 0 {
		t.Fatalf("R7: records %d lines %d duplicates %d, want 4, 24, 0", report.Records, report.Lines, report.DuplicateLines)
	}
	if report.Totals.Requests != 4 || report.Totals.UncachedInputTokens != 15 || report.Totals.OutputTokens != 15 {
		t.Fatalf("R7: totals %+v, want requests 4 uncached 15 output 15 (damaged lines excluded)", report.Totals.Tokens)
	}
}

// R7: the damaged list is capped at MaxHistoryDamagedListed while the count
// stays exact.
func TestReadHistoryDamagedListIsCappedButCountIsExact(t *testing.T) {
	var lines []string
	for range 130 {
		lines = append(lines, `{"broken":`)
	}
	lines = append(lines, recordJSON(t, hrec{session: "s1", requestID: "ok", model: hOpus, uncached: 3}, nil))
	history := writeHistory(t, strings.Join(lines, "\n")+"\n")
	report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
	if report.DamagedLines != 130 || len(report.Damaged) != usagelog.MaxHistoryDamagedListed || report.Records != 1 {
		t.Fatalf("R7: damaged_lines %d listed %d records %d, want 130, %d, 1", report.DamagedLines, len(report.Damaged), report.Records, usagelog.MaxHistoryDamagedListed)
	}
	seen := map[int64]bool{}
	for _, d := range report.Damaged {
		if d.Line < 1 || d.Line > 130 || seen[d.Line] || strings.TrimSpace(d.Reason) == "" {
			t.Fatalf("R7: listed entry %+v is not a distinct damaged line 1..130 with a reason", d)
		}
		seen[d.Line] = true
	}
}

// R7: a damaged line never stops an append; a request whose only stored line
// is damaged is appended again.
func TestAppendHistoryDamagedLineDoesNotStopAppend(t *testing.T) {
	main := cleanSession(t, false)
	// A v=2 copy of req_1 (damaged) and a garbage line.
	seed := recordJSON(t, hrec{session: "s1", requestID: "req_1", model: hOpus, ts: "2026-11-04T08:00:00Z", uncached: 2, w5: 10, w1: 20, read: 100, out: 7, thinking: i64(3)},
		func(m map[string]any) { m["v"] = 2 }) + "\n" + "garbage line\n"
	history := writeHistory(t, seed)
	result := appendHistory(t, history, main)
	wantCounts(t, "append over damaged lines", result, 4, 4, 0, 0, 0, 0)
	if result.HistoryDamagedLines != 2 {
		t.Fatalf("R7: history_damaged_lines %d, want 2", result.HistoryDamagedLines)
	}
	data := string(readBytes(t, history))
	if !strings.HasPrefix(data, seed) {
		t.Fatalf("R7: append rewrote the damaged lines: %q", data)
	}
	report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
	if report.Records != 4 || report.DamagedLines != 2 || len(report.Damaged) != 2 || report.Damaged[0].Line != 1 || report.Damaged[1].Line != 2 {
		t.Fatalf("R7: records %d damaged %d %+v, want 4 records and lines 1, 2 damaged", report.Records, report.DamagedLines, report.Damaged)
	}
}

// R8: a history that ends without a newline is terminated before the new
// records are written, so every new record is a whole line.
func TestAppendHistoryTornTailIsTerminated(t *testing.T) {
	full := recordJSON(t, hrec{session: "s0", requestID: "old_1", model: hOpus, uncached: 1}, nil) + "\n"
	t.Run("torn invalid line", func(t *testing.T) {
		main := cleanSession(t, false)
		seed := full + `{"v":1,"kind":"cw.metrics_req`
		history := writeHistory(t, seed)
		result := appendHistory(t, history, main)
		wantCounts(t, "append after torn tail", result, 4, 4, 0, 0, 0, 0)
		if result.HistoryDamagedLines != 1 {
			t.Fatalf("R8: history_damaged_lines %d, want 1 (the torn line)", result.HistoryDamagedLines)
		}
		data := string(readBytes(t, history))
		if !strings.HasPrefix(data, seed+"\n") {
			t.Fatalf("R8: torn line was not terminated before the new records: %q", data)
		}
		rest := strings.TrimPrefix(data, seed+"\n")
		if !strings.HasSuffix(rest, "\n") || strings.Count(rest, "\n") != 4 {
			t.Fatalf("R8: new records are not 4 whole lines: %q", rest)
		}
		for _, line := range strings.Split(strings.TrimSuffix(rest, "\n"), "\n") {
			if _, _, err := validRecord(line); err != nil {
				t.Fatalf("R8: new line is not a valid record (%v): %q", err, line)
			}
		}
		report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
		if report.Records != 5 || report.DamagedLines != 1 || len(report.Damaged) != 1 || report.Damaged[0].Line != 2 {
			t.Fatalf("R8: records %d damaged %d %+v, want 5 records and line 2 damaged", report.Records, report.DamagedLines, report.Damaged)
		}
	})
	t.Run("torn valid line", func(t *testing.T) {
		main := cleanSession(t, false)
		seed := full + recordJSON(t, hrec{session: "s0", requestID: "old_2", model: hOpus, uncached: 1}, nil)
		history := writeHistory(t, seed)
		wantCounts(t, "append after unterminated record", appendHistory(t, history, main), 4, 4, 0, 0, 0, 0)
		data := string(readBytes(t, history))
		if !strings.HasPrefix(data, seed+"\n") {
			t.Fatalf("R8: unterminated record was not terminated: %q", data)
		}
		if n := len(historyLines(t, history)); n != 6 {
			t.Fatalf("R8: %d lines, want 6", n)
		}
		report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
		if report.Records != 6 || report.DamagedLines != 0 {
			t.Fatalf("R8: records %d damaged %d, want 6 and 0", report.Records, report.DamagedLines)
		}
	})
}

// R9: lines repeating an identity count once (the first); the rest are
// duplicate_lines.
func TestReadHistoryDuplicateLinesCountFirstOnly(t *testing.T) {
	a := recordJSON(t, hrec{session: "s1", requestID: "req_a", model: hOpus, uncached: 10, read: 1, out: 3}, nil)
	b := recordJSON(t, hrec{session: "s1", requestID: "req_b", model: hOpus, uncached: 20, out: 4}, nil)
	aChanged := recordJSON(t, hrec{session: "s1", requestID: "req_a", model: hOpus, uncached: 500, out: 500}, nil)
	history := writeHistory(t, strings.Join([]string{a, b, a, aChanged}, "\n")+"\n")
	report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
	if report.Records != 2 || report.DuplicateLines != 2 || report.DamagedLines != 0 || report.Lines != 4 {
		t.Fatalf("R9: records %d duplicates %d damaged %d lines %d, want 2, 2, 0, 4", report.Records, report.DuplicateLines, report.DamagedLines, report.Lines)
	}
	// First req_a (10/1/3) + req_b (20/0/4): uncached 30, read 1, out 7, total input 31.
	checkGroup(t, "R9 totals", report.Totals, wantGroup{requests: 2, uncached: 30, read: 1, out: 7, tin: 31})
}

// R10: no transcript text, tool input/result, file content, meta description
// or transcript path reaches the history.
func TestAppendHistoryStoresNoTranscriptText(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj-DIRSECRET-e5e5")
	main := filepath.Join(dir, "s1.jsonl")
	secrets := []string{"DIRSECRET-e5e5", "USERSECRET-91c2", "ASSISTANTSECRET-55ab", "THINKSECRET-7a7a", "TOOLINPUTSECRET-0d1e",
		"TOOLRESULTSECRET-a8b8", "FILECONTENTSECRET-3c3c", "METADESCSECRET-4c4c", "SUBAGENTTEXTSECRET-6f6f"}
	cwd := map[string]any{"cwd": dir}
	writeFileLines(t, main,
		hjson(t, map[string]any{"type": "user", "sessionId": "s1", "cwd": dir, "message": map[string]any{"role": "user", "content": "please USERSECRET-91c2"}}),
		assistantLine(t, areq{session: "s1", requestID: "req_1", messageID: "msg_1", model: hOpus, timestamp: "2026-11-04T08:00:00Z", extra: cwd,
			content: []any{
				map[string]any{"type": "thinking", "thinking": "THINKSECRET-7a7a"},
				map[string]any{"type": "text", "text": "ASSISTANTSECRET-55ab"},
				map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": map[string]any{"command": "cat TOOLINPUTSECRET-0d1e"}},
			},
			tok: tok{in: 1, out: 2}}),
		hjson(t, map[string]any{"type": "user", "sessionId": "s1", "cwd": dir,
			"message":       map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "TOOLRESULTSECRET-a8b8"}}},
			"toolUseResult": map[string]any{"file": map[string]any{"filePath": filepath.Join(dir, "f.txt"), "content": "FILECONTENTSECRET-3c3c"}}}),
		assistantLine(t, areq{session: "s1", requestID: "req_2", messageID: "msg_2", model: hOpus, timestamp: "2026-11-04T08:01:00Z", extra: cwd, tok: tok{in: 3, out: 4}}),
	)
	writeFileLines(t, subagentFile(main, "s1", "a1"),
		assistantLine(t, areq{session: "s1", requestID: "req_a1", messageID: "msg_a1", model: hOpus, extra: cwd,
			content: []any{map[string]any{"type": "text", "text": "SUBAGENTTEXTSECRET-6f6f"}}, tok: tok{in: 5, out: 6}}))
	writeMeta(t, main, "s1", "a1", map[string]any{"agentType": "general-purpose", "description": "METADESCSECRET-4c4c"})

	history := filepath.Join(t.TempDir(), "history.jsonl")
	wantCounts(t, "privacy append", appendHistory(t, history, main), 3, 3, 0, 0, 0, 0)
	data := readBytes(t, history)
	for _, secret := range append(secrets, main, dir) {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("R10: history contains %q:\n%s", secret, data)
		}
	}
}

// R11: a newly created history is 0600 and a newly created parent 0700.
func TestAppendHistoryCreatesPrivateFileAndDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}
	main := cleanSession(t, false)
	for name, history := range map[string]string{
		"existing parent": filepath.Join(t.TempDir(), "history.jsonl"),
		"new parent":      filepath.Join(t.TempDir(), "new", "metrics", "history.jsonl"),
	} {
		t.Run(name, func(t *testing.T) {
			appendHistory(t, history, main)
			info, err := os.Lstat(history)
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatalf("R11: history mode %v, want regular 0600", info.Mode())
			}
			if name == "new parent" {
				dirInfo, err := os.Stat(filepath.Dir(history))
				if err != nil {
					t.Fatal(err)
				}
				if dirInfo.Mode().Perm() != 0o700 {
					t.Fatalf("R11: new parent mode %v, want 0700", dirInfo.Mode().Perm())
				}
			}
		})
	}
}

// R11: a symlink (to anything), a directory or another non-regular file at the
// history path makes the append fail without writing or following the link.
func TestAppendHistoryRefusesNonRegularHistoryPath(t *testing.T) {
	main := cleanSession(t, false)
	refuse := func(t *testing.T, history string) {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := usagelog.AppendHistory(context.Background(), history, usagelog.Options{ClaudeTranscript: main})
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("R11: AppendHistory(%s) succeeded, want an error", history)
			}
		case <-time.After(30 * time.Second):
			// Bounds the wait only: a FIFO opened for I/O blocks until a peer opens it.
			if f, err := os.OpenFile(history, os.O_RDWR, 0); err == nil {
				f.Close()
			}
			t.Fatalf("R11: AppendHistory(%s) blocked on a non-regular file", history)
		}
	}
	t.Run("symlink to a history", func(t *testing.T) {
		target := writeHistory(t, recordJSON(t, hrec{session: "s0", requestID: "old", model: hOpus, uncached: 1}, nil)+"\n")
		before := readBytes(t, target)
		link := filepath.Join(t.TempDir(), "history.jsonl")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		refuse(t, link)
		if !bytes.Equal(readBytes(t, target), before) {
			t.Fatal("R11: the symlink target was written")
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("R11: the link was replaced: %v %v", info, err)
		}
	})
	t.Run("dangling symlink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "absent.jsonl")
		link := filepath.Join(t.TempDir(), "history.jsonl")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		refuse(t, link)
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("R11: the link was followed and %s created (%v)", target, err)
		}
	})
	t.Run("symlink to a directory", func(t *testing.T) {
		target := t.TempDir()
		link := filepath.Join(t.TempDir(), "history.jsonl")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		refuse(t, link)
		if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
			t.Fatalf("R11: the linked directory was written: %v %v", entries, err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		history := filepath.Join(t.TempDir(), "history.jsonl")
		if err := os.Mkdir(history, 0o700); err != nil {
			t.Fatal(err)
		}
		refuse(t, history)
		if entries, err := os.ReadDir(history); err != nil || len(entries) != 0 {
			t.Fatalf("R11: the directory was written: %v %v", entries, err)
		}
	})
	t.Run("fifo", func(t *testing.T) {
		mkfifo, err := exec.LookPath("mkfifo")
		if err != nil {
			t.Skip("mkfifo is not available on this platform")
		}
		history := filepath.Join(t.TempDir(), "history.jsonl")
		if out, err := exec.Command(mkfifo, history).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo: %v %s", err, out)
		}
		refuse(t, history)
	})
}

// ---------------------------------------------------------------------------
// R12, R13: report groups, filters and recomputed dollars.

// reportFixture (hand-computed):
//
//	A sA opus   2026-11-01T10:00:00Z       unc 100  w5 200 w1 300 uns 0   read 1000 out 50  think 10
//	B sA sonnet 2026-11-02T23:30:00Z       unc 10                         read 20   out 30  think null
//	C sB opus   2026-11-03T01:00:00+05:00  unc 1    w5 2   w1 3           read 4    out 5   think 1   (UTC day 2026-11-02)
//	D sB haiku  (no timestamp)             unc 1000               uns 400           out 100 think null
//	E sC opus   2026-11-04T00:00:00Z                              uns 50            out 0   think null
//
// A blank line sits between B and C.
func reportFixture(t *testing.T) string {
	t.Helper()
	return writeHistory(t, strings.Join([]string{
		recordJSON(t, hrec{session: "sA", requestID: "A", model: hOpus, ts: "2026-11-01T10:00:00Z", uncached: 100, w5: 200, w1: 300, read: 1000, out: 50, thinking: i64(10)}, nil),
		recordJSON(t, hrec{session: "sA", requestID: "B", model: hSonnet, ts: "2026-11-02T23:30:00Z", uncached: 10, read: 20, out: 30}, nil),
		"",
		recordJSON(t, hrec{session: "sB", requestID: "C", model: hOpus, ts: "2026-11-03T01:00:00+05:00", uncached: 1, w5: 2, w1: 3, read: 4, out: 5, thinking: i64(1)}, nil),
		recordJSON(t, hrec{session: "sB", requestID: "D", model: hHaiku, uncached: 1000, unsplit: 400, out: 100}, nil),
		recordJSON(t, hrec{session: "sC", requestID: "E", model: hOpus, ts: "2026-11-04T00:00:00Z", unsplit: 50}, nil),
	}, "\n")+"\n")
}

func TestReadHistoryAggregatesGroupsAndFilters(t *testing.T) {
	history := reportFixture(t)
	report := readHistory(t, history, usagelog.HistoryFilter{}, nil)
	if report.SchemaVersion != 1 || report.Kind != usagelog.HistoryReportKind || !filepath.IsAbs(report.History) || !samePathH(report.History, history) {
		t.Fatalf("R12: schema %d kind %q history %q", report.SchemaVersion, report.Kind, report.History)
	}
	if report.Lines != 5 || report.Records != 5 || report.DuplicateLines != 0 || report.DamagedLines != 0 || len(report.Damaged) != 0 {
		t.Fatalf("R12: lines %d records %d duplicates %d damaged %d, want 5 5 0 0", report.Lines, report.Records, report.DuplicateLines, report.DamagedLines)
	}
	// Totals: unc 1111, w5 202, w1 303, uns 450, read 1024, out 185; total input 3090; thinking 11 of 2.
	checkGroup(t, "totals", report.Totals, wantGroup{key: "", requests: 5, uncached: 1111, w5: 202, w1: 303, unsplit: 450, read: 1024, out: 185, tin: 3090, thinking: i64(11), thinkingReported: 2})
	checkGroups(t, "by_model", report.ByModel, []wantGroup{
		{key: hHaiku, requests: 1, uncached: 1000, unsplit: 400, out: 100, tin: 1400},
		{key: hOpus, requests: 3, uncached: 101, w5: 202, w1: 303, unsplit: 50, read: 1004, out: 55, tin: 1660, thinking: i64(11), thinkingReported: 2},
		{key: hSonnet, requests: 1, uncached: 10, read: 20, out: 30, tin: 30},
	})
	checkGroups(t, "by_day", report.ByDay, []wantGroup{
		{key: "2026-11-01", requests: 1, uncached: 100, w5: 200, w1: 300, read: 1000, out: 50, tin: 1600, thinking: i64(10), thinkingReported: 1},
		{key: "2026-11-02", requests: 2, uncached: 11, w5: 2, w1: 3, read: 24, out: 35, tin: 40, thinking: i64(1), thinkingReported: 1},
		{key: "2026-11-04", requests: 1, unsplit: 50, tin: 50},
		{key: "unknown", requests: 1, uncached: 1000, unsplit: 400, out: 100, tin: 1400},
	})
	checkGroups(t, "by_session", report.BySession, []wantGroup{
		{key: "sA", requests: 2, uncached: 110, w5: 200, w1: 300, read: 1020, out: 80, tin: 1630, thinking: i64(10), thinkingReported: 1},
		{key: "sB", requests: 2, uncached: 1001, w5: 2, w1: 3, unsplit: 400, read: 4, out: 105, tin: 1410, thinking: i64(1), thinkingReported: 1},
		{key: "sC", requests: 1, unsplit: 50, tin: 50},
	})
	// R13 without prices: every amount null with a reason.
	checkDollars(t, "totals", report.Totals.Dollars, nil)
	for _, groups := range [][]usagelog.HistoryGroup{report.ByModel, report.ByDay, report.BySession} {
		for _, g := range groups {
			checkDollars(t, "group "+g.Key, g.Dollars, nil)
		}
	}

	for _, c := range []struct {
		name     string
		filter   usagelog.HistoryFilter
		records  int64
		uncached int64
		days     []string
	}{
		{"session", usagelog.HistoryFilter{SessionID: "sB"}, 2, 1001, []string{"2026-11-02", "unknown"}},
		{"model", usagelog.HistoryFilter{Model: hOpus}, 3, 101, []string{"2026-11-01", "2026-11-02", "2026-11-04"}},
		// C is 2026-11-03 locally but 2026-11-02 in UTC; D has no timestamp.
		{"one UTC day", usagelog.HistoryFilter{Since: "2026-11-02", Until: "2026-11-02"}, 2, 11, []string{"2026-11-02"}},
		{"since inclusive", usagelog.HistoryFilter{Since: "2026-11-04"}, 1, 0, []string{"2026-11-04"}},
		{"until inclusive", usagelog.HistoryFilter{Until: "2026-11-01"}, 1, 100, []string{"2026-11-01"}},
		{"until excludes later UTC day", usagelog.HistoryFilter{Until: "2026-11-03"}, 3, 111, []string{"2026-11-01", "2026-11-02"}},
		{"date bound drops undated", usagelog.HistoryFilter{SessionID: "sB", Since: "2026-01-01"}, 1, 1, []string{"2026-11-02"}},
		{"no match", usagelog.HistoryFilter{Model: "claude-none"}, 0, 0, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := readHistory(t, history, c.filter, nil)
			if got.Filter != c.filter {
				t.Fatalf("R12: filter echoed as %+v, want %+v", got.Filter, c.filter)
			}
			if got.Records != c.records || got.Totals.Requests != c.records || got.Totals.UncachedInputTokens != c.uncached {
				t.Fatalf("R12: records %d totals requests %d uncached %d, want %d %d %d", got.Records, got.Totals.Requests, got.Totals.UncachedInputTokens, c.records, c.records, c.uncached)
			}
			if got.Lines != 5 {
				t.Fatalf("R12: lines %d, want 5 regardless of filter", got.Lines)
			}
			var days []string
			for _, g := range got.ByDay {
				days = append(days, g.Key)
			}
			if !slices.Equal(days, c.days) {
				t.Fatalf("R12: by_day keys %q, want %q", days, c.days)
			}
		})
	}
}

func rateTable(models map[string][5]float64) *usagelog.PriceTable {
	table := &usagelog.PriceTable{Currency: "USD", Evidence: "local:history-test-table", Models: map[string]usagelog.ModelRates{}}
	for model, r := range models {
		table.Models[model] = usagelog.ModelRates{
			InputPerMillion: f64(r[0]), OutputPerMillion: f64(r[1]),
			CacheWrite5mPerMillion: f64(r[2]), CacheWrite1hPerMillion: f64(r[3]), CacheReadPerMillion: f64(r[4]),
		}
	}
	return table
}

// R13: dollars are recomputed from the supplied table on every read.
//
// Rates [input, output, 5m, 1h, read] per million.
// P1: opus [4,20,5,8,0.2] (5m != 1h: unsplit unpriced), sonnet [2,10,2.5,4,0.1], haiku [1,5,1.25,1.25,0.1].
//
//	A 100*4+50*20+200*5+300*8+1000*0.2 = 5000      B 10*2+30*10+20*0.1 = 322
//	C 1*4+5*20+2*5+3*8+4*0.2 = 138.8               D 1000*1+100*5+400*1.25 = 2000   E unsplit 50 at 5 != 8: unknown
//
// P2: opus [8,40,10,10,1] (equal: unsplit at 10), sonnet [3,15,3.75,6,0.3], haiku absent.
//
//	A 800+2000+2000+3000+1000 = 8800   B 30+450+6 = 486   C 8+200+20+30+4 = 262   D unknown   E 50*10 = 500
func TestReadHistoryDollarsFollowSuppliedPriceTable(t *testing.T) {
	history := reportFixture(t)
	p1 := rateTable(map[string][5]float64{hOpus: {4, 20, 5, 8, 0.2}, hSonnet: {2, 10, 2.5, 4, 0.1}, hHaiku: {1, 5, 1.25, 1.25, 0.1}})
	p2 := rateTable(map[string][5]float64{hOpus: {8, 40, 10, 10, 1}, hSonnet: {3, 15, 3.75, 6, 0.3}})
	p2.Evidence = "local:second-table"
	m := func(units float64) *float64 { return f64(units / 1e6) }

	r1 := readHistory(t, history, usagelog.HistoryFilter{}, p1)
	if r1.PriceTableEvidence != "local:history-test-table" {
		t.Fatalf("R13: price_table_evidence %q", r1.PriceTableEvidence)
	}
	checkDollars(t, "P1 totals", r1.Totals.Dollars, nil)
	checkDollars(t, "P1 haiku", groupByKey(t, r1.ByModel, hHaiku).Dollars, m(2000))
	checkDollars(t, "P1 opus", groupByKey(t, r1.ByModel, hOpus).Dollars, nil)
	checkDollars(t, "P1 sonnet", groupByKey(t, r1.ByModel, hSonnet).Dollars, m(322))
	checkDollars(t, "P1 2026-11-01", groupByKey(t, r1.ByDay, "2026-11-01").Dollars, m(5000))
	checkDollars(t, "P1 2026-11-02", groupByKey(t, r1.ByDay, "2026-11-02").Dollars, m(460.8))
	checkDollars(t, "P1 2026-11-04", groupByKey(t, r1.ByDay, "2026-11-04").Dollars, nil)
	checkDollars(t, "P1 unknown day", groupByKey(t, r1.ByDay, "unknown").Dollars, m(2000))
	checkDollars(t, "P1 sA", groupByKey(t, r1.BySession, "sA").Dollars, m(5322))
	checkDollars(t, "P1 sB", groupByKey(t, r1.BySession, "sB").Dollars, m(2138.8))
	checkDollars(t, "P1 sC", groupByKey(t, r1.BySession, "sC").Dollars, nil)

	r2 := readHistory(t, history, usagelog.HistoryFilter{}, p2)
	if r2.PriceTableEvidence != "local:second-table" {
		t.Fatalf("R13: price_table_evidence %q", r2.PriceTableEvidence)
	}
	checkDollars(t, "P2 totals", r2.Totals.Dollars, nil)
	checkDollars(t, "P2 haiku", groupByKey(t, r2.ByModel, hHaiku).Dollars, nil)
	checkDollars(t, "P2 opus", groupByKey(t, r2.ByModel, hOpus).Dollars, m(9562))
	checkDollars(t, "P2 sonnet", groupByKey(t, r2.ByModel, hSonnet).Dollars, m(486))
	checkDollars(t, "P2 2026-11-01", groupByKey(t, r2.ByDay, "2026-11-01").Dollars, m(8800))
	checkDollars(t, "P2 2026-11-02", groupByKey(t, r2.ByDay, "2026-11-02").Dollars, m(748))
	checkDollars(t, "P2 2026-11-04", groupByKey(t, r2.ByDay, "2026-11-04").Dollars, m(500))
	checkDollars(t, "P2 unknown day", groupByKey(t, r2.ByDay, "unknown").Dollars, nil)
	checkDollars(t, "P2 sA", groupByKey(t, r2.BySession, "sA").Dollars, m(9286))
	checkDollars(t, "P2 sB", groupByKey(t, r2.BySession, "sB").Dollars, nil)
	checkDollars(t, "P2 sC", groupByKey(t, r2.BySession, "sC").Dollars, m(500))

	// A filtered report prices its totals: sA = A+B.
	checkDollars(t, "P1 sA totals", readHistory(t, history, usagelog.HistoryFilter{SessionID: "sA"}, p1).Totals.Dollars, m(5322))
	checkDollars(t, "P2 sA totals", readHistory(t, history, usagelog.HistoryFilter{SessionID: "sA"}, p2).Totals.Dollars, m(9286))
	// Pricing never changes the history.
	before := readBytes(t, history)
	readHistory(t, history, usagelog.HistoryFilter{}, p1)
	if !bytes.Equal(readBytes(t, history), before) {
		t.Fatal("R13: reading with prices changed the history")
	}
}

// ---------------------------------------------------------------------------
// R14, R15: option and runtime errors.

func TestHistoryInvalidOptions(t *testing.T) {
	history := reportFixture(t)
	for name, filter := range map[string]usagelog.HistoryFilter{
		"since with slashes":  {Since: "2026/11/01"},
		"until day first":     {Until: "01-11-2026"},
		"since one-digit day": {Since: "2026-11-1"},
		"until words":         {Until: "yesterday"},
		"since with time":     {Since: "2026-11-01T00:00:00Z"},
		"since after until":   {Since: "2026-11-03", Until: "2026-11-02"},
	} {
		t.Run("read "+name, func(t *testing.T) {
			_, err := usagelog.ReadHistory(t.Context(), history, filter, nil)
			if !errors.Is(err, usagelog.ErrInvalidOptions) {
				t.Fatalf("R14: ReadHistory(%+v) error %v, want ErrInvalidOptions", filter, err)
			}
		})
	}
	main := cleanSession(t, false)
	prices := rateTable(map[string][5]float64{hOpus: {4, 20, 5, 8, 0.2}})
	for name, options := range map[string]usagelog.Options{
		"prices": {ClaudeTranscript: main, Prices: prices},
	} {
		t.Run("append "+name, func(t *testing.T) {
			history := filepath.Join(t.TempDir(), "history.jsonl")
			_, err := usagelog.AppendHistory(t.Context(), history, options)
			if !errors.Is(err, usagelog.ErrInvalidOptions) {
				t.Fatalf("R14: AppendHistory(%s) error %v, want ErrInvalidOptions", name, err)
			}
			if _, err := os.Lstat(history); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("R14: AppendHistory(%s) created the history (%v)", name, err)
			}
		})
	}
}

func TestHistoryRuntimeErrors(t *testing.T) {
	t.Run("read missing history", func(t *testing.T) {
		_, err := usagelog.ReadHistory(t.Context(), filepath.Join(t.TempDir(), "absent.jsonl"), usagelog.HistoryFilter{}, nil)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("R15: ReadHistory of a missing file: %v, want os.ErrNotExist", err)
		}
	})
	t.Run("append missing transcript", func(t *testing.T) {
		history := filepath.Join(t.TempDir(), "metrics", "history.jsonl")
		_, err := usagelog.AppendHistory(t.Context(), history, usagelog.Options{ClaudeTranscript: filepath.Join(t.TempDir(), "s9.jsonl")})
		if err == nil {
			t.Fatal("R15: AppendHistory with a missing transcript succeeded")
		}
		if _, err := os.Lstat(history); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("R15: a failed import created the history (%v)", err)
		}
	})
}
