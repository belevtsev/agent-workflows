package cli

// End-to-end tests for contract metrics-history revision 1 (R1-R15) through
// Execute, written from requirements-r1.md and contract-usagelog-r1.json only.
// Transcript fixtures reuse writeClaudeSession, whose per-request tokens are
// listed in its doc comment (metrics_import_test.go); every expected value
// below is computed by hand from those tokens.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
)

// mhEnv is one isolated environment: a home, a working directory and the
// environment variables Getenv returns.
type mhEnv struct {
	home, wd string
	vars     map[string]string
}

func newMHEnv(t *testing.T) mhEnv {
	t.Helper()
	return mhEnv{home: t.TempDir(), wd: t.TempDir(), vars: map[string]string{}}
}

func (e mhEnv) config() (Config, *bytes.Buffer, *bytes.Buffer) {
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)
	return Config{
		Version: "v1.2.3", Revision: fixtureSHA, BuiltAt: "now",
		In: strings.NewReader(""), Out: out, Err: errOut,
		HomeDir:    func() (string, error) { return e.home, nil },
		WorkingDir: func() (string, error) { return e.wd, nil },
		Executable: os.Executable,
		Getenv:     func(key string) string { return e.vars[key] },
	}, out, errOut
}

func (e mhEnv) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	config, out, errOut := e.config()
	err := Execute(t.Context(), args, config)
	return out.String(), errOut.String(), err
}

// mhOne decodes stdout as exactly one JSON value.
func mhOne(t *testing.T, stdout string, into any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(into); err != nil {
		t.Fatalf("stdout is not one JSON value: %v\n%s", err, stdout)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Fatalf("stdout holds more than one JSON value:\n%s", stdout)
	}
}

func (e mhEnv) appendOK(t *testing.T, args ...string) (usagelog.AppendResult, map[string]any) {
	t.Helper()
	stdout, stderr, err := e.run(t, args...)
	if err != nil || ExitCode(err) != 0 {
		t.Fatalf("%v: err %v exit %d\nstderr %s", args, err, ExitCode(err), stderr)
	}
	var result usagelog.AppendResult
	mhOne(t, stdout, &result)
	var raw map[string]any
	mhOne(t, stdout, &raw)
	if sum := result.Appended + result.AlreadyPresent + result.Conflicting + result.Unkeyed + result.Rejected; sum != result.Requests {
		t.Fatalf("R2: bucket sum %d != requests %d: %+v", sum, result.Requests, result)
	}
	return result, raw
}

func (e mhEnv) historyOK(t *testing.T, args ...string) (usagelog.HistoryReport, map[string]any, string) {
	t.Helper()
	stdout, stderr, err := e.run(t, append([]string{"metrics", "history"}, args...)...)
	if err != nil || ExitCode(err) != 0 {
		t.Fatalf("metrics history %v: err %v exit %d\nstderr %s", args, err, ExitCode(err), stderr)
	}
	var report usagelog.HistoryReport
	mhOne(t, stdout, &report)
	var raw map[string]any
	mhOne(t, stdout, &raw)
	return report, raw, stderr
}

func mhRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mhAbsent(t *testing.T, path, why string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s: %s exists (%v), want nothing written", why, path, err)
	}
}

// mhRecords parses every history line strictly as a HistoryRecord.
func mhRecords(t *testing.T, path string) (map[string]usagelog.HistoryRecord, map[string]map[string]any) {
	t.Helper()
	data := mhRead(t, path)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("R1: history is empty or does not end with a newline: %q", data)
	}
	records, raws := map[string]usagelog.HistoryRecord{}, map[string]map[string]any{}
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		var rec usagelog.HistoryRecord
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("R1: history line %d is not a record (%v): %q", i+1, err, line)
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatal(err)
		}
		if _, dup := records[rec.RequestID]; dup {
			t.Fatalf("R1: request %q stored twice", rec.RequestID)
		}
		records[rec.RequestID], raws[rec.RequestID] = rec, raw
	}
	return records, raws
}

func mhRecordLine(t *testing.T, session, requestID, model, ts string, uncached, out int64) string {
	t.Helper()
	m := map[string]any{
		"v": 1, "kind": "cw.metrics_request", "source": "claude_transcript", "session_id": session,
		"request_id": requestID, "agent_id": "main", "agent_role": "main", "model": model,
		"imported_at": "2026-11-06T12:00:00Z", "uncached_input_tokens": uncached, "cache_write_5m_tokens": 0,
		"cache_write_1h_tokens": 0, "cache_write_unsplit_tokens": 0, "cache_read_tokens": 0,
		"output_tokens": out, "thinking_tokens": nil,
	}
	if ts != "" {
		m["timestamp"] = ts
	}
	return mustJSON(t, m)
}

