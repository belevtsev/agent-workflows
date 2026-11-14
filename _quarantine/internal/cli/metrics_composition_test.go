package cli

// Behavioural tests for `cw metrics import ... --composition` written from
// contract wp7-composition revision 11 (requirement IDs: attribution, reread,
// repeats, stale-phase, roles, reconcile, summary, codex). Every expected
// number below is computed by hand from the synthetic fixtures; the arithmetic
// is written next to each value.
//
// Rates (per million tokens) from the approved scaffolding:
//   opus   input 4, output 20, cache_write_5m 5,   cache_write_1h 8, cache_read 0.2
//   sonnet input 2, output 10, cache_write_5m 2.5, cache_write_1h 4, cache_read 0.1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const compTol = 1e-9

// ---------------------------------------------------------------------------
// Fixture line builders
// ---------------------------------------------------------------------------

type compUsage struct {
	uncached, w5m, w1h, read, out int
}

func compAssistant(t *testing.T, requestID, model string, u compUsage, item map[string]any) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type":      "assistant",
		"sessionId": sessionID,
		"requestId": requestID,
		"message": map[string]any{
			"id":      "msg_" + requestID,
			"model":   model,
			"role":    "assistant",
			"content": []any{item},
			"usage": map[string]any{
				"input_tokens":                u.uncached,
				"cache_creation_input_tokens": u.w5m + u.w1h,
				"cache_read_input_tokens":     u.read,
				"output_tokens":               u.out,
				"cache_creation": map[string]any{
					"ephemeral_5m_input_tokens": u.w5m,
					"ephemeral_1h_input_tokens": u.w1h,
				},
			},
		},
	})
}

func compToolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func compTextItem(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func compThinkingItem(text string) map[string]any {
	return map[string]any{"type": "thinking", "thinking": text}
}

func compUserPrompt(t *testing.T, text string) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type":      "user",
		"sessionId": sessionID,
		"message":   map[string]any{"role": "user", "content": text},
	})
}

func compToolResult(t *testing.T, toolUseID, text string, toolUseResult map[string]any) string {
	t.Helper()
	line := map[string]any{
		"type":      "user",
		"sessionId": sessionID,
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": text},
		}},
	}
	if toolUseResult != nil {
		line["toolUseResult"] = toolUseResult
	}
	return mustJSON(t, line)
}

func compToolResultParts(t *testing.T, toolUseID string, parts []string) string {
	t.Helper()
	content := make([]any, 0, len(parts))
	for _, p := range parts {
		content = append(content, compTextItem(p))
	}
	return mustJSON(t, map[string]any{
		"type":      "user",
		"sessionId": sessionID,
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": content},
		}},
	})
}

func compSkillBody(t *testing.T, skillToolUseID, text string) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type":            "user",
		"sessionId":       sessionID,
		"isMeta":          true,
		"sourceToolUseID": skillToolUseID,
		"message":         map[string]any{"role": "user", "content": []any{compTextItem(text)}},
	})
}

func compAttachment(t *testing.T, kind, text string) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type":       "attachment",
		"sessionId":  sessionID,
		"attachment": map[string]any{"type": kind},
		"rendered":   []any{map[string]any{"content": text}},
	})
}

func compCompactBoundary(t *testing.T) string {
	t.Helper()
	return mustJSON(t, map[string]any{"type": "system", "subtype": "compact_boundary", "sessionId": sessionID})
}

type compModelUsage struct {
	uncached, read, w5m, w1h, out int
	cost                          float64
}

func compCostState(t *testing.T, total float64, unknown bool, models map[string]compModelUsage) string {
	t.Helper()
	usage := map[string]any{}
	for model, u := range models {
		usage[model] = map[string]any{
			"inputTokens":                         u.uncached,
			"outputTokens":                        u.out,
			"thinkingTokens":                      0,
			"cacheReadInputTokens":                u.read,
			"cacheCreationInputTokens":            u.w5m + u.w1h,
			"cacheCreationEphemeral5mInputTokens": u.w5m,
			"cacheCreationEphemeral1hInputTokens": u.w1h,
			"webSearchRequests":                   0,
			"costUSD":                             u.cost,
		}
	}
	return mustJSON(t, map[string]any{
		"type":                "cost-state",
		"sessionId":           sessionID,
		"totalCostUSD":        total,
		"hasUnknownModelCost": unknown,
		"modelUsage":          usage,
	})
}

// compFill returns exactly n ASCII bytes.
func compFill(n int, c byte) string { return strings.Repeat(string(c), n) }

func compSubagentPath(dir, id string) string {
	return filepath.Join(dir, sessionID, "subagents", "agent-"+id+".jsonl")
}

func compMetaPath(dir, id string) string {
	return filepath.Join(dir, sessionID, "subagents", "agent-"+id+".meta.json")
}

// ---------------------------------------------------------------------------
// Report access helpers (generic JSON decoding)
// ---------------------------------------------------------------------------

func compImport(t *testing.T, args ...string) map[string]any {
	t.Helper()
	out, err := executeImport(t, args...)
	if err != nil {
		t.Fatalf("metrics import %v: %v (exit %d)", args, err, ExitCode(err))
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	return report
}

func compComposition(t *testing.T, report map[string]any) map[string]any {
	t.Helper()
	raw, ok := report["composition"]
	if !ok {
		t.Fatalf("report has no composition key")
	}
	comp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("composition = %#v, want object", raw)
	}
	return comp
}

func compObj(t *testing.T, v any, what string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", what, v)
	}
	return m
}

func compArr(t *testing.T, v any, what string) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", what, v)
	}
	return a
}

func compWantNum(t *testing.T, m map[string]any, key string, want float64, ctx string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("%s: missing key %q", ctx, key)
		return
	}
	got, ok := v.(float64)
	if !ok {
		t.Errorf("%s: %s = %#v, want number %v", ctx, key, v, want)
		return
	}
	if math.Abs(got-want) > compTol {
		t.Errorf("%s: %s = %v, want %v", ctx, key, got, want)
	}
}

func compWantStr(t *testing.T, m map[string]any, key, want, ctx string) {
	t.Helper()
	got, ok := m[key].(string)
	if !ok || got != want {
		t.Errorf("%s: %s = %#v, want %q", ctx, key, m[key], want)
	}
}

func compWantNull(t *testing.T, m map[string]any, key, ctx string) {
	t.Helper()
	if v := m[key]; v != nil {
		t.Errorf("%s: %s = %#v, want null", ctx, key, v)
	}
}

func compAgent(t *testing.T, comp map[string]any, id string) map[string]any {
	t.Helper()
	for _, a := range compArr(t, comp["agents"], "composition.agents") {
		m := compObj(t, a, "agent")
		if m["agent_id"] == id {
			return m
		}
	}
	t.Fatalf("composition.agents has no agent_id %q", id)
	return nil
}

func compFind(t *testing.T, list []any, key, value, what string) map[string]any {
	t.Helper()
	for _, item := range list {
		m := compObj(t, item, what)
		if m[key] == value {
			return m
		}
	}
	t.Fatalf("%s has no entry with %s=%q", what, key, value)
	return nil
}

type compSourceWant struct {
	source, category string
	blocks           int // -1: not asserted
	newTokens        float64
	inputTokens      float64
	rereadTokens     float64
	inputDollars     float64
	rereadDollars    float64
}

func compCheckSources(t *testing.T, agent map[string]any, agentInput float64, wants []compSourceWant) {
	t.Helper()
	id, _ := agent["agent_id"].(string)
	sources := compArr(t, agent["sources"], id+".sources")
	gotOrder := make([]string, 0, len(sources))
	for _, s := range sources {
		gotOrder = append(gotOrder, compObj(t, s, "source")["source"].(string))
	}
	wantOrder := make([]string, 0, len(wants))
	for _, w := range wants {
		wantOrder = append(wantOrder, w.source)
	}
	if strings.Join(gotOrder, "|") != strings.Join(wantOrder, "|") {
		t.Errorf("%s sources order = %q, want %q", id, gotOrder, wantOrder)
	}
	var sumInput, sumDollars float64
	for _, s := range sources {
		m := compObj(t, s, "source")
		sumInput += m["input_tokens"].(float64)
		sumDollars += m["input_dollars"].(float64)
	}
	// Invariant: sum of sources[].input_tokens == agent input_tokens.
	if math.Abs(sumInput-agentInput) > compTol {
		t.Errorf("%s: sum of sources input_tokens = %v, want %v", id, sumInput, agentInput)
	}
	// Sum of blocks' input_dollars == agent input_dollars.
	if ad, ok := agent["input_dollars"].(float64); ok && math.Abs(sumDollars-ad) > compTol {
		t.Errorf("%s: sum of sources input_dollars = %v, agent input_dollars = %v", id, sumDollars, ad)
	}
	for _, w := range wants {
		m := compFind(t, sources, "source", w.source, id+".sources")
		ctx := id + " source " + w.source
		compWantStr(t, m, "category", w.category, ctx)
		if w.blocks >= 0 {
			compWantNum(t, m, "blocks", float64(w.blocks), ctx)
		}
		compWantNum(t, m, "new_tokens", w.newTokens, ctx)
		compWantNum(t, m, "input_tokens", w.inputTokens, ctx)
		compWantNum(t, m, "input_share", w.inputTokens/agentInput, ctx)
		compWantNum(t, m, "reread_tokens", w.rereadTokens, ctx)
		compWantNum(t, m, "input_dollars", w.inputDollars, ctx)
		compWantNum(t, m, "reread_dollars", w.rereadDollars, ctx)
	}
}

func compSnapshot(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(files)
	return files
}

func compLines(out string) []string {
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

func compHasLinePrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func compHasLineContaining(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Fixture A: main (opus, 9 requests) + subagent a1 (sonnet, 2 requests)
// ---------------------------------------------------------------------------
//
// Main transcript, with visible bytes and hand-assigned tokens.
//   skill_listing attachment 400 B, user prompt 600 B           (initial context)
//   r1  T=10000 (w1h 10000)              out 50  Read reference (t1)
//   t1 result 2000 B -> reference:references/test-author.md
//   r2  T=10550 (read 10000, w1h 550)    out 40  d=550=50+500, R=500, sample 2000/500=4
//   t2 result 1200 B (two text parts) -> tool:Read
//   r3  T=10990 (read 10550, w1h 440)    out 30  d=440=40+400, R=400, sample 1200/400=3
//   t3 result 100 B + skill body 900 B -> skill:go-testing
//   r4  T=11220 (read 10990, w1h 230)    out 20  d=230=30+200, R=200, sample 1000/200=5
//                                                 shares 100/1000*200=20, 900/1000*200=180
//   t4 result 300 B -> tool:Bash (ls -la)
//   r5  T=11315 (read 11220, w1h 95)     out 10  d=95=20+75,   R=75,  sample 300/75=4
//   t5 result 300 B -> tool:Bash (ls -la, repeat)
//   r6  T=11475 (read 11315, w1h 160)    out 60  d=160=10+150, R=150, sample 300/150=2
//   t6 result 600 B -> cw task assign
//   r7  T=11635 (read 11475, w1h 160)    out 25  d=160=60+100, R=100, sample 600/100=6
//   t7 result 800 B -> agent_result (agentId a1)
//   r8  T=11860 (read 11635, w1h 225)    out 15  d=225=25+200, R=200, sample 800/200=4
//   (nothing but O8)
//   r9  T=11975 (read 11860, w1h 115)    out 80  d=115=15+100, R=100 -> unrecorded
//
//   samples {4,3,5,4,2,6,4} sorted {2,3,4,4,4,5,6} -> r = 4
//   E = round((400+600)/4) = 250 (<= 10000); skill_listing 400/1000*250 = 100,
//   user_turn 600/1000*250 = 150; system_and_tools = 10000-250 = 9750.
//   input_tokens = 10000+10550+10990+11220+11315+11475+11635+11860+11975 = 101020
//   output_tokens = 50+40+30+20+10+60+25+15+80 = 330
//   reads = 101020-11975 = 89045; w1h = 11975
//   input $ = 89045*0.2e-6 + 11975*8e-6 = 0.017809 + 0.0958 = 0.113609
//   output $ = 330*20e-6 = 0.0066; main total = 0.120209
//
// Subagent a1 (sonnet):
//   brief 2000 B
//   q1 T=5000 (w5m 5000)                 out 20  Bash "go test ./..."
//   st1 result 1000 B -> tool:Bash
//   q2 T=5520 (uncached 520, read 5000)  out 100 d=520=20+500, R=500, sample 1000/500=2
//   r = 2; E = round(2000/2) = 1000 -> agent_brief 1000, system_and_tools 4000
//   input_tokens = 5000+5520 = 10520; output_tokens = 120
//   input $ = 5000*2.5e-6 + 5000*0.1e-6 + 520*2e-6 = 0.0125+0.0005+0.00104 = 0.01404
//   output $ = 120*10e-6 = 0.0012; sub total = 0.01524
//
// Session price-table total = 0.120209 + 0.01524 = 0.135449.

const compRefPath = "/home/dev/.claude/skills/task-orchestration/references/test-author.md"

// Rev 5: the brief carries an assignment id that the Agent prompt names.
const compAssignA = `cw task assign --task wp7 --json '{"id":"impl-1","role":"implementation","brief":"independent test author for metrics"}'`

const compHaikuModel = "claude-haiku-5"

var compHaikuRates = rates{1, 5, 1.25, 2, 0.1} // input, output, w5m, w1h, read

type compCostA struct {
	omit     bool
	total    float64
	unknown  bool
	opusCost float64
	opusOut  int
	haikuSub bool // add a tiny haiku subagent that the cost-state does not record (transcript_only)
}

func compDefaultCostA() compCostA {
	return compCostA{total: 0.135449, opusCost: 0.120209, opusOut: 330}
}

func compWriteFixtureA(t *testing.T, cost compCostA) (dir, mainPath, pricesPath string) {
	t.Helper()
	dir = t.TempDir()
	mainPath = filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	lines := []string{
		compAttachment(t, "skill_listing", compFill(400, 'a')),
		compUserPrompt(t, compFill(600, 'b')),
		compAssistant(t, "r1", o, compUsage{w1h: 10000, out: 50}, compThinkingItem("plan the work")),
		compAssistant(t, "r1", o, compUsage{w1h: 10000, out: 50}, compToolUse("t1", "Read", map[string]any{"file_path": compRefPath})),
		compToolResult(t, "t1", compFill(2000, 'c'), nil),
		compAssistant(t, "r2", o, compUsage{read: 10000, w1h: 550, out: 40}, compToolUse("t2", "Read", map[string]any{"file_path": "/repo/internal/cli/main.go"})),
		compToolResultParts(t, "t2", []string{compFill(600, 'd'), compFill(600, 'e')}),
		compAssistant(t, "r3", o, compUsage{read: 10550, w1h: 440, out: 30}, compToolUse("t3", "Skill", map[string]any{"skill": "go-testing"})),
		compToolResult(t, "t3", compFill(100, 'f'), nil),
		compSkillBody(t, "t3", compFill(900, 'g')),
		compAssistant(t, "r4", o, compUsage{read: 10990, w1h: 230, out: 20}, compToolUse("t4", "Bash", map[string]any{"command": "ls -la"})),
		compToolResult(t, "t4", compFill(300, 'h'), nil),
		compAssistant(t, "r5", o, compUsage{read: 11220, w1h: 95, out: 10}, compToolUse("t5", "Bash", map[string]any{"command": "ls -la"})),
		compToolResult(t, "t5", compFill(300, 'h'), nil),
		compAssistant(t, "r6", o, compUsage{read: 11315, w1h: 160, out: 60}, compToolUse("t6", "Bash", map[string]any{"command": compAssignA})),
		compToolResult(t, "t6", compFill(600, 'i'), nil),
		compAssistant(t, "r7", o, compUsage{read: 11475, w1h: 160, out: 25}, compToolUse("t7", "Agent", map[string]any{
			"description": "write tests", "subagent_type": "general-purpose", "prompt": "write the tests for impl-1",
		})),
		compToolResult(t, "t7", compFill(800, 'j'), map[string]any{"agentId": "a1", "status": "completed"}),
		compAssistant(t, "r8", o, compUsage{read: 11635, w1h: 225, out: 15}, compThinkingItem("consider the result")),
		compAssistant(t, "r9", o, compUsage{read: 11860, w1h: 115, out: 80}, compTextItem("done")),
	}
	if !cost.omit {
		models := map[string]compModelUsage{
			// opus: reads 89045, 1h writes 11975, output 330 (see header).
			opusModel: {read: 89045, w1h: 11975, out: cost.opusOut, cost: cost.opusCost},
			// sonnet: uncached 520, reads 5000, 5m writes 5000, output 20+100=120.
			sonnetModel: {uncached: 520, read: 5000, w5m: 5000, out: 120, cost: 0.01524},
		}
		lines = append(lines, compCostState(t, cost.total, cost.unknown, models))
	}
	writeLines(t, mainPath, lines...)

	s := sonnetModel
	writeLines(t, compSubagentPath(dir, "a1"),
		compUserPrompt(t, compFill(2000, 'k')),
		compAssistant(t, "q1", s, compUsage{w5m: 5000, out: 20}, compToolUse("st1", "Bash", map[string]any{"command": "go test ./..."})),
		compToolResult(t, "st1", compFill(1000, 'l'), nil),
		compAssistant(t, "q2", s, compUsage{uncached: 520, read: 5000, out: 100}, compTextItem("tests written")),
	)
	writeLines(t, compMetaPath(dir, "a1"), mustJSON(t, map[string]any{"agentType": "general-purpose", "description": "write tests"}))

	if cost.haikuSub {
		// One haiku request: 100 uncached input, 10 output.
		// Priced 100*1e-6 + 10*5e-6 = 0.0001 + 0.00005 = 0.00015.
		writeLines(t, compSubagentPath(dir, "h1"),
			compUserPrompt(t, compFill(40, 'm')),
			compAssistant(t, "hq1", compHaikuModel, compUsage{uncached: 100, out: 10}, compTextItem("ok")),
		)
	}

	pricesPath = writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates, compHaikuModel: compHaikuRates})
	return dir, mainPath, pricesPath
}

