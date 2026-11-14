// Package tokens_test holds the scenarios of the specification token-report
// (openspec/changes/add-role-workflow/specs/token-report/spec.md), except
// those of the requirement "Published comparison", and the check of task 4.4
// of openspec/changes/add-role-workflow/tasks.md (the plugin's hooks).
//
// The host's records S1 and S2 are generated here from the design, section
// "Tokens" (openspec/changes/add-role-workflow/design.md): a Claude Code
// transcript is one JSON object per line, with "user" and "assistant" lines;
// a Codex rollout is one JSON object per line, with "type" and "payload".
package tokens_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// counts are the four kinds of tokens of one start or one answer.
type counts struct {
	Input, CacheWrite, CacheRead, Output int
}

func (c counts) total() int { return c.Input + c.CacheWrite + c.CacheRead + c.Output }

// split gives the generic breakdown of a start of total tokens: 10% uncached
// input, 15% cache write, 5% output and the rest cache read.
func split(total int) counts {
	c := counts{Input: total / 10, CacheWrite: total * 15 / 100, Output: total / 20}
	c.CacheRead = total - c.Input - c.CacheWrite - c.Output
	return c
}

// start is one start of a role in the test data.
type start struct {
	ID       string // the agent id the host gives
	Role     string // the role, for the reader of the test only
	Label    string // the first line of the brief; empty for none
	Sentence string // the second line of the brief
	Model    string
	Tokens   counts
	Answers  int  // how many answers the start is split into
	Empty    bool // the record is an empty file
	Missing  bool // the record holds no count (Codex: only rate limits)
}

const (
	opus   = "claude-opus-4-1-20250805"
	sonnet = "claude-sonnet-4-5-20250929"
	haiku  = "claude-haiku-4-5-20251001"
	codex  = "gpt-5-codex"
)

// developerSentence is a sentence of the brief of the first developer start,
// searched for by the scenario "No text is kept".
const developerSentence = "Make the greeter print Hello, Ada when the name Ada is given, and keep src/app.txt as it is."

// s1Session is the session of S1, and s1SessionTokens its own tokens.
const s1Session = "5f0c2a7e-3d1b-4c8e-9a6f-1b2c3d4e5f60"

var s1SessionTokens = split(40000)

// s1Starts are the role starts of S1, in the order of the fixture's table.
func s1Starts() []start {
	return []start{
		{ID: "a1f0001", Role: "lookup", Label: "cw-task: add-greeting lookup 1", Sentence: "Find where the app prints its text.", Model: haiku, Tokens: split(6000), Answers: 2},
		{ID: "a1f0002", Role: "lookup", Label: "cw-task: add-greeting lookup 1", Sentence: "Find the tests of the app.", Model: haiku, Tokens: split(4000), Answers: 1},
		{ID: "a1f0003", Role: "spec-writer", Label: "cw-task: add-greeting spec 1", Sentence: "Write the specification of the greeting.", Model: opus, Tokens: split(60000), Answers: 3},
		{ID: "a1f0004", Role: "reviewer", Label: "cw-task: add-greeting spec-review 1", Sentence: "Review the specification of the greeting.", Model: opus, Tokens: split(30000), Answers: 2},
		{ID: "a1f0005", Role: "architect", Label: "cw-task: add-greeting design 1", Sentence: "Write the design of the greeting.", Model: opus, Tokens: split(80000), Answers: 3},
		{ID: "a1f0006", Role: "reviewer", Label: "cw-task: add-greeting design-review 1", Sentence: "Review the design of the greeting.", Model: opus, Tokens: split(30000), Answers: 2},
		{ID: "a1f0007", Role: "quality-engineer", Label: "cw-task: add-greeting tests 1", Sentence: "Write the tests of the greeting.", Model: sonnet, Tokens: split(90000), Answers: 3},
		{ID: "a1f0008", Role: "developer", Label: "cw-task: add-greeting code 1", Sentence: developerSentence, Model: sonnet,
			Tokens: counts{Input: 20000, CacheWrite: 30000, CacheRead: 140000, Output: 10000}, Answers: 4},
		{ID: "a1f0009", Role: "reviewer", Label: "cw-task: add-greeting review 1", Sentence: "Review the code of the greeting.", Model: opus, Tokens: split(50000), Answers: 2},
		{ID: "a1f0010", Role: "developer", Label: "cw-task: add-greeting code 2", Sentence: "Fix the findings of review 1.", Model: sonnet, Tokens: split(40000), Answers: 2},
		{ID: "a1f0011", Role: "reviewer", Label: "cw-task: add-greeting review 2", Sentence: "Review the fixes of round 2.", Model: opus, Tokens: split(20000), Answers: 1},
		{ID: "a1f0012", Role: "git", Label: "cw-task: add-greeting commit 1", Sentence: "Commit the change with the message feat: add greeting.", Model: haiku, Tokens: split(5000), Answers: 1},
	}
}