func mhWriteHistory(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	writeLines(t, path, lines...)
	return path
}

func mhDollars(t *testing.T, name string, got usagelog.Amount, want *float64) {
	t.Helper()
	if want == nil {
		if got.Amount != nil || strings.TrimSpace(got.UnknownReason) == "" {
			t.Fatalf("%s dollars = %v (reason %q), want null with a reason", name, deref(got.Amount), got.UnknownReason)
		}
		return
	}
	near(t, name+" dollars", got.Amount, *want)
	if got.Basis != usagelog.BasisPriceTable {
		t.Fatalf("%s dollars basis %q, want %q", name, got.Basis, usagelog.BasisPriceTable)
	}
}

func mhGroup(t *testing.T, groups []usagelog.HistoryGroup, key string) usagelog.HistoryGroup {
	t.Helper()
	for _, g := range groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("no group %q in %+v", key, groups)
	return usagelog.HistoryGroup{}
}

func mhKeys(groups []usagelog.HistoryGroup) []string {
	keys := []string{}
	for _, g := range groups {
		keys = append(keys, g.Key)
	}
	return keys
}

// ---------------------------------------------------------------------------

// R1, R2, R10, R11: --append through Execute stores the writeClaudeSession
// requests, reports the same request count and status as the import report,
// and creates the default history under --state-dir with private modes.
func TestMetricsHistoryAppendStoresImportedRequests(t *testing.T) {
	root, main := writeClaudeSession(t)
	env := newMHEnv(t)
	state := filepath.Join(t.TempDir(), "state")
	report, _ := runImport(t, "--claude-transcript", main)

	result, raw := env.appendOK(t, "--state-dir", state, "metrics", "import", "--claude-transcript", main, "--append")
	history := filepath.Join(state, "metrics", "history.jsonl")
	if result.SchemaVersion != 1 || result.Kind != "cw.metrics_history_append" || result.Source != usagelog.SourceClaude || result.SessionID != sessionID {
		t.Fatalf("R1: result identity %+v", result)
	}
	if !filepath.IsAbs(result.History) || !samePath(result.History, history) {
		t.Fatalf("R1: history %q, want %q", result.History, history)
	}
	// R2: the six distinct requests of the fixture, all keyed.
	if result.Requests != report.Totals.Requests || result.Requests != 6 || result.Appended != 6 {
		t.Fatalf("R2: requests %d appended %d, want import totals.requests %d = 6 appended", result.Requests, result.Appended, report.Totals.Requests)
	}
	if result.ImportStatus != report.Status || !slices.Equal(result.IncompleteReasons, report.IncompleteReasons) {
		t.Fatalf("R1: import_status %q %v, want the import report's %q %v", result.ImportStatus, result.IncompleteReasons, report.Status, report.IncompleteReasons)
	}
	if _, ok := raw["incomplete_reasons"]; !ok {
		t.Fatalf("R1: incomplete_reasons missing from %v", raw)
	}

	records, raws := mhRecords(t, history)
	if len(records) != 6 {
		t.Fatalf("R1: %d records, want 6", len(records))
	}
	type want struct {
		agent, role, agentType, model     string
		uncached, w5, w1, read, out, thnk int64 // thnk -1: null
	}
	for id, w := range map[string]want{
		"req_1":         {"main", "main", "", opusModel, 2, 10, 20, 100, 7, 3},
		"req_2":         {"main", "main", "", opusModel, 5, 0, 0, 200, 11, -1},
		"message:msg_x": {"main", "main", "", opusModel, 1, 4, 0, 0, 2, -1},
		"message:msg_y": {"main", "main", "", sonnetModel, 3, 0, 6, 50, 9, 4},
		"req_a1":        {"agent-a1", "subagent", "general-purpose", opusModel, 4, 8, 0, 40, 6, 2},
		"req_b1":        {"agent-b2", "subagent", "", sonnetModel, 1, 0, 0, 10, 3, -1},
	} {
		r, ok := records[id]
		if !ok {
			t.Fatalf("R1: no record for %s", id)
		}
		if r.V != 1 || r.Kind != "cw.metrics_request" || r.Source != usagelog.SourceClaude || r.SessionID != sessionID ||
			r.AgentID != w.agent || r.AgentRole != w.role || r.AgentType != w.agentType || r.Model != w.model {
			t.Fatalf("R1: %s labels %+v, want %+v", id, r, w)
		}
		if r.UncachedInputTokens != w.uncached || r.CacheWrite5mTokens != w.w5 || r.CacheWrite1hTokens != w.w1 ||
			r.CacheWriteUnsplitTokens != 0 || r.CacheReadTokens != w.read || r.OutputTokens != w.out {
			t.Fatalf("R1: %s tokens %+v, want %+v", id, r, w)
		}
		if w.thnk < 0 {
			if !isNullMember(raws[id], "thinking_tokens") {
				t.Fatalf("R1: %s thinking_tokens = %v, want JSON null", id, raws[id]["thinking_tokens"])
			}
		} else if r.ThinkingTokens == nil || *r.ThinkingTokens != w.thnk {
			t.Fatalf("R1: %s thinking_tokens %v, want %d", id, r.ThinkingTokens, w.thnk)
		}
		// The fixture has no timestamps; empty agent types are omitted.
		if _, ok := raws[id]["timestamp"]; ok {
			t.Fatalf("R1: %s timestamp %v, want omitted", id, raws[id]["timestamp"])
		}
		if _, ok := raws[id]["agent_type"]; ok && w.agentType == "" {
			t.Fatalf("R1: %s agent_type %v, want omitted", id, raws[id]["agent_type"])
		}
	}

	// R10: the meta description and transcript location never reach the history.
	data := mhRead(t, history)
	for _, secret := range []string{"Review the fix", "hello", root, main} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("R10: history contains %q", secret)
		}
	}
	// R11: new file 0600, new parent directory 0700.
	if runtime.GOOS != "windows" {
		if info, err := os.Lstat(history); err != nil || info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
			t.Fatalf("R11: history mode %v (%v), want regular 0600", info, err)
		}
		if info, err := os.Stat(filepath.Dir(history)); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("R11: metrics directory mode %v (%v), want 0700", info, err)
		}
	}
}

