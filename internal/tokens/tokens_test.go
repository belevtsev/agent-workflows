package tokens

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func TestNoticeRole(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"architect":       "architect",
		"kitsu:architect": "architect",
		"a:b:git":         "git",
		"Explore":         "",
		"kitsu:":          "",
		"":                "",
	} {
		if got := noticeRole(name); got != want {
			t.Errorf("noticeRole(%q) = %q, want %q", name, got, want)
		}
	}
}

func known(session, agent, task, stage string, n int64) Start {
	return Start{
		Version: 1, Host: Claude, Session: session, Agent: agent, Record: agent + ".jsonl",
		Task: task, Stage: stage, Round: 1, Known: true,
		Models: map[string]Counts{"m": {Input: n}},
	}
}

func unknown(session, agent, role string) Start {
	return Start{
		Version: 1, Host: Claude, Session: session, Agent: agent, Record: agent + ".jsonl",
		Role: role, Why: "the record holds no token count",
	}
}

func task(t *testing.T, report *Report, name string) Task {
	t.Helper()
	found, _, _, ok := report.Find(name)
	if !ok {
		t.Fatalf("the report holds no task %q", name)
	}
	return *found
}

// A start whose record gives no label and no count, in a session that ran
// one task, is a start of the role the host named.
func TestUnknownStartWithARoleGoesToTheOneTask(t *testing.T) {
	t.Parallel()
	report := Build([]Start{
		known("s", "agent-1", "one", "code", 100),
		unknown("s", "agent-2", "architect"),
		known("s", SessionAgent, "", "", 10),
	}, nil)
	got := task(t, report, "one")
	if got.Complete || got.Total != 110 || got.Starts != 2 {
		t.Errorf("complete %v, total %d, starts %d; want false, 110, 2", got.Complete, got.Total, got.Starts)
	}
	if !slices.Equal(got.UnknownRecords, []string{"agent-2.jsonl"}) {
		t.Errorf("unknown records %v, want the record of the architect", got.UnknownRecords)
	}
	at := slices.IndexFunc(got.Roles, func(e Entry) bool { return e.Role == "architect" })
	if at < 0 || !got.Roles[at].Total.Unknown || got.Roles[at].UnknownStarts != 1 {
		t.Errorf("the roles %+v hold no architect whose total is unknown", got.Roles)
	}
	if len(got.Stages) != 1 {
		t.Errorf("the stages are %+v; a start without a label is in no stage", got.Stages)
	}
	if len(report.Unlabelled) != 0 {
		t.Errorf("the start is also listed without a task: %+v", report.Unlabelled)
	}
}

// With two tasks in the session nothing says which of them the start
// belongs to: both are incomplete, and the start is in neither.
func TestUnknownStartInASessionOfTwoTasks(t *testing.T) {
	t.Parallel()
	report := Build([]Start{
		known("s", "agent-1", "one", "code", 100),
		known("s", "agent-2", "two", "code", 200),
		unknown("s", "agent-3", "architect"),
	}, nil)
	for _, name := range []string{"one", "two"} {
		got := task(t, report, name)
		if got.Complete || got.Starts != 1 || !slices.Contains(got.UnknownRecords, "agent-3.jsonl") {
			t.Errorf("%s: complete %v, starts %d, unknown records %v", name, got.Complete, got.Starts, got.UnknownRecords)
		}
	}
	if len(report.Unlabelled) != 1 || report.Unlabelled[0].Role != "architect" || !report.Unlabelled[0].Total.Unknown {
		t.Errorf("unlabelled is %+v, want the one start with its role and an unknown total", report.Unlabelled)
	}
}

// Without a role the start is not counted as a start of the task, and the
// task is incomplete all the same.
func TestUnknownStartWithoutARole(t *testing.T) {
	t.Parallel()
	report := Build([]Start{
		known("s", "agent-1", "one", "code", 100),
		unknown("s", "agent-2", ""),
	}, nil)
	got := task(t, report, "one")
	if got.Complete || got.Starts != 1 || !slices.Contains(got.UnknownRecords, "agent-2.jsonl") {
		t.Errorf("complete %v, starts %d, unknown records %v", got.Complete, got.Starts, got.UnknownRecords)
	}
	if len(report.Unlabelled) != 1 {
		t.Errorf("unlabelled is %+v, want the one start", report.Unlabelled)
	}
}