// variant changes the test data of S1 as a scenario says.
type variant struct {
	NoGitLabel     bool // the first line of the git brief is removed
	EmptyArchitect bool // the record of the architect start is an empty file
	SecondTask     bool // a developer start of fix-typo with 15,000 tokens
}

func (v variant) starts() []start {
	starts := s1Starts()
	for i := range starts {
		if v.NoGitLabel && starts[i].Role == "git" {
			starts[i].Label = ""
		}
		if v.EmptyArchitect && starts[i].Role == "architect" {
			starts[i].Empty = true
		}
	}
	if v.SecondTask {
		starts = append(starts, start{ID: "a1f0013", Role: "developer", Label: "cw-task: fix-typo code 1",
			Sentence: "Fix the typo in README.md.", Model: sonnet, Tokens: split(15000), Answers: 2})
	}
	return starts
}

// session is a generated record of a host: the notices it gives, in order.
type session struct {
	Host    string // the value of --host
	Notices []string
	Paths   map[string]string // agent id, or "session", to the record's path
}

// writeLines writes JSON lines, each ending with a line end.
func writeLines(t *testing.T, path string, lines []any) {
	t.Helper()
	var b strings.Builder
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	writeFile(t, path, b.String())
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// parts divides n into k parts that add up to n.
func parts(n, k int) []int {
	out := make([]int, k)
	for i := range out {
		out[i] = n / k
	}
	out[k-1] += n - (n/k)*k
	return out
}

// brief is the text of a brief: the label, then the sentence.
func brief(s start) string {
	if s.Label == "" {
		return s.Sentence
	}
	return s.Label + "\n" + s.Sentence + "\nReport what you changed."
}

// claudeUsage is message.usage of an assistant line.
func claudeUsage(c counts) map[string]any {
	return map[string]any{
		"input_tokens":                c.Input,
		"cache_creation_input_tokens": c.CacheWrite,
		"cache_read_input_tokens":     c.CacheRead,
		"output_tokens":               c.Output,
		"service_tier":                "standard",
	}
}

func claudeAssistant(sessionID, msgID, model string, usage counts, text string) map[string]any {
	return map[string]any{
		"type": "assistant", "sessionId": sessionID, "requestId": "req_" + msgID,
		"message": map[string]any{
			"id": msgID, "type": "message", "role": "assistant", "model": model,
			"content": []any{map[string]any{"type": "text", "text": text}},
			"usage":   claudeUsage(usage),
		},
	}
}

// claudeAnswers are the assistant lines of a start of the given counts, split
// over n answers. The first answer is written twice, as Claude Code does
// while it streams: first with part of its output, then whole. Lines with the
// same message.id are one answer and count once.
func claudeAnswers(sessionID, prefix, model string, c counts, n int) []any {
	in, cw, cr, out := parts(c.Input, n), parts(c.CacheWrite, n), parts(c.CacheRead, n), parts(c.Output, n)
	var lines []any
	for i := range n {
		id := fmt.Sprintf("msg_%s_%02d", prefix, i+1)
		whole := counts{Input: in[i], CacheWrite: cw[i], CacheRead: cr[i], Output: out[i]}
		if i == 0 {
			partial := whole
			partial.Output = whole.Output / 2
			lines = append(lines, claudeAssistant(sessionID, id, model, partial, "Reading the files."))
		}
		lines = append(lines, claudeAssistant(sessionID, id, model, whole, "Reading the files."))
		if i == n-1 && n > 1 {
			// The same line once more, unchanged.
			lines = append(lines, claudeAssistant(sessionID, id, model, whole, "Done."))
		}
		if i < n-1 {
			lines = append(lines, map[string]any{"type": "user", "sessionId": sessionID, "message": map[string]any{
				"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_" + id, "content": "ok"}},
			}})
		}
	}
	return lines
}