func compRunA(t *testing.T, cost compCostA) map[string]any {
	t.Helper()
	_, mainPath, prices := compWriteFixtureA(t, cost)
	return compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
}

// ---------------------------------------------------------------------------
// attribution
// ---------------------------------------------------------------------------

func TestMetricsCompositionAttributionMainAgent(t *testing.T) {
	comp := compRunA(t, compDefaultCostA())
	compWantStr(t, comp, "status", "reported", "composition")

	main := compAgent(t, comp, "main")
	compWantStr(t, main, "role", "main", "main")            // no task_start boundary
	compWantStr(t, main, "role_basis", "main", "main")      // main agent
	compWantNum(t, main, "requests", 9, "main")             // r1..r9
	compWantNum(t, main, "input_tokens", 101020, "main")    // sum of T1..T9 (header)
	compWantNum(t, main, "output_tokens", 330, "main")      // 50+40+30+20+10+60+25+15+80
	compWantNum(t, main, "input_dollars", 0.113609, "main") // 89045*0.2e-6 + 11975*8e-6
	compWantNum(t, main, "output_dollars", 0.0066, "main")  // 330*20e-6
	compWantNum(t, main, "bytes_per_token", 4, "main")      // median of {2,3,4,4,4,5,6}
	compWantNum(t, main, "initial_context_tokens", 10000, "main")
	compWantNum(t, main, "unrecorded_tokens", 100, "main") // r9: R=115-15
	compWantNum(t, main, "compactions", 0, "main")
	cr := compObj(t, main["context_reductions"], "main.context_reductions")
	compWantNum(t, cr, "events", 0, "main.context_reductions")
	compWantNum(t, cr, "tokens", 0, "main.context_reductions")
	compWantNull(t, main, "phases", "main") // no boundaries (assign role implementation is not one)

	// Block assigned in request k is resident in requests k..9 (10-k requests);
	// reread = new*(10-k-1). input $ = new*8e-6 (its first request, all 1h writes)
	// + reread*0.2e-6 (later requests, all cache reads).
	compCheckSources(t, main, 101020, []compSourceWant{
		// 9750*9=87750; 9750*8=78000; 9750*8e-6=0.078 + 78000*0.2e-6=0.0156 -> 0.0936
		{"system_and_tools", "fixed_prefix", 1, 9750, 87750, 78000, 0.0936, 0.0156},
		// k=2: 500*8=4000; 500*7=3500; 0.004+0.0007=0.0047
		{"reference:references/test-author.md", "reference_read", 1, 500, 4000, 3500, 0.0047, 0.0007},
		// k=3: 400*7=2800; 400*6=2400; 0.0032+0.00048=0.00368
		{"tool:Read", "tool_output", 1, 400, 2800, 2400, 0.00368, 0.00048},
		// initial: 150*9=1350; 150*8=1200; 0.0012+0.00024=0.00144
		{"user_turn", "user_turn", 1, 150, 1350, 1200, 0.00144, 0.00024},
		// O1..O8 = 50,40,30,20,10,60,25,15 (=250) assigned in r2..r9:
		// input 50*8+40*7+30*6+20*5+10*4+60*3+25*2+15*1 = 400+280+180+100+40+180+50+15 = 1245
		// reread 50*7+40*6+30*5+20*4+10*3+60*2+25*1+15*0 = 350+240+150+80+30+120+25 = 995
		// $ 250*8e-6=0.002 + 995*0.2e-6=0.000199 -> 0.002199
		// blocks: O1..O9 (rev 3: the last request's O9 is a block with 0 tokens) = 9
		{"assistant_output", "assistant_output", 9, 250, 1245, 995, 0.002199, 0.000199},
		// k=4: 20+180=200; 200*6=1200; 200*5=1000; 0.0016+0.0002=0.0018
		{"skill:go-testing", "skill_load", 2, 200, 1200, 1000, 0.0018, 0.0002},
		// 75 at k=5, 150 at k=6: input 75*5+150*4=375+600=975; reread 75*4+150*3=300+450=750
		// $ 225*8e-6=0.0018 + 750*0.2e-6=0.00015 -> 0.00195
		{"tool:Bash", "tool_output", 2, 225, 975, 750, 0.00195, 0.00015},
		// initial: 100*9=900; 100*8=800; 0.0008+0.00016=0.00096
		{"skill_listing", "fixed_prefix", 1, 100, 900, 800, 0.00096, 0.00016},
		// k=8: 200*2=400; 200*1=200; 0.0016+0.00004=0.00164
		{"agent_result", "agent_handoff", 1, 200, 400, 200, 0.00164, 0.00004},
		// k=7: 100*3=300; 100*2=200; 0.0008+0.00004=0.00084
		{"cw task assign", "cw_output", 1, 100, 300, 200, 0.00084, 0.00004},
		// k=9: 100*1=100; 0; 0.0008
		{"unrecorded", "unrecorded", 1, 100, 100, 0, 0.0008, 0},
	})

	// Invariant without reduction/compaction: sum of new_tokens == last T (11975).
	var sumNew float64
	for _, s := range compArr(t, main["sources"], "main.sources") {
		sumNew += compObj(t, s, "source")["new_tokens"].(float64)
	}
	if math.Abs(sumNew-11975) > compTol {
		t.Errorf("main: sum of new_tokens = %v, want 11975 (= T9)", sumNew)
	}
	// input_dollars + output_dollars == price-table dollars 0.120209.
	total := main["input_dollars"].(float64) + main["output_dollars"].(float64)
	if math.Abs(total-0.120209) > compTol {
		t.Errorf("main: input+output dollars = %v, want 0.120209", total)
	}
}

func TestMetricsCompositionAttributionSubagentCalibration(t *testing.T) {
	comp := compRunA(t, compDefaultCostA())
	sub := compAgent(t, comp, "agent-a1")
	compWantNum(t, sub, "requests", 2, "agent-a1")
	compWantNum(t, sub, "input_tokens", 10520, "agent-a1")    // 5000+5520
	compWantNum(t, sub, "output_tokens", 120, "agent-a1")     // 20+100
	compWantNum(t, sub, "input_dollars", 0.01404, "agent-a1") // 0.0125+0.0005+0.00104
	compWantNum(t, sub, "output_dollars", 0.0012, "agent-a1") // 120*10e-6
	compWantNum(t, sub, "bytes_per_token", 2, "agent-a1")     // own single sample 1000/500
	compWantNum(t, sub, "initial_context_tokens", 5000, "agent-a1")
	compWantNum(t, sub, "unrecorded_tokens", 0, "agent-a1")

	compCheckSources(t, sub, 10520, []compSourceWant{
		// 5000-1000=4000; 4000*2=8000; reread 4000; 4000*2.5e-6=0.01 + 4000*0.1e-6=0.0004 -> 0.0104
		{"system_and_tools", "fixed_prefix", 1, 4000, 8000, 4000, 0.0104, 0.0004},
		// round(2000/2)=1000; 2000; 1000; 0.0025+0.0001=0.0026
		{"agent_brief", "agent_handoff", 1, 1000, 2000, 1000, 0.0026, 0.0001},
		// R=500 in q2 (uncached at input rate): 500*2e-6=0.001
		{"tool:Bash", "tool_output", 1, 500, 500, 0, 0.001, 0},
		// O1=20 in q2 (uncached): 20*2e-6=0.00004
		// blocks: O1, O2 (O2 has 0 tokens) = 2
		{"assistant_output", "assistant_output", 2, 20, 20, 0, 0.00004, 0},
	})
	if rc, ok := sub["repeated_calls"].([]any); ok && len(rc) != 0 {
		t.Errorf("agent-a1 repeated_calls = %v, want none", rc)
	}
}

func TestMetricsCompositionAttributionCategories(t *testing.T) {
	comp := compRunA(t, compDefaultCostA())
	cats := compArr(t, comp["categories"], "categories")
	// Session input = 101020 + 10520 = 111540.
	const session = 111540.0
	wants := []struct {
		category                       string
		input, inputDollars, rereadDol float64
	}{
		// 87750+900+8000; $0.0936+0.00096+0.0104; reread $0.0156+0.00016+0.0004
		{"fixed_prefix", 96650, 0.10496, 0.01616},
		// 2800+975+500; $0.00368+0.00195+0.001; reread $0.00048+0.00015
		{"tool_output", 4275, 0.00663, 0.00063},
		{"reference_read", 4000, 0.0047, 0.0007},
		// 400+2000; $0.00164+0.0026; reread $0.00004+0.0001
		{"agent_handoff", 2400, 0.00424, 0.00014},
		{"user_turn", 1350, 0.00144, 0.00024},
		// 1245+20; $0.002199+0.00004
		{"assistant_output", 1265, 0.002239, 0.000199},
		{"skill_load", 1200, 0.0018, 0.0002},
		{"cw_output", 300, 0.00084, 0.00004},
		{"unrecorded", 100, 0.0008, 0},
	}
	if len(cats) != len(wants) {
		t.Fatalf("categories = %d entries, want %d: %v", len(cats), len(wants), cats)
	}
	for i, w := range wants {
		m := compObj(t, cats[i], "category")
		ctx := "categories[" + w.category + "]"
		compWantStr(t, m, "category", w.category, ctx)
		compWantNum(t, m, "input_tokens", w.input, ctx)
		compWantNum(t, m, "input_share", w.input/session, ctx)
		compWantNum(t, m, "input_dollars", w.inputDollars, ctx)
		compWantNum(t, m, "reread_dollars", w.rereadDol, ctx)
	}
}

// ---------------------------------------------------------------------------
// reread
// ---------------------------------------------------------------------------

func TestMetricsCompositionRereadTopBlocks(t *testing.T) {
	comp := compRunA(t, compDefaultCostA())
	main := compAgent(t, comp, "main")
	top := compArr(t, main["top_blocks"], "main.top_blocks")
	if len(top) == 0 || len(top) > 10 {
		t.Fatalf("main.top_blocks has %d entries, want 1..10", len(top))
	}
	prev := math.Inf(1)
	for i, b := range top {
		// Revision 2: only blocks with reread_tokens > 0 are listed.
		if rt, _ := compObj(t, b, "top_block")["reread_tokens"].(float64); !(rt > 0) {
			t.Errorf("top_blocks[%d] reread_tokens = %v, want > 0", i, compObj(t, b, "top_block")["reread_tokens"])
		}
		d := compObj(t, b, "top_block")["reread_dollars"].(float64)
		if d > prev+compTol {
			t.Errorf("top_blocks[%d].reread_dollars %v > previous %v (want desc)", i, d, prev)
		}
		prev = d
	}
	// Blocks with reread_tokens > 0, ranked by reread dollars (all opus, so by
	// reread tokens = tokens * later_requests). request = request at which the block
	// first entered context; later_requests = 9 - request.
	//   system_and_tools 9750*8=78000 | reference 500*7=3500 | tool:Read 400*6=2400 |
	//   user_turn 150*8=1200 | skill body 180*5=900 | skill_listing 100*8=800 |
	//   Bash t5 150*3=450 | O1 50*7=350 | Bash t4 75*4=300 | O2 40*6=240 |
	//   (cut at 10: cw task assign 200, O3 150, O6 120, skill result 100, ...)
	type topWant struct {
		source, label                  string
		request, tokens, later, reread float64
	}
	wants := []topWant{
		{"system_and_tools", "system prompt and tool definitions", 1, 9750, 8, 78000},
		{"reference:references/test-author.md", compRefPath, 2, 500, 7, 3500},
		{"tool:Read", "/repo/internal/cli/main.go", 3, 400, 6, 2400},
		{"user_turn", "", 1, 150, 8, 1200},
		{"skill:go-testing", "go-testing", 4, 180, 5, 900},
		{"skill_listing", "skill_listing", 1, 100, 8, 800},
		{"tool:Bash", "ls -la", 6, 150, 3, 450},
		{"assistant_output", "", 2, 50, 7, 350},
		{"tool:Bash", "ls -la", 5, 75, 4, 300},
		{"assistant_output", "", 3, 40, 6, 240},
	}
	if len(top) != len(wants) {
		t.Fatalf("main.top_blocks has %d entries, want %d: %v", len(top), len(wants), top)
	}
	for i, w := range wants {
		m := compObj(t, top[i], "top_block")
		ctx := "top_blocks[" + w.source + "]"
		compWantStr(t, m, "source", w.source, ctx)
		compWantStr(t, m, "label", w.label, ctx)
		compWantNum(t, m, "request", w.request, ctx)
		compWantNum(t, m, "tokens", w.tokens, ctx)
		compWantNum(t, m, "later_requests", w.later, ctx)
		compWantNum(t, m, "reread_tokens", w.reread, ctx)
		compWantNum(t, m, "reread_dollars", w.reread*0.2e-6, ctx) // opus cache_read 0.2/M
	}
}

// ---------------------------------------------------------------------------
// repeats
// ---------------------------------------------------------------------------

func TestMetricsCompositionRepeatsCanonicalHash(t *testing.T) {
	comp := compRunA(t, compDefaultCostA())
	main := compAgent(t, comp, "main")
	groups := compArr(t, main["repeated_calls"], "main.repeated_calls")
	if len(groups) != 1 {
		t.Fatalf("main.repeated_calls = %v, want exactly one group (Bash ls -la x2)", groups)
	}
	g := compObj(t, groups[0], "repeated_call")
	sum := sha256.Sum256([]byte(`{"command":"ls -la"}`)) // RFC 8785 canonical form of the input
	compWantStr(t, g, "tool", "Bash", "repeated")
	compWantStr(t, g, "input_sha256", hex.EncodeToString(sum[:]), "repeated")
	compWantStr(t, g, "label", "ls -la", "repeated")
	compWantNum(t, g, "calls", 2, "repeated")
	compWantNum(t, g, "result_tokens", 225, "repeated") // t4 75 + t5 150
}

// ---------------------------------------------------------------------------
// reconcile
// ---------------------------------------------------------------------------

func TestMetricsCompositionReconcileReported(t *testing.T) {
	dir, mainPath, prices := compWriteFixtureA(t, compDefaultCostA())
	before := compSnapshot(t, dir)
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	after := compSnapshot(t, dir)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("import changed the fixture directory:\nbefore %v\nafter  %v", before, after)
	}

	compWantStr(t, comp, "status", "reported", "composition")
	rec := compObj(t, comp["reconciliation"], "reconciliation")
	compWantNum(t, rec, "recorded_total", 0.135449, "reconciliation")
	compWantNum(t, rec, "price_table_total", 0.135449, "reconciliation") // 0.120209+0.01524
	compWantNum(t, rec, "relative_difference", 0, "reconciliation")
	compWantNum(t, rec, "tolerance", 0.01, "reconciliation")
	compWantStr(t, rec, "status", "within_tolerance", "reconciliation")
	compWantNum(t, rec, "attributed_input_tokens", 111540, "reconciliation") // 101020+10520
	compWantNum(t, rec, "transcript_input_tokens", 111540, "reconciliation")

	agents := compArr(t, comp["agents"], "agents")
	if len(agents) != 2 {
		t.Errorf("agents = %d entries, want 2 (main, agent-a1)", len(agents))
	}
	roles := compArr(t, comp["roles"], "roles")
	if len(roles) != 2 {
		t.Fatalf("roles = %v, want 2 entries", roles)
	}
	// Ordered by dollars desc: main 0.120209 > test_author 0.01524.
	r0 := compObj(t, roles[0], "role")
	compWantStr(t, r0, "role", "main", "roles[0]")
	compWantNum(t, r0, "agents", 1, "roles[0]")
	compWantNum(t, r0, "requests", 9, "roles[0]")
	compWantNum(t, r0, "input_tokens", 101020, "roles[0]")
	compWantNum(t, r0, "output_tokens", 330, "roles[0]")
	compWantNum(t, r0, "input_dollars", 0.113609, "roles[0]")
	compWantNum(t, r0, "output_dollars", 0.0066, "roles[0]")
	compWantNum(t, r0, "dollars", 0.120209, "roles[0]")
	r1 := compObj(t, roles[1], "role")
	compWantStr(t, r1, "role", "test_author", "roles[1]")
	compWantNum(t, r1, "agents", 1, "roles[1]")
	compWantNum(t, r1, "requests", 2, "roles[1]")
	compWantNum(t, r1, "input_tokens", 10520, "roles[1]")
	compWantNum(t, r1, "output_tokens", 120, "roles[1]")
	compWantNum(t, r1, "input_dollars", 0.01404, "roles[1]")
	compWantNum(t, r1, "output_dollars", 0.0012, "roles[1]")
	compWantNum(t, r1, "dollars", 0.01524, "roles[1]")

	cov := compObj(t, comp["role_coverage"], "role_coverage")
	compWantNum(t, cov, "agents", 2, "role_coverage")
	compWantNum(t, cov, "mapped_agents", 2, "role_coverage") // neither is "other"
	compWantNum(t, cov, "input_tokens", 111540, "role_coverage")
	compWantNum(t, cov, "mapped_input_tokens", 111540, "role_coverage")
}