// R1: the default history follows the state directory (XDG_STATE_HOME, else
// <home>/.local/state), and a relative --history resolves against WorkingDir.
func TestMetricsHistoryAppendLocations(t *testing.T) {
	_, main := writeClaudeSession(t)
	t.Run("XDG_STATE_HOME", func(t *testing.T) {
		env := newMHEnv(t)
		xdg := t.TempDir()
		env.vars["XDG_STATE_HOME"] = xdg
		result, _ := env.appendOK(t, "metrics", "import", "--claude-transcript", main, "--append")
		want := filepath.Join(xdg, "agent-workflows", "metrics", "history.jsonl")
		if !samePath(result.History, want) || result.Appended != 6 {
			t.Fatalf("R1: history %q appended %d, want %q and 6", result.History, result.Appended, want)
		}
		mhRecords(t, want)
	})
	t.Run("home fallback", func(t *testing.T) {
		env := newMHEnv(t)
		result, _ := env.appendOK(t, "metrics", "import", "--claude-transcript", main, "--append")
		want := filepath.Join(env.home, ".local", "state", "agent-workflows", "metrics", "history.jsonl")
		if !samePath(result.History, want) || result.Appended != 6 {
			t.Fatalf("R1: history %q appended %d, want %q and 6", result.History, result.Appended, want)
		}
		mhRecords(t, want)
	})
	t.Run("relative --history", func(t *testing.T) {
		env := newMHEnv(t)
		result, _ := env.appendOK(t, "metrics", "import", "--claude-transcript", main, "--append", "--history", filepath.Join("costs", "h.jsonl"))
		want := filepath.Join(env.wd, "costs", "h.jsonl")
		if !filepath.IsAbs(result.History) || !samePath(result.History, want) || result.Appended != 6 {
			t.Fatalf("R1: history %q appended %d, want %q and 6", result.History, result.Appended, want)
		}
		mhRecords(t, want)
	})
}

