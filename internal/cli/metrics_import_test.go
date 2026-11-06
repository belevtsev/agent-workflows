package cli

// Contract contract-usage-import revision 1. Every expected value below is
// computed by hand from the fixture tokens written in this file; see the
// comments next to each assertion.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/usagelog"
)

const (
	opusModel   = "claude-opus-5-5"
	sonnetModel = "claude-sonnet-5"
	haikuModel  = "claude-haiku-5"
	sessionID   = "s1"
)

// usageSpec is one Claude request's usage. w5/w1 are the 5m/1h cache writes.
type usageSpec struct {
	in, w5, w1, read, out int64
	thinking              int64
	hasThinking           bool
}

func claudeAssistant(t *testing.T, session, requestID, messageID, model string, usage *usageSpec) string {
	t.Helper()
	message := map[string]any{"id": messageID, "model": model, "role": "assistant", "content": []any{}}
	if usage != nil {
		u := map[string]any{
			"input_tokens":                usage.in,
			"cache_creation_input_tokens": usage.w5 + usage.w1,
			"cache_read_input_tokens":     usage.read,
			"output_tokens":               usage.out,
			"cache_creation":              map[string]any{"ephemeral_5m_input_tokens": usage.w5, "ephemeral_1h_input_tokens": usage.w1},
		}
		if usage.hasThinking {
			u["output_tokens_details"] = map[string]any{"thinking_tokens": usage.thinking}
		}
		message["usage"] = u
	}
	line := map[string]any{"type": "assistant", "sessionId": session, "message": message}
	if requestID != "" {
		line["requestId"] = requestID
	}
	return mustJSON(t, line)
}

type recordedSpec struct {
	in, out, think, read, w5, w1 int64
	cost                         float64
}