func TestMetricsCompositionReconcileWithinTolerance(t *testing.T) {
	// Recorded 0.136 vs price table 0.135449: |diff| = 0.000551, 0.000551/0.136 ~ 0.00405 <= 0.01.
	cost := compDefaultCostA()
	cost.total = 0.136
	cost.opusCost = 0.12076 // 0.120209 + 0.000551
	comp := compRunA(t, cost)
	compWantStr(t, comp, "status", "reported", "composition")
	rec := compObj(t, comp["reconciliation"], "reconciliation")
	compWantNum(t, rec, "recorded_total", 0.136, "reconciliation")
	compWantNum(t, rec, "price_table_total", 0.135449, "reconciliation")
	compWantNum(t, rec, "relative_difference", 0.000551/0.136, "reconciliation")
	compWantStr(t, rec, "status", "within_tolerance", "reconciliation")
}

func TestMetricsCompositionReconcileRecordedAboveTranscript(t *testing.T) {
	// Revision 2, section 8 step 5: recorded tokens above the transcript's do not
	// withhold. Cost-state opus output 331 vs transcript 330 (one background token,
	// 1*20e-6 = $0.00002): recorded total 0.135449+0.00002 = 0.135469, opus cost
	// 0.120209+0.00002 = 0.120229. Price table stays 0.135449 (opus is in the
	// transcript, so it is priced from transcript tokens).
	// relative difference 0.00002/0.135469 ~ 0.000148 <= 0.01 -> reported.
	cost := compDefaultCostA()
	cost.opusOut = 331
	cost.total = 0.135469
	cost.opusCost = 0.120229
	comp := compRunA(t, cost)
	compWantStr(t, comp, "status", "reported", "composition")
	rec := compObj(t, comp["reconciliation"], "reconciliation")
	compWantNum(t, rec, "recorded_total", 0.135469, "reconciliation")
	compWantNum(t, rec, "price_table_total", 0.135449, "reconciliation")
	compWantNum(t, rec, "relative_difference", 0.00002/0.135469, "reconciliation")
	compWantStr(t, rec, "status", "within_tolerance", "reconciliation")
	compWantNum(t, rec, "attributed_input_tokens", 111540, "reconciliation")
	compWantNum(t, rec, "transcript_input_tokens", 111540, "reconciliation")
}

func compAssertWithheld(t *testing.T, comp map[string]any) {
	t.Helper()
	compWantStr(t, comp, "status", "withheld", "composition")
	if r, _ := comp["reason"].(string); strings.TrimSpace(r) == "" {
		t.Errorf("withheld composition reason = %#v, want non-empty", comp["reason"])
	}
	for _, key := range []string{"roles", "role_coverage", "categories", "agents"} {
		compWantNull(t, comp, key, "withheld composition")
	}
}

func TestMetricsCompositionReconcileWithheld(t *testing.T) {
	t.Run("price total exceeds tolerance", func(t *testing.T) {
		// Recorded 0.142449 vs price table 0.135449: 0.007/0.142449 ~ 0.0491 > 0.01.
		cost := compDefaultCostA()
		cost.total = 0.142449
		cost.opusCost = 0.127209 // 0.120209 + 0.007
		comp := compRunA(t, cost)
		compAssertWithheld(t, comp)
		rec := compObj(t, comp["reconciliation"], "reconciliation")
		compWantNum(t, rec, "recorded_total", 0.142449, "reconciliation")
		compWantNum(t, rec, "price_table_total", 0.135449, "reconciliation")
		compWantNum(t, rec, "relative_difference", 0.007/0.142449, "reconciliation")
		compWantNum(t, rec, "tolerance", 0.01, "reconciliation")
		compWantStr(t, rec, "status", "exceeds_tolerance", "reconciliation")
	})
	t.Run("transcript tokens exceed recorded", func(t *testing.T) {
		// Transcript opus output is 330; cost-state records 329 (and $0.00002 less:
		// opus 0.120189, total 0.135429), so the transcript carries a token the
		// recorded cost lacks. Dollars alone would be within 1% (0.00002/0.135429).
		cost := compDefaultCostA()
		cost.opusOut = 329
		cost.total = 0.135429
		cost.opusCost = 0.120189
		comp := compRunA(t, cost)
		compAssertWithheld(t, comp)
		// Rev 4: the step 5 reason names the model.
		if r, _ := comp["reason"].(string); !strings.Contains(r, opusModel) {
			t.Errorf("reason = %q, want it to name %s", r, opusModel)
		}
	})
	t.Run("model only in transcript", func(t *testing.T) {
		// A haiku subagent request (priced 0.00015) is absent from the cost-state
		// (transcript_only). Recorded stays 0.135449; price table 0.135449+0.00015 =
		// 0.135599; 0.00015/0.135449 ~ 0.0011 <= 0.01, so only step 5 withholds.
		cost := compDefaultCostA()
		cost.haikuSub = true
		comp := compRunA(t, cost)
		compAssertWithheld(t, comp)
		if r, _ := comp["reason"].(string); !strings.Contains(r, compHaikuModel) {
			t.Errorf("reason = %q, want it to name %s", r, compHaikuModel)
		}
	})
	t.Run("unknown model cost", func(t *testing.T) {
		cost := compDefaultCostA()
		cost.unknown = true
		compAssertWithheld(t, compRunA(t, cost))
	})
	t.Run("no cost-state", func(t *testing.T) {
		compAssertWithheld(t, compRunA(t, compCostA{omit: true}))
	})
}

// ---------------------------------------------------------------------------
// summary (flags, absence of the key, text output)
// ---------------------------------------------------------------------------

