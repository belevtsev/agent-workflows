package cli

// End-to-end tests for contract metrics-history revision 2 (R16, R20) through
// Execute, written from requirements-r2.md, requirements-r1.md and
// contract-usagelog-r2.json only. The Codex fixture is writeCodexTree
// (metrics_import_test.go); its per-request tokens are listed in its doc
// comment, and every expected value below is computed by hand from them.

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
)

type mh2Want struct {
	agentID, role, agentType, model     string
	uncached, unsplit, read, out, think int64
}

// R16 through the CLI: --codex-session ... --append stores one codex_rollout
// record per keyed request, re-importing appends nothing, and metrics history
// aggregates the records.
//
// writeCodexTree, mapped per R16 (no line carries a top-level timestamp):
//
//	response:r1  T1 main             sol   input 100 cached 60 write 10 out 20 reasoning 5 -> uncached 30 read 60 unsplit 10 thinking 5
//	response:r2  T1 main             mini  input 50 out 8 reasoning 0                        -> uncached 50 thinking 0
//	response:r3  T2 subagent explorer sol  input 40 cached 30 out 10 reasoning 2             -> uncached 10 read 30 thinking 2
//	response:r4  T3 subagent (none)  sol   input 20 write 5 out 4 reasoning 1                -> uncached 15 unsplit 5 thinking 1
//	T9, T10: unrelated, never imported.
//
// Totals: requests 4, uncached 105, unsplit 15, read 90, out 42, total input 210, thinking 8 (4 reported).
// by_model mini: 1, uncached 50, out 8, total 50; sol: 3, uncached 55, unsplit 15, read 90, out 34, total 160.
//
// Fails when the CLI does not pass the Codex options to the append, stores a
// wrong field, re-appends on an unchanged import, or the report does not see
// the Codex records.
func TestMetricsHistoryCodexAppendThroughExecute(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	rootPath := writeCodexTree(t, archive)
	history := filepath.Join(t.TempDir(), "metrics", "history.jsonl")
	env := newMHEnv(t)
	args := []string{"metrics", "import", "--codex-session", rootPath, "--codex-sessions-root", archive, "--append", "--history", history}

	result, _ := env.appendOK(t, args...)
	if result.Kind != usagelog.HistoryAppendKind || result.Source != usagelog.SourceCodex || result.SessionID != "T1" {
		t.Fatalf("R16: result kind %q source %q session %q, want %q %q T1", result.Kind, result.Source, result.SessionID, usagelog.HistoryAppendKind, usagelog.SourceCodex)
	}
	if result.Requests != 4 || result.Appended != 4 || result.AlreadyPresent != 0 || result.Conflicting != 0 || result.Unkeyed != 0 || result.Rejected != 0 {
		t.Fatalf("R16: counts %+v, want requests 4 appended 4", result)
	}
	records, raws := mhRecords(t, history)
	if len(records) != 4 {
		t.Fatalf("R16: %d records, want 4 (unrelated rollouts excluded)", len(records))
	}
	for id, w := range map[string]mh2Want{
		"response:r1": {"T1", "main", "", "gpt-6.1-sol", 30, 10, 60, 20, 5},
		"response:r2": {"T1", "main", "", "gpt-6.1-mini", 50, 0, 0, 8, 0},
		"response:r3": {"T2", "subagent", "explorer", "gpt-6.1-sol", 10, 0, 30, 10, 2},
		"response:r4": {"T3", "subagent", "", "gpt-6.1-sol", 15, 5, 0, 4, 1},
	} {
		r, ok := records[id]
		if !ok {
			t.Fatalf("R16: no record %s", id)
		}
		if r.Source != usagelog.SourceCodex || r.SessionID != "T1" || r.AgentID != w.agentID || r.AgentRole != w.role || r.AgentType != w.agentType || r.Model != w.model {
			t.Fatalf("R16: %s = source %q session %q agent %q/%q/%q model %q, want %q T1 %q/%q/%q %q",
				id, r.Source, r.SessionID, r.AgentID, r.AgentRole, r.AgentType, r.Model, usagelog.SourceCodex, w.agentID, w.role, w.agentType, w.model)
		}
		if _, ok := raws[id]["agent_type"]; w.agentType == "" && ok {
			t.Fatalf("R16: %s agent_type %v present, want omitted", id, raws[id]["agent_type"])
		}
		if _, ok := raws[id]["timestamp"]; ok {
			t.Fatalf("R16: %s timestamp %v present, want omitted (the rollout line has none)", id, raws[id]["timestamp"])
		}
		if r.UncachedInputTokens != w.uncached || r.CacheWrite5mTokens != 0 || r.CacheWrite1hTokens != 0 || r.CacheWriteUnsplitTokens != w.unsplit ||
			r.CacheReadTokens != w.read || r.OutputTokens != w.out || r.ThinkingTokens == nil || *r.ThinkingTokens != w.think {
			t.Fatalf("R16: %s tokens %+v, want uncached %d w5 0 w1 0 unsplit %d read %d out %d thinking %d", id, r, w.uncached, w.unsplit, w.read, w.out, w.think)
		}
	}

	stored := mhRead(t, history)
	again, _ := env.appendOK(t, args...)
	if again.Requests != 4 || again.Appended != 0 || again.AlreadyPresent != 4 {
		t.Fatalf("R3: codex re-import counts %+v, want requests 4 appended 0 already_present 4", again)
	}
	if !bytes.Equal(mhRead(t, history), stored) {
		t.Fatal("R3: codex re-import changed the history")
	}

	report, _, _ := env.historyOK(t, "--history", history)
	if report.Records != 4 || report.DamagedLines != 0 {
		t.Fatalf("R16: history records %d damaged %d, want 4 0", report.Records, report.DamagedLines)
	}
	tt := report.Totals.Tokens
	if tt.Requests != 4 || tt.UncachedInputTokens != 105 || tt.CacheWriteUnsplitTokens != 15 || tt.CacheReadTokens != 90 || tt.OutputTokens != 42 ||
		tt.TotalInputTokens != 210 || tt.ThinkingTokens == nil || *tt.ThinkingTokens != 8 || tt.ThinkingReportedRequests != 4 {
		t.Fatalf("R16: totals %+v, want requests 4 uncached 105 unsplit 15 read 90 out 42 total 210 thinking 8 (4 reported)", tt)
	}
	if keys := mhKeys(report.BySession); !slices.Equal(keys, []string{"T1"}) {
		t.Fatalf("R16: by_session keys %q, want [T1]", keys)
	}
	if keys := mhKeys(report.ByDay); !slices.Equal(keys, []string{"unknown"}) {
		t.Fatalf("R16: by_day keys %q, want [unknown] (no timestamps)", keys)
	}
	if keys := mhKeys(report.ByModel); !slices.Equal(keys, []string{"gpt-6.1-mini", "gpt-6.1-sol"}) {
		t.Fatalf("R16: by_model keys %q", keys)
	}
	sol := mhGroup(t, report.ByModel, "gpt-6.1-sol")
	if sol.Requests != 3 || sol.UncachedInputTokens != 55 || sol.CacheWriteUnsplitTokens != 15 || sol.CacheReadTokens != 90 || sol.OutputTokens != 34 || sol.TotalInputTokens != 160 {
		t.Fatalf("R16: by_model sol %+v, want 3 / uncached 55 / unsplit 15 / read 90 / out 34 / total 160", sol.Tokens)
	}
	mini := mhGroup(t, report.ByModel, "gpt-6.1-mini")
	if mini.Requests != 1 || mini.UncachedInputTokens != 50 || mini.OutputTokens != 8 || mini.TotalInputTokens != 50 {
		t.Fatalf("R16: by_model mini %+v, want 1 / uncached 50 / out 8 / total 50", mini.Tokens)
	}
	dated, _, _ := env.historyOK(t, "--history", history, "--since", "2026-01-01")
	if dated.Records != 0 {
		t.Fatalf("R16: --since matched %d undated codex records, want 0", dated.Records)
	}
}