func claudeCostState(t *testing.T, total float64, unknown bool, models map[string]recordedSpec) string {
	t.Helper()
	usage := map[string]any{}
	for model, r := range models {
		usage[model] = map[string]any{
			"inputTokens": r.in, "outputTokens": r.out, "thinkingTokens": r.think,
			"cacheReadInputTokens": r.read, "cacheCreationInputTokens": r.w5 + r.w1,
			"cacheCreationEphemeral5mInputTokens": r.w5, "cacheCreationEphemeral1hInputTokens": r.w1,
			"webSearchRequests": 0, "costUSD": r.cost,
		}
	}
	return mustJSON(t, map[string]any{
		"type": "cost-state", "sessionId": sessionID, "totalCostUSD": total,
		"hasUnknownModelCost": unknown, "modelUsage": usage,
	})
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Recorded token sets that equal the transcript attribution per model
// (see the per-model sums in writeClaudeSession).
var (
	opusRecordedEqual   = recordedSpec{in: 12, out: 26, think: 5, read: 340, w5: 22, w1: 20, cost: 1.0}
	sonnetRecordedEqual = recordedSpec{in: 4, out: 12, think: 4, read: 60, w5: 0, w1: 6, cost: 0.2}
	haikuRecordedOnly   = recordedSpec{in: 1000, out: 100, cost: 0.05}
)

// writeClaudeSession writes the main transcript <root>/project/s1.jsonl and two
// subagents. The first cost-state (if any) is placed early, the rest at the end.
//
// Main distinct requests (opus unless noted):
//
//	req_1           in 2  w5 10 w1 20 read 100 out 7  thinking 3   (line repeated: 1 duplicate)
//	req_2           in 5  w5 0  w1 0  read 200 out 11 (no thinking)
//	msg_x (no rid)  in 1  w5 4  w1 0  read 0   out 2  (no thinking) (line repeated: 1 duplicate)
//	msg_y (no rid)  sonnet in 3 w5 0 w1 6 read 50 out 9 thinking 4
//
// Skipped: 1 synthetic, 1 without usage, 1 other session, 1 malformed line.
// Main: requests 4, uncached 11, w5 14, w1 26, read 350, out 29, thinking 7 (2 reported), total input 401.
// agent-a1 (opus, meta): in 4 w5 8 w1 0 read 40 out 6 thinking 2 -> total input 52.
// agent-b2 (sonnet, no meta): in 1 read 10 out 3, no thinking -> total input 11.
// Session: requests 6, uncached 16, w5 22, w1 26, read 400, out 38, thinking 9, total input 464.
// Per model: opus requests 4, uncached 12, w5 22, w1 20, read 340, out 26, thinking 5;
// sonnet requests 2, uncached 4, w5 0, w1 6, read 60, out 12, thinking 4.
func writeClaudeSession(t *testing.T, costStates ...string) (root, main string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "project")
	main = filepath.Join(dir, sessionID+".jsonl")
	req1 := claudeAssistant(t, sessionID, "req_1", "msg_1", opusModel, &usageSpec{in: 2, w5: 10, w1: 20, read: 100, out: 7, thinking: 3, hasThinking: true})
	msgX := claudeAssistant(t, sessionID, "", "msg_x", opusModel, &usageSpec{in: 1, w5: 4, out: 2})
	lines := []string{
		mustJSON(t, map[string]any{"type": "user", "sessionId": sessionID, "message": map[string]any{"role": "user", "content": "hello"}}),
		req1,
	}
	if len(costStates) > 0 {
		lines = append(lines, costStates[0])
	}
	lines = append(lines,
		req1,
		claudeAssistant(t, sessionID, "req_2", "msg_2", opusModel, &usageSpec{in: 5, read: 200, out: 11}),
		msgX,
		msgX,
		claudeAssistant(t, sessionID, "", "msg_y", sonnetModel, &usageSpec{in: 3, w1: 6, read: 50, out: 9, thinking: 4, hasThinking: true}),
		claudeAssistant(t, sessionID, "req_syn", "msg_syn", "<synthetic>", &usageSpec{in: 1000, out: 1000}),
		claudeAssistant(t, sessionID, "req_nu", "msg_nu", opusModel, nil),
		claudeAssistant(t, "other-session", "req_other", "msg_other", opusModel, &usageSpec{in: 7000, out: 7000}),
		`{"type":"assistant","sessionId":"s1","requestId":"req_bad",`,
	)
	if len(costStates) > 1 {
		lines = append(lines, costStates[1:]...)
	}
	writeLines(t, main, lines...)
	subagents := filepath.Join(dir, sessionID, "subagents")
	// Written in reverse name order so sorting by file name is observable.
	writeLines(t, filepath.Join(subagents, "agent-b2.jsonl"),
		claudeAssistant(t, sessionID, "req_b1", "msg_b1", sonnetModel, &usageSpec{in: 1, read: 10, out: 3}))
	writeLines(t, filepath.Join(subagents, "agent-a1.jsonl"),
		claudeAssistant(t, sessionID, "req_a1", "msg_a1", opusModel, &usageSpec{in: 4, w5: 8, read: 40, out: 6, thinking: 2, hasThinking: true}))
	if err := os.WriteFile(filepath.Join(subagents, "agent-a1.meta.json"),
		[]byte(`{"agentType":"general-purpose","description":"Review the fix","model":"opus","effort":"high"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, main
}

// rates: input, output, cache_write_5m, cache_write_1h, cache_read (per million).
type rates [5]float64

func writePrices(t *testing.T, models map[string]rates) string {
	t.Helper()
	table := map[string]any{}
	for model, r := range models {
		table[model] = map[string]any{
			"input_per_million": r[0], "output_per_million": r[1],
			"cache_write_5m_per_million": r[2], "cache_write_1h_per_million": r[3], "cache_read_per_million": r[4],
		}
	}
	path := filepath.Join(t.TempDir(), "prices.json")
	data := mustJSON(t, map[string]any{"currency": "USD", "evidence": "local:test-price-table", "models": table})
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var (
	opusRates   = rates{4, 20, 5, 8, 0.2}
	sonnetRates = rates{2, 10, 2.5, 4, 0.1}
	haikuRates  = rates{1, 5, 1.25, 2, 0.1}
)

func executeImport(t *testing.T, args ...string) (string, error) {
	t.Helper()
	config, out, _, _ := testConfig(t)
	config.Err = new(bytes.Buffer)
	err := Execute(t.Context(), append([]string{"metrics", "import"}, args...), config)
	return out.String(), err
}

func runImport(t *testing.T, args ...string) (usagelog.Report, map[string]any) {
	t.Helper()
	out, err := executeImport(t, args...)
	if err != nil {
		t.Fatalf("import %v failed: %v", args, err)
	}
	var report usagelog.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("stdout is not one report: %v\n%s", err, out)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	return report, raw
}

func object(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T (%v), want object", path, value, value)
	}
	return m
}

func nonEmptyString(t *testing.T, m map[string]any, key, path string) {
	t.Helper()
	if s, _ := m[key].(string); strings.TrimSpace(s) == "" {
		t.Fatalf("%s.%s = %v, want a non-empty reason", path, key, m[key])
	}
}

func isNullMember(m map[string]any, key string) bool {
	value, ok := m[key]
	return ok && value == nil
}

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-12 {
		t.Fatalf("%s = %v, want %.12g", name, deref(got), want)
	}
}

func deref(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func checkTokens(t *testing.T, name string, got usagelog.Tokens, requests, uncached, w5, w1, read, out, totalInput int64) {
	t.Helper()
	if got.Requests != requests || got.UncachedInputTokens != uncached || got.CacheWrite5mTokens != w5 ||
		got.CacheWrite1hTokens != w1 || got.CacheReadTokens != read || got.OutputTokens != out || got.TotalInputTokens != totalInput {
		t.Fatalf("%s tokens = %+v, want requests %d uncached %d w5 %d w1 %d read %d out %d total_input %d",
			name, got, requests, uncached, w5, w1, read, out, totalInput)
	}
}

func checkThinking(t *testing.T, name string, got usagelog.Tokens, thinking *int64, reported int64) {
	t.Helper()
	if (got.ThinkingTokens == nil) != (thinking == nil) || (thinking != nil && *got.ThinkingTokens != *thinking) || got.ThinkingReportedRequests != reported {
		t.Fatalf("%s thinking = %v (%d reported), want %v (%d reported)", name, got.ThinkingTokens, got.ThinkingReportedRequests, thinking, reported)
	}
}

func ptr(value int64) *int64 { return &value }

func modelUsage(t *testing.T, models []usagelog.ModelUsage, model string) usagelog.ModelUsage {
	t.Helper()
	for _, m := range models {
		if m.Model == model {
			return m
		}
	}
	t.Fatalf("model %s missing from %+v", model, models)
	return usagelog.ModelUsage{}
}

func reconciliation(t *testing.T, recorded *usagelog.RecordedDollars, model string) usagelog.Reconciliation {
	t.Helper()
	for _, r := range recorded.Reconciliation {
		if r.Model == model {
			return r
		}
	}
	t.Fatalf("no reconciliation for %s in %+v", model, recorded.Reconciliation)
	return usagelog.Reconciliation{}
}

// IMP-CLI: source selection and positional arguments are usage errors.
func TestMetricsImportUsageErrors(t *testing.T) {
	_, main := writeClaudeSession(t)
	codex := filepath.Join(t.TempDir(), "sessions", "2026", "11", "06", "rollout-root.jsonl")
	writeLines(t, codex, `{"type":"session_meta","payload":{"id":"T1","session_id":"T1","cwd":"/x"}}`)
	for name, args := range map[string][]string{
		"neither source":                 {},
		"both sources":                   {"--claude-transcript", main, "--codex-session", codex},
		"positional argument":            {"--claude-transcript", main, "extra"},
		"sessions root without codex":    {"--claude-transcript", main, "--codex-sessions-root", filepath.Dir(codex)},
		"sessions root alone":            {"--codex-sessions-root", filepath.Dir(codex)},
		"positional argument with codex": {"--codex-session", codex, "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := executeImport(t, args...)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("%v: err %v exit %d, want usage error (2)", args, err, ExitCode(err))
			}
			if strings.Contains(out, usagelog.ReportKind) {
				t.Fatalf("usage error still printed a report: %s", out)
			}
		})
	}
}

// IMP-CLI: a missing source or an invalid price table fails with exit 1 and no report.
func TestMetricsImportRuntimeFailuresHaveNoReport(t *testing.T) {
	_, main := writeClaudeSession(t)
	pricesDir := t.TempDir()
	writePrice := func(name, body string) string {
		path := filepath.Join(pricesDir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	unknown := writePrice("unknown.json", `{"currency":"USD","evidence":"local:x","discount":1,"models":{"claude-opus-5-5":{"input_per_million":4,"output_per_million":20}}}`)
	unknownRate := writePrice("unknown-rate.json", `{"currency":"USD","evidence":"local:x","models":{"claude-opus-5-5":{"input_per_million":4,"output_per_million":20,"web_search_per_request":1}}}`)
	negative := writePrice("negative.json", `{"currency":"USD","evidence":"local:x","models":{"claude-opus-5-5":{"input_per_million":-1,"output_per_million":20}}}`)
	blank := writePrice("blank.json", `{"currency":"USD","evidence":"   ","models":{"claude-opus-5-5":{"input_per_million":4,"output_per_million":20}}}`)
	for name, args := range map[string][]string{
		"missing claude transcript": {"--claude-transcript", filepath.Join(t.TempDir(), "absent.jsonl")},
		"missing codex rollout":     {"--codex-session", filepath.Join(t.TempDir(), "sessions", "rollout-absent.jsonl")},
		"unknown price member":      {"--claude-transcript", main, "--prices", unknown},
		"unknown rate member":       {"--claude-transcript", main, "--prices", unknownRate},
		"negative rate":             {"--claude-transcript", main, "--prices", negative},
		"blank evidence":            {"--claude-transcript", main, "--prices", blank},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := executeImport(t, args...)
			if err == nil || ExitCode(err) != 1 {
				t.Fatalf("%v: err %v exit %d, want 1", args, err, ExitCode(err))
			}
			if strings.Contains(out, usagelog.ReportKind) {
				t.Fatalf("failure still printed a report: %s", out)
			}
		})
	}
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := fmt.Sprintf("%v|%d|%d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			state += "|" + hex.EncodeToString(sum[:])
		}
		tree[path] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// IMP-CLI: the command never writes files.
func TestMetricsImportLeavesInputsUnchanged(t *testing.T) {
	root, main := writeClaudeSession(t, claudeCostState(t, 1.2, false, map[string]recordedSpec{opusModel: opusRecordedEqual, sonnetModel: sonnetRecordedEqual}))
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	codexRoot := t.TempDir()
	writeCodexTree(t, filepath.Join(codexRoot, "sessions"))
	before, pricesBefore, codexBefore := snapshotTree(t, root), snapshotTree(t, filepath.Dir(prices)), snapshotTree(t, codexRoot)
	runImport(t, "--claude-transcript", main, "--prices", prices)
	runImport(t, "--codex-session", filepath.Join(codexRoot, "sessions", "2026", "11", "06", "rollout-root.jsonl"))
	for name, pair := range map[string][2]map[string]string{
		"claude": {before, snapshotTree(t, root)}, "prices": {pricesBefore, snapshotTree(t, filepath.Dir(prices))}, "codex": {codexBefore, snapshotTree(t, codexRoot)},
	} {
		if !mapsEqual(pair[0], pair[1]) {
			t.Fatalf("%s inputs changed:\nbefore %v\nafter  %v", name, pair[0], pair[1])
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}

// IMP-DEDUP, IMP-AGENTS, IMP-NOCOST, IMP-USAGE (no cost-state, no prices).
func TestMetricsImportClaudeDedupAgentsAndNoCost(t *testing.T) {
	_, main := writeClaudeSession(t)
	report, raw := runImport(t, "--claude-transcript", main)
	if report.Kind != usagelog.ReportKind || report.Source != usagelog.SourceClaude || report.SessionID != sessionID {
		t.Fatalf("wrong report identity: kind %q source %q session %q", report.Kind, report.Source, report.SessionID)
	}
	// IMP-DEDUP: skip kinds.
	s := report.Skipped
	if s.MalformedLines != 1 || s.SyntheticEntries != 1 || s.NoUsageEntries != 1 || s.DuplicateEntries != 2 || s.OtherSessionEntries != 1 {
		t.Fatalf("skipped = %+v, want malformed 1, synthetic 1, no_usage 1, duplicates 2 (req_1, msg_x), other_session 1", s)
	}
	// IMP-DEDUP / IMP-AGENTS: session totals.
	checkTokens(t, "totals", report.Totals, 6, 16, 22, 26, 400, 38, 464)
	checkThinking(t, "totals", report.Totals, ptr(9), 3)

	// IMP-AGENTS: order, roles, meta and per-agent counters.
	if len(report.Agents) != 3 {
		t.Fatalf("agents = %d, want main + 2 subagents: %+v", len(report.Agents), report.Agents)
	}
	mainAgent, a1, b2 := report.Agents[0], report.Agents[1], report.Agents[2]
	if mainAgent.Role != "main" || a1.Role != "subagent" || b2.Role != "subagent" {
		t.Fatalf("roles = %q %q %q", mainAgent.Role, a1.Role, b2.Role)
	}
	checkTokens(t, "main", mainAgent.Tokens, 4, 11, 14, 26, 350, 29, 401)
	checkThinking(t, "main", mainAgent.Tokens, ptr(7), 2)
	checkTokens(t, "agent-a1", a1.Tokens, 1, 4, 8, 0, 40, 6, 52)
	checkThinking(t, "agent-a1", a1.Tokens, ptr(2), 1)
	checkTokens(t, "agent-b2", b2.Tokens, 1, 1, 0, 0, 10, 3, 11)
	checkThinking(t, "agent-b2", b2.Tokens, nil, 0)
	if a1.Description != "Review the fix" || a1.AgentType != "general-purpose" || a1.ConfiguredModel != "opus" || a1.ConfiguredEffort != "high" {
		t.Fatalf("agent-a1 meta = %+v", a1)
	}
	if b2.Description != "" || b2.AgentType != "" || b2.ConfiguredModel != "" || b2.ConfiguredEffort != "" {
		t.Fatalf("agent-b2 has invented meta: %+v", b2)
	}
	agents := raw["agents"].([]any)
	if b2Raw := object(t, agents[2], "agents[2]"); !isNullMember(b2Raw, "thinking_tokens") {
		t.Fatalf("agent-b2 thinking_tokens = %v, want JSON null", b2Raw["thinking_tokens"])
	}

	// IMP-AGENTS: per-model session totals.
	opus, sonnet := modelUsage(t, report.Models, opusModel), modelUsage(t, report.Models, sonnetModel)
	checkTokens(t, "model opus", opus.Tokens, 4, 12, 22, 20, 340, 26, 394)
	checkTokens(t, "model sonnet", sonnet.Tokens, 2, 4, 0, 6, 60, 12, 70)
	if len(report.Models) != 2 {
		t.Fatalf("models = %+v, want only opus and sonnet (synthetic/other-session excluded)", report.Models)
	}

	// IMP-NOCOST.
	dollars := object(t, raw["dollars"], "dollars")
	if !isNullMember(dollars, "recorded") || !isNullMember(dollars, "price_table") {
		t.Fatalf("dollars = %v, want recorded and price_table null", dollars)
	}
	nonEmptyString(t, dollars, "recorded_unknown_reason", "dollars")
	nonEmptyString(t, dollars, "price_table_unknown_reason", "dollars")
	if _, ok := raw["cost"]; ok {
		t.Fatalf("cost = %v, want absent without cost-state", raw["cost"])
	}
	for i, agent := range agents {
		amount := object(t, object(t, agent, "agent")["dollars"], fmt.Sprintf("agents[%d].dollars", i))
		if !isNullMember(amount, "amount") {
			t.Fatalf("agents[%d].dollars.amount = %v, want null", i, amount["amount"])
		}
		nonEmptyString(t, amount, "unknown_reason", fmt.Sprintf("agents[%d].dollars", i))
	}

	// IMP-USAGE: whole-session counters.
	u := report.Usage
	if u == nil || u.InputTokens == nil || *u.InputTokens != 464 || u.OutputTokens == nil || *u.OutputTokens != 38 ||
		u.UncachedInputTokens == nil || *u.UncachedInputTokens != 16 || u.CacheWrite5mTokens == nil || *u.CacheWrite5mTokens != 22 ||
		u.CacheWrite1hTokens == nil || *u.CacheWrite1hTokens != 26 || u.CacheReadTokens == nil || *u.CacheReadTokens != 400 {
		t.Fatalf("usage = %+v, want input 464 output 38 uncached 16 w5 22 w1 26 read 400", u)
	}
}

// IMP-RECORDED, IMP-USAGE: recorded total is the last cost-state; cost is
// present for a complete, fully attributed session without unknown model cost.
func TestMetricsImportRecordedCostState(t *testing.T) {
	first := claudeCostState(t, 0.5, false, map[string]recordedSpec{opusModel: {in: 2, out: 7, think: 3, read: 100, w5: 10, w1: 20, cost: 0.5}})
	last := claudeCostState(t, 1.2, false, map[string]recordedSpec{opusModel: opusRecordedEqual, sonnetModel: sonnetRecordedEqual})
	_, main := writeClaudeSession(t, first, last)
	report, raw := runImport(t, "--claude-transcript", main)
	recorded := report.Dollars.Recorded
	if recorded == nil {
		t.Fatalf("dollars.recorded = null: %v", raw["dollars"])
	}
	if math.Abs(recorded.Total-1.2) > 1e-12 {
		t.Fatalf("recorded total = %v, want the last cost-state 1.2", recorded.Total)
	}
	for model, want := range map[string]float64{opusModel: 1.0, sonnetModel: 0.2} {
		found := false
		for _, m := range recorded.Models {
			if m.Model == model {
				found = true
				near(t, "recorded cost "+model, m.Cost, want)
			}
		}
		if !found {
			t.Fatalf("recorded models %+v lack %s", recorded.Models, model)
		}
	}
	for _, model := range []string{opusModel, sonnetModel} {
		if r := reconciliation(t, recorded, model); r.Status != "equal" {
			t.Fatalf("reconciliation %s = %+v, want equal", model, r)
		}
	}
	if len(recorded.UnattributedModels) != 0 {
		t.Fatalf("unattributed models = %v, want none", recorded.UnattributedModels)
	}
	// Recorded cost is never allocated to agents.
	for i, agent := range raw["agents"].([]any) {
		amount := object(t, object(t, agent, "agent")["dollars"], "dollars")
		if !isNullMember(amount, "amount") {
			t.Fatalf("agents[%d] received an allocated recorded amount: %v", i, amount)
		}
		nonEmptyString(t, amount, "unknown_reason", fmt.Sprintf("agents[%d].dollars", i))
	}
	// IMP-USAGE.
	if report.Usage == nil || report.Usage.APIRequests == nil || *report.Usage.APIRequests != 6 {
		t.Fatalf("usage = %+v, want api_requests 6", report.Usage)
	}
	if report.Cost == nil || report.Cost.Currency != "USD" || strings.TrimSpace(report.Cost.Evidence) == "" {
		t.Fatalf("cost = %+v, want USD recorded amount with evidence", report.Cost)
	}
	near(t, "cost.amount", report.Cost.Amount, 1.2)
	if report.Cost.Rates != nil {
		t.Fatalf("cost carries rates: %+v", report.Cost.Rates)
	}
}

// IMP-USAGE: hasUnknownModelCost suppresses cost; --prices never creates it.
func TestMetricsImportCostAbsentWhenUnknownOrPriced(t *testing.T) {
	_, unknownMain := writeClaudeSession(t, claudeCostState(t, 1.2, true, map[string]recordedSpec{opusModel: opusRecordedEqual, sonnetModel: sonnetRecordedEqual}))
	if _, raw := runImport(t, "--claude-transcript", unknownMain); raw["cost"] != nil {
		t.Fatalf("cost = %v, want absent with hasUnknownModelCost", raw["cost"])
	}
	_, plainMain := writeClaudeSession(t)
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	if _, raw := runImport(t, "--claude-transcript", plainMain, "--prices", prices); raw["cost"] != nil {
		t.Fatalf("cost = %v, want absent without cost-state even with --prices", raw["cost"])
	}
}

// IMP-RECORDED / IMP-USAGE: a differing model and a recorded-only model.
func TestMetricsImportRecordedReconciliationAndUnattributed(t *testing.T) {
	sonnetDiffers := sonnetRecordedEqual
	sonnetDiffers.out = 13 // transcript 12 -> transcript minus recorded = -1
	_, main := writeClaudeSession(t, claudeCostState(t, 1.25, false, map[string]recordedSpec{
		opusModel: opusRecordedEqual, sonnetModel: sonnetDiffers, haikuModel: haikuRecordedOnly,
	}))
	report, raw := runImport(t, "--claude-transcript", main)
	recorded := report.Dollars.Recorded
	if recorded == nil {
		t.Fatal("dollars.recorded = null")
	}
	if math.Abs(recorded.Total-1.25) > 1e-12 {
		t.Fatalf("recorded total = %v, want 1.25", recorded.Total)
	}
	if r := reconciliation(t, recorded, opusModel); r.Status != "equal" {
		t.Fatalf("opus reconciliation = %+v, want equal", r)
	}
	r := reconciliation(t, recorded, sonnetModel)
	var nonzero []int64
	for _, delta := range r.TranscriptMinusRecorded {
		if delta != 0 {
			nonzero = append(nonzero, delta)
		}
	}
	if r.Status != "differs" || len(nonzero) == 0 || slices.ContainsFunc(nonzero, func(d int64) bool { return d != -1 }) {
		t.Fatalf("sonnet reconciliation = %+v, want differs with output delta -1 only", r)
	}
	if r := reconciliation(t, recorded, haikuModel); r.Status != "recorded_only" {
		t.Fatalf("haiku reconciliation = %+v, want recorded_only", r)
	}
	if !slices.Equal(recorded.UnattributedModels, []string{haikuModel}) {
		t.Fatalf("unattributed models = %v, want [%s]", recorded.UnattributedModels, haikuModel)
	}
	near(t, "unattributed_total", recorded.UnattributedTotal, 0.05)
	// r2 IMP-CONSISTENCY: a differing model suppresses usage and cost; the
	// per-agent and per-model numbers are still reported.
	requireNoObservation(t, raw)
	checkTokens(t, "totals", report.Totals, 6, 16, 22, 26, 400, 38, 464)
	checkTokens(t, "model sonnet", modelUsage(t, report.Models, sonnetModel).Tokens, 2, 4, 0, 6, 60, 12, 70)
}

// requireNoObservation: usage is JSON null with a reason and there is no cost.
func requireNoObservation(t *testing.T, raw map[string]any) {
	t.Helper()
	if !isNullMember(raw, "usage") {
		t.Fatalf("usage = %v, want JSON null", raw["usage"])
	}
	nonEmptyString(t, raw, "usage_unknown_reason", "report")
	if _, ok := raw["cost"]; ok {
		t.Fatalf("cost = %v, want absent", raw["cost"])
	}
}

// IMP-USAGE (r1, kept by r2): with every transcript model equal and one
// recorded-only model, usage is reported with api_requests null.
func TestMetricsImportRecordedOnlyModelKeepsUsageWithoutRequests(t *testing.T) {
	_, main := writeClaudeSession(t, claudeCostState(t, 1.25, false, map[string]recordedSpec{
		opusModel: opusRecordedEqual, sonnetModel: sonnetRecordedEqual, haikuModel: haikuRecordedOnly,
	}))
	report, raw := runImport(t, "--claude-transcript", main)
	if r := reconciliation(t, report.Dollars.Recorded, haikuModel); r.Status != "recorded_only" {
		t.Fatalf("haiku reconciliation = %+v, want recorded_only", r)
	}
	usage := object(t, raw["usage"], "usage")
	if usage["api_requests"] != nil {
		t.Fatalf("usage.api_requests = %v, want null with a recorded-only model", usage["api_requests"])
	}
	// Transcript (input 464, uncached 16, output 38) plus haiku's recorded
	// counters (uncached 1000, writes 0, read 0, output 100).
	if u := report.Usage; u.InputTokens == nil || *u.InputTokens != 1464 || u.UncachedInputTokens == nil || *u.UncachedInputTokens != 1016 ||
		u.OutputTokens == nil || *u.OutputTokens != 138 {
		t.Fatalf("usage = %+v, want input_tokens 1464, uncached 1016, output 138", u)
	}
}

// IMP-CONSISTENCY: a transcript model absent from cost-state is transcript_only,
// which suppresses usage and cost while dollars.recorded stays reported.
func TestMetricsImportTranscriptOnlyModelSuppressesObservation(t *testing.T) {
	_, main := writeClaudeSession(t, claudeCostState(t, 1.0, false, map[string]recordedSpec{opusModel: opusRecordedEqual}))
	report, raw := runImport(t, "--claude-transcript", main)
	recorded := report.Dollars.Recorded
	if recorded == nil || math.Abs(recorded.Total-1.0) > 1e-12 {
		t.Fatalf("dollars.recorded = %+v, want total 1.0", recorded)
	}
	if r := reconciliation(t, recorded, opusModel); r.Status != "equal" {
		t.Fatalf("opus reconciliation = %+v, want equal", r)
	}
	if r := reconciliation(t, recorded, sonnetModel); r.Status != "transcript_only" {
		t.Fatalf("sonnet reconciliation = %+v, want transcript_only", r)
	}
	requireNoObservation(t, raw)
	checkTokens(t, "totals", report.Totals, 6, 16, 22, 26, 400, 38, 464)
	checkTokens(t, "main", report.Agents[0].Tokens, 4, 11, 14, 26, 350, 29, 401)
	checkTokens(t, "model opus", modelUsage(t, report.Models, opusModel).Tokens, 4, 12, 22, 20, 340, 26, 394)
}

// IMP-CONSISTENCY: cost-state recording more tokens than the transcript (as
// when the transcript lost lines) is differs and suppresses usage and cost.
func TestMetricsImportRecordedAboveTranscriptSuppressesObservation(t *testing.T) {
	opusMore := opusRecordedEqual
	opusMore.in, opusMore.read = 20, 500 // transcript 12 and 340
	_, main := writeClaudeSession(t, claudeCostState(t, 1.2, false, map[string]recordedSpec{opusModel: opusMore, sonnetModel: sonnetRecordedEqual}))
	report, raw := runImport(t, "--claude-transcript", main)
	if r := reconciliation(t, report.Dollars.Recorded, opusModel); r.Status != "differs" {
		t.Fatalf("opus reconciliation = %+v, want differs", r)
	}
	requireNoObservation(t, raw)
	checkTokens(t, "totals", report.Totals, 6, 16, 22, 26, 400, 38, 464)
}

// IMP-SESSION-MISMATCH: a file whose entries all carry another session id fails.
func TestMetricsImportSessionMismatchFails(t *testing.T) {
	_, main := writeClaudeSession(t)
	data, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(t.TempDir(), "renamed-copy.jsonl")
	if err := os.WriteFile(renamed, data, 0o600); err != nil {
		t.Fatal(err)
	}
	subagent := filepath.Join(filepath.Dir(main), sessionID, "subagents", "agent-a1.jsonl")
	for name, path := range map[string]string{"renamed copy": renamed, "subagent passed directly": subagent} {
		t.Run(name, func(t *testing.T) {
			out, err := executeImport(t, "--claude-transcript", path)
			if err == nil || ExitCode(err) != 1 {
				t.Fatalf("err %v exit %d, want 1", err, ExitCode(err))
			}
			if strings.Contains(out, usagelog.ReportKind) {
				t.Fatalf("session mismatch printed a report: %s", out)
			}
		})
	}
}

// IMP-UNREADABLE: a subagent path that is not a regular file is not counted
// and makes the import incomplete.
func TestMetricsImportClaudeIrregularSubagentFile(t *testing.T) {
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			_, main := writeClaudeSession(t)
			path := filepath.Join(filepath.Dir(main), sessionID, "subagents", "agent-c3.jsonl")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "outside.jsonl")
				writeLines(t, target, claudeAssistant(t, sessionID, "req_c1", "msg_c1", opusModel, &usageSpec{in: 500, out: 500}))
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			report, _ := runImport(t, "--claude-transcript", main)
			if report.Status != usagelog.StatusIncomplete || !slices.Contains(report.IncompleteReasons, "unreadable_file") {
				t.Fatalf("status %q reasons %v, want incomplete with unreadable_file", report.Status, report.IncompleteReasons)
			}
			// Base fixture totals: the irregular path contributes nothing.
			checkTokens(t, "totals", report.Totals, 6, 16, 22, 26, 400, 38, 464)
		})
	}
}

// IMP-UNREADABLE: an unreadable Codex rollout under the scanned root.
func TestMetricsImportCodexUnreadableRollout(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file mode 0000 is readable by root")
	}
	sessions := filepath.Join(t.TempDir(), "sessions")
	rootPath := writeCodexTree(t, sessions)
	locked := filepath.Join(sessions, "2026", "11", "07", "rollout-locked.jsonl")
	writeLines(t, locked, `{"type":"session_meta","payload":{"id":"T20","session_id":"T20","cwd":"/x"}}`)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	report, _ := runImport(t, "--codex-session", rootPath)
	if report.Status != usagelog.StatusIncomplete || !slices.Contains(report.IncompleteReasons, "unreadable_file") {
		t.Fatalf("status %q reasons %v, want incomplete with unreadable_file", report.Status, report.IncompleteReasons)
	}
}

// IMP-CODEX-SYMLINK: a symlinked sessions root is scanned like the real one.
func TestMetricsImportCodexSymlinkedSessionsRoot(t *testing.T) {
	t.Run("derived sessions ancestor is a symlink", func(t *testing.T) {
		base := t.TempDir()
		store := filepath.Join(base, "store")
		writeCodexTree(t, store)
		link := filepath.Join(base, "sessions")
		if err := os.Symlink(store, link); err != nil {
			t.Fatal(err)
		}
		report, _ := runImport(t, "--codex-session", filepath.Join(link, "2026", "11", "06", "rollout-root.jsonl"))
		checkCodexAgents(t, report)
		if slices.Contains(report.IncompleteReasons, "root_not_in_sessions_root") || slices.Contains(report.IncompleteReasons, "child_discovery_not_attempted") {
			t.Fatalf("symlinked root not scanned as the real one: %v", report.IncompleteReasons)
		}
	})
	t.Run("explicit sessions root is a symlink", func(t *testing.T) {
		base := t.TempDir()
		archive := filepath.Join(base, "archive")
		rootPath := writeCodexTree(t, archive)
		link := filepath.Join(base, "archive-link")
		if err := os.Symlink(archive, link); err != nil {
			t.Fatal(err)
		}
		report, _ := runImport(t, "--codex-session", rootPath, "--codex-sessions-root", link)
		checkCodexAgents(t, report)
		if slices.Contains(report.IncompleteReasons, "root_not_in_sessions_root") {
			t.Fatalf("symlinked root not scanned as the real one: %v", report.IncompleteReasons)
		}
	})
}

// IMP-CODEX-SYMLINK: a scanned root that does not contain the root rollout's session.
func TestMetricsImportCodexRootOutsideSessionsRoot(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	rootPath := writeCodexTree(t, archive)
	// 2026/11/07 holds only the grandchild and an unrelated child, not T1.
	report, _ := runImport(t, "--codex-session", rootPath, "--codex-sessions-root", filepath.Join(archive, "2026", "11", "07"))
	if report.Status != usagelog.StatusIncomplete || !slices.Contains(report.IncompleteReasons, "root_not_in_sessions_root") {
		t.Fatalf("status %q reasons %v, want incomplete with root_not_in_sessions_root", report.Status, report.IncompleteReasons)
	}
}

// IMP-PRICED: price-table amounts per agent and per session, separate from recorded.
//
// Rates per million (input, output, w5, w1, read): opus 4/20/5/8/0.2, sonnet 2/10/2.5/4/0.1, haiku 1/5/1.25/2/0.1.
// main  opus:   8*4 + 14*5 + 20*8 + 300*0.2 + 20*20 = 722; sonnet: 3*2 + 6*4 + 50*0.1 + 9*10 = 125 -> 847e-6
// a1    opus:   4*4 + 8*5 + 40*0.2 + 6*20 = 184 -> 184e-6
// b2    sonnet: 1*2 + 10*0.1 + 3*10 = 33 -> 33e-6
// total 1064e-6 (models: opus 906e-6, sonnet 158e-6); haiku recorded-only: 1000*1 + 100*5 = 1500e-6.
// recorded_minus_price_table = 1.25 - (1064e-6 + 1500e-6) = 1.247436.
func TestMetricsImportPriceTable(t *testing.T) {
	costState := claudeCostState(t, 1.25, false, map[string]recordedSpec{
		opusModel: opusRecordedEqual, sonnetModel: sonnetRecordedEqual, haikuModel: haikuRecordedOnly,
	})
	_, main := writeClaudeSession(t, costState)

	t.Run("all models priced", func(t *testing.T) {
		prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates, haikuModel: haikuRates})
		report, _ := runImport(t, "--claude-transcript", main, "--prices", prices)
		for i, want := range []float64{847e-6, 184e-6, 33e-6} {
			agent := report.Agents[i]
			near(t, fmt.Sprintf("agents[%d].dollars.amount", i), agent.Dollars.Amount, want)
			if agent.Dollars.Basis != usagelog.BasisPriceTable {
				t.Fatalf("agents[%d] basis = %q, want %q", i, agent.Dollars.Basis, usagelog.BasisPriceTable)
			}
		}
		table := report.Dollars.PriceTable
		if table == nil {
			t.Fatal("dollars.price_table = null")
		}
		near(t, "price_table.total", table.Total, 1064e-6)
		if len(table.UnattributedRecorded) != 1 || table.UnattributedRecorded[0].Model != haikuModel {
			t.Fatalf("unattributed_recorded = %+v, want haiku only", table.UnattributedRecorded)
		}
		near(t, "unattributed_recorded[haiku]", table.UnattributedRecorded[0].Amount.Amount, 1500e-6)
		near(t, "unattributed_recorded_total", table.UnattributedRecordedTotal, 1500e-6)
		near(t, "recorded_minus_price_table", report.Dollars.RecordedMinusPriceTable, 1.25-(1064e-6+1500e-6))
		if report.Dollars.Recorded == nil || math.Abs(report.Dollars.Recorded.Total-1.25) > 1e-12 {
			t.Fatalf("recorded dollars altered by --prices: %+v", report.Dollars.Recorded)
		}
		if report.Cost != nil && (report.Cost.Amount == nil || math.Abs(*report.Cost.Amount-1.25) > 1e-12) {
			t.Fatalf("cost derived from --prices: %+v", report.Cost)
		}
	})

	t.Run("recorded-only model missing from table", func(t *testing.T) {
		prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
		report, raw := runImport(t, "--claude-transcript", main, "--prices", prices)
		table := report.Dollars.PriceTable
		if table == nil {
			t.Fatal("dollars.price_table = null")
		}
		near(t, "price_table.total", table.Total, 1064e-6)
		if len(table.UnattributedRecorded) != 1 || table.UnattributedRecorded[0].Model != haikuModel ||
			table.UnattributedRecorded[0].Amount.Amount != nil || strings.TrimSpace(table.UnattributedRecorded[0].UnknownReason) == "" {
			t.Fatalf("unattributed_recorded = %+v, want haiku with null amount and a reason", table.UnattributedRecorded)
		}
		if table.UnattributedRecordedTotal != nil {
			t.Fatalf("unattributed_recorded_total = %v, want null", *table.UnattributedRecordedTotal)
		}
		dollars := object(t, raw["dollars"], "dollars")
		if !isNullMember(dollars, "recorded_minus_price_table") {
			t.Fatalf("recorded_minus_price_table = %v, want null", dollars["recorded_minus_price_table"])
		}
		nonEmptyString(t, dollars, "difference_unknown_reason", "dollars")
	})

	t.Run("agent model missing from table", func(t *testing.T) {
		prices := writePrices(t, map[string]rates{opusModel: opusRates, haikuModel: haikuRates})
		report, raw := runImport(t, "--claude-transcript", main, "--prices", prices)
		near(t, "agents[1] (opus only)", report.Agents[1].Dollars.Amount, 184e-6)
		agents := raw["agents"].([]any)
		for _, i := range []int{0, 2} { // main and b2 used sonnet
			amount := object(t, object(t, agents[i], "agent")["dollars"], "dollars")
			if !isNullMember(amount, "amount") {
				t.Fatalf("agents[%d].dollars.amount = %v, want null (never 0) for an unpriced model", i, amount["amount"])
			}
			nonEmptyString(t, amount, "unknown_reason", fmt.Sprintf("agents[%d].dollars", i))
		}
		table := object(t, object(t, raw["dollars"], "dollars")["price_table"], "dollars.price_table")
		if !isNullMember(table, "total") {
			t.Fatalf("price_table.total = %v, want null", table["total"])
		}
	})
}

// writeCodexTree writes a root rollout T1 with child T2 and grandchild T3,
// plus unrelated T9 and its child T10, beneath sessionsDir.
//
// T1: r1 (sol/max) input 100 cached 60 write 10 output 20 reasoning 5, repeated once and
// echoed by a token_count event; r2 (mini/low) input 50 output 8 reasoning 0.
// -> requests 2, uncached 30+50=80, read 60, writes 10, output 28, thinking 5, total input 150.
// T2: r3 (sol/high) input 40 cached 30 output 10 reasoning 2 -> uncached 10, read 30, total input 40.
// T3: r4 (sol/medium) input 20 write 5 output 4 reasoning 1 -> uncached 15, writes 5, total input 20.
// Session: requests 4, uncached 105, read 90, output 42, thinking 8, total input 210.
func writeCodexTree(t *testing.T, sessionsDir string) string {
	t.Helper()
	meta := func(id, session, parent string, extra map[string]any) string {
		payload := map[string]any{"id": id, "session_id": session, "cwd": "/x"}
		if parent != "" {
			payload["parent_thread_id"] = parent
		}
		for k, v := range extra {
			payload[k] = v
		}
		return mustJSON(t, map[string]any{"type": "session_meta", "payload": payload})
	}
	turn := func(model, effort string) string {
		return mustJSON(t, map[string]any{"type": "turn_context", "payload": map[string]any{"model": model, "effort": effort}})
	}
	record := func(response, thread, session string, input, cached, write, output, reasoning int64) string {
		return mustJSON(t, map[string]any{"type": "token_usage_record", "payload": map[string]any{
			"response_id": response, "thread_id": thread, "turn_id": "u-" + response, "session_id": session,
			"usage": map[string]any{
				"input_tokens": input, "cached_input_tokens": cached, "cache_write_input_tokens": write,
				"output_tokens": output, "reasoning_output_tokens": reasoning, "total_tokens": input + output,
			},
		}})
	}
	tokenCount := mustJSON(t, map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count", "info": map[string]any{
		"total_token_usage": map[string]any{"input_tokens": 100, "cached_input_tokens": 60, "output_tokens": 20, "reasoning_output_tokens": 5, "total_tokens": 120},
		"last_token_usage":  map[string]any{"input_tokens": 100, "cached_input_tokens": 60, "output_tokens": 20, "reasoning_output_tokens": 5, "total_tokens": 120},
	}}})
	rootPath := filepath.Join(sessionsDir, "2026", "11", "06", "rollout-root.jsonl")
	writeLines(t, rootPath,
		meta("T1", "T1", "", nil),
		turn("gpt-6.1-sol", "max"),
		record("r1", "T1", "T1", 100, 60, 10, 20, 5),
		tokenCount,
		record("r1", "T1", "T1", 100, 60, 10, 20, 5),
		turn("gpt-6.1-mini", "low"),
		record("r2", "T1", "T1", 50, 0, 0, 8, 0),
	)
	writeLines(t, filepath.Join(sessionsDir, "2026", "11", "06", "rollout-child.jsonl"),
		meta("T2", "T1", "T1", map[string]any{"agent_nickname": "scout", "agent_role": "explorer"}),
		turn("gpt-6.1-sol", "high"),
		record("r3", "T2", "T1", 40, 30, 0, 10, 2),
	)
	writeLines(t, filepath.Join(sessionsDir, "2026", "11", "07", "rollout-grandchild.jsonl"),
		meta("T3", "T1", "T2", nil),
		turn("gpt-6.1-sol", "medium"),
		record("r4", "T3", "T1", 20, 0, 5, 4, 1),
	)
	writeLines(t, filepath.Join(sessionsDir, "2026", "11", "06", "rollout-unrelated.jsonl"),
		meta("T9", "T9", "", nil),
		turn("gpt-6.1-sol", "max"),
		record("r9", "T9", "T9", 9999, 0, 0, 9999, 0),
	)
	writeLines(t, filepath.Join(sessionsDir, "2026", "11", "07", "rollout-unrelated-child.jsonl"),
		meta("T10", "T9", "T9", nil),
		turn("gpt-6.1-sol", "max"),
		record("r10", "T10", "T9", 8888, 0, 0, 8888, 0),
	)
	return rootPath
}

func agentAtDepth(t *testing.T, agents []usagelog.Agent, depth int) usagelog.Agent {
	t.Helper()
	var found []usagelog.Agent
	for _, agent := range agents {
		if agent.Depth == depth {
			found = append(found, agent)
		}
	}
	if len(found) != 1 {
		t.Fatalf("agents at depth %d = %+v, want exactly one", depth, found)
	}
	return found[0]
}

func sameSet(got, want []string) bool {
	a, b := slices.Clone(got), slices.Clone(want)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func checkCodexAgents(t *testing.T, report usagelog.Report) {
	t.Helper()
	if len(report.Agents) != 3 {
		t.Fatalf("agents = %d, want T1, T2, T3 only (unrelated excluded): %+v", len(report.Agents), report.Agents)
	}
	root, child, grandchild := agentAtDepth(t, report.Agents, 0), agentAtDepth(t, report.Agents, 1), agentAtDepth(t, report.Agents, 2)
	if root.ParentID != "" || child.ParentID != "T1" || grandchild.ParentID != "T2" {
		t.Fatalf("parents = %q %q %q, want \"\" T1 T2", root.ParentID, child.ParentID, grandchild.ParentID)
	}
	if child.Nickname != "scout" || child.AgentRole != "explorer" {
		t.Fatalf("child nickname/role = %q/%q", child.Nickname, child.AgentRole)
	}
	if root.Requests != 2 || root.UncachedInputTokens != 80 || root.CacheReadTokens != 60 || root.OutputTokens != 28 || root.TotalInputTokens != 150 {
		t.Fatalf("root tokens = %+v, want requests 2 uncached 80 read 60 output 28 total_input 150", root.Tokens)
	}
	if root.ThinkingTokens == nil || *root.ThinkingTokens != 5 {
		t.Fatalf("root thinking = %v, want 5 (reasoning)", root.ThinkingTokens)
	}
	if !sameSet(root.ModelsSeen, []string{"gpt-6.1-sol", "gpt-6.1-mini"}) || !sameSet(root.EffortsSeen, []string{"max", "low"}) {
		t.Fatalf("root models/efforts seen = %v / %v", root.ModelsSeen, root.EffortsSeen)
	}
	sol, mini := modelUsage(t, root.ByModel, "gpt-6.1-sol"), modelUsage(t, root.ByModel, "gpt-6.1-mini")
	if sol.Requests != 1 || sol.UncachedInputTokens != 30 || sol.CacheReadTokens != 60 || sol.OutputTokens != 20 || sol.TotalInputTokens != 100 ||
		mini.Requests != 1 || mini.UncachedInputTokens != 50 || mini.OutputTokens != 8 || mini.TotalInputTokens != 50 {
		t.Fatalf("root by_model = %+v / %+v, want sol (1, 30, read 60, out 20, in 100) mini (1, 50, out 8, in 50)", sol, mini)
	}
	if child.Requests != 1 || child.UncachedInputTokens != 10 || child.CacheReadTokens != 30 || child.OutputTokens != 10 || child.TotalInputTokens != 40 ||
		child.ThinkingTokens == nil || *child.ThinkingTokens != 2 || !sameSet(child.EffortsSeen, []string{"high"}) {
		t.Fatalf("child tokens = %+v efforts %v", child.Tokens, child.EffortsSeen)
	}
	if grandchild.Requests != 1 || grandchild.UncachedInputTokens != 15 || grandchild.CacheReadTokens != 0 || grandchild.OutputTokens != 4 ||
		grandchild.TotalInputTokens != 20 || grandchild.ThinkingTokens == nil || *grandchild.ThinkingTokens != 1 {
		t.Fatalf("grandchild tokens = %+v", grandchild.Tokens)
	}
	tt := report.Totals
	if tt.Requests != 4 || tt.UncachedInputTokens != 105 || tt.CacheReadTokens != 90 || tt.OutputTokens != 42 || tt.TotalInputTokens != 210 ||
		tt.ThinkingTokens == nil || *tt.ThinkingTokens != 8 {
		t.Fatalf("totals = %+v, want requests 4 uncached 105 read 90 output 42 thinking 8 total_input 210", tt)
	}
}

// IMP-CODEX via the nearest "sessions" ancestor, with IMP-PRICED for Codex.
//
// Rates: sol 4/20/5/8/0.2, mini 1/2/1/1/0.1 (input, output, write5m, write1h, read).
// T1: sol 30*4 + 60*0.2 + 10*5 + 20*20 = 582; mini 50*1 + 8*2 = 66 -> 648e-6.
// T2: 10*4 + 30*0.2 + 10*20 = 246e-6. T3: 15*4 + 5*5 + 4*20 = 165e-6. Total 1059e-6.
func TestMetricsImportCodexSessionsAncestor(t *testing.T) {
	rootPath := writeCodexTree(t, filepath.Join(t.TempDir(), "sessions"))
	prices := writePrices(t, map[string]rates{"gpt-6.1-sol": {4, 20, 5, 8, 0.2}, "gpt-6.1-mini": {1, 2, 1, 1, 0.1}})
	report, raw := runImport(t, "--codex-session", rootPath, "--prices", prices)
	if report.Source != usagelog.SourceCodex {
		t.Fatalf("source = %q", report.Source)
	}
	if slices.Contains(report.IncompleteReasons, "child_discovery_not_attempted") {
		t.Fatalf("discovery reported not attempted under a sessions ancestor: %v", report.IncompleteReasons)
	}
	checkCodexAgents(t, report)
	for depth, want := range map[int]float64{0: 648e-6, 1: 246e-6, 2: 165e-6} {
		near(t, fmt.Sprintf("codex depth %d amount", depth), agentAtDepth(t, report.Agents, depth).Dollars.Amount, want)
	}
	if report.Dollars.PriceTable == nil {
		t.Fatal("price_table = null")
	}
	near(t, "codex price_table.total", report.Dollars.PriceTable.Total, 1059e-6)
	if report.Dollars.Recorded != nil {
		t.Fatalf("codex has recorded dollars: %+v", report.Dollars.Recorded)
	}
	if _, ok := raw["cost"]; ok {
		t.Fatalf("codex cost = %v, want absent", raw["cost"])
	}
}

// IMP-CODEX via --codex-sessions-root, and discovery not attempted without one.
func TestMetricsImportCodexSessionsRootFlagAndNoDiscovery(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "archive")
	rootPath := writeCodexTree(t, archive)
	report, _ := runImport(t, "--codex-session", rootPath, "--codex-sessions-root", archive)
	checkCodexAgents(t, report)

	report, _ = runImport(t, "--codex-session", rootPath)
	if report.Status != usagelog.StatusIncomplete || !slices.Contains(report.IncompleteReasons, "child_discovery_not_attempted") {
		t.Fatalf("status %q reasons %v, want incomplete with child_discovery_not_attempted", report.Status, report.IncompleteReasons)
	}
	for _, agent := range report.Agents {
		if agent.ParentID != "" {
			t.Fatalf("child agent reported without discovery: %+v", agent)
		}
	}
}

// rawCostState writes a cost-state whose modelUsage members are given verbatim,
// so token fields can be omitted.
func rawCostState(t *testing.T, session string, total float64, models map[string]map[string]any) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type": "cost-state", "sessionId": session, "totalCostUSD": total,
		"hasUnknownModelCost": false, "modelUsage": models,
	})
}

// IMP-SESSION-MISMATCH (r3): a resumed session whose file starts with copied
// old-session history and has only a user entry and a cost-state of its own.
func TestMetricsImportResumedSessionWithoutOwnRequests(t *testing.T) {
	const resumed, old = "new1", "old0"
	main := filepath.Join(t.TempDir(), "project", resumed+".jsonl")
	writeLines(t, main,
		claudeAssistant(t, old, "req_old1", "msg_old1", opusModel, &usageSpec{in: 2, read: 100, out: 7}),
		claudeAssistant(t, old, "req_old2", "msg_old2", opusModel, &usageSpec{in: 3, w5: 4, out: 5}),
		mustJSON(t, map[string]any{"type": "user", "sessionId": resumed, "message": map[string]any{"role": "user", "content": "continue"}}),
		rawCostState(t, resumed, 0.75, map[string]map[string]any{opusModel: {"inputTokens": 5, "outputTokens": 3, "costUSD": 0.75}}),
	)
	report, _ := runImport(t, "--claude-transcript", main)
	if report.SessionID != resumed || report.Status != usagelog.StatusComplete {
		t.Fatalf("session %q status %q reasons %v, want %s complete", report.SessionID, report.Status, report.IncompleteReasons, resumed)
	}
	if report.Totals.Requests != 0 || report.Skipped.OtherSessionEntries != 2 {
		t.Fatalf("requests %d other_session_entries %d, want 0 and 2", report.Totals.Requests, report.Skipped.OtherSessionEntries)
	}
	if report.Dollars.Recorded == nil || math.Abs(report.Dollars.Recorded.Total-0.75) > 1e-12 {
		t.Fatalf("dollars.recorded = %+v, want total 0.75", report.Dollars.Recorded)
	}
}

// IMP-CONSISTENCY (r3): a cost-state model with costUSD but no token field is
// not_compared and withholds usage and cost; per-agent numbers stay reported.
func TestMetricsImportNotComparedModelSuppressesObservation(t *testing.T) {
	opus := map[string]any{
		"inputTokens": 12, "outputTokens": 26, "thinkingTokens": 5, "cacheReadInputTokens": 340, "cacheCreationInputTokens": 42,
		"cacheCreationEphemeral5mInputTokens": 22, "cacheCreationEphemeral1hInputTokens": 20, "webSearchRequests": 0, "costUSD": 1.0,
	}
	_, main := writeClaudeSession(t, rawCostState(t, sessionID, 1.2, map[string]map[string]any{opusModel: opus, sonnetModel: {"costUSD": 0.2}}))
	report, raw := runImport(t, "--claude-transcript", main)
	if r := reconciliation(t, report.Dollars.Recorded, sonnetModel); r.Status != "not_compared" {
		t.Fatalf("sonnet reconciliation = %+v, want not_compared", r)
	}
	if r := reconciliation(t, report.Dollars.Recorded, opusModel); r.Status != "equal" {
		t.Fatalf("opus reconciliation = %+v, want equal", r)
	}
	requireNoObservation(t, raw)
	if math.Abs(report.Dollars.Recorded.Total-1.2) > 1e-12 {
		t.Fatalf("recorded total = %v, want 1.2", report.Dollars.Recorded.Total)
	}
	checkTokens(t, "totals", report.Totals, 6, 16, 22, 26, 400, 38, 464)
	checkTokens(t, "agent-b2", report.Agents[2].Tokens, 1, 1, 0, 0, 10, 3, 11)
}

// IMP-CONSISTENCY (r3): one comparable token field keeps the r1/r2 behaviour;
// sonnet's recorded output 12 equals the transcript, so usage and cost stay.
func TestMetricsImportSingleComparableFieldKeepsObservation(t *testing.T) {
	opus := map[string]any{
		"inputTokens": 12, "outputTokens": 26, "thinkingTokens": 5, "cacheReadInputTokens": 340, "cacheCreationInputTokens": 42,
		"cacheCreationEphemeral5mInputTokens": 22, "cacheCreationEphemeral1hInputTokens": 20, "webSearchRequests": 0, "costUSD": 1.0,
	}
	_, main := writeClaudeSession(t, rawCostState(t, sessionID, 1.2, map[string]map[string]any{opusModel: opus, sonnetModel: {"outputTokens": 12, "costUSD": 0.2}}))
	report, raw := runImport(t, "--claude-transcript", main)
	if r := reconciliation(t, report.Dollars.Recorded, sonnetModel); r.Status != "equal" {
		t.Fatalf("sonnet reconciliation = %+v, want equal", r)
	}
	if report.Usage == nil || report.Usage.InputTokens == nil || *report.Usage.InputTokens != 464 ||
		report.Usage.APIRequests == nil || *report.Usage.APIRequests != 6 {
		t.Fatalf("usage = %+v (reason %v), want input 464 and api_requests 6", report.Usage, raw["usage_unknown_reason"])
	}
	if report.Cost == nil {
		t.Fatal("cost absent, want the recorded amount")
	}
	near(t, "cost.amount", report.Cost.Amount, 1.2)
}