// R3, R12, R13: append twice (the second appends nothing), then report the
// history under two price tables and without one.
//
// writeClaudeSession per model: opus requests 4, uncached 12, w5 22, w1 20,
// read 340, out 26, thinking 5 (2 reported), total input 394; sonnet requests
// 2, uncached 4, w5 0, w1 6, read 60, out 12, thinking 4 (1 reported), total
// input 70. Totals: 6, 16, 22, 26, 400, 38, thinking 9 (3), total input 464.
//
// Table 1 (opus 4/20/5/8/0.2, sonnet 2/10/2.5/4/0.1):
// opus 12*4+26*20+22*5+20*8+340*0.2 = 906; sonnet 4*2+12*10+6*4+60*0.1 = 158; total 1064 (per million).
// Table 2 (opus at 1/5/1.25/2/0.1, sonnet at 4/20/5/8/0.2):
// opus 12+130+27.5+40+34 = 243.5; sonnet 16+240+0+48+12 = 316; total 559.5.
func TestMetricsHistoryReportRecomputesDollars(t *testing.T) {
	_, main := writeClaudeSession(t)
	env := newMHEnv(t)
	history := filepath.Join(t.TempDir(), "history.jsonl")
	env.appendOK(t, "metrics", "import", "--claude-transcript", main, "--append", "--history", history)
	stored := mhRead(t, history)
	again, _ := env.appendOK(t, "metrics", "import", "--claude-transcript", main, "--append", "--history", history)
	if again.Appended != 0 || again.AlreadyPresent != 6 || !bytes.Equal(mhRead(t, history), stored) {
		t.Fatalf("R3: re-import appended %d already_present %d (history changed: %v), want 0, 6, unchanged",
			again.Appended, again.AlreadyPresent, !bytes.Equal(mhRead(t, history), stored))
	}

	plain, raw, _ := env.historyOK(t, "--history", history)
	if plain.SchemaVersion != 1 || plain.Kind != "cw.metrics_history" || !samePath(plain.History, history) || !filepath.IsAbs(plain.History) {
		t.Fatalf("R12: identity schema %d kind %q history %q", plain.SchemaVersion, plain.Kind, plain.History)
	}
	if plain.Lines != 6 || plain.Records != 6 || plain.DuplicateLines != 0 || plain.DamagedLines != 0 {
		t.Fatalf("R12: lines %d records %d duplicates %d damaged %d, want 6 6 0 0", plain.Lines, plain.Records, plain.DuplicateLines, plain.DamagedLines)
	}
	checkTokens(t, "totals", plain.Totals.Tokens, 6, 16, 22, 26, 400, 38, 464)
	checkThinking(t, "totals", plain.Totals.Tokens, ptr(9), 3)
	if got := mhKeys(plain.ByModel); !slices.Equal(got, []string{opusModel, sonnetModel}) {
		t.Fatalf("R12: by_model keys %q", got)
	}
	checkTokens(t, "opus", plain.ByModel[0].Tokens, 4, 12, 22, 20, 340, 26, 394)
	checkThinking(t, "opus", plain.ByModel[0].Tokens, ptr(5), 2)
	checkTokens(t, "sonnet", plain.ByModel[1].Tokens, 2, 4, 0, 6, 60, 12, 70)
	checkThinking(t, "sonnet", plain.ByModel[1].Tokens, ptr(4), 1)
	if got := mhKeys(plain.ByDay); !slices.Equal(got, []string{"unknown"}) {
		t.Fatalf("R12: by_day keys %q, want [unknown] (no timestamps)", got)
	}
	if got := mhKeys(plain.BySession); !slices.Equal(got, []string{sessionID}) {
		t.Fatalf("R12: by_session keys %q", got)
	}
	if plain.Totals.Key != "" {
		t.Fatalf("R12: totals key %q, want empty", plain.Totals.Key)
	}
	// R13 without --prices: null amounts with reasons.
	mhDollars(t, "plain totals", plain.Totals.Dollars, nil)
	for _, g := range slices.Concat(plain.ByModel, plain.ByDay, plain.BySession) {
		mhDollars(t, "plain "+g.Key, g.Dollars, nil)
	}
	totals := object(t, raw["totals"], "totals")
	if !isNullMember(object(t, totals["dollars"], "totals.dollars"), "amount") {
		t.Fatalf("R13: totals.dollars.amount = %v, want JSON null", totals["dollars"])
	}

	m := func(units float64) *float64 { v := units / 1e6; return &v }
	table1 := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	r1, _, _ := env.historyOK(t, "--history", history, "--prices", table1)
	if r1.PriceTableEvidence != "local:test-price-table" {
		t.Fatalf("R13: price_table_evidence %q", r1.PriceTableEvidence)
	}
	mhDollars(t, "table1 totals", r1.Totals.Dollars, m(1064))
	mhDollars(t, "table1 opus", mhGroup(t, r1.ByModel, opusModel).Dollars, m(906))
	mhDollars(t, "table1 sonnet", mhGroup(t, r1.ByModel, sonnetModel).Dollars, m(158))
	mhDollars(t, "table1 day", mhGroup(t, r1.ByDay, "unknown").Dollars, m(1064))
	mhDollars(t, "table1 session", mhGroup(t, r1.BySession, sessionID).Dollars, m(1064))

	table2 := writePrices(t, map[string]rates{opusModel: haikuRates, sonnetModel: opusRates})
	r2, _, _ := env.historyOK(t, "--history", history, "--prices", table2)
	mhDollars(t, "table2 totals", r2.Totals.Dollars, m(559.5))
	mhDollars(t, "table2 opus", mhGroup(t, r2.ByModel, opusModel).Dollars, m(243.5))
	mhDollars(t, "table2 sonnet", mhGroup(t, r2.ByModel, sonnetModel).Dollars, m(316))

	// A table without sonnet prices opus only; groups holding sonnet are unknown.
	table3 := writePrices(t, map[string]rates{opusModel: opusRates})
	r3, _, _ := env.historyOK(t, "--history", history, "--prices", table3)
	mhDollars(t, "table3 opus", mhGroup(t, r3.ByModel, opusModel).Dollars, m(906))
	mhDollars(t, "table3 sonnet", mhGroup(t, r3.ByModel, sonnetModel).Dollars, nil)
	mhDollars(t, "table3 totals", r3.Totals.Dollars, nil)
	mhDollars(t, "table3 session", mhGroup(t, r3.BySession, sessionID).Dollars, nil)

	if !bytes.Equal(mhRead(t, history), stored) {
		t.Fatal("R13: reporting changed the history")
	}
}

