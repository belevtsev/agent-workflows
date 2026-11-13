package cli

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// taskUsageHookInput is a PreToolUse input for command, with the session's
// transcript at transcript; codex adds the two members only Codex sends.
func taskUsageHookInput(t *testing.T, cwd, transcript, command string, codex bool) []byte {
	t.Helper()
	input := map[string]any{"session_id": "s1", "transcript_path": transcript, "cwd": cwd, "hook_event_name": "PreToolUse", "tool_name": "Bash",
		"tool_input": map[string]any{"command": command}}
	if codex {
		input["turn_id"], input["model"] = "t", "m"
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// runTaskUsageHook runs cw hook task-usage on input and requires what every
// case needs: no error (exit 0) and no output, so it never decides.
func runTaskUsageHook(t *testing.T, h *taskBundleHarness, input []byte) {
	t.Helper()
	stderr := new(bytes.Buffer)
	h.config.In, h.config.Err = bytes.NewReader(input), stderr
	h.out.Reset()
	if err := Execute(t.Context(), []string{"hook", "task-usage"}, h.config); err != nil || h.out.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("cw hook task-usage: %v, stdout %q, stderr %q; want exit 0 and no output", err, h.out.String(), stderr.String())
	}
}

// writeCodexRollout writes a Codex rollout, under a sessions directory as
// Codex keeps them, with one in-task request of 100 input and 20 output
// tokens, and returns its path.
func writeCodexRollout(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions", "2100", "01", "01", "rollout-2100-01-01T00-00-00-c1.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTranscriptLines(t, path,
		`{"timestamp":"`+inTask+`","type":"session_meta","payload":{"id":"c1"}}`,
		`{"timestamp":"`+inTask+`","type":"turn_context","payload":{"model":"gpt-6.1-sol"}}`,
		`{"timestamp":"`+inTask+`","type":"token_usage_record","payload":{"response_id":"resp-1","usage":{"input_tokens":100,"cached_input_tokens":0,"output_tokens":20}}}`)
	return path
}

// Oracle: before `cw task assign --task DIR`, the hook stores one sample of
// the session from the input's transcript_path: session s1, the transcript's
// in-task tokens, and opusUSD of its requests, estimated from the built-in
// table. A broken transcript and a command naming no task store nothing: the
// record keeps its revision. Under Codex it samples the rollout the input
// names (review finding 5): session c1 from codex_rollout with its 100 input
// and 20 output tokens. Every case exits 0 without output, so no decision,
// deny or ask, reaches the host.
func TestTaskUsageHookSamplesTheNamedTaskAndNeverDecides(t *testing.T) {
	h := newTaskBundleHarness(t)
	dir := t.TempDir()
	transcript := filepath.Join(dir, "s1.jsonl")
	requests := []usageRequest{firstRequest, {20, 0, 300, 15}}
	writeUsageTranscript(t, transcript, -1, requests...)
	assign := "cw task assign --task " + h.record.Directory + " --input brief.json"

	for _, c := range []struct {
		name, transcript, command string
	}{
		{"a broken transcript", filepath.Join(dir, "broken.jsonl"), assign},
		{"no task", transcript, "cw task start --plan plan.json"},
		{"not cw", transcript, "echo cw task --task " + h.record.Directory},
	} {
		t.Run(c.name, func(t *testing.T) {
			writeTranscriptLines(t, filepath.Join(dir, "broken.jsonl"), `{"type":"assistant","message":`, "not json")
			before := loadUsageRecord(t, h).Revision
			runTaskUsageHook(t, h, taskUsageHookInput(t, h.repo, c.transcript, c.command, false))
			if record := loadUsageRecord(t, h); record.Revision != before || len(record.Usage) != 0 {
				t.Fatalf("revision %d (was %d), usage %+v; want nothing stored", record.Revision, before, record.Usage)
			}
		})
	}

	// The command line is read as the shell reads it: the cw task command
	// after another command, with the task's directory relative to cwd.
	relative, err := filepath.Rel(h.repo, h.record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	runTaskUsageHook(t, h, taskUsageHookInput(t, h.repo, transcript, "true && CW=1 cw task assign --input - --task="+relative, false))
	record := loadUsageRecord(t, h)
	if len(record.Usage) != 1 {
		t.Fatalf("usage %+v, want one sample", record.Usage)
	}
	sample := record.Usage[0]
	if sample.SessionID != "s1" || !usdNear(sample.USD, opusUSD(requests...)) || !sample.USDEstimated || sample.Tokens == nil || sample.Tokens.Output != 20 {
		t.Fatalf("sample %+v, want session s1 with %v estimated USD and 20 output tokens", sample, opusUSD(requests...))
	}

	runTaskUsageHook(t, h, taskUsageHookInput(t, h.repo, writeCodexRollout(t), "cw task run --check unit --task "+h.record.Directory, true))
	record = loadUsageRecord(t, h)
	if len(record.Usage) != 2 {
		t.Fatalf("usage %+v, want a second sample from the Codex rollout", record.Usage)
	}
	codex := record.Usage[1]
	if codex.Source != taskflow.UsageSourceCodex || codex.SessionID != "c1" || codex.Tokens == nil || codex.Tokens.UncachedInput != 100 || codex.Tokens.Output != 20 {
		t.Fatalf("Codex sample %+v (tokens %+v), want session c1 from codex_rollout with 100 input and 20 output tokens", codex, codex.Tokens)
	}
}

// Oracle: hookTaskDirs finds the last --task of each cw task command, by any
// path and past cw's global options, resolves a relative directory from cwd
// and ~/ from home, and skips a directory written with a variable and words
// that only mention cw. A task ID is read from the store the command itself
// would use (review finding 8): its CW_TASK_STORE assignment, else the
// hook's CW_TASK_STORE, else its --state-dir, else XDG_STATE_HOME (its
// assignment first), else the state directory under its --home or HOME, else
// under the hook's home.
func TestTaskUsageHookFindsEachCwTaskCommandsTask(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	config := func(env map[string]string) Config {
		return Config{HomeDir: func() (string, error) { return "/home/u", nil }, Getenv: func(name string) string { return env[name] }}
	}
	plain := config(nil)
	got := hookTaskDirs(`/bin/cw --home /h task verify --task a --task b; ./cw task show --task=~/c && cw task run --task "$T"; echo cw task --task d; cw --task e task`, "/repo", plain)
	if want := []string{"/repo/b", "/home/u/c"}; !slices.Equal(got, want) {
		t.Fatalf("hookTaskDirs = %q, want %q", got, want)
	}
	if got := hookTaskDirs("cw task verify --task x; cw task show --task x", "/r", plain); !slices.Equal(got, []string{"/r/x"}) {
		t.Fatalf("duplicates: %q", got)
	}
	state := "/home/u/.local/state/agent-workflows/workflow-tasks/"
	for _, c := range []struct {
		command string
		env     map[string]string
		want    string
	}{
		{"cw task show --task " + id, nil, state + id},
		{"cw task show --task " + id, map[string]string{"CW_TASK_STORE": "/hook-store"}, "/hook-store/" + id},
		{"CW_TASK_STORE=/line-store cw task show --task " + id, map[string]string{"CW_TASK_STORE": "/hook-store"}, "/line-store/" + id},
		{"CW_TASK_STORE=rel cw task show --task " + id, nil, "/repo/rel/" + id},
		{"cw --state-dir /s task show --task " + id, nil, "/s/workflow-tasks/" + id},
		{"cw --state-dir=/s task show --task " + id, map[string]string{"XDG_STATE_HOME": "/x"}, "/s/workflow-tasks/" + id},
		{"cw task show --task " + id, map[string]string{"XDG_STATE_HOME": "/x"}, "/x/agent-workflows/workflow-tasks/" + id},
		{"XDG_STATE_HOME=/y cw task show --task " + id, map[string]string{"XDG_STATE_HOME": "/x"}, "/y/agent-workflows/workflow-tasks/" + id},
		{"cw --home /h task show --task " + id, nil, "/h/.local/state/agent-workflows/workflow-tasks/" + id},
		{"HOME=/g cw task show --task " + id, nil, "/g/.local/state/agent-workflows/workflow-tasks/" + id},
	} {
		got := hookTaskDirs(c.command, "/repo", config(c.env))
		if !slices.Equal(got, []string{c.want}) {
			t.Errorf("hookTaskDirs(%q, env %v) = %q, want %q", c.command, c.env, got, c.want)
		}
	}
	if got := hookTaskDirs("XDG_STATE_HOME=relative cw task show --task "+id, "/repo", plain); len(got) != 0 {
		t.Errorf("a relative XDG_STATE_HOME gave %q; cw refuses it, so nothing is sampled", got)
	}
}