// claudeAgentRecord writes the record of one role start.
func claudeAgentRecord(t *testing.T, path, sessionID string, s start, asList bool) {
	t.Helper()
	if s.Empty {
		writeFile(t, path, "")
		return
	}
	var content any = brief(s)
	if asList {
		content = []any{map[string]any{"type": "text", "text": brief(s)}}
	}
	lines := []any{map[string]any{
		"type": "user", "sessionId": sessionID, "isSidechain": true, "agentId": s.ID,
		"message": map[string]any{"role": "user", "content": content},
	}}
	lines = append(lines, claudeAnswers(sessionID, s.ID, s.Model, s.Tokens, s.Answers)...)
	writeLines(t, path, lines)
}

// claudeSessionRecord writes the record of the session itself: the
// developer's own prompt, its answers, and a synthetic answer with no usage.
func claudeSessionRecord(t *testing.T, path, sessionID string) {
	t.Helper()
	lines := []any{map[string]any{
		"type": "user", "sessionId": sessionID, "isSidechain": false,
		"message": map[string]any{"role": "user", "content": "Run the task add-greeting on the full path."},
	}}
	lines = append(lines, claudeAnswers(sessionID, "main", opus, s1SessionTokens, 5)...)
	lines = append(lines, claudeAssistant(sessionID, "msg_synthetic_01", "<synthetic>", counts{}, "No response requested."))
	writeLines(t, path, lines)
}

// buildS1 writes S1, changed as v says, into a temporary directory laid out
// as Claude Code lays out its records, and returns its notices: one for each
// role that ended, in order, then one for the end of the session.
func buildS1(t *testing.T, v variant) session {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".claude", "projects", "-tmp-demo")
	sessionPath := filepath.Join(dir, s1Session+".jsonl")
	claudeSessionRecord(t, sessionPath, s1Session)
	s := session{Host: "claude", Paths: map[string]string{"session": sessionPath}}
	for i, st := range v.starts() {
		path := filepath.Join(dir, s1Session, "subagents", "agent-"+st.ID+".jsonl")
		claudeAgentRecord(t, path, s1Session, st, i%2 == 1)
		s.Paths[st.ID] = path
		s.Notices = append(s.Notices, notice(map[string]any{
			"session_id": s1Session, "transcript_path": sessionPath, "cwd": "/tmp/demo",
			"permission_mode": "default", "hook_event_name": "SubagentStop", "stop_hook_active": false,
			"agent_id": st.ID, "agent_type": st.Role, "agent_transcript_path": path,
		}))
	}
	s.Notices = append(s.Notices, notice(map[string]any{
		"session_id": s1Session, "transcript_path": sessionPath, "cwd": "/tmp/demo",
		"hook_event_name": "SessionEnd", "reason": "prompt_input_exit",
	}))
	return s
}

func notice(fields map[string]any) string {
	data, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(data) + "\n"
}

// s2Session is the session of S2, a Codex session on the small path.
const s2Session = "0199a3b4-7c5d-7e6f-8a9b-0c1d2e3f4a5b"

var s2SessionTokens = split(20000)

// s2Starts are the starts of S2. The record of the git start holds only
// token_count events without info, which give the rate limits and no count,
// so its tokens are unknown.
func s2Starts() []start {
	return []start{
		{ID: "b2e0001", Role: "quality-engineer", Label: "cw-task: fix-empty-app tests 1", Sentence: "Write a test for an empty src/app.txt.", Model: codex, Tokens: split(30000), Answers: 2},
		{ID: "b2e0002", Role: "developer", Label: "cw-task: fix-empty-app code 1", Sentence: "Make the app accept an empty src/app.txt.", Model: codex, Tokens: split(50000), Answers: 3},
		{ID: "b2e0003", Role: "reviewer", Label: "cw-task: fix-empty-app review 1", Sentence: "Review the fix of the empty app.", Model: codex, Tokens: split(15000), Answers: 1},
		{ID: "b2e0004", Role: "git", Label: "cw-task: fix-empty-app commit 1", Sentence: "Commit the fix.", Model: codex, Tokens: split(4000), Answers: 1, Missing: true},
	}
}

// codexUsage is a usage object of a token_count event: input_tokens is the
// whole input, of which cached_input_tokens were read from the cache and
// cache_write_input_tokens written to it; output_tokens holds the reasoning.
func codexUsage(c counts) map[string]any {
	in := c.Input + c.CacheWrite + c.CacheRead
	return map[string]any{
		"input_tokens":             in,
		"cached_input_tokens":      c.CacheRead,
		"cache_write_input_tokens": c.CacheWrite,
		"output_tokens":            c.Output,
		"reasoning_output_tokens":  c.Output / 3,
		"total_tokens":             in + c.Output,
	}
}

func codexLine(kind string, payload map[string]any) map[string]any {
	return map[string]any{"timestamp": "2026-11-14T10:00:00.000Z", "type": kind, "payload": payload}
}

