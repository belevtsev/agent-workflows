package cli

import (
	"bytes"
	json "encoding/json/v2"
	"path/filepath"
	"slices"
	"testing"
)

// taskUsageHookInput is a Claude Code PreToolUse input for command, with
// the session's transcript at transcript.
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

// Oracle: before `cw task assign --task DIR`, the hook stores one sample of
// the session from the input's transcript_path: session s1, the transcript's
// in-task tokens, and opusUSD of its requests, estimated from the built-in
// table. A broken transcript, a Codex input and a command naming no task
// store nothing: the record keeps its revision. Every case exits 0 without
// output, so no decision, deny or ask, reaches the host.
func TestTaskUsageHookSamplesTheNamedTaskAndNeverDecides(t *testing.T) {
	h := newTaskBundleHarness(t)
	dir := t.TempDir()
	transcript := filepath.Join(dir, "s1.jsonl")
	requests := []usageRequest{firstRequest, {20, 0, 300, 15}}
	writeUsageTranscript(t, transcript, -1, requests...)
	assign := "cw task assign --task " + h.record.Directory + " --input brief.json"

	for _, c := range []struct {
		name, transcript, command string
		codex                     bool
	}{
		{"a broken transcript", filepath.Join(dir, "broken.jsonl"), assign, false},
		{"Codex", transcript, assign, true},
		{"no task", transcript, "cw task start --plan plan.json", false},
		{"not cw", transcript, "echo cw task --task " + h.record.Directory, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			writeTranscriptLines(t, filepath.Join(dir, "broken.jsonl"), `{"type":"assistant","message":`, "not json")
			before := loadUsageRecord(t, h).Revision
			runTaskUsageHook(t, h, taskUsageHookInput(t, h.repo, c.transcript, c.command, c.codex))
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
}

// Oracle: hookTaskDirs finds the last --task of each cw task command, by any
// path and past cw's global options, resolves a relative directory from cwd,
// ~/ from home and a task ID in the store, and skips a directory written
// with a variable and words that only mention cw.
func TestTaskUsageHookFindsEachCwTaskCommandsTask(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	store := func() (string, error) { return "/store", nil }
	got := hookTaskDirs(`/bin/cw --home /h task verify --task a --task b; ./cw task show --task=~/c && cw task run --task `+id+
		` | cw task run --task "$T"; echo cw task --task d; cw --task e task`, "/repo", "/home/u", store)
	want := []string{"/repo/b", "/home/u/c", "/store/" + id}
	if !slices.Equal(got, want) {
		t.Fatalf("hookTaskDirs = %q, want %q", got, want)
	}
	if got := hookTaskDirs("cw task verify --task x; cw task show --task x", "/r", "", store); !slices.Equal(got, []string{"/r/x"}) {
		t.Fatalf("duplicates: %q", got)
	}
}