// R12: each filter flag selects records by exact session and model and by
// inclusive UTC date bounds; undated records never match a date bound.
//
//	X s1 opus   2026-11-01T23:59:59Z        uncached 1
//	Y s1 sonnet 2026-11-02T00:00:00Z        uncached 10
//	Z s2 opus   2026-11-03T12:00:00-13:00   uncached 100   (UTC 2026-11-04T01:00:00Z)
//	W s2 opus   no timestamp                uncached 1000
func TestMetricsHistoryFilters(t *testing.T) {
	history := mhWriteHistory(t,
		mhRecordLine(t, "s1", "X", opusModel, "2026-11-01T23:59:59Z", 1, 1),
		mhRecordLine(t, "s1", "Y", sonnetModel, "2026-11-02T00:00:00Z", 10, 10),
		mhRecordLine(t, "s2", "Z", opusModel, "2026-11-03T12:00:00-13:00", 100, 100),
		mhRecordLine(t, "s2", "W", opusModel, "", 1000, 1000),
	)
	env := newMHEnv(t)
	for _, c := range []struct {
		name     string
		args     []string
		filter   usagelog.HistoryFilter
		records  int64
		uncached int64
	}{
		{"none", nil, usagelog.HistoryFilter{}, 4, 1111},
		{"session", []string{"--session", "s2"}, usagelog.HistoryFilter{SessionID: "s2"}, 2, 1100},
		{"model", []string{"--model", opusModel}, usagelog.HistoryFilter{Model: opusModel}, 3, 1101},
		{"since", []string{"--since", "2026-11-02"}, usagelog.HistoryFilter{Since: "2026-11-02"}, 2, 110},
		{"until", []string{"--until", "2026-11-03"}, usagelog.HistoryFilter{Until: "2026-11-03"}, 2, 11},
		{"single day", []string{"--since", "2026-11-01", "--until", "2026-11-01"}, usagelog.HistoryFilter{Since: "2026-11-01", Until: "2026-11-01"}, 1, 1},
		{"all filters", []string{"--session", "s2", "--model", opusModel, "--since", "2026-11-04", "--until", "2026-11-04"},
			usagelog.HistoryFilter{SessionID: "s2", Model: opusModel, Since: "2026-11-04", Until: "2026-11-04"}, 1, 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			report, _, _ := env.historyOK(t, append([]string{"--history", history}, c.args...)...)
			if report.Filter != c.filter {
				t.Fatalf("R12: filter %+v, want %+v", report.Filter, c.filter)
			}
			if report.Lines != 4 || report.Records != c.records || report.Totals.Requests != c.records || report.Totals.UncachedInputTokens != c.uncached {
				t.Fatalf("R12: lines %d records %d requests %d uncached %d, want 4 %d %d %d",
					report.Lines, report.Records, report.Totals.Requests, report.Totals.UncachedInputTokens, c.records, c.records, c.uncached)
			}
		})
	}
	// by_day keys use the UTC date: Z lands on 2026-11-04, W on "unknown".
	report, _, _ := env.historyOK(t, "--history", history)
	if got := mhKeys(report.ByDay); !slices.Equal(got, []string{"2026-11-01", "2026-11-02", "2026-11-04", "unknown"}) {
		t.Fatalf("R12: by_day keys %q", got)
	}
	if got := mhKeys(report.BySession); !slices.Equal(got, []string{"s1", "s2"}) {
		t.Fatalf("R12: by_session keys %q", got)
	}
}

