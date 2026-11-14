package tokens

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNoticeRole(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"architect":                 "architect",
		"agent-workflows:architect": "architect",
		"a:b:git":                   "git",
		"Explore":                   "",
		"agent-workflows:":          "",
		"":                          "",
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

const (
	codexModel   = `{"type":"turn_context","payload":{"model":"m"}}`
	codexRequest = `{"type":"token_usage_record","payload":{"response_id":"r1","usage":{"input_tokens":100,"cached_input_tokens":60,"cache_write_input_tokens":10,"output_tokens":5}}}`
	codexRunning = `{"type":"token_usage_record","payload":{"usage":{"input_tokens":100,"cached_input_tokens":60,"cache_write_input_tokens":10,"output_tokens":5}}}`
)

func TestCodexRunningTotalIsNotCounted(t *testing.T) {
	t.Parallel()
	r := readCodex(codexFile(t, codexModel, codexRequest, codexRequest, codexRunning))
	want := Counts{Input: 30, CacheWrite: 10, CacheRead: 60, Output: 5}
	if r.why != "" || r.models["m"] != want {
		t.Errorf("read %+v (%s), want %+v once", r.models, r.why, want)
	}
}

func TestCodexRecordOfRunningTotalsHoldsNoCount(t *testing.T) {
	t.Parallel()
	r := readCodex(codexFile(t, codexModel, codexRunning))
	if r.why == "" || r.models != nil {
		t.Errorf("read %+v, want unknown tokens", r.models)
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
	notice := Notice{Session: "s", AgentID: "a1", AgentTranscript: empty, AgentType: "agent-workflows:architect"}
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

// A damaged line is found also where a line holds no count and is not
// decoded.
func TestClaudeRecordWithADamagedLine(t *testing.T) {
	t.Parallel()
	const (
		brief  = `{"type":"user","message":{"role":"user","content":"cw-task: one code 1\nx"}}`
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