// R20 through the CLI: a malformed --since/--until or since after until is a
// usage error that names the flag and does not mention "import options".
func TestMetricsHistoryFilterErrorNamesTheFlag(t *testing.T) {
	existing := mhWriteHistory(t, mhRecordLine(t, "s1", "ok", opusModel, "2026-11-01T00:00:00Z", 1, 1))
	env := newMHEnv(t)
	for name, c := range map[string]struct {
		args  []string
		names []string // the error must contain one of these
	}{
		"malformed since":   {[]string{"--since", "2026/11/01"}, []string{"since"}},
		"malformed until":   {[]string{"--until", "yesterday"}, []string{"until"}},
		"bad since, until":  {[]string{"--since", "2026-11-1", "--until", "2026-11-30"}, []string{"since"}},
		"since, bad until":  {[]string{"--since", "2026-11-01", "--until", "30-11-2026"}, []string{"until"}},
		"since after until": {[]string{"--since", "2026-11-03", "--until", "2026-11-02"}, []string{"since", "until"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr, err := env.run(t, append([]string{"metrics", "history", "--history", existing}, c.args...)...)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("R14: %v: err %v exit %d, want 2", c.args, err, ExitCode(err))
			}
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "import options") || strings.Contains(strings.ToLower(stderr), "import options") {
				t.Fatalf("R20: %v: error %q / stderr %q mentions import options", c.args, err, stderr)
			}
			if !slices.ContainsFunc(c.names, func(n string) bool { return strings.Contains(msg, n) }) {
				t.Fatalf("R20: %v: error %q names none of %q", c.args, err, c.names)
			}
		})
	}
}