// R7, R9: damaged and duplicate lines are reported, warned about on stderr,
// and the command still exits 0.
func TestMetricsHistoryDamagedLinesWarnAndExitZero(t *testing.T) {
	good := mhRecordLine(t, "s1", "ok_1", opusModel, "2026-11-01T00:00:00Z", 5, 5)
	lines := []string{good}
	for i := range 7 {
		lines = append(lines, fmt.Sprintf(`{"damaged":%d`, i))
	}
	lines = append(lines, good, mhRecordLine(t, "s1", "ok_2", opusModel, "", 7, 7))
	history := mhWriteHistory(t, lines...)
	report, _, stderr := newMHEnv(t).historyOK(t, "--history", history)
	if report.DamagedLines != 7 || len(report.Damaged) != 7 || report.Damaged[0].Line != 2 || report.Damaged[6].Line != 8 {
		t.Fatalf("R7: damaged_lines %d %+v, want 7 at lines 2..8", report.DamagedLines, report.Damaged)
	}
	if report.Records != 2 || report.DuplicateLines != 1 || report.Lines != 10 || report.Totals.UncachedInputTokens != 12 {
		t.Fatalf("R7/R9: records %d duplicates %d lines %d uncached %d, want 2 1 10 12",
			report.Records, report.DuplicateLines, report.Lines, report.Totals.UncachedInputTokens)
	}
	if !regexp.MustCompile(`\b7\b`).MatchString(stderr) {
		t.Fatalf("R7: stderr %q does not name the 7 damaged lines", stderr)
	}
}

// R14: usage errors exit 2 and write nothing.
func TestMetricsHistoryUsageErrors(t *testing.T) {
	_, main := writeClaudeSession(t)
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	existing := mhWriteHistory(t, mhRecordLine(t, "s1", "ok", opusModel, "2026-11-01T00:00:00Z", 1, 1))
	before := mhRead(t, existing)
	env := newMHEnv(t)
	t.Run("import", func(t *testing.T) {
		for name, extra := range map[string][]string{
			"--history without --append":  {"--history", "HISTORY"},
			"--append with --prices":      {"--append", "--history", "HISTORY", "--prices", prices},
			"--append with --composition": {"--append", "--history", "HISTORY", "--composition"},
			"--append with --summary":     {"--append", "--history", "HISTORY", "--summary"},
			"--append with all three":     {"--append", "--history", "HISTORY", "--prices", prices, "--composition", "--summary"},
		} {
			t.Run(name, func(t *testing.T) {
				history := filepath.Join(t.TempDir(), "history.jsonl")
				args := []string{"metrics", "import", "--claude-transcript", main}
				for _, a := range extra {
					args = append(args, strings.ReplaceAll(a, "HISTORY", history))
				}
				stdout, _, err := env.run(t, args...)
				if err == nil || ExitCode(err) != 2 {
					t.Fatalf("R14: %v: err %v exit %d, want 2", args, err, ExitCode(err))
				}
				if strings.Contains(stdout, "cw.metrics_") {
					t.Fatalf("R14: usage error printed a result: %s", stdout)
				}
				mhAbsent(t, history, "R14")
			})
		}
	})
	t.Run("history", func(t *testing.T) {
		for name, extra := range map[string][]string{
			"since with slashes":  {"--since", "2026/11/01"},
			"until day first":     {"--until", "01-11-2026"},
			"since one-digit day": {"--since", "2026-11-1"},
			"until words":         {"--until", "yesterday"},
			"since after until":   {"--since", "2026-11-03", "--until", "2026-11-02"},
			"positional argument": {"extra"},
		} {
			t.Run(name, func(t *testing.T) {
				stdout, _, err := env.run(t, append([]string{"metrics", "history", "--history", existing}, extra...)...)
				if err == nil || ExitCode(err) != 2 {
					t.Fatalf("R14: %v: err %v exit %d, want 2", extra, err, ExitCode(err))
				}
				if strings.Contains(stdout, "cw.metrics_history") {
					t.Fatalf("R14: usage error printed a report: %s", stdout)
				}
				if !bytes.Equal(mhRead(t, existing), before) {
					t.Fatal("R14: usage error changed the history")
				}
			})
		}
	})
}