func codexMessage(role, text string) map[string]any {
	return codexLine("response_item", map[string]any{"type": "message", "role": role,
		"content": []any{map[string]any{"type": "input_text", "text": text}}})
}

// codexRecord writes a Codex rollout as codex-cli 0.142 writes one: the
// thread, the host's context as a first user message, the model, the brief,
// and after each request a token_count event with the running total of the
// thread and the request itself. A start whose count is missing has only
// token_count events without info.
func codexRecord(t *testing.T, path, text string, s start) {
	t.Helper()
	limits := codexLine("event_msg", map[string]any{"type": "token_count", "info": nil,
		"rate_limits": map[string]any{"limit_id": "codex", "primary": map[string]any{"used_percent": 1.5, "window_minutes": 300}}})
	lines := []any{
		codexLine("session_meta", map[string]any{"id": s2Session, "session_id": s2Session, "cwd": "/tmp/demo",
			"source": "vscode", "thread_source": "user", "cli_version": "0.142.3"}),
		codexMessage("developer", "Follow the rules of the sandbox."),
		codexLine("response_item", map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "# AGENTS.md instructions for /tmp/demo\n\nKeep the tests green."},
			map[string]any{"type": "input_text", "text": "<environment_context>\n  <cwd>/tmp/demo</cwd>\n</environment_context>"},
		}}),
		codexLine("turn_context", map[string]any{"turn_id": "turn-1", "model": s.Model, "cwd": "/tmp/demo", "effort": "high"}),
		codexMessage("user", text),
		limits,
	}
	c := s.Tokens
	in, cw, cr, out := parts(c.Input, s.Answers), parts(c.CacheWrite, s.Answers), parts(c.CacheRead, s.Answers), parts(c.Output, s.Answers)
	var total counts
	for i := range s.Answers {
		if s.Missing {
			break
		}
		last := counts{Input: in[i], CacheWrite: cw[i], CacheRead: cr[i], Output: out[i]}
		total = counts{total.Input + last.Input, total.CacheWrite + last.CacheWrite, total.CacheRead + last.CacheRead, total.Output + last.Output}
		lines = append(lines, codexMessage("assistant", "Working on it."), codexLine("event_msg", map[string]any{"type": "token_count",
			"info": map[string]any{"total_token_usage": codexUsage(total), "last_token_usage": codexUsage(last), "model_context_window": 258400}}))
		if i == 0 {
			// Codex writes some totals twice.
			lines = append(lines, lines[len(lines)-1])
		}
	}
	lines = append(lines, codexLine("event_msg", map[string]any{"type": "task_complete", "turn_id": "turn-1"}))
	writeLines(t, path, lines)
}

// buildS2 writes S2 and returns its notices.
func buildS2(t *testing.T) session {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".codex", "sessions", "2026", "11", "14")
	sessionPath := filepath.Join(dir, "rollout-2026-11-14T10-00-00-"+s2Session+".jsonl")
	codexRecord(t, sessionPath, "Run the task fix-empty-app on the small path.",
		start{Model: codex, Tokens: s2SessionTokens, Answers: 3})
	s := session{Host: "codex", Paths: map[string]string{"session": sessionPath}}
	for _, st := range s2Starts() {
		path := filepath.Join(dir, "rollout-2026-11-14T10-05-00-"+st.ID+".jsonl")
		codexRecord(t, path, brief(st), st)
		s.Paths[st.ID] = path
		s.Notices = append(s.Notices, notice(map[string]any{
			"session_id": s2Session, "transcript_path": sessionPath, "cwd": "/tmp/demo",
			"hook_event_name": "SubagentStop", "agent_id": st.ID, "agent_transcript_path": path,
		}))
	}
	s.Notices = append(s.Notices, notice(map[string]any{
		"session_id": s2Session, "transcript_path": sessionPath, "cwd": "/tmp/demo", "hook_event_name": "SessionEnd",
	}))
	return s
}

