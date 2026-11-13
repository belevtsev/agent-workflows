package main_test

import (
	json "encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Tests for the Codex hooks file, hooks/codex-hooks.json, which
// .codex-plugin/plugin.json names in place of hooks/hooks.json
// (docs/hooks.md, Codex).

// The agreed Codex PreToolUse handlers. Codex ignores a handler's if, so
// there are two, without one: the gate and the usage sampler. Each passes
// --host codex and, when cw rejects the flag with a usage error (exit 2, a
// cw older than --host), runs the same hook without it, so an older cw still
// gates (it then tells Codex apart by its input).
const (
	codexPreToolUseCommand      = `command -v cw >/dev/null 2>&1 || exit 0; cw hook pre-publish --host codex 2>/dev/null; [ "$?" -ne 2 ] || cw hook pre-publish; exit 0`
	codexPreToolUseUsageCommand = `command -v cw >/dev/null 2>&1 || exit 0; cw hook task-usage --host codex >/dev/null 2>&1; [ "$?" -ne 2 ] || cw hook task-usage >/dev/null 2>&1; exit 0`
)

// codexHooksRead reads ROOT/hooks/codex-hooks.json and decodes it strictly.
func codexHooksRead(t *testing.T) hooksFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "hooks", "codex-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file hooksFile
	if err := json.Unmarshal(data, &file, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("codex-hooks.json does not match the agreed shape: %v", err)
	}
	return file
}