// R15, R11: runtime failures exit 1, print no result and write nothing.
func TestMetricsHistoryRuntimeErrors(t *testing.T) {
	_, main := writeClaudeSession(t)
	env := newMHEnv(t)
	fail := func(t *testing.T, args ...string) {
		t.Helper()
		stdout, _, err := env.run(t, args...)
		if err == nil || ExitCode(err) != 1 {
			t.Fatalf("%v: err %v exit %d, want 1", args, err, ExitCode(err))
		}
		if strings.Contains(stdout, "cw.metrics_history") {
			t.Fatalf("failure printed a result: %s", stdout)
		}
	}
	t.Run("missing history", func(t *testing.T) {
		fail(t, "metrics", "history", "--history", filepath.Join(t.TempDir(), "absent.jsonl"))
	})
	renamed := filepath.Join(t.TempDir(), "renamed-copy.jsonl")
	if err := os.WriteFile(renamed, mhRead(t, main), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, transcript := range map[string]string{
		"missing transcript":           filepath.Join(t.TempDir(), "s9.jsonl"),
		"session id does not match":    renamed,
		"subagent passed as a session": filepath.Join(filepath.Dir(main), sessionID, "subagents", "agent-a1.jsonl"),
	} {
		t.Run(name, func(t *testing.T) {
			history := filepath.Join(t.TempDir(), "metrics", "history.jsonl")
			fail(t, "metrics", "import", "--claude-transcript", transcript, "--append", "--history", history)
			mhAbsent(t, history, "R15")
		})
	}
	t.Run("history is a symlink", func(t *testing.T) {
		target := mhWriteHistory(t, mhRecordLine(t, "s0", "old", opusModel, "", 1, 1))
		before := mhRead(t, target)
		link := filepath.Join(t.TempDir(), "history.jsonl")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		fail(t, "metrics", "import", "--claude-transcript", main, "--append", "--history", link)
		if !bytes.Equal(mhRead(t, target), before) {
			t.Fatal("R11: the symlink target was written")
		}
	})
	t.Run("history is a directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "history.jsonl")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		fail(t, "metrics", "import", "--claude-transcript", main, "--append", "--history", dir)
	})
}

// ---------------------------------------------------------------------------
// R6 across processes: separate cw processes append overlapping transcripts to
// one history. An in-process mutex alone cannot serialise them; without a
// file lock every process can read the same identities and append them again.

const (
	mhHelperArgs = "CW_METRICS_HISTORY_HELPER_ARGS"
	mhHelperDir  = "CW_METRICS_HISTORY_HELPER_DIR"
)

// TestMetricsHistoryHelperProcess is the child of
// TestMetricsHistoryConcurrentProcesses; it does nothing in a normal run.
func TestMetricsHistoryHelperProcess(t *testing.T) {
	encoded := os.Getenv(mhHelperArgs)
	if encoded == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil {
		fmt.Fprintln(os.Stderr, "bad helper args:", err)
		os.Exit(3)
	}
	dir := os.Getenv(mhHelperDir)
	config := Config{
		Version: "v1.2.3", Revision: fixtureSHA, BuiltAt: "now",
		In: strings.NewReader(""), Out: os.Stdout, Err: os.Stderr,
		HomeDir:    func() (string, error) { return dir, nil },
		WorkingDir: func() (string, error) { return dir, nil },
		Executable: os.Executable,
		Getenv:     func(string) string { return "" },
	}
	// Barrier: announce readiness, then block until the parent releases stdin.
	fmt.Fprintln(os.Stderr, "ready")
	var release [1]byte
	_, _ = os.Stdin.Read(release[:])
	err := Execute(context.Background(), args, config)
	os.Exit(ExitCode(err))
}