// demo is the repository demo of the role-definitions specification.
func demo(t *testing.T) *cwtest.Repo {
	t.Helper()
	repo := cwtest.Demo(t)
	repo.Write("tests/app_test.txt", "app test\n")
	repo.Write("cw-roles.toml", "version = 1\n\n[paths]\ntests = [\"tests/**\"]\n")
	repo.Write("openspec/changes/add-greeting/proposal.md", "# add-greeting\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "fixture of role-definitions")
	return repo
}

// noticeCwd is the cwd the generated notices give. recordNotice replaces it
// with the directory of the repository, as the host would give it.
const noticeCwd = `"cwd":"/tmp/demo"`

// recordNotice gives one notice to cw tokens record in repo, with the cwd of
// repo.
func recordNotice(t *testing.T, repo *cwtest.Repo, host, n string) cwtest.Result {
	t.Helper()
	return recordNoticeIn(t, repo, repo.Dir, host, n)
}

// recordNoticeIn runs cw tokens record in the directory of runIn, with a
// notice whose cwd is cwd.
func recordNoticeIn(t *testing.T, runIn *cwtest.Repo, cwd, host, n string) cwtest.Result {
	t.Helper()
	args := []string{"tokens", "record"}
	if host == "codex" {
		args = append(args, "--host", "codex")
	}
	dir, err := json.Marshal(cwd)
	if err != nil {
		t.Fatal(err)
	}
	return runIn.Run(strings.ReplaceAll(n, noticeCwd, `"cwd":`+string(dir)), cwProgram(t), args...)
}

// cwProgram is the path of the cw under test. It is a full path, because a
// program name is looked up on the PATH of the test process, which may hold
// another cw.
func cwProgram(t *testing.T) string {
	name := "cw"
	if runtime.GOOS == "windows" {
		name = "cw.exe"
	}
	return filepath.Join(cwtest.Binary(t), name)
}

// record gives every notice of s, in order, to cw tokens record in repo and
// fails the test when one does not exit 0.
func record(t *testing.T, repo *cwtest.Repo, s session) {
	t.Helper()
	for i, n := range s.Notices {
		if r := recordNotice(t, repo, s.Host, n); r.Code != 0 {
			t.Fatalf("cw tokens record of notice %d: exit status %d\n%s\nnotice: %s", i+1, r.Code, r.Output(), n)
		}
	}
}

// tokensJSON runs cw tokens with args and --json and decodes its output.
func tokensJSON(t *testing.T, repo *cwtest.Repo, args ...string) map[string]any {
	t.Helper()
	args = append(append([]string{"tokens"}, args...), "--json")
	r := repo.CW(args...)
	if r.Code != 0 {
		t.Fatalf("cw %s: exit status %d\n%s", strings.Join(args, " "), r.Code, r.Output())
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("cw %s did not print one JSON object: %v\n%s", strings.Join(args, " "), err, r.Output())
	}
	return out
}

// tokensText runs cw tokens with args and returns what it printed.
func tokensText(t *testing.T, repo *cwtest.Repo, args ...string) string {
	t.Helper()
	args = append([]string{"tokens"}, args...)
	r := repo.CW(args...)
	if r.Code != 0 {
		t.Fatalf("cw %s: exit status %d\n%s", strings.Join(args, " "), r.Code, r.Output())
	}
	return r.Stdout
}

// recorded is the repository demo in which S1, changed as v says, is
// recorded.
func recorded(t *testing.T, v variant) *cwtest.Repo {
	t.Helper()
	repo := demo(t)
	record(t, repo, buildS1(t, v))
	return repo
}

// field returns obj[name], failing the test when it is absent.
func field(t *testing.T, obj map[string]any, name string) any {
	t.Helper()
	v, ok := obj[name]
	if !ok {
		t.Fatalf("no field %q in %s", name, show(obj))
	}
	return v
}

// wantNumber fails the test unless obj[name] is the number want.
func wantNumber(t *testing.T, obj map[string]any, name string, want int) {
	t.Helper()
	got, ok := field(t, obj, name).(float64)
	if !ok || got != float64(want) {
		t.Errorf("%s is %v, want %d, in %s", name, obj[name], want, show(obj))
	}
}

// entries returns obj[name] as a list of objects.
func entries(t *testing.T, obj map[string]any, name string) []map[string]any {
	t.Helper()
	list, ok := field(t, obj, name).([]any)
	if !ok {
		if obj[name] == nil {
			return nil
		}
		t.Fatalf("%s is not a list: %s", name, show(obj))
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("an entry of %s is not an object: %v", name, item)
		}
		out = append(out, m)
	}
	return out
}

// find returns the entry of list whose fields match, or fails the test.
func find(t *testing.T, list []map[string]any, match map[string]any) map[string]any {
	t.Helper()
	for _, e := range list {
		ok := true
		for k, v := range match {
			if fmt.Sprint(e[k]) != fmt.Sprint(v) {
				ok = false
			}
		}
		if ok {
			return e
		}
	}
	t.Fatalf("no entry with %v in %v", match, list)
	return nil
}

func show(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}