// A start whose brief was read and holds no label belongs to no task, and
// leaves the task complete.
func TestKnownStartWithoutALabel(t *testing.T) {
	t.Parallel()
	other := known("s", "agent-2", "", "", 50)
	other.Round = 0
	report := Build([]Start{known("s", "agent-1", "one", "code", 100), other, known("s", SessionAgent, "", "", 10)}, nil)
	got := task(t, report, "one")
	if !got.Complete || got.Total != 110 {
		t.Errorf("complete %v, total %d; want true, 110", got.Complete, got.Total)
	}
	if len(report.Unlabelled) != 1 || report.Unlabelled[0].Total.N != 50 {
		t.Errorf("unlabelled is %+v, want the one start with 50 tokens", report.Unlabelled)
	}
}

func codexFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := ""
	for _, line := range lines {
		data += line + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// codexModel is a turn_context line of Codex for the model m.
func codexModel(m string) string {
	return `{"timestamp":"t","type":"turn_context","payload":{"turn_id":"u","cwd":"/x","model":"` + m + `","effort":"high","summary":"auto"}}`
}

// codexUsageJSON is a usage object as Codex writes it: input_tokens holds
// the cached input, and total_tokens is input and output.
func codexUsageJSON(c Counts) string {
	in := c.Input + c.CacheRead + c.CacheWrite
	return fmt.Sprintf(`{"input_tokens":%d,"cached_input_tokens":%d,"cache_write_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d,"total_tokens":%d}`,
		in, c.CacheRead, c.CacheWrite, c.Output, c.Output/2, in+c.Output)
}

// codexCount is an event_msg of payload.type token_count with a running
// total and the last request.
func codexCount(total, last Counts) string {
	return `{"timestamp":"t","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":` + codexUsageJSON(total) +
		`,"last_token_usage":` + codexUsageJSON(last) + `,"model_context_window":1000},"rate_limits":{"limit_id":"codex","primary":{"used_percent":1.0,"window_minutes":300,"resets_at":1}}}}`
}

// codexLimits is a token_count event that holds only the rate limits.
const codexLimits = `{"timestamp":"t","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex"}}}`

func codexUser(text string) string {
	return `{"timestamp":"t","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":` + strconv.Quote(text) + `}]}}`
}

// The totals of Codex are running totals: the thread is counted once, from
// its last total, and a total written twice is not counted twice. Each rise
// goes to the model of its turn.
func TestCodexCountsTheLastRunningTotal(t *testing.T) {
	t.Parallel()
	a := Counts{Input: 30, CacheWrite: 10, CacheRead: 60, Output: 5}
	b := Counts{Input: 7, CacheRead: 90, Output: 3}
	ab := Counts{Input: 37, CacheWrite: 10, CacheRead: 150, Output: 8}
	c := Counts{Input: 1, CacheRead: 100, Output: 2}
	r := readCodex(codexFile(t, `{"timestamp":"t","type":"session_meta","payload":{"id":"s","source":"vscode"}}`, codexModel("m"),
		codexLimits, codexCount(a, a), codexCount(ab, b), codexCount(ab, b), `{"timestamp":"t","type":"compacted","payload":{}}`,
		codexModel("n"), codexCount(Counts{Input: 38, CacheWrite: 10, CacheRead: 250, Output: 10}, c)))
	if r.why != "" || r.models["m"] != ab || r.models["n"] != c || len(r.models) != 2 {
		t.Errorf("read %+v (%s), want m %+v and n %+v", r.models, r.why, ab, c)
	}
}

// A first total that holds more than its own request is not understood: the
// counts are unknown, not a guess.
func TestCodexTotalsThatAreNotUnderstood(t *testing.T) {
	t.Parallel()
	a := Counts{Input: 30, CacheRead: 60, Output: 5}
	small := Counts{Input: 3, CacheRead: 6, Output: 1}
	for name, lines := range map[string][]string{
		"only rate limits":       {codexModel("m"), codexLimits},
		"an inherited total":     {codexModel("m"), codexCount(a, small)},
		"a total without output": {codexModel("m"), `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5},"last_token_usage":{"input_tokens":5}}}}`},
		"cached input above all": {codexModel("m"), `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5,"cached_input_tokens":9,"output_tokens":1},"last_token_usage":{"input_tokens":5,"cached_input_tokens":9,"output_tokens":1}}}}`},
	} {
		if r := readCodex(codexFile(t, lines...)); r.why == "" || r.models != nil {
			t.Errorf("%s: read %+v, want unknown tokens", name, r.models)
		}
	}
}

// Codex writes the AGENTS.md instructions and the environment as user
// messages before the first prompt; the brief is the first other one.
func TestCodexBriefFollowsTheHostContext(t *testing.T) {
	t.Parallel()
	a := Counts{Input: 1, Output: 1}
	context := `{"type":"response_item","payload":{"type":"message","role":"user","content":[` +
		`{"type":"input_text","text":"# AGENTS.md instructions for /x\n\nWrite well."},{"type":"input_text","text":"<environment_context>\n</environment_context>"}]}}`
	r := readCodex(codexFile(t, `{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"rules"}]}}`,
		context, codexUser("<environment_context>\n  <cwd>/x</cwd>\n</environment_context>"), codexModel("m"),
		codexUser("kitsu-task: one code 1\nWrite the code."), codexCount(a, a)))
	if label, ok := ParseLabel(r.brief); !ok || label.Task != "one" {
		t.Errorf("brief %q gives no label of the task one", r.brief)
	}
}

// The role of the notice is kept only when the record gives no label.
func TestRecordKeepsTheRoleOfTheNotice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	empty := filepath.Join(dir, "agent-a1.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	notice := Notice{Session: "s", AgentID: "a1", AgentTranscript: empty, AgentType: "kitsu:architect"}
	if err := Record(filepath.Join(dir, "tokens"), Claude, notice); err != nil {
		t.Fatal(err)
	}
	starts, damaged, err := Load(filepath.Join(dir, "tokens"))
	if err != nil || len(damaged) != 0 || len(starts) != 1 {
		t.Fatalf("loaded %+v, damaged %v, error %v", starts, damaged, err)
	}
	if got := starts[0]; got.Role != "architect" || got.Known || got.Task != "" {
		t.Errorf("stored %+v, want the role architect, unknown tokens and no task", got)
	}
}

// A stored file that cannot be read makes the tasks of its session
// incomplete, and is named.
func TestDamagedStoredFile(t *testing.T) {
	t.Parallel()
	damaged := []string{filepath.Join("tokens", "s", "agent-9.json"), filepath.Join("tokens", "other", "agent-1.json")}
	report := Build([]Start{known("s", "agent-1", "one", "code", 100), known("s", SessionAgent, "", "", 10)}, damaged)
	got := task(t, report, "one")
	if got.Complete || !slices.Equal(got.UnknownRecords, damaged[:1]) {
		t.Errorf("complete %v, unknown records %v; want false and the damaged file of the session", got.Complete, got.UnknownRecords)
	}
	if !slices.Equal(report.Damaged, damaged) {
		t.Errorf("damaged is %v, want both files", report.Damaged)
	}
}

// The record of the session is not a start of a role.
func TestUnknownSessionIsNotAStart(t *testing.T) {
	t.Parallel()
	session := unknown("s", SessionAgent, "")
	report := Build([]Start{known("s", "agent-1", "one", "code", 100), unknown("s", "agent-2", "architect"), session}, nil)
	got := task(t, report, "one")
	if starts, sessions := got.Unknown(); starts != 1 || sessions != 1 {
		t.Errorf("unknown starts %d and sessions %d, want 1 and 1 (records %v)", starts, sessions, got.UnknownRecords)
	}
}

// A role that ended without a record is kept as a start with unknown
// tokens.
func TestNoticeWithoutARecordOfTheRole(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	main := filepath.Join(dir, "main.jsonl")
	line := `{"type":"assistant","message":{"id":"m","model":"m","usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(main, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	notice := Notice{Session: "s", Transcript: main, AgentID: "a7", AgentType: "reviewer"}
	if err := Record(filepath.Join(dir, "tokens"), Codex, notice); err != nil {
		t.Fatal(err)
	}
	starts, _, err := Load(filepath.Join(dir, "tokens"))
	if err != nil || len(starts) != 2 {
		t.Fatalf("loaded %+v, error %v; want the role and the session", starts, err)
	}
	at := slices.IndexFunc(starts, func(s Start) bool { return s.Agent == "agent-a7" })
	if at < 0 || starts[at].Known || starts[at].Role != "reviewer" {
		t.Errorf("stored %+v, want the start agent-a7 of the reviewer with unknown tokens", starts)
	}
}

// claudeLine is an assistant line as Claude Code 2.1 writes it: one line
// for each block of an answer, each with the usage of the whole answer.
func claudeLine(id, model, stop string, c Counts) string {
	usage := fmt.Sprintf(`"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d`, c.Input, c.CacheWrite, c.CacheRead, c.Output)
	return `{"parentUuid":"p","isSidechain":true,"agentId":"a1","type":"assistant","requestId":"req_` + id + `","message":{"model":"` + model + `","id":"` + id +
		`","type":"message","role":"assistant","content":[{"type":"text","text":"x"}],"stop_reason":"` + stop + `","stop_sequence":null,"usage":{` + usage +
		`,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":` + strconv.FormatInt(c.CacheWrite, 10) +
		`},"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","iterations":[{"type":"message",` + usage +
		`}],"output_tokens_details":{"thinking_tokens":0}}},"uuid":"u","timestamp":"t"}`
}

// The lines of a record of Claude Code 2.1.311, with invented text: the
// brief as a string, an answer of three lines, an answer of one, an error
// the host made up with zero usage, and lines of other types that hold
// numbers of tokens or the word usage and are not answers.
func TestClaudeRecordOfVersion2(t *testing.T) {
	t.Parallel()
	a := Counts{Input: 3, CacheWrite: 500, CacheRead: 20000, Output: 40}
	b := Counts{Input: 1, CacheWrite: 60, CacheRead: 20500, Output: 300}
	lines := []string{
		`{"parentUuid":null,"isSidechain":true,"agentId":"a1","type":"user","message":{"role":"user","content":"kitsu-task: one code 1\nWrite it."},"uuid":"u","timestamp":"t"}`,
		`{"type":"api-request","id":"r","sessionId":"s","agentId":"a1","params":{},"timestamp":"t"}`,
		claudeLine("msg_a", "claude-opus-5-5", "tool_use", a), claudeLine("msg_a", "claude-opus-5-5", "tool_use", a), claudeLine("msg_a", "claude-opus-5-5", "tool_use", a),
		`{"type":"user","isSidechain":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"the usage of x"}]},"toolUseResult":{"usage":{"input_tokens":9}}}`,
		claudeLine("msg_b", "claude-opus-5-5", "end_turn", b),
		`{"type":"assistant","isApiErrorMessage":true,"error":"unknown","message":{"model":"<synthetic>","id":"e","role":"assistant","content":[],"stop_reason":"stop_sequence","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"service_tier":null}}}`,
		`{"type":"cost-state","sessionId":"s","modelUsage":{"claude-opus-5-5":{"inputTokens":99,"outputTokens":99}},"totalCostUSD":1.5}`,
	}
	path := filepath.Join(t.TempDir(), "agent-a1.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := readClaude(path)
	want := Counts{Input: 4, CacheWrite: 560, CacheRead: 40500, Output: 340}
	if r.why != "" || len(r.models) != 1 || r.models["claude-opus-5-5"] != want {
		t.Errorf("read %+v (%s), want %+v", r.models, r.why, want)
	}
	if label, ok := ParseLabel(r.brief); !ok || label.Stage != "code" {
		t.Errorf("brief %q gives no label of the stage code", r.brief)
	}
}

// A damaged line is found also where a line holds no count and is not
// decoded.
func TestClaudeRecordWithADamagedLine(t *testing.T) {
	t.Parallel()
	const (
		brief  = `{"type":"user","message":{"role":"user","content":"kitsu-task: one code 1\nx"}}`
		answer = `{"type":"assistant","message":{"id":"a","model":"m","usage":{"input_tokens":1,"output_tokens":2}}}`
	)
	for name, c := range map[string]struct {
		lines []string
		known bool
	}{
		"whole":                        {[]string{brief, answer, `{"type":"system"}`}, true},
		"a line that is no JSON":       {[]string{brief, answer, "this is not json"}, false},
		"a line that is cut":           {[]string{brief, `{"type":"system","text":"cut`, answer}, false},
		"a line that is not an object": {[]string{brief, answer, `["a"]`}, false},
	} {
		path := filepath.Join(t.TempDir(), "record.jsonl")
		if err := os.WriteFile(path, []byte(strings.Join(c.lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if r := readClaude(path); (r.why == "") != c.known {
			t.Errorf("%s: known %v (%s), want %v", name, r.why == "", r.why, c.known)
		}
	}
}

// Requirement: Both hosts, scenario: A Codex total that is reset. After a
// context window overflows, Codex sets the running total to a value with
// no input and no output; a total may also fall to a smaller one. The total
// before the fall is carried, and the count is the carried sum and the last
// total.
func TestCodexTotalThatIsReset(t *testing.T) {
	t.Parallel()
	a := Counts{Input: 30, CacheRead: 60, Output: 5}
	small := Counts{Input: 3, CacheRead: 6, Output: 1}
	x := Counts{Input: 4, CacheRead: 8, Output: 2}
	for name, c := range map[string]struct {
		lines []string
		want  Counts
	}{
		"a filler of zero":   {[]string{codexModel("m"), codexCount(a, a), codexCount(Counts{}, Counts{}), codexCount(x, x)}, Counts{Input: 34, CacheRead: 68, Output: 7}},
		"a smaller total":    {[]string{codexModel("m"), codexCount(a, a), codexCount(small, small)}, Counts{Input: 33, CacheRead: 66, Output: 6}},
		"two falls and rise": {[]string{codexModel("m"), codexCount(a, a), codexCount(small, small), codexCount(Counts{}, Counts{}), codexCount(x, x)}, Counts{Input: 37, CacheRead: 74, Output: 8}},
	} {
		if r := readCodex(codexFile(t, c.lines...)); r.why != "" || r.models["m"] != c.want {
			t.Errorf("%s: read %+v (%s), want %+v", name, r.models, r.why, c.want)
		}
	}
}

// A line token_usage_record, which codex-cli 0.168.0 is said to write after
// each response, is not read and does not change the count.
func TestCodexTokenUsageRecordIsIgnored(t *testing.T) {
	t.Parallel()
	a := Counts{Input: 30, CacheRead: 60, Output: 5}
	usage := codexUsageJSON(a)
	record := `{"timestamp":"t","type":"token_usage_record","payload":{"response_id":"resp_1","usage":` + usage +
		`,"turn_token_usage":` + usage + `,"thread_token_usage":` + usage + `}}`
	r := readCodex(codexFile(t, codexModel("m"), record, codexCount(a, a), record))
	if r.why != "" || len(r.models) != 1 || r.models["m"] != a {
		t.Errorf("read %+v (%s), want m %+v", r.models, r.why, a)
	}
}

// The roles and the stages of the token report are those of the roles and
// of the change folder: a role renamed there makes this test fail.
func TestOneVocabulary(t *testing.T) {
	t.Parallel()
	if got, want := slices.Sorted(slices.Values(roleOrder())), slices.Sorted(slices.Values(roles.IDs())); !slices.Equal(got, want) {
		t.Errorf("the token report knows the roles %v, the roles are %v", got, want)
	}
	for _, s := range stageRoles {
		if !roles.Known(s.role) {
			t.Errorf("the stage %s maps to %q, which is no role", s.stage, s.role)
		}
	}
	for _, stage := range []string{change.StageSpec, change.StageDesign, change.StageTests, change.StageCode, change.StageReview, change.StageCommit} {
		if RoleOf(stage) == "" {
			t.Errorf("the stage %s of a change folder is not a stage of a label", stage)
		}
	}
	for _, id := range roles.IDs() {
		if noticeRole("kitsu:"+id) != id {
			t.Errorf("a notice of the role %s is not read as that role", id)
		}
	}
}

// Requirement: Notices as the host gives them, scenario: A record that grows
// after its notice. A start stored at an earlier notice is read again at a
// later one, and keeps its counts when the new reading is unknown.
func TestRecordReadsStoredStartsAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := filepath.Join(dir, "tokens")
	agent, main := filepath.Join(dir, "agent-a1.jsonl"), filepath.Join(dir, "s.jsonl")
	brief := `{"type":"user","message":{"role":"user","content":"kitsu-task: one code 1\nx"}}`
	answer := func(id string) string {
		return `{"type":"assistant","message":{"id":"` + id + `","model":"m","usage":{"input_tokens":1,"output_tokens":2}}}`
	}
	write := func(path string, lines ...string) {
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stored := func() Start {
		starts, _, err := Load(store)
		at := slices.IndexFunc(starts, func(s Start) bool { return s.Agent == "agent-a1" })
		if err != nil || at < 0 {
			t.Fatalf("loaded %+v, error %v", starts, err)
		}
		return starts[at]
	}
	write(main, answer("s"))
	write(agent, brief, answer("x"))
	if err := Record(store, Claude, Notice{Session: "s", Transcript: main, AgentID: "a1", AgentTranscript: agent, AgentType: "developer"}); err != nil {
		t.Fatal(err)
	}
	write(agent, brief, answer("x"), answer("y"))
	if err := Record(store, Claude, Notice{Session: "s", Transcript: main}); err != nil {
		t.Fatal(err)
	}
	if got := stored(); !got.Known || got.Task != "one" || got.Models["m"] != (Counts{Input: 2, Output: 4}) {
		t.Errorf("after the later notice the start is %+v, want both answers", got)
	}
	write(agent, brief, "not json", answer("x"))
	if err := Record(store, Claude, Notice{Session: "s", Transcript: main}); err != nil {
		t.Fatal(err)
	}
	if got := stored(); !got.Known || got.Models["m"] != (Counts{Input: 2, Output: 4}) {
		t.Errorf("an unknown reading replaced the stored counts: %+v", got)
	}
}

// Requirement: Notices as the host gives them, scenario: An internal agent of
// the host. A notice of Claude Code that names an agent with an empty
// agent_type stores the session and no start; on Codex the start is kept.
func TestRecordIgnoresInternalAgentsOfClaude(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	main := filepath.Join(dir, "s.jsonl")
	line := `{"type":"assistant","message":{"id":"m","model":"m","usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(main, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]int{Claude: 1, Codex: 2} {
		store := filepath.Join(dir, host)
		if err := Record(store, host, Notice{Session: "s", Transcript: main, AgentID: "aside", AgentTranscript: main}); err != nil {
			t.Fatal(err)
		}
		if starts, _, _ := Load(store); len(starts) != want {
			t.Errorf("%s: stored %+v, want %d files", host, starts, want)
		}
	}
	if err := Record(filepath.Join(dir, "none"), Claude, Notice{Session: "s", AgentID: "aside"}); err != nil {
		t.Errorf("a notice of an internal agent alone: %v", err)
	}
}

// What `kitsu tokens record` stores is for its user alone: the folders it
// creates, the folder of kitsu among them, and the file of a start.
func TestRecordKeepsItsFoldersPrivate(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits; a file there has the access of its folder")
	}
	dir := t.TempDir()
	empty := filepath.Join(dir, "agent-a1.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "kitsu", "tokens")
	if err := Record(store, Claude, Notice{Session: "s", AgentID: "a1", AgentTranscript: empty, AgentType: "kitsu:architect"}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(store, "s", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("stored the files %v (%v), want one", files, err)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Join(dir, "kitsu"): 0o700, store: 0o700, filepath.Join(store, "s"): 0o700, files[0]: 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has the mode %o, want %o", path, got, want)
		}
	}
}