func TestMetricsHistoryConcurrentProcesses(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const (
		processes = 8
		rounds    = 4
	)
	transcript := func(session string, from, to int) string {
		path := filepath.Join(t.TempDir(), "project", session+".jsonl")
		var lines []string
		for i := from; i < to; i++ {
			lines = append(lines, claudeAssistant(t, session, fmt.Sprintf("req_%04d", i), fmt.Sprintf("msg_%04d", i), opusModel,
				&usageSpec{in: int64(i + 1), w5: 1, read: 2, out: int64(i + 3)}))
		}
		writeLines(t, path, lines...)
		return path
	}
	// "pc" in two overlapping copies (req_0040..req_0079 shared) and a disjoint
	// "pq": 120 + 40 = 160 distinct keyed requests.
	paths := []string{transcript("pc", 0, 80), transcript("pc", 40, 120), transcript("pq", 0, 40)}
	const distinct = 160

	for round := range rounds {
		history := filepath.Join(t.TempDir(), "history.jsonl")
		type child struct {
			cmd    *exec.Cmd
			stdin  io.WriteCloser
			stdout bytes.Buffer
			stderr bytes.Buffer
			copied chan struct{}
		}
		children := make([]*child, processes)
		for i := range processes {
			args, _ := json.Marshal([]string{"metrics", "import", "--claude-transcript", paths[i%len(paths)], "--append", "--history", history})
			c := &child{copied: make(chan struct{})}
			c.cmd = exec.Command(exe, "-test.run=^TestMetricsHistoryHelperProcess$", "-test.count=1")
			c.cmd.Env = append(os.Environ(), mhHelperArgs+"="+string(args), mhHelperDir+"="+t.TempDir())
			c.cmd.Stdout = &c.stdout
			if c.stdin, err = c.cmd.StdinPipe(); err != nil {
				t.Fatal(err)
			}
			stderr, err := c.cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := c.cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.cmd.Process.Kill() })
			reader := bufio.NewReader(stderr)
			if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatalf("round %d child %d did not reach the barrier: %q %v", round, i, line, err)
			}
			go func() {
				defer close(c.copied)
				_, _ = io.Copy(&c.stderr, reader)
			}()
			children[i] = c
		}
		// Every child is blocked on stdin: release them together.
		var release sync.WaitGroup
		for _, c := range children {
			release.Add(1)
			go func() {
				defer release.Done()
				_, _ = c.stdin.Write([]byte{1})
				_ = c.stdin.Close()
			}()
		}
		release.Wait()

		var appended int64
		for i, c := range children {
			<-c.copied
			if err := c.cmd.Wait(); err != nil {
				t.Fatalf("round %d child %d: %v\nstdout %s\nstderr %s", round, i, err, c.stdout.String(), c.stderr.String())
			}
			var result usagelog.AppendResult
			mhOne(t, c.stdout.String(), &result)
			if result.Appended+result.AlreadyPresent != result.Requests || result.Conflicting != 0 {
				t.Fatalf("round %d child %d: %+v, want every request appended or already present", round, i, result)
			}
			appended += result.Appended
		}
		records, _ := mhRecordsAll(t, history)
		if records != distinct || appended != distinct {
			t.Fatalf("R6: round %d: %d distinct records, sum of appended %d, want %d each", round, records, appended, distinct)
		}
	}
}

// mhRecordsAll checks every line is a whole record and no identity repeats,
// returning the number of identities.
func mhRecordsAll(t *testing.T, path string) (int, []string) {
	t.Helper()
	data := mhRead(t, path)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("R6: history does not end with a newline")
	}
	seen := map[string]bool{}
	var ids []string
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		var rec usagelog.HistoryRecord
		if err := dec.Decode(&rec); err != nil || rec.V != 1 || rec.Kind != "cw.metrics_request" || rec.RequestID == "" {
			t.Fatalf("R6: line %d is not a whole record (%v): %q", i+1, err, line)
		}
		id := rec.Source + "\x00" + rec.SessionID + "\x00" + rec.RequestID
		if seen[id] {
			t.Fatalf("R6: line %d repeats %q", i+1, id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return len(seen), ids
}