func TestCodexHooksFileShape(t *testing.T) {
	// Oracle: .codex-plugin/plugin.json names ./hooks/codex-hooks.json,
	// which Codex runs instead of hooks/hooks.json. The file has a
	// description; SessionStart with the matcher startup|resume|clear|compact
	// and one handler that runs cw hook session-start --host codex with the
	// plugin root and data directory Codex sets, and the same command without
	// --host as the fallback; PreToolUse with one Bash group of exactly the
	// two agreed handlers, without if. Every entry is an inline command with
	// a timeout from 1 to 60 that never says exec, and every PreToolUse
	// command ends with exit 0.
	var manifest struct {
		Hooks string `json:"hooks"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", ".codex-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Hooks != "./hooks/codex-hooks.json" {
		t.Fatalf(".codex-plugin/plugin.json hooks %q (%v), want ./hooks/codex-hooks.json", manifest.Hooks, err)
	}
	file := codexHooksRead(t)
	if strings.TrimSpace(file.Description) == "" {
		t.Fatal("description is empty")
	}
	execWord := regexp.MustCompile(`\bexec\b`)
	events := make([]string, 0, len(file.Hooks))
	for event, groups := range file.Hooks {
		events = append(events, event)
		for _, group := range groups {
			for _, entry := range group.Hooks {
				if entry.Type != "command" || entry.If != "" || entry.Timeout < 1 || entry.Timeout > 60 || execWord.MatchString(entry.Command) {
					t.Errorf("%s entry %+v: want type command, no if, a timeout in 1..60 and no exec", event, entry)
				}
				for _, forbidden := range []string{".sh", ".py", ".bash", ".zsh", "/scripts/", "/hooks/"} {
					if strings.Contains(entry.Command, forbidden) {
						t.Errorf("%s command %q references %q; commands must be inline", event, entry.Command, forbidden)
					}
				}
			}
		}
	}
	slices.Sort(events)
	if !slices.Equal(events, []string{"PreToolUse", "SessionStart"}) {
		t.Fatalf("hook events %v, want [PreToolUse SessionStart]", events)
	}
	session := file.Hooks["SessionStart"]
	if len(session) != 1 || session[0].Matcher != "startup|resume|clear|compact" || len(session[0].Hooks) != 1 {
		t.Fatalf("SessionStart groups %+v", session)
	}
	command := session[0].Hooks[0].Command
	for _, want := range []string{
		`f="${PLUGIN_ROOT}/global/AGENTS.md"`,
		`cw hook session-start --host codex --instructions "$f" --cache "${PLUGIN_DATA}"`,
		`if [ "$s" -eq 2 ]; then out=$(cw hook session-start --instructions "$f" --cache "${PLUGIN_DATA}" 2>/dev/null)`,
	} {
		if !strings.Contains(command, want) {
			t.Errorf("SessionStart command %q does not contain %q", command, want)
		}
	}
	pre := file.Hooks["PreToolUse"]
	if len(pre) != 1 || pre[0].Matcher != "Bash" || len(pre[0].Hooks) != 2 {
		t.Fatalf("PreToolUse groups %+v, want one Bash group with two handlers", pre)
	}
	if got := []string{pre[0].Hooks[0].Command, pre[0].Hooks[1].Command}; !slices.Equal(got, []string{codexPreToolUseCommand, codexPreToolUseUsageCommand}) {
		t.Fatalf("PreToolUse commands %q, want %q and %q", got, codexPreToolUseCommand, codexPreToolUseUsageCommand)
	}
	if pre[0].Hooks[0].Timeout != 30 || pre[0].Hooks[1].Timeout != 15 || session[0].Hooks[0].Timeout != 15 {
		t.Fatalf("timeouts %d, %d and %d, want 30 for the gate and 15 for the sampler and session start", pre[0].Hooks[0].Timeout, pre[0].Hooks[1].Timeout, session[0].Hooks[0].Timeout)
	}
}

// codexOldCW stands in for a cw older than --host: given --host it fails
// with cobra's usage error (exit 2) before reading stdin, as such a cw does;
// otherwise it appends its arguments and stdin to $CW_STUB_LOG and prints
// the line in $CW_STUB_OUT.
const codexOldCW = `#!/bin/sh
for a in "$@"; do
  if [ "$a" = --host ]; then echo "Error: unknown flag: --host" >&2; exit 2; fi
done
{ printf 'args: %s\n' "$*"; printf 'stdin: '; cat; printf '\n'; } >>"$CW_STUB_LOG"
printf '%s\n' "$CW_STUB_OUT"
`

// codexFailingCW stands in for a cw that knows --host and fails with a
// status other than 2: it logs each call and exits 1 with no output.
const codexFailingCW = `#!/bin/sh
printf 'args: %s\n' "$*" >>"$CW_STUB_LOG"
exit 1
`

func TestCodexHooksEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/cw")
	}
	if runtime.GOOS == "windows" {
		t.Skip("hook commands run under sh")
	}
	file := codexHooksRead(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := buildPlainCW(t)
	withCW := filepath.Dir(binary) + ":/usr/bin:/bin"
	const withoutCW = "/usr/bin:/bin"
	for _, dir := range []string{"/usr/bin", "/bin"} {
		if _, err := os.Stat(filepath.Join(dir, "cw")); err == nil {
			t.Skipf("%s/cw exists; cannot run the hooks without cw on PATH", dir)
		}
	}
	home := hooksCanonicalTempDir(t)
	store := filepath.Join(hooksCanonicalTempDir(t), "store")
	repo := hooksCanonicalTempDir(t)
	hooksGit(t, home, repo, "init", "-q")
	hooksWriteFile(t, repo, "source.txt", "source\n")
	// Codex sets PLUGIN_ROOT and PLUGIN_DATA; the file reads only those.
	data := hooksCanonicalTempDir(t)
	env := func(path string, extra ...string) []string {
		return append([]string{"PATH=" + path, "HOME=" + home, "CW_TASK_STORE=" + store, "PLUGIN_ROOT=" + root, "PLUGIN_DATA=" + data}, extra...)
	}
	plan := filepath.Join(hooksCanonicalTempDir(t), "plan.json")
	hooksWriteFile(t, filepath.Dir(plan), "plan.json", `{"title":"T","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`)
	start := exec.CommandContext(t.Context(), binary, "task", "start", "--full", "--repo", repo, "--plan", plan,
		"--policy", filepath.Join(root, "skills", "task-orchestration", "model-policy.yaml"), "--summary", "--store", store)
	start.Env = []string{"HOME=" + home, "PATH=" + withoutCW}
	start.Dir = t.TempDir()
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("cw task start: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 {
		t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	record, err := taskflow.Load(filepath.Join(store, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	// preStdin is a PreToolUse input as both hosts send it, without the
	// turn_id and model only Codex adds: the host comes from --host.
	preStdin := func(t *testing.T, cwd, bash string) []byte {
		t.Helper()
		stdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": cwd, "tool_input": map[string]any{"command": bash}})
		if err != nil {
			t.Fatal(err)
		}
		return stdin
	}
	gate := file.Hooks["PreToolUse"][0].Hooks[0].Command
	usage := file.Hooks["PreToolUse"][0].Hooks[1].Command
	sessionCommand := file.Hooks["SessionStart"][0].Hooks[0].Command
	const codexDeny = "agent-workflows: Codex cannot ask for confirmation, so this command is denied where Claude Code would ask: "

	t.Run("the gate denies under --host codex", func(t *testing.T) {
		// Oracle (OD-11): the open record denies git commit, and the reason
		// ends by naming the option variables, as every Codex reason does;
		// cw task abandon, an ask under Claude Code, is a deny carrying the
		// question; echo passes with no output.
		code, stdout, stderr := hooksSh(t, gate, preStdin(t, repo, "git commit -m x"), env(withCW))
		if code != 0 {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		if reason := hooksExpectDecision(t, stdout, "deny"); !strings.Contains(reason, record.ID) || !strings.HasSuffix(reason, "CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT=false.") {
			t.Fatalf("deny reason %q does not name task %s and the option variables", reason, record.ID)
		}
		code, stdout, stderr = hooksSh(t, gate, preStdin(t, repo, "cw task abandon --task "+record.Directory+" --reason x"), env(withCW))
		if reason := hooksExpectDecision(t, stdout, "deny"); code != 0 || !strings.HasPrefix(reason, codexDeny) || !strings.Contains(reason, record.ID) {
			t.Fatalf("exit %d stderr %q reason %q, want a deny starting %q naming %s", code, stderr, reason, codexDeny, record.ID)
		}
		if code, stdout, stderr := hooksSh(t, gate, preStdin(t, repo, "echo x"), env(withCW)); code != 0 || stdout != "" {
			t.Fatalf("echo: exit %d stdout %q stderr %q, want exit 0 and no output", code, stdout, stderr)
		}
	})

	t.Run("an older cw without --host still gates", func(t *testing.T) {
		// Oracle: a cw that rejects --host with exit 2 gets the same hook
		// again without the flag, with the whole input on stdin, and its
		// decision reaches Codex unchanged; the usage error is not shown.
		// The sampler and session start fall back the same way.
		log := filepath.Join(hooksCanonicalTempDir(t), "calls")
		const deny = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"old"}}`
		path := hooksStubCW(t, codexOldCW)
		input := preStdin(t, repo, "git commit -m x")
		code, stdout, stderr := hooksSh(t, gate, input, env(path, "CW_STUB_LOG="+log, "CW_STUB_OUT="+deny))
		if code != 0 || stdout != deny+"\n" || stderr != "" {
			t.Fatalf("exit %d stdout %q stderr %q; want exit 0, the older cw's deny and no stderr", code, stdout, stderr)
		}
		code, stdout, stderr = hooksSh(t, usage, input, env(path, "CW_STUB_LOG="+log, "CW_STUB_OUT=ignored"))
		if code != 0 || stdout != "" || stderr != "" {
			t.Fatalf("task-usage: exit %d stdout %q stderr %q; want exit 0 and no output", code, stdout, stderr)
		}
		const context = `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"old"}}`
		code, stdout, stderr = hooksSh(t, sessionCommand, []byte(`{"source":"startup"}`), env(path, "CW_STUB_LOG="+log, "CW_STUB_OUT="+context))
		if code != 0 || stdout != context+"\n" {
			t.Fatalf("session-start: exit %d stdout %q stderr %q; want exit 0 and the older cw's context", code, stdout, stderr)
		}
		calls, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		want := "args: hook pre-publish\nstdin: " + string(input) + "\n" +
			"args: hook task-usage\nstdin: " + string(input) + "\n" +
			"args: hook session-start --instructions " + filepath.Join(root, "global", "AGENTS.md") + " --cache " + data + "\nstdin: {\"source\":\"startup\"}\n"
		if got := string(calls); got != want {
			t.Fatalf("calls without --host:\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("only a usage error runs the hook again", func(t *testing.T) {
		// Oracle: a cw that knows --host and fails with exit 1 is called
		// once: the fallback is for a rejected flag, not for any failure.
		log := filepath.Join(hooksCanonicalTempDir(t), "calls")
		code, stdout, _ := hooksSh(t, gate, preStdin(t, repo, "git commit -m x"), env(hooksStubCW(t, codexFailingCW), "CW_STUB_LOG="+log))
		calls, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 || stdout != "" || string(calls) != "args: hook pre-publish --host codex\n" {
			t.Fatalf("exit %d stdout %q calls %q; want exit 0, no output and one call with --host codex", code, stdout, calls)
		}
	})

	t.Run("without cw the gate passes and session start says it is inactive", func(t *testing.T) {
		// Oracle (E4): with no cw on PATH the PreToolUse commands exit 0
		// with no output, and session start prints global/AGENTS.md and the
		// inactive-gate line.
		for _, command := range []string{gate, usage} {
			if code, stdout, stderr := hooksSh(t, command, preStdin(t, repo, "git commit -m x"), env(withoutCW)); code != 0 || stdout != "" {
				t.Fatalf("exit %d stdout %q stderr %q, want exit 0 and no output", code, stdout, stderr)
			}
		}
		agents, err := os.ReadFile(filepath.Join(root, "global", "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		if code, stdout, stderr := hooksSh(t, sessionCommand, []byte(`{"source":"startup"}`), env(withoutCW)); code != 0 || stdout != string(agents)+gateInactiveLine(t)+"\n" {
			t.Fatalf("exit %d stderr %q stdout %q; want the file and the inactive-gate line", code, stderr, stdout)
		}
	})

	t.Run("session start adds the context on startup and nothing on resume", func(t *testing.T) {
		// Oracle (F19): under --host codex, startup gives global/AGENTS.md
		// and the cw gate install line, and resume one SessionStart object
		// without additionalContext, exactly as cw wrote it.
		agents, err := os.ReadFile(filepath.Join(root, "global", "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		input := func(source string) []byte {
			data, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": source, "cwd": repo, "model": "m"})
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		code, stdout, stderr := hooksSh(t, sessionCommand, input("startup"), env(withCW))
		var output struct {
			HookSpecificOutput struct {
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(stdout), &output); code != 0 || err != nil {
			t.Fatalf("startup: exit %d stderr %q stdout %q (%v)", code, stderr, stdout, err)
		}
		const gateLine = "git hooks absent, so a commit or push from a script, an alias or a terminal skips the verify gate: suggest cw gate install to the user"
		if want := strings.TrimRight(string(agents), "\n") + "\n\n" + gateLine; output.HookSpecificOutput.AdditionalContext != want {
			t.Fatalf("startup additionalContext ends %q, want global/AGENTS.md and %q", output.HookSpecificOutput.AdditionalContext[max(0, len(output.HookSpecificOutput.AdditionalContext)-200):], gateLine)
		}
		const none = `{"hookSpecificOutput":{"hookEventName":"SessionStart"}}` + "\n"
		if code, stdout, stderr := hooksSh(t, sessionCommand, input("resume"), env(withCW)); code != 0 || stdout != none {
			t.Fatalf("resume: exit %d stderr %q stdout %q; want %q", code, stderr, stdout, none)
		}
	})

	t.Run("the sampler reads the Codex rollout", func(t *testing.T) {
		// Oracle (C2): before cw task show --task DIR, the sampler stores one
		// sample from the rollout in transcript_path: session c1 from
		// codex_rollout with 100 input and 20 output tokens, and prints
		// nothing.
		rollout := filepath.Join(hooksCanonicalTempDir(t), "sessions", "2100", "01", "01", "rollout-2100-01-01T00-00-00-c1.jsonl")
		const at = "2100-01-01T00:00:00Z"
		hooksWriteFile(t, filepath.Dir(rollout), filepath.Base(rollout),
			`{"timestamp":"`+at+`","type":"session_meta","payload":{"id":"c1"}}`+"\n"+
				`{"timestamp":"`+at+`","type":"turn_context","payload":{"model":"gpt-6.1-sol"}}`+"\n"+
				`{"timestamp":"`+at+`","type":"token_usage_record","payload":{"response_id":"resp-1","usage":{"input_tokens":100,"cached_input_tokens":0,"output_tokens":20}}}`+"\n")
		stdin, err := json.Marshal(map[string]any{"session_id": "c1", "transcript_path": rollout, "hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": repo,
			"tool_input": map[string]any{"command": "cw task show --task " + record.Directory}})
		if err != nil {
			t.Fatal(err)
		}
		if code, stdout, stderr := hooksSh(t, usage, stdin, env(withCW)); code != 0 || stdout != "" || stderr != "" {
			t.Fatalf("exit %d stdout %q stderr %q; want exit 0 and no output", code, stdout, stderr)
		}
		loaded, err := taskflow.Load(record.Directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.Usage) != 1 || loaded.Usage[0].Source != taskflow.UsageSourceCodex || loaded.Usage[0].SessionID != "c1" || loaded.Usage[0].Tokens == nil || loaded.Usage[0].Tokens.Output != 20 {
			t.Fatalf("usage %+v, want one Codex sample of session c1 with 20 output tokens", loaded.Usage)
		}
	})
}