func TestMetricsCompositionSummaryUsageErrors(t *testing.T) {
	_, mainPath, prices := compWriteFixtureA(t, compDefaultCostA())
	t.Run("composition without prices", func(t *testing.T) {
		out, err := executeImport(t, "--claude-transcript", mainPath, "--composition")
		if code := ExitCode(err); code != 2 {
			t.Errorf("exit code = %d (err %v), want 2", code, err)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
	})
	t.Run("summary without composition", func(t *testing.T) {
		out, err := executeImport(t, "--claude-transcript", mainPath, "--prices", prices, "--summary")
		if code := ExitCode(err); code != 2 {
			t.Errorf("exit code = %d (err %v), want 2", code, err)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
	})
}

func TestMetricsCompositionSummaryAbsentWithoutFlag(t *testing.T) {
	_, mainPath, prices := compWriteFixtureA(t, compDefaultCostA())
	t.Run("with prices", func(t *testing.T) {
		report := compImport(t, "--claude-transcript", mainPath, "--prices", prices)
		if _, ok := report["composition"]; ok {
			t.Errorf("report has a composition key without --composition")
		}
	})
	t.Run("without prices", func(t *testing.T) {
		report := compImport(t, "--claude-transcript", mainPath)
		if _, ok := report["composition"]; ok {
			t.Errorf("report has a composition key without --composition")
		}
	})
}

func TestMetricsCompositionSummaryTextReported(t *testing.T) {
	_, mainPath, prices := compWriteFixtureA(t, compDefaultCostA())
	out, err := executeImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition", "--summary")
	if err != nil {
		t.Fatalf("summary: %v (exit %d)", err, ExitCode(err))
	}
	if json.Valid([]byte(out)) {
		t.Fatalf("summary output is JSON, want plain text:\n%s", out)
	}
	lines := compLines(out)
	if !strings.HasPrefix(lines[0], "composition: reported") {
		t.Errorf("line 1 = %q, want prefix %q", lines[0], "composition: reported")
	}
	// recorded 0.135449 -> $0.1354, price-table 0.135449 -> $0.1354, diff ~0 -> 0.00%
	// Rev 3: line 1 ends right after the diff.
	if !strings.Contains(lines[0], " recorded $0.1354 price-table $0.1354 diff ") || !strings.HasSuffix(lines[0], "0.00%") {
		t.Errorf("line 1 = %q, want recorded/price-table $0.1354 and diff 0.00%%", lines[0])
	}
	for _, prefix := range []string{
		"roles:",
		"  main",
		"  test_author",
		"coverage: 2/2 agents",
		"agent main (main)",
		"agent agent-a1 (test_author)",
	} {
		if !compHasLinePrefix(lines, prefix) {
			t.Errorf("summary has no line starting %q:\n%s", prefix, out)
		}
	}
	if !compHasLineContaining(lines, "repeated: Bash x2") {
		t.Errorf("summary has no line containing %q:\n%s", "repeated: Bash x2", out)
	}
	if compHasLinePrefix(lines, "reason:") {
		t.Errorf("reported summary has a reason line:\n%s", out)
	}
}

func TestMetricsCompositionSummaryTextWithheld(t *testing.T) {
	cost := compDefaultCostA()
	cost.total = 0.142449
	cost.opusCost = 0.127209
	_, mainPath, prices := compWriteFixtureA(t, cost)
	out, err := executeImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition", "--summary")
	if err != nil {
		t.Fatalf("summary: %v (exit %d)", err, ExitCode(err))
	}
	lines := compLines(out)
	if !strings.HasPrefix(lines[0], "composition: withheld") {
		t.Errorf("line 1 = %q, want prefix %q", lines[0], "composition: withheld")
	}
	// recorded $0.1424 (0.142449), price-table $0.1354 (0.135449),
	// diff 0.007/0.142449 = 4.914...% -> 4.91%
	if !strings.HasSuffix(lines[0], " recorded $0.1424 price-table $0.1354 diff 4.91%") {
		t.Errorf("line 1 = %q, want recorded $0.1424 price-table $0.1354 diff 4.91%%", lines[0])
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "reason: ") && strings.TrimSpace(strings.TrimPrefix(l, "reason: ")) != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("withheld summary has no non-empty reason line:\n%s", out)
	}
	if compHasLinePrefix(lines, "roles:") {
		t.Errorf("withheld summary has a role table:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// stale-phase
// ---------------------------------------------------------------------------
//
// Main only (opus):
//   user prompt 400 B (initial)
//   c1 T=10000 (w1h 10000)              out 0   Bash "cw task start" (p1)  -> boundary, request 2
//   p1 result 200 B (phase 2) -> cw task start
//   c2 T=10050 (read 10000, w1h 50)     out 20  d=50=0+50, R=50, sample 200/50=4; Write brief (p2)
//   p2 result 100 B (phase 2) -> tool:Write
//   c3 T=10095 (read 10050, w1h 45)     out 0   d=45=20+25, R=25, sample 100/25=4;
//                                               Bash "cw task assign --input <brief>" (p3) -> review_assign, request 4
//   p3 result 400 B (phase 3) -> cw task assign
//   c4 T=10195 (read 10095, w1h 100)    out 40  d=100=0+100, R=100, sample 400/100=4;
//                                               Bash "ls /x/bin/cw cat && cw task verify" (p4) -> task_verify,
//                                               request null (rev 2: "cw cat" is skipped, SUB must be a cw command)
//   r = 4; E = round(400/4) = 100; system_and_tools 9900.
//   Phases: c1 phase 1, c2-c3 phase 2, c4 phase 3.
//   Stale in c4: non-initial blocks from phases < 3 resident and reread in c4:
//     p1 result 50 + O2 20 + p2 result 25 = 95 tokens (O1 and O3 carry 0 tokens).
//   stale_reread_dollars = 95*0.2e-6 = 0.000019; stale_input_dollars = 0.000019 (all in the read segment).
//   prefix_rewrite_dollars = 10000 * 2 (non-null boundaries) * 8e-6 = 0.16
//   input $ = 0.08 + (0.002+0.0004) + (0.00201+0.00036) + (0.002019+0.0008) = 0.087589
//   output $ = 60*20e-6 = 0.0012; total 0.088789

func compWriteFixtureC(t *testing.T) (mainPath, prices string) {
	t.Helper()
	dir := t.TempDir()
	mainPath = filepath.Join(dir, sessionID+".jsonl")
	briefPath := filepath.Join(dir, "work", "review-brief.json")
	o := opusModel
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "c1", o, compUsage{w1h: 10000, out: 0}, compToolUse("p1", "Bash", map[string]any{"command": "cw task start --title wp7"})),
		compToolResult(t, "p1", compFill(200, 'b'), nil),
		compAssistant(t, "c2", o, compUsage{read: 10000, w1h: 50, out: 20}, compToolUse("p2", "Write", map[string]any{
			"file_path": briefPath,
			"content":   `{"role": "review", "brief": "check the diff"}`,
		})),
		compToolResult(t, "p2", compFill(100, 'c'), nil),
		compAssistant(t, "c3", o, compUsage{read: 10050, w1h: 45, out: 0}, compToolUse("p3", "Bash", map[string]any{"command": "cw task assign --task wp7 --input " + briefPath})),
		compToolResult(t, "p3", compFill(400, 'd'), nil),
		compAssistant(t, "c4", o, compUsage{read: 10095, w1h: 100, out: 40}, compToolUse("p4", "Bash", map[string]any{"command": "ls /x/bin/cw cat && cw task verify --task wp7"})),
		compToolResult(t, "p4", compFill(40, 'e'), nil),
		compCostState(t, 0.088789, false, map[string]compModelUsage{
			// reads 10000+10050+10095=30145; 1h writes 10000+50+45+100=10195; output 20+40=60
			opusModel: {read: 30145, w1h: 10195, out: 60, cost: 0.088789},
		}),
	)
	return mainPath, writePrices(t, map[string]rates{opusModel: opusRates})
}

func TestMetricsCompositionStalePhase(t *testing.T) {
	mainPath, prices := compWriteFixtureC(t)
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantStr(t, main, "role", "coordinator", "main") // has a task_start boundary
	compWantStr(t, main, "role_basis", "main", "main")
	compWantNum(t, main, "input_tokens", 40340, "main")     // 10000+10050+10095+10195
	compWantNum(t, main, "input_dollars", 0.087589, "main") // header
	compWantNum(t, main, "output_dollars", 0.0012, "main")  // 60*20e-6
	compWantNum(t, main, "bytes_per_token", 4, "main")

	sources := compArr(t, main["sources"], "main.sources")
	start := compFind(t, sources, "source", "cw task start", "main.sources")
	compWantStr(t, start, "category", "cw_output", "cw task start")
	compWantNum(t, start, "new_tokens", 50, "cw task start")
	write := compFind(t, sources, "source", "tool:Write", "main.sources")
	compWantNum(t, write, "new_tokens", 25, "tool:Write")
	assign := compFind(t, sources, "source", "cw task assign", "main.sources")
	compWantNum(t, assign, "new_tokens", 100, "cw task assign")

	ph := compObj(t, main["phases"], "main.phases")
	compWantNum(t, ph, "phase_count", 4, "phases") // 3 boundaries + 1
	bounds := compArr(t, ph["boundaries"], "phases.boundaries")
	if len(bounds) != 3 {
		t.Fatalf("boundaries = %v, want 3", bounds)
	}
	wantKinds := []string{"task_start", "review_assign", "task_verify"}
	wantReq := []any{2.0, 4.0, nil} // c2 and c4 start new phases; the verify is after the last request
	for i, b := range bounds {
		m := compObj(t, b, "boundary")
		compWantStr(t, m, "kind", wantKinds[i], "boundary")
		if m["request"] != wantReq[i] {
			t.Errorf("boundary %d (%s) request = %#v, want %#v", i, wantKinds[i], m["request"], wantReq[i])
		}
	}
	compWantNum(t, ph, "stale_reread_tokens", 95, "phases")        // 50+20+25
	compWantNum(t, ph, "stale_reread_dollars", 0.000019, "phases") // 95*0.2e-6
	compWantNum(t, ph, "stale_input_dollars", 0.000019, "phases")  // read segment of c4
	compWantNum(t, ph, "prefix_rewrite_dollars", 0.16, "phases")   // 10000*2*8e-6

	out, err := executeImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition", "--summary")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	lines := compLines(out)
	if !compHasLinePrefix(lines, "agent main (coordinator)") {
		t.Errorf("summary has no line starting %q:\n%s", "agent main (coordinator)", out)
	}
	if !compHasLineContaining(lines, "stale-phase re-read: $") {
		t.Errorf("summary has no stale-phase line:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// roles
// ---------------------------------------------------------------------------
//
// Main (opus): user prompt 400 B; m1 T=10000 (w1h 10000) out 100 issuing 3 cw task
// assign Bash calls and 12 Agent calls; 15 results x 40 B = 600 B;
// m2 T=10250 (read 10000, w1h 250) out 10: d=250=100+150, R=150, sample 600/150=4.
// Main input 20250; input $ 0.08+0.002+0.002 = 0.084; output $ 110*20e-6 = 0.0022 -> 0.0862.
// 12 subagents (sonnet), each: brief 400 B, one request T=1000 (w1h) out 10, no own
// samples -> session median 4, E = round(400/4) = 100, system_and_tools 900.
// Each sub: 1000*4e-6 + 10*10e-6 = 0.0041; 12 subs = 0.0492; session 0.1354.

func compAssignCmd(id, role, brief string) string {
	return `cw task assign --task wp7 --json '{"id":"` + id + `","role":"` + role + `","brief":"` + brief + `"}'`
}

func TestMetricsCompositionRolesMapping(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	type call struct {
		id, name string
		input    map[string]any
	}
	agent := func(desc, typ string) map[string]any {
		return map[string]any{"description": desc, "subagent_type": typ, "prompt": "do it"}
	}
	// Rev 5: an Agent call takes the brief whose id its prompt names.
	agentFor := func(desc, briefID string) map[string]any {
		return map[string]any{"description": desc, "subagent_type": "general-purpose", "prompt": "Handle assignment " + briefID + "."}
	}
	calls := []call{
		{"ra1", "Bash", map[string]any{"command": compAssignCmd("review-1", "review", "review the diff")}},
		{"ag10", "Agent", agentFor("first pass", "review-1")},
		{"ra2", "Bash", map[string]any{"command": compAssignCmd("review-2", "review", "second look")}},
		{"ag11", "Agent", agentFor("second pass", "review-2")},
		{"ra3", "Bash", map[string]any{"command": compAssignCmd("verify-1", "verification", "run the checks")}},
		{"ag12", "Agent", agentFor("third pass", "verify-1")},
		{"ag1", "Agent", agent("Independent test-author pass", "general-purpose")},
		{"ag2", "Agent", agent("Follow-up review of fixes", "general-purpose")},
		{"ag3", "Agent", agent("Code review", "general-purpose")},
		{"ag4", "Agent", agent("Open pull request", "general-purpose")},
		{"ag5", "Agent", agent("Fix flaky test", "general-purpose")},
		{"ag6", "Agent", agent("Investigate logs", "general-purpose")},
		{"ag7", "Agent", agent("scan tree", "Explore")},
		{"ag8", "Agent", agent("draft plan", "Plan")},
		{"ag9", "Agent", agent("misc helper", "general-purpose")},
	}
	lines := []string{compUserPrompt(t, compFill(400, 'a'))}
	for _, c := range calls {
		lines = append(lines, compAssistant(t, "m1", opusModel, compUsage{w1h: 10000, out: 100}, compToolUse(c.id, c.name, c.input)))
	}
	for _, c := range calls {
		var res map[string]any
		if c.name == "Agent" {
			res = map[string]any{"agentId": "b" + strings.TrimPrefix(c.id, "ag"), "status": "completed"}
		}
		lines = append(lines, compToolResult(t, c.id, compFill(40, 'r'), res))
	}
	lines = append(lines,
		compAssistant(t, "m2", opusModel, compUsage{read: 10000, w1h: 250, out: 10}, compTextItem("done")),
		compCostState(t, 0.1354, false, map[string]compModelUsage{
			opusModel:   {read: 10000, w1h: 10250, out: 110, cost: 0.0862}, // 10000+250 writes; 100+10 out
			sonnetModel: {w1h: 12000, out: 120, cost: 0.0492},              // 12 x 1000 writes; 12 x 10 out
		}),
	)
	writeLines(t, mainPath, lines...)

	metas := map[string]map[string]any{
		"b1":  {"agentType": "general-purpose", "description": "Independent test-author pass"},
		"b2":  {"agentType": "general-purpose", "description": "Follow-up review of fixes"},
		"b3":  {"agentType": "general-purpose", "description": "Code review"},
		"b4":  {"agentType": "general-purpose", "description": "Open pull request"},
		"b5":  {"agentType": "general-purpose", "description": "Fix flaky test"},
		"b6":  {"agentType": "general-purpose", "description": "Investigate logs"},
		"b7":  {"agentType": "Explore"}, // no description: falls back to the Agent call's "scan tree"
		"b8":  {"agentType": "Plan", "description": "draft plan"},
		"b9":  {"agentType": "general-purpose", "description": "misc helper"},
		"b10": {"agentType": "general-purpose", "description": "first pass"},
		"b11": {"agentType": "general-purpose", "description": "second pass"},
		"b12": {"agentType": "general-purpose", "description": "third pass"},
	}
	for id, meta := range metas {
		writeLines(t, compSubagentPath(dir, id),
			compUserPrompt(t, compFill(400, 'p')),
			compAssistant(t, id+"-q1", sonnetModel, compUsage{w1h: 1000, out: 10}, compTextItem("ok")),
		)
		writeLines(t, compMetaPath(dir, id), mustJSON(t, meta))
	}
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")

	wantRoles := map[string][2]string{
		"main":      {"main", "main"},
		"agent-b1":  {"test_author", "description"},
		"agent-b2":  {"review_followup", "description"},
		"agent-b3":  {"review", "description"},
		"agent-b4":  {"github", "description"},
		"agent-b5":  {"implementation", "description"},
		"agent-b6":  {"evidence", "description"},
		"agent-b7":  {"evidence", "agent_type"},
		"agent-b8":  {"planning", "agent_type"},
		"agent-b9":  {"other", "none"},
		"agent-b10": {"review", "cw_task_assign"},          // first review brief
		"agent-b11": {"review_followup", "cw_task_assign"}, // later review brief
		"agent-b12": {"verification", "cw_task_assign"},
	}
	if n := len(compArr(t, comp["agents"], "agents")); n != len(wantRoles) {
		t.Errorf("agents = %d entries, want %d", n, len(wantRoles))
	}
	for id, w := range wantRoles {
		a := compAgent(t, comp, id)
		compWantStr(t, a, "role", w[0], id)
		compWantStr(t, a, "role_basis", w[1], id)
	}

	// Session-median calibration for a subagent without samples.
	b1 := compAgent(t, comp, "agent-b1")
	compWantNum(t, b1, "bytes_per_token", 4, "agent-b1") // main's only sample 600/150
	compWantNum(t, b1, "initial_context_tokens", 1000, "agent-b1")
	compWantNum(t, b1, "input_tokens", 1000, "agent-b1")
	b1src := compArr(t, b1["sources"], "agent-b1.sources")
	compWantNum(t, compFind(t, b1src, "source", "agent_brief", "agent-b1.sources"), "new_tokens", 100, "agent_brief")           // round(400/4)
	compWantNum(t, compFind(t, b1src, "source", "system_and_tools", "agent-b1.sources"), "new_tokens", 900, "system_and_tools") // 1000-100

	// Coverage: 13 agents, b9 is "other" (1000 tokens). Session input 20250 + 12*1000 = 32250.
	cov := compObj(t, comp["role_coverage"], "role_coverage")
	compWantNum(t, cov, "agents", 13, "role_coverage")
	compWantNum(t, cov, "mapped_agents", 12, "role_coverage")
	compWantNum(t, cov, "input_tokens", 32250, "role_coverage")
	compWantNum(t, cov, "mapped_input_tokens", 31250, "role_coverage") // 32250-1000

	// Roles by dollars desc then name: main 0.0862; 2-agent roles 0.0082 (evidence,
	// review, review_followup); 1-agent roles 0.0041 alphabetical.
	wantOrder := []string{"main", "evidence", "review", "review_followup",
		"github", "implementation", "other", "planning", "test_author", "verification"}
	wantAgents := map[string]float64{"main": 1, "evidence": 2, "review": 2, "review_followup": 2}
	roles := compArr(t, comp["roles"], "roles")
	got := make([]string, 0, len(roles))
	for _, r := range roles {
		m := compObj(t, r, "role")
		name, _ := m["role"].(string)
		got = append(got, name)
		agents := 1.0
		if v, ok := wantAgents[name]; ok {
			agents = v
		}
		compWantNum(t, m, "agents", agents, "role "+name)
		if name != "main" {
			compWantNum(t, m, "input_tokens", 1000*agents, "role "+name)
			compWantNum(t, m, "dollars", 0.0041*agents, "role "+name)
		}
	}
	if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("roles order = %v, want %v", got, wantOrder)
	}
	other := compFind(t, roles, "role", "other", "roles")
	compWantNum(t, other, "input_tokens", 1000, "role other")

	// Inline review briefs are phase boundaries; the verification brief is not.
	main := compAgent(t, comp, "main")
	ph := compObj(t, main["phases"], "main.phases")
	compWantNum(t, ph, "phase_count", 3, "phases")
	bounds := compArr(t, ph["boundaries"], "phases.boundaries")
	if len(bounds) != 2 {
		t.Fatalf("boundaries = %v, want 2 review_assign", bounds)
	}
	for _, b := range bounds {
		m := compObj(t, b, "boundary")
		compWantStr(t, m, "kind", "review_assign", "boundary")
		compWantNum(t, m, "request", 2, "boundary") // both issued in m1
	}
	compWantNum(t, ph, "prefix_rewrite_dollars", 0.16, "phases") // 10000*2*8e-6
}

// ---------------------------------------------------------------------------
// attribution: context reduction and compaction
// ---------------------------------------------------------------------------
//
// Main only (opus):
//   user prompt 400 B (initial)
//   d1 T=10000 (w1h 10000)            out 0  Read /repo/a.go
//   a.go 800 B
//   d2 T=10200 (read 10000, w1h 200)  out 0  d=200, R=200, sample 800/200=4 -> a.go 200
//   b.go 400 B
//   d3 T=10300 (read 10200, w1h 100)  out 0  R=100, sample 4 -> b.go 100
//   Bash "ls /x/bin/cw cat" result 40 B -> tool:Bash (rev 2: not a cw call)
//   d4 T=10150 (read 10150)           out 0  d=-150 -> reduction: a.go 200->100, b.go 100->50
//   compact_boundary
//   user prompt 200 B
//   d5 T=10050 (read 10000, w1h 50)   out 10 d=10050-10000=50 -> user prompt 50 (sample 200/50=4)
//   r=4; E=round(400/4)=100; system_and_tools 9900.
//   input_tokens = 10000+10200+10300+10150+10050 = 50700
//   system 9900*5=49500; user_turn 100*5+50=550; tool:Read 200+300+150=650 -> 50700
//   input $ = 0.08 + 0.0036 + 0.00284 + 0.00203 + 0.0024 = 0.09087; output $ 10*20e-6 = 0.0002

func TestMetricsCompositionAttributionReductionAndCompaction(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "d1", o, compUsage{w1h: 10000}, compToolUse("rd1", "Read", map[string]any{"file_path": "/repo/a.go"})),
		compToolResult(t, "rd1", compFill(800, 'b'), nil),
		compAssistant(t, "d2", o, compUsage{read: 10000, w1h: 200}, compToolUse("rd2", "Read", map[string]any{"file_path": "/repo/b.go"})),
		compToolResult(t, "rd2", compFill(400, 'c'), nil),
		compAssistant(t, "d3", o, compUsage{read: 10200, w1h: 100}, compToolUse("rd3", "Bash", map[string]any{"command": "ls /x/bin/cw cat"})),
		compToolResult(t, "rd3", compFill(40, 'd'), nil),
		compAssistant(t, "d4", o, compUsage{read: 10150}, compThinkingItem("summarize")),
		compCompactBoundary(t),
		compUserPrompt(t, compFill(200, 'f')),
		compAssistant(t, "d5", o, compUsage{read: 10000, w1h: 50, out: 10}, compTextItem("done")),
		compCostState(t, 0.09107, false, map[string]compModelUsage{
			// reads 10000+10200+10150+10000=40350; 1h writes 10000+200+100+50=10350; out 10
			opusModel: {read: 40350, w1h: 10350, out: 10, cost: 0.09107},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	rec := compObj(t, comp["reconciliation"], "reconciliation")
	compWantNum(t, rec, "attributed_input_tokens", 50700, "reconciliation")
	compWantNum(t, rec, "transcript_input_tokens", 50700, "reconciliation")

	main := compAgent(t, comp, "main")
	compWantNum(t, main, "input_tokens", 50700, "main")
	compWantNum(t, main, "input_dollars", 0.09087, "main")
	compWantNum(t, main, "output_dollars", 0.0002, "main")
	compWantNum(t, main, "compactions", 1, "main")
	compWantNum(t, main, "unrecorded_tokens", 0, "main")
	cr := compObj(t, main["context_reductions"], "main.context_reductions")
	compWantNum(t, cr, "events", 1, "context_reductions")
	compWantNum(t, cr, "tokens", 150, "context_reductions") // 10300-10150

	sources := compArr(t, main["sources"], "main.sources")
	var sum float64
	for _, s := range sources {
		sum += compObj(t, s, "source")["input_tokens"].(float64)
	}
	if math.Abs(sum-50700) > compTol {
		t.Errorf("sum of sources input_tokens = %v, want 50700", sum)
	}
	compWantNum(t, compFind(t, sources, "source", "system_and_tools", "sources"), "input_tokens", 49500, "system_and_tools") // 9900*5
	compWantNum(t, compFind(t, sources, "source", "user_turn", "sources"), "input_tokens", 550, "user_turn")                 // 100*5 + 50*1
	compWantNum(t, compFind(t, sources, "source", "tool:Read", "sources"), "input_tokens", 650, "tool:Read")                 // d2 200 + d3 300 + d4 150
	// Rev 3: new_tokens are the tokens at first entry, before the d4 reduction.
	compWantNum(t, compFind(t, sources, "source", "tool:Read", "sources"), "new_tokens", 300, "tool:Read")                // a.go 200 + b.go 100
	compWantNum(t, compFind(t, sources, "source", "user_turn", "sources"), "new_tokens", 150, "user_turn")                // 100 + 50 after compaction
	compWantNum(t, compFind(t, sources, "source", "system_and_tools", "sources"), "new_tokens", 9900, "system_and_tools") // 10000-100
	ao := compFind(t, sources, "source", "assistant_output", "sources")
	compWantNum(t, ao, "blocks", 5, "assistant_output") // O1..O5
	compWantNum(t, ao, "new_tokens", 0, "assistant_output")
	// The 40 B result of "ls /x/bin/cw cat" arrived in P of d4 (d<0) and got 0 tokens.
	lsBlock := compFind(t, sources, "source", "tool:Bash", "sources")
	compWantStr(t, lsBlock, "category", "tool_output", "tool:Bash")
	compWantNum(t, lsBlock, "input_tokens", 0, "tool:Bash")
	compWantNum(t, lsBlock, "new_tokens", 0, "tool:Bash")
	for _, s := range sources {
		if name, _ := compObj(t, s, "source")["source"].(string); strings.HasPrefix(name, "cw ") {
			t.Errorf("source %q: \"ls /x/bin/cw cat\" must not be a cw call", name)
		}
	}
}

// ---------------------------------------------------------------------------
// codex
// ---------------------------------------------------------------------------

func TestMetricsCompositionCodexUnsupported(t *testing.T) {
	const id = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	path := filepath.Join(t.TempDir(), "sessions", "rollout-2026-11-07T10-00-00-"+id+".jsonl")
	writeLines(t, path,
		mustJSON(t, map[string]any{"type": "session_meta", "payload": map[string]any{"id": id}}),
		mustJSON(t, map[string]any{"type": "turn_context", "payload": map[string]any{"model": "gpt-x"}}),
		mustJSON(t, map[string]any{"type": "token_usage_record", "payload": map[string]any{
			"response_id": "r1",
			"thread_id":   id,
			"usage": map[string]any{
				"input_tokens":             100,
				"cached_input_tokens":      0,
				"cache_write_input_tokens": 0,
				"output_tokens":            10,
				"reasoning_output_tokens":  0,
			},
		}}),
	)
	prices := writePrices(t, map[string]rates{"gpt-x": {1, 2, 1.25, 2, 0.1}})
	comp := compComposition(t, compImport(t, "--codex-session", path, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "unsupported", "composition")
	reason, _ := comp["reason"].(string)
	if !strings.Contains(reason, "Codex") || !strings.Contains(reason, "cost") {
		t.Errorf("reason = %q, want it to mention Codex and cost", reason)
	}
	compWantNull(t, comp, "reconciliation", "codex composition")
	compWantNull(t, comp, "agents", "codex composition")
}

// ---------------------------------------------------------------------------
// attribution + repeats: even sample count and raw (non-canonical) tool input
// ---------------------------------------------------------------------------
//
// Main only (opus):
//   skill_listing attachment 450 B, user prompt 1350 B (initial, 1800 B)
//   e1 T=10000 (w1h 10000)           out 0  Bash "go build ./..." (x1)
//   x1 result 200 B
//   e2 T=10100 (read 10000, w1h 100) out 0  d=R=100, sample 200/100=2;  Grep raw input 1 (x2)
//   x2 result 400 B
//   e3 T=10200 (read 10100, w1h 100) out 0  R=100, sample 400/100=4;     Grep raw input 2 (x3)
//   x3 result 500 B
//   e4 T=10300 (read 10200, w1h 100) out 0  R=100, sample 500/100=5;     Bash "go test ./..." (x4)
//   x4 result 600 B
//   e5 T=10400 (read 10300, w1h 100) out 10 R=100, sample 600/100=6
//   samples {2,4,5,6}: even count -> r = (4+5)/2 = 4.5
//     (lower middle 4 -> E 450, upper 5 -> E 360, mean of all 4.25 -> E 424)
//   E = round(1800/4.5) = 400: skill_listing 450/1800*400 = 100, user_turn 1350/1800*400 = 300,
//   system_and_tools 10000-400 = 9600.
//   input_tokens = 10000+10100+10200+10300+10400 = 51000
//   input $ = 10000*8e-6 + 40600*0.2e-6 + 400*8e-6 = 0.08+0.00812+0.0032 = 0.09132
//   output $ = 10*20e-6 = 0.0002; total 0.09152

// Two Grep inputs with the same canonical form but different raw bytes.
const (
	compRawGrep1 = `{"pattern": "TODO", "path": "/repo", "-i": true}`
	compRawGrep2 = `{ "path" : "/repo" ,  "-i":true,   "pattern":"TODO" }`
	// RFC 8785: keys sorted by UTF-16 code units ("-" < "path" < "pattern"), no whitespace.
	compCanonGrep = `{"-i":true,"path":"/repo","pattern":"TODO"}`
)

// compAssistantRawInput writes a tool_use line whose input is the given raw JSON text verbatim.
func compAssistantRawInput(t *testing.T, requestID string, u compUsage, id, name, rawInput string) string {
	t.Helper()
	line := compAssistant(t, requestID, opusModel, u, compToolUse(id, name, nil))
	if strings.Count(line, `"input":null`) != 1 {
		t.Fatalf("cannot place raw input in %s", line)
	}
	line = strings.Replace(line, `"input":null`, `"input":`+rawInput, 1)
	if !json.Valid([]byte(line)) {
		t.Fatalf("raw input line is not JSON: %s", line)
	}
	return line
}

func compWriteFixtureE(t *testing.T) (mainPath, prices string) {
	t.Helper()
	dir := t.TempDir()
	mainPath = filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	writeLines(t, mainPath,
		compAttachment(t, "skill_listing", compFill(450, 'a')),
		compUserPrompt(t, compFill(1350, 'b')),
		compAssistant(t, "e1", o, compUsage{w1h: 10000}, compToolUse("x1", "Bash", map[string]any{"command": "go build ./..."})),
		compToolResult(t, "x1", compFill(200, 'c'), nil),
		compAssistantRawInput(t, "e2", compUsage{read: 10000, w1h: 100}, "x2", "Grep", compRawGrep1),
		compToolResult(t, "x2", compFill(400, 'd'), nil),
		compAssistantRawInput(t, "e3", compUsage{read: 10100, w1h: 100}, "x3", "Grep", compRawGrep2),
		compToolResult(t, "x3", compFill(500, 'e'), nil),
		compAssistant(t, "e4", o, compUsage{read: 10200, w1h: 100}, compToolUse("x4", "Bash", map[string]any{"command": "go test ./..."})),
		compToolResult(t, "x4", compFill(600, 'f'), nil),
		compAssistant(t, "e5", o, compUsage{read: 10300, w1h: 100, out: 10}, compTextItem("done")),
		compCostState(t, 0.09152, false, map[string]compModelUsage{
			// reads 10000+10100+10200+10300=40600; 1h writes 10000+4*100=10400; out 10
			opusModel: {read: 40600, w1h: 10400, out: 10, cost: 0.09152},
		}),
	)
	return mainPath, writePrices(t, map[string]rates{opusModel: opusRates})
}

func TestMetricsCompositionAttributionEvenSampleMedian(t *testing.T) {
	mainPath, prices := compWriteFixtureE(t)
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantNum(t, main, "bytes_per_token", 4.5, "main") // (4+5)/2 of {2,4,5,6}
	compWantNum(t, main, "initial_context_tokens", 10000, "main")
	compWantNum(t, main, "input_tokens", 51000, "main")
	compWantNum(t, main, "input_dollars", 0.09132, "main")
	sources := compArr(t, main["sources"], "main.sources")
	compWantNum(t, compFind(t, sources, "source", "skill_listing", "sources"), "new_tokens", 100, "skill_listing")        // 450/1800*400
	compWantNum(t, compFind(t, sources, "source", "user_turn", "sources"), "new_tokens", 300, "user_turn")                // 1350/1800*400
	compWantNum(t, compFind(t, sources, "source", "system_and_tools", "sources"), "new_tokens", 9600, "system_and_tools") // 10000-400
}

func TestMetricsCompositionRepeatsCanonicalJSONRawInput(t *testing.T) {
	mainPath, prices := compWriteFixtureE(t)
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	main := compAgent(t, comp, "main")
	groups := compArr(t, main["repeated_calls"], "main.repeated_calls")
	if len(groups) != 1 {
		t.Fatalf("main.repeated_calls = %v, want exactly one Grep group", groups)
	}
	g := compObj(t, groups[0], "repeated_call")
	canon := sha256.Sum256([]byte(compCanonGrep))
	raw1 := sha256.Sum256([]byte(compRawGrep1))
	raw2 := sha256.Sum256([]byte(compRawGrep2))
	compWantStr(t, g, "tool", "Grep", "repeated")
	compWantStr(t, g, "input_sha256", hex.EncodeToString(canon[:]), "repeated")
	if h, _ := g["input_sha256"].(string); h == hex.EncodeToString(raw1[:]) || h == hex.EncodeToString(raw2[:]) {
		t.Errorf("input_sha256 %s hashes raw bytes, want the canonical form", h)
	}
	compWantStr(t, g, "label", "TODO", "repeated") // Grep label = pattern
	compWantNum(t, g, "calls", 2, "repeated")
	compWantNum(t, g, "result_tokens", 200, "repeated") // x2 result 100 (in e3) + x3 result 100 (in e4)
}

// ---------------------------------------------------------------------------
// stale-phase: the boundary request's own output block
// ---------------------------------------------------------------------------
//
// Main only (opus):
//   user prompt 400 B (initial)
//   f1 T=10000 (w1h 10000)           out 30 Bash "cw task start" (q1) -> boundary, request 2; O1 phase 1
//   q1 result 200 B (phase 2)
//   f2 T=10080 (read 10000, w1h 80)  out 0  d=80=30+50, R=50, sample 200/50=4; Bash "go test ./..." (q2)
//   q2 result 100 B (phase 2)
//   f3 T=10105 (read 10080, w1h 25)  out 0  d=25=0+25, R=25, sample 100/25=4
//   r = 4; E = round(400/4) = 100; system_and_tools 9900.
//   f2 (phase 2): O1 (30, phase 1) is read for the first time -> not stale.
//   f3 (phase 2): O1 re-read -> stale 30 tokens, 30*0.2e-6 = 0.000006 $ (read segment,
//   read 10080 = initial 10000 + O1 30 + q1 result 50).
//   prefix_rewrite_dollars = 10000*1*8e-6 = 0.08
//   input $ = 0.08 + (0.002+0.00064) + (0.002016+0.0002) = 0.084856; output $ 30*20e-6 = 0.0006

func TestMetricsCompositionStalePhaseBoundaryOutputFirstRead(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "f1", o, compUsage{w1h: 10000, out: 30}, compToolUse("q1", "Bash", map[string]any{"command": "cw task start --title wp7"})),
		compToolResult(t, "q1", compFill(200, 'b'), nil),
		compAssistant(t, "f2", o, compUsage{read: 10000, w1h: 80}, compToolUse("q2", "Bash", map[string]any{"command": "go test ./..."})),
		compToolResult(t, "q2", compFill(100, 'c'), nil),
		compAssistant(t, "f3", o, compUsage{read: 10080, w1h: 25}, compTextItem("done")),
		compCostState(t, 0.085456, false, map[string]compModelUsage{
			// reads 10000+10080=20080; 1h writes 10000+80+25=10105; out 30
			opusModel: {read: 20080, w1h: 10105, out: 30, cost: 0.085456},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantStr(t, main, "role", "coordinator", "main")
	compWantNum(t, main, "input_tokens", 30185, "main")     // 10000+10080+10105
	compWantNum(t, main, "input_dollars", 0.084856, "main") // header
	compWantNum(t, main, "output_dollars", 0.0006, "main")  // 30*20e-6
	ph := compObj(t, main["phases"], "main.phases")
	compWantNum(t, ph, "phase_count", 2, "phases")
	bounds := compArr(t, ph["boundaries"], "phases.boundaries")
	if len(bounds) != 1 {
		t.Fatalf("boundaries = %v, want 1", bounds)
	}
	b := compObj(t, bounds[0], "boundary")
	compWantStr(t, b, "kind", "task_start", "boundary")
	compWantNum(t, b, "request", 2, "boundary")
	compWantNum(t, ph, "stale_reread_tokens", 30, "phases")        // O1 in f3 only (not in f2: first read)
	compWantNum(t, ph, "stale_reread_dollars", 0.000006, "phases") // 30*0.2e-6
	compWantNum(t, ph, "stale_input_dollars", 0.000006, "phases")  // O1 inside f3's read segment
	compWantNum(t, ph, "prefix_rewrite_dollars", 0.08, "phases")   // 10000*1*8e-6
}

// ---------------------------------------------------------------------------
// Revision 4 helpers
// ---------------------------------------------------------------------------

// compOtherSession rewrites a line built for sessionID to another session id.
func compOtherSession(t *testing.T, line, session string) string {
	t.Helper()
	from := `"sessionId":"` + sessionID + `"`
	if strings.Count(line, from) != 1 {
		t.Fatalf("cannot rewrite session in %s", line)
	}
	return strings.Replace(line, from, `"sessionId":"`+session+`"`, 1)
}

// compPadded returns prefix + filler + suffix of exactly total bytes.
func compPadded(t *testing.T, prefix, suffix string, total int) string {
	t.Helper()
	n := total - len(prefix) - len(suffix)
	if n < 0 {
		t.Fatalf("padding target %d too small for %q...%q", total, prefix, suffix)
	}
	return prefix + compFill(n, ' ') + suffix
}

func compSourceNames(t *testing.T, agent map[string]any) []string {
	t.Helper()
	var names []string
	for _, s := range compArr(t, agent["sources"], "sources") {
		name, _ := compObj(t, s, "source")["source"].(string)
		names = append(names, name)
	}
	return names
}

// ---------------------------------------------------------------------------
// stale-phase (rev 4, section 3a/6): cw mentioned only in non-command positions
// ---------------------------------------------------------------------------
//
// Main only (opus):
//   user prompt 400 B (initial)
//   h1 T=10000 (w1h 10000) out 30, three Bash calls (k1 heredoc to a file, k2 grep of a
//      quoted string, k3 python heredoc), none of which is a cw call
//   results k1 100 B, k2 100 B, k3 200 B
//   h2 T=10130 (read 10000, w1h 130) out 20  d=130=30+100, R=100, sample 400/100=4
//      shares k1 25, k2 25, k3 50; Bash k4 "cw task verify ... && cw task verify ..."
//   k4 result 200 B
//   h3 T=10200 (read 10130, w1h 70)  out 10  d=70=20+50, R=50, sample 200/50=4
//   r=4, E=100, system_and_tools 9900. input_tokens 10000+10130+10200 = 30330.
//   One boundary (task_verify, request 3), phase_count 2.
//   Stale in h3: O1 30 + k1..k3 results 100 (first read in h2) = 130; O2 (k4's own
//   block, phase 1) is first read in h3 -> not stale. $ 130*0.2e-6 = 0.000026.
//   input $ = 0.08 + (0.002+0.00104) + (0.002026+0.00056) = 0.085626; output 60*20e-6 = 0.0012

func TestMetricsCompositionStalePhaseIgnoresNonCommandMentions(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	u1 := compUsage{w1h: 10000, out: 30}
	k1 := "cat > /tmp/plan.json <<'EOF'\nphases: cw task start, cw task consult, cw task verify\nEOF"
	k2 := `grep -n "cw task start" notes.md`
	k3 := "python3 - <<'PY'\nprint('cw task verify')\nPY"
	k4 := "cw task verify --task t && cw task verify --task t"
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "h1", o, u1, compToolUse("k1", "Bash", map[string]any{"command": k1})),
		compAssistant(t, "h1", o, u1, compToolUse("k2", "Bash", map[string]any{"command": k2})),
		compAssistant(t, "h1", o, u1, compToolUse("k3", "Bash", map[string]any{"command": k3})),
		compToolResult(t, "k1", compFill(100, 'b'), nil),
		compToolResult(t, "k2", compFill(100, 'c'), nil),
		compToolResult(t, "k3", compFill(200, 'd'), nil),
		compAssistant(t, "h2", o, compUsage{read: 10000, w1h: 130, out: 20}, compToolUse("k4", "Bash", map[string]any{"command": k4})),
		compToolResult(t, "k4", compFill(200, 'e'), nil),
		compAssistant(t, "h3", o, compUsage{read: 10130, w1h: 70, out: 10}, compTextItem("done")),
		compCostState(t, 0.086826, false, map[string]compModelUsage{
			// reads 10000+10130=20130; 1h writes 10000+130+70=10200; out 30+20+10=60
			opusModel: {read: 20130, w1h: 10200, out: 60, cost: 0.086826},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantStr(t, main, "role", "main", "main") // no real cw task start
	compWantNum(t, main, "input_tokens", 30330, "main")
	compWantNum(t, main, "input_dollars", 0.085626, "main")

	sources := compArr(t, main["sources"], "main.sources")
	for _, name := range compSourceNames(t, main) {
		if strings.HasPrefix(name, "cw ") && name != "cw task verify" {
			t.Errorf("unexpected cw source %q from a non-command mention", name)
		}
	}
	bash := compFind(t, sources, "source", "tool:Bash", "sources")
	compWantNum(t, bash, "blocks", 3, "tool:Bash")       // k1, k2, k3
	compWantNum(t, bash, "new_tokens", 100, "tool:Bash") // 25+25+50
	verify := compFind(t, sources, "source", "cw task verify", "sources")
	compWantNum(t, verify, "blocks", 1, "cw task verify")
	compWantNum(t, verify, "new_tokens", 50, "cw task verify")

	ph := compObj(t, main["phases"], "main.phases")
	bounds := compArr(t, ph["boundaries"], "phases.boundaries")
	if len(bounds) != 1 {
		t.Fatalf("boundaries = %v, want one task_verify", bounds)
	}
	b := compObj(t, bounds[0], "boundary")
	compWantStr(t, b, "kind", "task_verify", "boundary")
	compWantNum(t, b, "request", 3, "boundary")
	compWantNum(t, ph, "phase_count", 2, "phases")
	compWantNum(t, ph, "stale_reread_tokens", 130, "phases")       // O1 30 + 100
	compWantNum(t, ph, "stale_reread_dollars", 0.000026, "phases") // 130*0.2e-6
	compWantNum(t, ph, "prefix_rewrite_dollars", 0.08, "phases")   // 10000*1*8e-6
}

// ---------------------------------------------------------------------------
// roles + stale-phase (rev 4): review briefs written by heredocs
// ---------------------------------------------------------------------------
//
// Main (opus): user prompt 400 B; i1 T=10000 (w1h) out 50 with five calls:
//   n1 Bash: heredoc brief -> /x/brief.json, then on the next line cw task assign --input /x/brief.json
//   n2 Agent "helper one" -> agentId v1
//   n3 Bash: heredoc brief -> /x/brief2.json only
//   n4 Bash: cw task assign --input /x/brief2.json
//   n5 Agent "helper two" -> agentId v2
//   results 5 x 80 B = 400 B; i2 T=10150 (read 10000, w1h 150) out 10: d=150=50+100, sample 4.
//   main $ = 0.08 + 0.002 + 0.0012 + 60*20e-6 = 0.0844
// Subagents v1, v2 (sonnet): brief 400 B, one request T=1000 (w1h) out 10: 0.004+0.0001 = 0.0041 each.
// Without the briefs both descriptions/types would map to "other".

func TestMetricsCompositionRolesHeredocReviewBrief(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	u1 := compUsage{w1h: 10000, out: 50}
	n1 := "cat > /x/brief.json <<'EOF'\n{\"id\": \"rv-1\", \"role\": \"review\", \"brief\": \"check the diff\"}\nEOF\ncw task assign --task t --input /x/brief.json"
	n3 := "cat > /x/brief2.json <<'EOF'\n{\"id\": \"rv-2\", \"role\": \"review\", \"brief\": \"confirm the fixes\"}\nEOF"
	n4 := "cw task assign --task t --input /x/brief2.json"
	agent := func(desc, briefID string) map[string]any {
		return map[string]any{"description": desc, "subagent_type": "general-purpose", "prompt": "Assignment " + briefID + ": do it"}
	}
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "i1", o, u1, compToolUse("n1", "Bash", map[string]any{"command": n1})),
		compAssistant(t, "i1", o, u1, compToolUse("n2", "Agent", agent("helper one", "rv-1"))),
		compAssistant(t, "i1", o, u1, compToolUse("n3", "Bash", map[string]any{"command": n3})),
		compAssistant(t, "i1", o, u1, compToolUse("n4", "Bash", map[string]any{"command": n4})),
		compAssistant(t, "i1", o, u1, compToolUse("n5", "Agent", agent("helper two", "rv-2"))),
		compToolResult(t, "n1", compFill(80, 'b'), nil),
		compToolResult(t, "n2", compFill(80, 'c'), map[string]any{"agentId": "v1", "status": "completed"}),
		compToolResult(t, "n3", compFill(80, 'd'), nil),
		compToolResult(t, "n4", compFill(80, 'e'), nil),
		compToolResult(t, "n5", compFill(80, 'f'), map[string]any{"agentId": "v2", "status": "completed"}),
		compAssistant(t, "i2", o, compUsage{read: 10000, w1h: 150, out: 10}, compTextItem("done")),
		compCostState(t, 0.0926, false, map[string]compModelUsage{
			opusModel:   {read: 10000, w1h: 10150, out: 60, cost: 0.0844}, // 0.0844 (header)
			sonnetModel: {w1h: 2000, out: 20, cost: 0.0082},               // 2 x 0.0041
		}),
	)
	for _, id := range []string{"v1", "v2"} {
		writeLines(t, compSubagentPath(dir, id),
			compUserPrompt(t, compFill(400, 'p')),
			compAssistant(t, id+"-q1", sonnetModel, compUsage{w1h: 1000, out: 10}, compTextItem("ok")),
		)
		desc := map[string]string{"v1": "helper one", "v2": "helper two"}[id]
		writeLines(t, compMetaPath(dir, id), mustJSON(t, map[string]any{"agentType": "general-purpose", "description": desc}))
	}
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")

	v1 := compAgent(t, comp, "agent-v1")
	compWantStr(t, v1, "role", "review", "agent-v1") // first review brief
	compWantStr(t, v1, "role_basis", "cw_task_assign", "agent-v1")
	v2 := compAgent(t, comp, "agent-v2")
	compWantStr(t, v2, "role", "review_followup", "agent-v2") // second review brief
	compWantStr(t, v2, "role_basis", "cw_task_assign", "agent-v2")

	main := compAgent(t, comp, "main")
	sources := compArr(t, main["sources"], "main.sources")
	compWantNum(t, compFind(t, sources, "source", "cw task assign", "sources"), "blocks", 2, "cw task assign") // n1, n4
	compWantNum(t, compFind(t, sources, "source", "tool:Bash", "sources"), "blocks", 1, "tool:Bash")           // n3 (heredoc only)
	ph := compObj(t, main["phases"], "main.phases")
	bounds := compArr(t, ph["boundaries"], "phases.boundaries")
	if len(bounds) != 2 {
		t.Fatalf("boundaries = %v, want 2 review_assign", bounds)
	}
	for _, b := range bounds {
		m := compObj(t, b, "boundary")
		compWantStr(t, m, "kind", "review_assign", "boundary")
		compWantNum(t, m, "request", 2, "boundary") // both issued in i1
	}
	compWantNum(t, ph, "phase_count", 3, "phases")
}

// ---------------------------------------------------------------------------
// attribution (rev 4): resumed session with copied history
// ---------------------------------------------------------------------------
//
// Main transcript s1 begins with history from session s0:
//   H1 assistant (s0, has usage) tool_use Read {"file_path":"/repo/old.go"} (hr1)
//   H2 tool_result hr1 (s0) 200 B
//   H3 compact_boundary (s0)       -> history compaction: H1, H2 dropped, compactions stays 0
//   H4 user prompt (s0) 300 B      -> user_turn
//   H5 assistant (s0, has usage) tool_use Read, same input (hr2): visible bytes
//      "Read" 4 + raw input `{"file_path":"/repo/old.go"}` 28 = 32 -> assistant_output
//   H6 tool_result hr2 (s0) 468 B  -> tool:Read
// Own lines:
//   user prompt 400 B
//   j1 T=10000 (w1h 10000) out 20 Bash "go test ./..." (jt1)
//   jt1 result 400 B
//   j2 T=10120 (read 10000, w1h 120) out 10  d=120=20+100, R=100, sample 400/100=4
// r=4; first-turn blocks H4 300 + H5 32 + H6 468 + prompt 400 = 1200 B, E = 300:
//   H4 300/1200*300=75, H5 32/1200*300=8, H6 468/1200*300=117, prompt 400/1200*300=100;
//   system_and_tools 9700. new: user_turn 175, assistant_output 8+20=28, tool:Read 117,
//   tool:Bash 100; 9700+175+28+117+100 = 10120 = T2.
// input $ = 0.08 + 0.002 + 0.00096 = 0.08296; output 30*20e-6 = 0.0006; total 0.08356.

func TestMetricsCompositionAttributionResumedSessionHistory(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	const other = "s0"
	readInput := map[string]any{"file_path": "/repo/old.go"}
	if raw := mustJSON(t, readInput); len(raw) != 28 {
		t.Fatalf("fixture: raw Read input %q is %d bytes, want 28", raw, len(raw))
	}
	writeLines(t, mainPath,
		compOtherSession(t, compAssistant(t, "hist1", o, compUsage{w1h: 5000, out: 7}, compToolUse("hr1", "Read", readInput)), other),
		compOtherSession(t, compToolResult(t, "hr1", compFill(200, 'a'), nil), other),
		compOtherSession(t, compCompactBoundary(t), other),
		compOtherSession(t, compUserPrompt(t, compFill(300, 'b')), other),
		compOtherSession(t, compAssistant(t, "hist2", o, compUsage{read: 5000, w1h: 300, out: 9}, compToolUse("hr2", "Read", readInput)), other),
		compOtherSession(t, compToolResult(t, "hr2", compFill(468, 'c'), nil), other),
		compUserPrompt(t, compFill(400, 'd')),
		compAssistant(t, "j1", o, compUsage{w1h: 10000, out: 20}, compToolUse("jt1", "Bash", map[string]any{"command": "go test ./..."})),
		compToolResult(t, "jt1", compFill(400, 'e'), nil),
		compAssistant(t, "j2", o, compUsage{read: 10000, w1h: 120, out: 10}, compTextItem("done")),
		compCostState(t, 0.08356, false, map[string]compModelUsage{
			// own requests only: reads 10000; 1h writes 10000+120=10120; out 20+10=30
			opusModel: {read: 10000, w1h: 10120, out: 30, cost: 0.08356},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantNum(t, main, "requests", 2, "main") // history lines are not requests
	compWantNum(t, main, "input_tokens", 20120, "main")
	compWantNum(t, main, "input_dollars", 0.08296, "main")
	compWantNum(t, main, "compactions", 0, "main") // history compaction is not counted
	compWantNum(t, main, "bytes_per_token", 4, "main")
	compWantNum(t, main, "initial_context_tokens", 10000, "main")

	for _, name := range compSourceNames(t, main) {
		if name == "tool:unknown" {
			t.Errorf("history tool_result attributed to tool:unknown")
		}
	}
	sources := compArr(t, main["sources"], "main.sources")
	compWantNum(t, compFind(t, sources, "source", "tool:Read", "sources"), "new_tokens", 117, "tool:Read")                // H2 dropped (0) + H6 117
	compWantNum(t, compFind(t, sources, "source", "user_turn", "sources"), "new_tokens", 175, "user_turn")                // 75 + 100
	compWantNum(t, compFind(t, sources, "source", "assistant_output", "sources"), "new_tokens", 28, "assistant_output")   // H5 8 + O1 20
	compWantNum(t, compFind(t, sources, "source", "system_and_tools", "sources"), "new_tokens", 9700, "system_and_tools") // 10000-300
	compWantNum(t, compFind(t, sources, "source", "tool:Bash", "sources"), "new_tokens", 100, "tool:Bash")

	// History tool_use items count for repeats: Read x2, results H2 0 + H6 117.
	groups := compArr(t, main["repeated_calls"], "main.repeated_calls")
	g := compFind(t, groups, "tool", "Read", "repeated_calls")
	compWantStr(t, g, "label", "/repo/old.go", "repeated Read")
	compWantNum(t, g, "calls", 2, "repeated Read")
	compWantNum(t, g, "result_tokens", 117, "repeated Read")
}

// ---------------------------------------------------------------------------
// roles (rev 4): a subagent with zero requests
// ---------------------------------------------------------------------------

func TestMetricsCompositionRolesZeroRequestAgentExcluded(t *testing.T) {
	dir, mainPath, prices := compWriteFixtureA(t, compDefaultCostA())
	// agent-z9 holds only a prompt; its description would map to "review" if counted.
	writeLines(t, compSubagentPath(dir, "z9"), compUserPrompt(t, compFill(100, 'z')))
	writeLines(t, compMetaPath(dir, "z9"), mustJSON(t, map[string]any{"agentType": "general-purpose", "description": "Code review"}))
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")

	z := compAgent(t, comp, "agent-z9")
	compWantNum(t, z, "requests", 0, "agent-z9")
	compWantNum(t, z, "input_tokens", 0, "agent-z9")

	cov := compObj(t, comp["role_coverage"], "role_coverage")
	compWantNum(t, cov, "agents", 2, "role_coverage") // main + agent-a1 only
	compWantNum(t, cov, "mapped_agents", 2, "role_coverage")
	compWantNum(t, cov, "input_tokens", 111540, "role_coverage") // fixture A total, unchanged
	roles := compArr(t, comp["roles"], "roles")
	var total float64
	var names []string
	for _, r := range roles {
		m := compObj(t, r, "role")
		total += m["agents"].(float64)
		names = append(names, m["role"].(string))
	}
	if total != 2 || strings.Join(names, ",") != "main,test_author" {
		t.Errorf("roles = %v (agents %v), want main,test_author with 2 agents", names, total)
	}
}

// ---------------------------------------------------------------------------
// attribution (rev 4): compaction summary and task notifications
// ---------------------------------------------------------------------------
//
// Main only (opus):
//   user prompt 400 B (initial)
//   g1 T=10000 (w1h 10000) out 0  Bash "go test ./..." (y1)
//   y1 result 400 B                         -> tool:Bash
//   queued_command 200 B, <task-notification> + <summary>Agent "x" finished  -> agent_result
//   queued_command 200 B, <task-notification> + <summary>Background command  -> context:task_notification
//   g2 T=10200 (read 10000, w1h 200) out 0  d=R=200, sample 800/200=4; shares 100, 50, 50
//   compact_boundary (own session, after the first request -> real compaction)
//   isCompactSummary user entry 300 B       -> compaction_summary (context)
//   user prompt 100 B
//   g3 T=10100 (read 10000, w1h 100) out 10  d=10100-10000=100, sample 400/100=4; shares 75, 25
//   r=4, E=100, system_and_tools 9900. input_tokens 10000+10200+10100 = 30300.
//   input $ = 0.08 + (0.002+0.0016) + (0.002+0.0008) = 0.0864; output 10*20e-6 = 0.0002

func TestMetricsCompositionAttributionCompactionSummaryAndNotifications(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	agentDone := compPadded(t, "<task-notification>\n<task-id>bg1</task-id>\n<summary>Agent \"x\" finished</summary>\n", "</task-notification>", 200)
	bgDone := compPadded(t, "<task-notification>\n<task-id>bg2</task-id>\n<summary>Background command \"go test\" completed</summary>\n", "</task-notification>", 200)
	summary := mustJSON(t, map[string]any{
		"type":             "user",
		"sessionId":        sessionID,
		"isCompactSummary": true,
		"message":          map[string]any{"role": "user", "content": compFill(300, 's')},
	})
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "g1", o, compUsage{w1h: 10000}, compToolUse("y1", "Bash", map[string]any{"command": "go test ./..."})),
		compToolResult(t, "y1", compFill(400, 'b'), nil),
		compAttachment(t, "queued_command", agentDone),
		compAttachment(t, "queued_command", bgDone),
		compAssistant(t, "g2", o, compUsage{read: 10000, w1h: 200}, compTextItem("waiting")),
		compCompactBoundary(t),
		summary,
		compUserPrompt(t, compFill(100, 'c')),
		compAssistant(t, "g3", o, compUsage{read: 10000, w1h: 100, out: 10}, compTextItem("done")),
		compCostState(t, 0.0866, false, map[string]compModelUsage{
			// reads 10000+10000=20000; 1h writes 10000+200+100=10300; out 10
			opusModel: {read: 20000, w1h: 10300, out: 10, cost: 0.0866},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantNum(t, main, "input_tokens", 30300, "main")
	compWantNum(t, main, "input_dollars", 0.0864, "main")
	compWantNum(t, main, "compactions", 1, "main")

	sources := compArr(t, main["sources"], "main.sources")
	cs := compFind(t, sources, "source", "compaction_summary", "sources")
	compWantStr(t, cs, "category", "context", "compaction_summary")
	compWantNum(t, cs, "new_tokens", 75, "compaction_summary")   // 300/400*100
	compWantNum(t, cs, "input_tokens", 75, "compaction_summary") // g3 only
	ar := compFind(t, sources, "source", "agent_result", "sources")
	compWantStr(t, ar, "category", "agent_handoff", "agent_result")
	compWantNum(t, ar, "new_tokens", 50, "agent_result")   // 200/800*200
	compWantNum(t, ar, "input_tokens", 50, "agent_result") // g2 only, dropped at compaction
	tn := compFind(t, sources, "source", "context:task_notification", "sources")
	compWantStr(t, tn, "category", "context", "context:task_notification")
	compWantNum(t, tn, "new_tokens", 50, "context:task_notification")
	compWantNum(t, compFind(t, sources, "source", "tool:Bash", "sources"), "new_tokens", 100, "tool:Bash")   // 400/800*200
	compWantNum(t, compFind(t, sources, "source", "user_turn", "sources"), "input_tokens", 325, "user_turn") // 100*3 + 25
}

// ---------------------------------------------------------------------------
// roles (rev 5/6): briefs are paired with Agent calls by the id the prompt names
// ---------------------------------------------------------------------------
//
// Main (opus): user prompt 400 B; request k1 T=10000 (w1h) out 180 with 18 calls (below);
// results 18 x 50 B = 900 B; k2 T=10405 (read 10000, w1h 405) out 10: d=405=180+225,
// R=225, sample 900/225=4. main $ = 0.08 + 0.002 + 0.00324 + 190*20e-6 = 0.08904.
// 8 subagents w1..w8 (sonnet), brief 400 B, one request T=1000 (w1h) out 10: 0.0041 each,
// 0.0328 in all. Session 0.12184.
//
// Expected roles:
//   w1 "helper a", prompt names ev-1 (brief id; task_id "ev-0" is not an id), after the
//      coordinator's own coord-1 assignment that no Agent names  -> evidence / cw_task_assign
//   w2 "Fix the build", prompt names no id after pending x-9      -> implementation / description
//   w3 "helper c", prompt names rj-1, a jq brief written by `jq -n ... > /x/rj-1.json`
//                                                                  -> review / cw_task_assign
//   w4 "helper d", prompt names review-10 (review-1 also exists)   -> verification / cw_task_assign
//   w5 "helper e", prompt names review-1 (test author brief text)  -> test_author / cw_task_assign
//   w6 "Local commit of the fix", evidence brief gh-1              -> github / description (rev 6)
//   w7 "Test author: metrics", implementation brief ta-1           -> test_author / description (rev 6)
//   w8 "Apply the fix", implementation brief fx-1                  -> implementation / cw_task_assign (rev 6)

func TestMetricsCompositionRolesBriefIDPairing(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	type call struct {
		id, name string
		input    map[string]any
		agentID  string
	}
	bash := func(cmd string) map[string]any { return map[string]any{"command": cmd} }
	assign := func(brief string) map[string]any {
		return bash("cw task assign --task t --json '" + brief + "'")
	}
	agent := func(desc, prompt string) map[string]any {
		return map[string]any{"description": desc, "subagent_type": "general-purpose", "prompt": prompt}
	}
	calls := []call{
		{"a1", "Bash", assign(`{"id":"coord-1","role":"implementation","brief":"coordinator refactor"}`), ""},
		{"a2", "Bash", assign(`{"task_id":"ev-0","id":"ev-1","role":"evidence","brief":"collect logs"}`), ""},
		{"a3", "Agent", agent("helper a", "Handle assignment ev-1 now."), "w1"},
		{"a4", "Bash", assign(`{"id":"x-9","role":"verification","brief":"verify"}`), ""},
		{"a5", "Agent", agent("Fix the build", "please repair the build"), "w2"},
		{"a6", "Bash", bash(`jq -n '{id:"rj-1",role:"review",brief:"jq brief"}' > /x/rj-1.json`), ""},
		{"a7", "Bash", bash("cw task assign --task t --input /x/rj-1.json"), ""},
		{"a8", "Agent", agent("helper c", "Work on rj-1."), "w3"},
		{"a9", "Bash", assign(`{"id":"review-1","role":"implementation","brief":"independent test author pass"}`), ""},
		{"a10", "Bash", assign(`{"id":"review-10","role":"verification","brief":"verify again"}`), ""},
		{"a11", "Agent", agent("helper d", "Take review-10 next."), "w4"},
		{"a12", "Agent", agent("helper e", "Own review-1."), "w5"},
		{"a13", "Bash", assign(`{"id":"gh-1","role":"evidence","brief":"commit"}`), ""},
		{"a14", "Agent", agent("Local commit of the fix", "Assignment gh-1."), "w6"},
		{"a15", "Bash", assign(`{"id":"ta-1","role":"implementation","brief":"write tests"}`), ""},
		{"a16", "Agent", agent("Test author: metrics", "Assignment ta-1."), "w7"},
		{"a17", "Bash", assign(`{"id":"fx-1","role":"implementation","brief":"apply it"}`), ""},
		{"a18", "Agent", agent("Apply the fix", "Assignment fx-1."), "w8"},
	}
	lines := []string{compUserPrompt(t, compFill(400, 'a'))}
	for _, c := range calls {
		lines = append(lines, compAssistant(t, "k1", o, compUsage{w1h: 10000, out: 180}, compToolUse(c.id, c.name, c.input)))
	}
	descs := map[string]string{}
	for _, c := range calls {
		var res map[string]any
		if c.agentID != "" {
			res = map[string]any{"agentId": c.agentID, "status": "completed"}
			descs[c.agentID] = c.input["description"].(string)
		}
		lines = append(lines, compToolResult(t, c.id, compFill(50, 'r'), res))
	}
	lines = append(lines,
		compAssistant(t, "k2", o, compUsage{read: 10000, w1h: 405, out: 10}, compTextItem("done")),
		compCostState(t, 0.12184, false, map[string]compModelUsage{
			opusModel:   {read: 10000, w1h: 10405, out: 190, cost: 0.08904}, // header
			sonnetModel: {w1h: 8000, out: 80, cost: 0.0328},                 // 8 x 0.0041
		}),
	)
	writeLines(t, mainPath, lines...)
	for id, desc := range descs {
		writeLines(t, compSubagentPath(dir, id),
			compUserPrompt(t, compFill(400, 'p')),
			compAssistant(t, id+"-q1", sonnetModel, compUsage{w1h: 1000, out: 10}, compTextItem("ok")),
		)
		writeLines(t, compMetaPath(dir, id), mustJSON(t, map[string]any{"agentType": "general-purpose", "description": desc}))
	}
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")

	want := map[string][2]string{
		"agent-w1": {"evidence", "cw_task_assign"},
		"agent-w2": {"implementation", "description"},
		"agent-w3": {"review", "cw_task_assign"},
		"agent-w4": {"verification", "cw_task_assign"},
		"agent-w5": {"test_author", "cw_task_assign"},
		"agent-w6": {"github", "description"},
		"agent-w7": {"test_author", "description"},
		"agent-w8": {"implementation", "cw_task_assign"},
	}
	for id, w := range want {
		a := compAgent(t, comp, id)
		compWantStr(t, a, "role", w[0], id)
		compWantStr(t, a, "role_basis", w[1], id)
	}

	// Only the jq review brief (rj-1) is a review assign; it was issued in k1.
	main := compAgent(t, comp, "main")
	ph := compObj(t, main["phases"], "main.phases")
	bounds := compArr(t, ph["boundaries"], "phases.boundaries")
	if len(bounds) != 1 {
		t.Fatalf("boundaries = %v, want one review_assign", bounds)
	}
	b := compObj(t, bounds[0], "boundary")
	compWantStr(t, b, "kind", "review_assign", "boundary")
	compWantNum(t, b, "request", 2, "boundary")
}

// ---------------------------------------------------------------------------
// stale-phase / roles (rev 6, section 3a): cw in a heredoc body line and inside quotes
// ---------------------------------------------------------------------------
//
// Main only (opus): user prompt 400 B;
//   q1 T=10000 (w1h 10000) out 30, three Bash calls:
//     m1 heredoc whose body line starts with "cw task start --repo x" (body, not a command)
//     m2 echo 'done; cw task verify --task t'         (separator inside quotes)
//     m3 printf "%s\n" "a && cw task consult --task t" (separator inside quotes)
//   results 100 B, 100 B, 200 B
//   q2 T=10130 (read 10000, w1h 130) out 10: d=130=30+100, R=100, sample 400/100=4;
//      shares 25, 25, 50.
//   input $ = 0.08 + 0.002 + 130*8e-6 = 0.08304; output 40*20e-6 = 0.0008; total 0.08384

func TestMetricsCompositionStalePhaseHeredocBodyAndQuotedSeparators(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	u1 := compUsage{w1h: 10000, out: 30}
	m1 := "cat > /tmp/notes.md <<'EOF'\ncw task start --repo x\nEOF"
	m2 := `echo 'done; cw task verify --task t'`
	m3 := `printf "%s\n" "a && cw task consult --task t"`
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "q1", o, u1, compToolUse("m1", "Bash", map[string]any{"command": m1})),
		compAssistant(t, "q1", o, u1, compToolUse("m2", "Bash", map[string]any{"command": m2})),
		compAssistant(t, "q1", o, u1, compToolUse("m3", "Bash", map[string]any{"command": m3})),
		compToolResult(t, "m1", compFill(100, 'b'), nil),
		compToolResult(t, "m2", compFill(100, 'c'), nil),
		compToolResult(t, "m3", compFill(200, 'd'), nil),
		compAssistant(t, "q2", o, compUsage{read: 10000, w1h: 130, out: 10}, compTextItem("done")),
		compCostState(t, 0.08384, false, map[string]compModelUsage{
			// reads 10000; 1h writes 10000+130=10130; out 30+10=40
			opusModel: {read: 10000, w1h: 10130, out: 40, cost: 0.08384},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantStr(t, main, "role", "main", "main") // the body's "cw task start" is not a call
	compWantNull(t, main, "phases", "main")      // no boundary at all
	compWantNum(t, main, "input_tokens", 20130, "main")
	for _, name := range compSourceNames(t, main) {
		if strings.HasPrefix(name, "cw ") {
			t.Errorf("unexpected cw source %q", name)
		}
	}
	bashSrc := compFind(t, compArr(t, main["sources"], "main.sources"), "source", "tool:Bash", "sources")
	compWantNum(t, bashSrc, "blocks", 3, "tool:Bash")       // m1, m2, m3
	compWantNum(t, bashSrc, "new_tokens", 100, "tool:Bash") // 25+25+50
}

// ---------------------------------------------------------------------------
// roles (rev 6, section 7 rule 1): longest of several whole-word brief ids
// ---------------------------------------------------------------------------
//
// Main (opus): user prompt 400 B; p1 T=10000 (w1h 10000) out 40:
//   b1 assign rv-1 (evidence), b2 assign rv-12 (verification), b3 Agent "helper z" whose
//   prompt names both rv-12 and rv-1 as whole words -> rv-12 (longest) -> verification.
//   results 3 x 100 B = 300 B; p2 T=10115 (read 10000, w1h 115) out 10: d=115=40+75, sample 4.
//   main $ = 0.08 + 0.002 + 0.00092 + 50*20e-6 = 0.08392
// Subagent u1 (sonnet): brief 400 B, one request T=1000 (w1h) out 10: 0.0041. Session 0.08802.

func TestMetricsCompositionRolesLongestBriefID(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	u1 := compUsage{w1h: 10000, out: 40}
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "p1", o, u1, compToolUse("b1", "Bash", map[string]any{"command": compAssignCmd("rv-1", "evidence", "gather facts")})),
		compAssistant(t, "p1", o, u1, compToolUse("b2", "Bash", map[string]any{"command": compAssignCmd("rv-12", "verification", "check it")})),
		compAssistant(t, "p1", o, u1, compToolUse("b3", "Agent", map[string]any{
			"description": "helper z", "subagent_type": "general-purpose", "prompt": "assignment rv-12 follows up on rv-1",
		})),
		compToolResult(t, "b1", compFill(100, 'b'), nil),
		compToolResult(t, "b2", compFill(100, 'c'), nil),
		compToolResult(t, "b3", compFill(100, 'd'), map[string]any{"agentId": "u1", "status": "completed"}),
		compAssistant(t, "p2", o, compUsage{read: 10000, w1h: 115, out: 10}, compTextItem("done")),
		compCostState(t, 0.08802, false, map[string]compModelUsage{
			opusModel:   {read: 10000, w1h: 10115, out: 50, cost: 0.08392}, // header
			sonnetModel: {w1h: 1000, out: 10, cost: 0.0041},
		}),
	)
	writeLines(t, compSubagentPath(dir, "u1"),
		compUserPrompt(t, compFill(400, 'p')),
		compAssistant(t, "u1-q1", sonnetModel, compUsage{w1h: 1000, out: 10}, compTextItem("ok")),
	)
	writeLines(t, compMetaPath(dir, "u1"), mustJSON(t, map[string]any{"agentType": "general-purpose", "description": "helper z"}))
	prices := writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	a := compAgent(t, comp, "agent-u1")
	compWantStr(t, a, "role", "verification", "agent-u1") // rv-12, not rv-1 (evidence)
	compWantStr(t, a, "role_basis", "cw_task_assign", "agent-u1")
}

// ---------------------------------------------------------------------------
// Revision 7 cases
// ---------------------------------------------------------------------------
//
// compWriteCallsFixture builds a main transcript (opus) with
//   user prompt 400 B
//   z1 T=10000 (w1h 10000) out 40, one assistant line per call
//   one 100 B tool_result per call (Agent results carry toolUseResult.agentId)
//   z2 T=10000+d (read 10000, w1h d) out 10, d = 40 + 25n (R = 25n, sample 100n/25n = 4)
// main $ = 10000*8e-6 + 10000*0.2e-6 + d*8e-6 + 50*20e-6 = 0.08 + 0.002 + d*8e-6 + 0.001.
// Every subagent named by an Agent call gets a brief of 400 B and, when it has a request,
// one sonnet request T=1000 (w1h) out 10 = 1000*4e-6 + 10*10e-6 = 0.0041.
// The cost-state records exactly these tokens and dollars, so the composition is reported.

type compCall struct {
	id, name string
	input    map[string]any
	agentID  string // Agent calls: the subagent the tool_result names
}

func compWriteCallsFixture(t *testing.T, calls []compCall, zeroRequest map[string]bool) (mainPath, prices string) {
	t.Helper()
	dir := t.TempDir()
	mainPath = filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	n := len(calls)
	d := 40 + 25*n
	lines := []string{compUserPrompt(t, compFill(400, 'a'))}
	for _, c := range calls {
		lines = append(lines, compAssistant(t, "z1", o, compUsage{w1h: 10000, out: 40}, compToolUse(c.id, c.name, c.input)))
	}
	subs := 0
	for _, c := range calls {
		var res map[string]any
		if c.agentID != "" {
			res = map[string]any{"agentId": c.agentID, "status": "completed"}
			sub := []string{compUserPrompt(t, compFill(400, 'p'))}
			if !zeroRequest[c.agentID] {
				sub = append(sub, compAssistant(t, c.agentID+"-q1", sonnetModel, compUsage{w1h: 1000, out: 10}, compTextItem("ok")))
				subs++
			}
			writeLines(t, compSubagentPath(dir, c.agentID), sub...)
			desc, _ := c.input["description"].(string)
			writeLines(t, compMetaPath(dir, c.agentID), mustJSON(t, map[string]any{"agentType": "general-purpose", "description": desc}))
		}
		lines = append(lines, compToolResult(t, c.id, compFill(100, 'r'), res))
	}
	mainCost := 0.08 + 0.002 + float64(d)*8e-6 + 0.001
	models := map[string]compModelUsage{
		opusModel: {read: 10000, w1h: 10000 + d, out: 50, cost: mainCost},
	}
	total := mainCost
	if subs > 0 {
		models[sonnetModel] = compModelUsage{w1h: 1000 * subs, out: 10 * subs, cost: 0.0041 * float64(subs)}
		total += 0.0041 * float64(subs)
	}
	lines = append(lines,
		compAssistant(t, "z2", o, compUsage{read: 10000, w1h: d, out: 10}, compTextItem("done")),
		compCostState(t, total, false, models),
	)
	writeLines(t, mainPath, lines...)
	return mainPath, writePrices(t, map[string]rates{opusModel: opusRates, sonnetModel: sonnetRates})
}

func compBashCalls(cmds ...string) []compCall {
	calls := make([]compCall, 0, len(cmds))
	for i, c := range cmds {
		calls = append(calls, compCall{id: "zb" + string(rune('1'+i)), name: "Bash", input: map[string]any{"command": c}})
	}
	return calls
}

// compWantBoundaries checks the main agent's role and boundary kinds; every boundary
// comes from z1's output, so each starts its phase at request 2.
func compWantBoundaries(t *testing.T, comp map[string]any, role string, kinds ...string) {
	t.Helper()
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantStr(t, main, "role", role, "main")
	ph := compObj(t, main["phases"], "main.phases")
	bounds := compArr(t, ph["boundaries"], "phases.boundaries")
	got := make([]string, 0, len(bounds))
	for _, b := range bounds {
		m := compObj(t, b, "boundary")
		k, _ := m["kind"].(string)
		got = append(got, k)
		compWantNum(t, m, "request", 2, "boundary "+k)
	}
	if strings.Join(got, ",") != strings.Join(kinds, ",") {
		t.Errorf("boundary kinds = %v, want %v", got, kinds)
	}
	compWantNum(t, ph, "phase_count", float64(len(kinds)+1), "phases")
}

func compRunCalls(t *testing.T, calls []compCall, zeroRequest map[string]bool) map[string]any {
	t.Helper()
	mainPath, prices := compWriteCallsFixture(t, calls, zeroRequest)
	return compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
}

// (1) A comment line with an apostrophe does not open a quote; the next line's cw call counts.
func TestMetricsCompositionStalePhaseCommentWithApostrophe(t *testing.T) {
	comp := compRunCalls(t, compBashCalls("echo a\n# don't stop\ncw task start --repo r"), nil)
	compWantBoundaries(t, comp, "coordinator", "task_start")
	main := compAgent(t, comp, "main")
	compFind(t, compArr(t, main["sources"], "main.sources"), "source", "cw task start", "sources")
}

// (2) Compound-command keywords and the timeout wrapper are skipped before the program.
func TestMetricsCompositionStalePhaseKeywordsAndTimeoutWrapper(t *testing.T) {
	comp := compRunCalls(t, compBashCalls(
		"for t in a b; do cw task consult --task $t; done",
		"if x; then cw task verify --task t; fi",
		"timeout -k 5 600 cw task start --repo r",
	), nil)
	// One boundary per Bash call, in tool_use order; task_start makes the main agent coordinator.
	compWantBoundaries(t, comp, "coordinator", "task_consult", "task_verify", "task_start")
}

// (3) A here-string and a quoted "<<" do not start a heredoc, so the next line is a command.
func TestMetricsCompositionStalePhaseHereStringAndQuotedHeredocMarker(t *testing.T) {
	comp := compRunCalls(t, compBashCalls(
		"cat <<< \"hello\"\ncw task start --repo r",
		"echo \"a << EOF\"\ncw task verify --task t",
	), nil)
	compWantBoundaries(t, comp, "coordinator", "task_start", "task_verify")
}

// (4) A cw task start only inside another session's copied history yields no boundary.
//
//	H1 assistant (s0, with usage) Bash "cw task start --repo r"; H2 its tool_result (s0) 100 B
//	own: user prompt 400 B; j1 T=10000 (w1h 10000) out 20 text
//	$ = 10000*8e-6 + 20*20e-6 = 0.08 + 0.0004 = 0.0804
func TestMetricsCompositionStalePhaseHistoryBoundaryIgnored(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	writeLines(t, mainPath,
		compOtherSession(t, compAssistant(t, "hist1", o, compUsage{w1h: 5000, out: 5}, compToolUse("hb1", "Bash", map[string]any{"command": "cw task start --repo r"})), "s0"),
		compOtherSession(t, compToolResult(t, "hb1", compFill(100, 'a'), nil), "s0"),
		compUserPrompt(t, compFill(400, 'b')),
		compAssistant(t, "j1", o, compUsage{w1h: 10000, out: 20}, compTextItem("ok")),
		compCostState(t, 0.0804, false, map[string]compModelUsage{
			opusModel: {w1h: 10000, out: 20, cost: 0.0804},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	comp := compComposition(t, compImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition"))
	compWantStr(t, comp, "status", "reported", "composition")
	main := compAgent(t, comp, "main")
	compWantStr(t, main, "role", "main", "main")
	compWantNull(t, main, "phases", "main")
	compWantNum(t, main, "requests", 1, "main")
	compWantNum(t, main, "input_tokens", 10000, "main")
}

// (5) After a compaction, an assistant line repeating an earlier requestId is context.
//
//	user prompt 400 B; c1 T=10000 (w1h 10000) out 20 text; compact_boundary;
//	assistant line with requestId c1 again (text 40 B); user prompt 100 B;
//	c2 T=10100 (read 10000, w1h 100) out 10.
//	input_tokens = 10000+10100 = 20100; $ = 0.08 + 0.002 + 0.0008 + 30*20e-6 = 0.0834
func TestMetricsCompositionAttributionRepeatedRequestAfterCompaction(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sessionID+".jsonl")
	o := opusModel
	c1 := compUsage{w1h: 10000, out: 20}
	writeLines(t, mainPath,
		compUserPrompt(t, compFill(400, 'a')),
		compAssistant(t, "c1", o, c1, compTextItem("first")),
		compCompactBoundary(t),
		compAssistant(t, "c1", o, c1, compTextItem(compFill(40, 'x'))),
		compUserPrompt(t, compFill(100, 'b')),
		compAssistant(t, "c2", o, compUsage{read: 10000, w1h: 100, out: 10}, compTextItem("done")),
		compCostState(t, 0.0834, false, map[string]compModelUsage{
			// reads 10000; 1h writes 10000+100=10100; out 20+10=30 (c1 counted once)
			opusModel: {read: 10000, w1h: 10100, out: 30, cost: 0.0834},
		}),
	)
	prices := writePrices(t, map[string]rates{opusModel: opusRates})
	out, err := executeImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition")
	if err != nil {
		t.Fatalf("import failed: %v (exit %d)", err, ExitCode(err))
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	comp := compComposition(t, report)
	compWantStr(t, comp, "status", "reported", "composition")
	rec := compObj(t, comp["reconciliation"], "reconciliation")
	compWantNum(t, rec, "attributed_input_tokens", 20100, "reconciliation")
	compWantNum(t, rec, "transcript_input_tokens", 20100, "reconciliation")
	main := compAgent(t, comp, "main")
	compWantNum(t, main, "requests", 2, "main")
	compWantNum(t, main, "input_tokens", 20100, "main")
	compWantNum(t, main, "compactions", 1, "main")
}

// (6) Equal-length ids named in one prompt: the latest recorded brief wins.
// rv-a (evidence) is recorded before rv-b (verification); the prompt names rv-b first and
// rv-a last, so neither "last named" nor "first recorded" would give verification.
func TestMetricsCompositionRolesEqualLengthIDsLatestBrief(t *testing.T) {
	comp := compRunCalls(t, []compCall{
		{id: "e1", name: "Bash", input: map[string]any{"command": compAssignCmd("rv-a", "evidence", "gather facts")}},
		{id: "e2", name: "Bash", input: map[string]any{"command": compAssignCmd("rv-b", "verification", "check it")}},
		{id: "e3", name: "Agent", agentID: "g1", input: map[string]any{
			"description": "helper g", "subagent_type": "general-purpose", "prompt": "assignment rv-b with rv-a",
		}},
	}, nil)
	compWantStr(t, comp, "status", "reported", "composition")
	a := compAgent(t, comp, "agent-g1")
	compWantStr(t, a, "role", "verification", "agent-g1")
	compWantStr(t, a, "role_basis", "cw_task_assign", "agent-g1")
}

// (7) A zero-request agent with a review brief takes no place in the review order:
// y0 (rz-1, zero requests) is dispatched before y1 (rz-2, one request); y1 is "review".
func TestMetricsCompositionRolesZeroRequestReviewAgentSkipped(t *testing.T) {
	agentFor := func(desc, briefID string) map[string]any {
		return map[string]any{"description": desc, "subagent_type": "general-purpose", "prompt": "Handle assignment " + briefID + "."}
	}
	comp := compRunCalls(t, []compCall{
		{id: "f1", name: "Bash", input: map[string]any{"command": compAssignCmd("rz-1", "review", "first look")}},
		{id: "f2", name: "Agent", agentID: "y0", input: agentFor("helper y0", "rz-1")},
		{id: "f3", name: "Bash", input: map[string]any{"command": compAssignCmd("rz-2", "review", "real look")}},
		{id: "f4", name: "Agent", agentID: "y1", input: agentFor("helper y1", "rz-2")},
	}, map[string]bool{"y0": true})
	compWantStr(t, comp, "status", "reported", "composition")
	y0 := compAgent(t, comp, "agent-y0")
	compWantNum(t, y0, "requests", 0, "agent-y0")
	y1 := compAgent(t, comp, "agent-y1")
	compWantStr(t, y1, "role", "review", "agent-y1")
	compWantStr(t, y1, "role_basis", "cw_task_assign", "agent-y1")
	cov := compObj(t, comp["role_coverage"], "role_coverage")
	compWantNum(t, cov, "agents", 2, "role_coverage") // main + y1
}

// ---------------------------------------------------------------------------
// Revision 8 cases (section 3a). Fixtures come from compWriteCallsFixture: every
// boundary is issued in request 1's output, so each starts its phase at request 2,
// and the cost-state matches the transcript, so the composition is reported.
// ---------------------------------------------------------------------------

func compWantSourcePresence(t *testing.T, comp map[string]any, present, absent string) {
	t.Helper()
	names := compSourceNames(t, compAgent(t, comp, "main"))
	has := func(s string) bool {
		for _, n := range names {
			if n == s {
				return true
			}
		}
		return false
	}
	if present != "" && !has(present) {
		t.Errorf("main sources %v lack %q", names, present)
	}
	if absent != "" && has(absent) {
		t.Errorf("main sources %v contain %q", names, absent)
	}
}

// (1) A heredoc inside "$(...)" starts a heredoc even within double quotes; its body,
// which holds one `"` and a cw task start line, is not a command; the cw call after the
// closing `)"` is.
func TestMetricsCompositionStalePhaseHeredocInCommandSubstitution(t *testing.T) {
	cmd := "git commit -m \"$(cat <<'EOF'\nmsg with one \" quote\ncw task start --repo r\nEOF\n)\" && cw task verify --task t"
	comp := compRunCalls(t, compBashCalls(cmd), nil)
	// Only task_verify; no task_start, so the main agent stays "main".
	compWantBoundaries(t, comp, "main", "task_verify")
	compWantSourcePresence(t, comp, "cw task verify", "cw task start")
}

// (2) A quote opened on an earlier line makes the "<<n" inside it no heredoc, so the
// cw task start line after the closing quote is a command.
func TestMetricsCompositionStalePhaseMultilineQuoteHidesHeredocMarker(t *testing.T) {
	cmd := "python3 -c \"\nprint(1 <<n)\n\"\ncw task start --repo r"
	comp := compRunCalls(t, compBashCalls(cmd), nil)
	compWantBoundaries(t, comp, "coordinator", "task_start")
	compWantSourcePresence(t, comp, "cw task start", "")
}

// (3) Wrapper options (with and without values) are skipped before the program.
func TestMetricsCompositionStalePhaseWrapperOptions(t *testing.T) {
	comp := compRunCalls(t, compBashCalls(
		"sudo -u bob cw task start --repo r", // -u takes "bob"
		"nice -n 5 cw task consult --task t", // -n takes "5"
		"env -i A=1 cw task verify --task t", // -i has no value; A=1 is an assignment
	), nil)
	compWantBoundaries(t, comp, "coordinator", "task_start", "task_consult", "task_verify")
}

// (4) Each remaining keyword or bare wrapper precedes exactly one cw call in its own Bash
// call, so each contributes exactly one boundary (seven in all, in tool_use order). Not
// skipping any single one removes its boundary and changes the asserted list.
func TestMetricsCompositionStalePhaseRemainingKeywordsAndWrappers(t *testing.T) {
	comp := compRunCalls(t, compBashCalls(
		"while cw task consult --task t; do sleep 1; done",          // while  -> task_consult
		"until cw task verify --task t; do sleep 1; done",           // until  -> task_verify
		"if false; then :; elif cw task start --repo r; then :; fi", // elif   -> task_start
		"if false; then :; else cw task verify --task t; fi",        // else   -> task_verify
		"nohup cw task consult --task t &",                          // nohup  -> task_consult
		"sudo cw task start --repo r",                               // sudo   -> task_start
		"nice cw task verify --task t",                              // nice   -> task_verify
	), nil)
	compWantBoundaries(t, comp, "coordinator",
		"task_consult", "task_verify", "task_start", "task_verify", "task_consult", "task_start", "task_verify")
}

// ---------------------------------------------------------------------------
// Revision 9 cases (section 3a). Same fixture and expectations as revision 8:
// every boundary is issued in request 1 and starts its phase at request 2.
// ---------------------------------------------------------------------------

// (1) The apostrophe in a comment opens no quote, so the next line's <<EOF is a real
// heredoc whose body (cw task start) is not a command.
func TestMetricsCompositionStalePhaseCommentApostropheBeforeHeredoc(t *testing.T) {
	cmd := "# don't\ncat > f <<EOF\ncw task start --repo r\nEOF\ncw task verify --task t"
	comp := compRunCalls(t, compBashCalls(cmd), nil)
	compWantBoundaries(t, comp, "main", "task_verify")
}

// (2) A heredoc started inside a backtick substitution within double quotes.
func TestMetricsCompositionStalePhaseBacktickHeredocInQuotes(t *testing.T) {
	cmd := "echo \"`cat <<EOF`\"\ncw task start --repo r\nEOF\ncw task consult --task t"
	comp := compRunCalls(t, compBashCalls(cmd), nil)
	compWantBoundaries(t, comp, "main", "task_consult")
}

// (3) A heredoc inside a subshell.
func TestMetricsCompositionStalePhaseSubshellHeredoc(t *testing.T) {
	cmd := "(cd x && cat <<EOF\ncw task start --repo r\nEOF\n) && cw task verify --task t"
	comp := compRunCalls(t, compBashCalls(cmd), nil)
	compWantBoundaries(t, comp, "main", "task_verify")
}

// (4) "--" ends wrapper options; (5) option values of exec, sudo -g, timeout -s, time -o,
// and bare command / builtin. One cw call per Bash call, so dropping any single rule
// removes exactly one boundary from the asserted list.
func TestMetricsCompositionStalePhaseWrapperOptionValuesAndDoubleDash(t *testing.T) {
	comp := compRunCalls(t, compBashCalls(
		"sudo -- cw task verify --task t",            // --        -> task_verify
		"exec -a name cw task start --repo r",        // exec -a   -> task_start
		"sudo -g wheel cw task consult --task t",     // sudo -g   -> task_consult
		"timeout -s KILL 30 cw task verify --task t", // timeout -s VALUE + duration -> task_verify
		"time -o out.txt cw task start --repo r",     // time -o   -> task_start
		"command cw task consult --task t",           // command   -> task_consult
		"builtin cw task verify --task t",            // builtin   -> task_verify
	), nil)
	compWantBoundaries(t, comp, "coordinator",
		"task_verify", "task_start", "task_consult", "task_verify", "task_start", "task_consult", "task_verify")
}

// (6) "<<" inside $((...)) is a shift, bare and within double quotes.
func TestMetricsCompositionStalePhaseArithmeticShift(t *testing.T) {
	comp := compRunCalls(t, compBashCalls(
		"echo $((1<<n))\ncw task start --repo r",
		"echo \"$((1<<n))\"\ncw task start --repo r",
	), nil)
	compWantBoundaries(t, comp, "coordinator", "task_start", "task_start")
}

// (7) $'it\'s' is one string; the following heredoc is real and the line after it is a command.
func TestMetricsCompositionStalePhaseDollarQuoteEscape(t *testing.T) {
	cmd := "echo $'it\\'s'\ncat <<EOF\nb\nEOF\ncw task start --repo r"
	comp := compRunCalls(t, compBashCalls(cmd), nil)
	compWantBoundaries(t, comp, "coordinator", "task_start")
}

// (8) Long commands split correctly end to end. Linearity itself (no rescan of the
// text before each "<<") is asserted by the usagelog work-count test
// TestSplitShellWorkLinear, which does not depend on machine speed; this test has
// no timing assertion.
//   - "double_quoted" / "single_quoted" (revision 10): echo "<100,000 a<<b words>" (about
//     500 KB) then the cw call on the next line; none of the "<<" is a heredoc, so the cw
//     call is a command.
//   - "first" (revision 9, 20,000 unquoted words, about 100 KB): the cw call precedes
//     the long line, so its boundary does not depend on how the heredoc markers are read.
func TestMetricsCompositionStalePhaseLongCommandLinear(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("a<<b ", 100000))
	short := strings.TrimSpace(strings.Repeat("a<<b ", 20000))
	cases := map[string]string{
		"first":         "cw task start --repo r\necho " + short,
		"double_quoted": "echo \"" + long + " \"\ncw task start --repo r",
		"single_quoted": "echo '" + long + "'\ncw task start --repo r",
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			mainPath, prices := compWriteCallsFixture(t, compBashCalls(cmd), nil)
			out, err := executeImport(t, "--claude-transcript", mainPath, "--prices", prices, "--composition")
			if err != nil {
				t.Fatalf("import: %v (exit %d)", err, ExitCode(err))
			}
			var report map[string]any
			if err := json.Unmarshal([]byte(out), &report); err != nil {
				t.Fatalf("decode: %v", err)
			}
			compWantBoundaries(t, compComposition(t, report), "coordinator", "task_start")
		})
	}
}

// Revision 10 section 3a examples: only cw task verify is a call in each.
//
//   - the subshell "(cd x)" closes inside "$( ... )", so <<'EOF' starts a heredoc whose
//     body (cw task start) is not a command;
//
//   - $'it\'s' is one string, so the next line's <<EOF is a heredoc with a cw task start body.
//
//   - the same substitution with a body line holding an unbalanced `"` (says "hi): the
//     body is still not part of any command.
//
// Three Bash calls -> boundaries [task_verify, task_verify, task_verify]; no task_start, role main.
func TestMetricsCompositionStalePhaseSubshellInSubstitutionAndDollarQuoteHeredoc(t *testing.T) {
	comp := compRunCalls(t, compBashCalls(
		"git commit -m \"$( (cd x) && cat <<'EOF'\ncw task start\nEOF\n)\" && cw task verify --task t",
		"echo $'it\\'s'\ncat <<EOF\ncw task start\nEOF\ncw task verify --task t",
		"git commit -m \"$( (cd x) && cat <<'EOF'\nsays \"hi\ncw task start --repo r\nEOF\n)\" && cw task verify --task t",
	), nil)
	compWantBoundaries(t, comp, "main", "task_verify", "task_verify", "task_verify")
	compWantSourcePresence(t, comp, "cw task verify", "cw task start")
}

// ---------------------------------------------------------------------------
// Revision 11 header examples (section 3a). Each runs alone in compWriteCallsFixture
// (n=1: d = 40+25 = 65; opus reads 10000, 1h writes 10065, output 50;
// $0.08 + 0.002 + 65*8e-6 + 0.001 = $0.08352), so every boundary is at request 2 and the
// exact list (and role) is asserted per example. "\n" in the contract is a newline.
// ---------------------------------------------------------------------------

func TestMetricsCompositionStalePhaseRevision11Examples(t *testing.T) {
	cases := []struct {
		name, cmd, role string
		kinds           []string
	}{
		// echo \$'a\'<<EOF / z / EOF / cw task verify: the escaped $ starts no $'...', so
		// 'a\' is a plain single-quoted string and <<EOF is a heredoc.
		{"escaped dollar before quote", "echo \\$'a\\'<<EOF\nz\nEOF\ncw task verify --task t", "main", []string{"task_verify"}},
		// Same, with a cw task start line as the heredoc body: the body is no command.
		{"escaped dollar before quote with cw body", "echo \\$'a\\'<<EOF\ncw task start --repo r\nEOF\ncw task verify --task t", "main", []string{"task_verify"}},
		// echo \"hi; cw task start: the escaped quote opens no string, ";" separates.
		{"escaped double quote", "echo \\\"hi; cw task start --repo r", "coordinator", []string{"task_start"}},
		// ((x<<n)) at a command position is arithmetic: the << is a shift.
		{"arithmetic command", "((x<<n))\ncw task start --repo r", "coordinator", []string{"task_start"}},
		// $(( (1<<n) )): a << inside parentheses nested in arithmetic is a shift.
		{"nested arithmetic parentheses", "echo $(( (1<<n) ))\ncw task start --repo r", "coordinator", []string{"task_start"}},
		// echo "a \" <<EOF": the escaped quote does not end the string, so << is inside it.
		{"escaped quote inside double quotes", "echo \"a \\\" <<EOF\"\ncw task start --repo r\nEOF", "coordinator", []string{"task_start"}},
		// (true)# don't: the # after ) starts a comment, so its apostrophe opens no quote and
		// the next line's <<EOF is a heredoc whose body (cw task start) is not a command.
		{"comment after closing parenthesis", "(true)# don't\ncat <<EOF\ncw task start\nEOF\ncw task verify --task t", "main", []string{"task_verify"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			comp := compRunCalls(t, compBashCalls(c.cmd), nil)
			compWantBoundaries(t, comp, c.role, c.kinds...)
		})
	}
}

// ---------------------------------------------------------------------------
// Revision 11, section 3a: a backslash outside quotes makes the next character literal.
// Each command glues text onto "cw" with an escaped character, so only the trailing
// call is a cw call. One Bash call per subtest in compWriteCallsFixture (n=1: opus reads
// 10000, 1h writes 10065, output 50, $0.08352); the boundary is at request 2.
// ---------------------------------------------------------------------------

func TestMetricsCompositionStalePhaseBackslashEscapesOutsideQuotes(t *testing.T) {
	cases := []struct {
		name, cmd, role string
		kinds           []string
	}{
		// "a;cw" is one word: the escaped ; separates nothing.
		{"escaped semicolon", "echo a\\;cw task start --repo r; cw task verify --task t", "main", []string{"task_verify"}},
		// "a|cw" is one word: the escaped | is no pipe.
		{"escaped pipe", "echo a\\|cw task start --repo r; cw task consult --task t", "main", []string{"task_consult"}},
		// "a&cw" is one word: the escaped & separates nothing.
		{"escaped ampersand", "echo a\\&cw task start --repo r; cw task verify --task t", "main", []string{"task_verify"}},
		// "a cw" is one word: the escaped space does not split it.
		{"escaped space", "echo a\\ cw task start --repo r; cw task verify --task t", "main", []string{"task_verify"}},
		// The escaped ' opens no string, so "; cw task start" is a separate command.
		{"escaped single quote", "echo \\'hi; cw task start --repo r", "coordinator", []string{"task_start"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			comp := compRunCalls(t, compBashCalls(c.cmd), nil)
			compWantBoundaries(t, comp, c.role, c.kinds...)
		})
	}
}
