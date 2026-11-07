package main_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

// Tests for the plugin hook file (R-JSON) and for running its inline commands
// against the built binary (R-E2E).

type hooksFile struct {
	Description string                  `json:"description"`
	Hooks       map[string][]hooksGroup `json:"hooks"`
}

type hooksGroup struct {
	Matcher string       `json:"matcher"`
	Hooks   []hooksEntry `json:"hooks"`
}

type hooksEntry struct {
	Type    string `json:"type"`
	If      string `json:"if"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// hooksRead reads ROOT/hooks/hooks.json and decodes it strictly.
func hooksRead(t *testing.T) ([]byte, hooksFile) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file hooksFile
	if err := json.Unmarshal(data, &file, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("hooks.json does not match the agreed shape: %v", err)
	}
	return data, file
}

func TestHooksJSONShape(t *testing.T) {
	data, file := hooksRead(t)

	t.Run("valid JSON with only description and hooks", func(t *testing.T) {
		// R-JSON: valid JSON whose only top-level keys are description and hooks.
		var top map[string]any
		if err := json.Unmarshal(data, &top); err != nil {
			t.Fatalf("hooks.json is not valid JSON: %v", err)
		}
		keys := make([]string, 0, len(top))
		for key := range top {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, []string{"description", "hooks"}) {
			t.Fatalf("top-level keys %v, want [description hooks]", keys)
		}
		if strings.TrimSpace(file.Description) == "" {
			t.Fatal("description is empty")
		}
	})

	t.Run("entries are bounded inline commands", func(t *testing.T) {
		// R-JSON: every entry is type command with a non-empty inline command
		// and a timeout between 1 and 60.
		if len(file.Hooks) == 0 {
			t.Fatal("no hook events")
		}
		for event, groups := range file.Hooks {
			if len(groups) == 0 {
				t.Errorf("%s has no matcher groups", event)
			}
			for _, group := range groups {
				if len(group.Hooks) == 0 {
					t.Errorf("%s matcher %q has no hooks", event, group.Matcher)
				}
				for _, entry := range group.Hooks {
					if entry.Type != "command" {
						t.Errorf("%s entry type %q, want command", event, entry.Type)
					}
					if strings.TrimSpace(entry.Command) == "" {
						t.Errorf("%s entry has an empty command", event)
					}
					if entry.Timeout < 1 || entry.Timeout > 60 {
						t.Errorf("%s entry timeout %d outside 1..60", event, entry.Timeout)
					}
					for _, forbidden := range []string{".sh", ".py", ".bash", ".zsh", "/scripts/", "/hooks/"} {
						if strings.Contains(entry.Command, forbidden) {
							t.Errorf("%s command %q references %q; commands must be inline", event, entry.Command, forbidden)
						}
					}
				}
			}
		}
	})

	t.Run("commands do not exec cw and pre-publish ends with exit 0", func(t *testing.T) {
		// R-JSON: no command string contains the word exec, and every
		// PreToolUse command ends with exit 0.
		execWord := regexp.MustCompile(`\bexec\b`)
		for event, groups := range file.Hooks {
			for _, group := range groups {
				for _, entry := range group.Hooks {
					if execWord.MatchString(entry.Command) {
						t.Errorf("%s command %q contains the word exec", event, entry.Command)
					}
					if event == "PreToolUse" && !strings.HasSuffix(strings.TrimSpace(entry.Command), "exit 0") {
						t.Errorf("PreToolUse command %q does not end with exit 0", entry.Command)
					}
				}
			}
		}
	})

	t.Run("events matchers and conditions", func(t *testing.T) {
		// R-JSON: SessionStart matcher startup|resume|clear|compact; PreToolUse
		// matcher Bash with if conditions exactly Bash(git commit *) and
		// Bash(git push *); commands call cw hook and reference the plugin
		// root and data directory.
		events := make([]string, 0, len(file.Hooks))
		for event := range file.Hooks {
			events = append(events, event)
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
		for _, want := range []string{"${CLAUDE_PLUGIN_ROOT}/global/AGENTS.md", "${CLAUDE_PLUGIN_DATA}", "cw hook session-start"} {
			if !strings.Contains(command, want) {
				t.Errorf("SessionStart command %q does not contain %q", command, want)
			}
		}
		pre := file.Hooks["PreToolUse"]
		if len(pre) != 1 || pre[0].Matcher != "Bash" {
			t.Fatalf("PreToolUse groups %+v", pre)
		}
		var conditions []string
		for _, entry := range pre[0].Hooks {
			conditions = append(conditions, entry.If)
			if !strings.Contains(entry.Command, "cw hook pre-publish") {
				t.Errorf("PreToolUse command %q does not run cw hook pre-publish", entry.Command)
			}
		}
		slices.Sort(conditions)
		if !slices.Equal(conditions, []string{"Bash(git commit *)", "Bash(git push *)"}) {
			t.Fatalf("PreToolUse if conditions %v", conditions)
		}
	})
}

// hooksCanonicalTempDir returns a fresh temporary directory in canonical form.
func hooksCanonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// hooksSh runs command with sh -c, stdin and exactly env, and returns the
// exit code and output.
func hooksSh(t *testing.T, command string, stdin []byte, env []string) (int, string, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", command)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("sh -c %q did not run: %v", command, err)
		}
		code = exit.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

// hooksStubCW writes an executable cw stub with script as its body into a
// fresh directory and returns a PATH that puts it before /usr/bin:/bin.
func hooksStubCW(t *testing.T, script string) string {
	t.Helper()
	dir := hooksCanonicalTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "cw"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return dir + ":/usr/bin:/bin"
}

// hooksOldCW stands in for a cw older than the hook subcommand.
const hooksOldCW = "#!/bin/sh\necho \"agent-workflows: unknown command: hook\" >&2\nexit 2\n"

// hooksSilentCW prints nothing and succeeds.
const hooksSilentCW = "#!/bin/sh\nexit 0\n"

func TestHooksEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/cw")
	}
	if runtime.GOOS == "windows" {
		t.Skip("hook commands run under sh")
	}
	_, file := hooksRead(t)
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
	data := hooksCanonicalTempDir(t)
	store := filepath.Join(hooksCanonicalTempDir(t), "store")
	repo := hooksCanonicalTempDir(t)
	initRepo := exec.CommandContext(t.Context(), "git", "-c", "init.defaultBranch=main", "init", "-q", repo)
	initRepo.Env = []string{"HOME=" + home, "PATH=" + withoutCW, "GIT_CONFIG_NOSYSTEM=1"}
	if output, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("source\n"), 0600); err != nil {
		t.Fatal(err)
	}

	env := func(path string, extra ...string) []string {
		return append([]string{
			"PATH=" + path,
			"HOME=" + home,
			"CW_TASK_STORE=" + store,
			"CLAUDE_PLUGIN_ROOT=" + root,
			"CLAUDE_PLUGIN_DATA=" + data,
		}, extra...)
	}

	// Each PreToolUse entry runs with the Bash command its condition matches.
	type preHook struct {
		name    string
		command string
		stdin   []byte
	}
	var pre []preHook
	for _, group := range file.Hooks["PreToolUse"] {
		for _, entry := range group.Hooks {
			bash := "git commit -m x"
			if strings.Contains(entry.If, "push") {
				bash = "git push origin main"
			}
			stdin, err := json.Marshal(map[string]any{
				"session_id":      "s",
				"hook_event_name": "PreToolUse",
				"tool_name":       "Bash",
				"cwd":             repo,
				"tool_input":      map[string]any{"command": bash},
			})
			if err != nil {
				t.Fatal(err)
			}
			pre = append(pre, preHook{name: entry.If, command: entry.Command, stdin: stdin})
		}
	}
	if len(pre) != 2 {
		t.Fatalf("found %d PreToolUse entries, want 2", len(pre))
	}

	expectAllow := func(t *testing.T, hook preHook, environment []string) {
		t.Helper()
		code, stdout, stderr := hooksSh(t, hook.command, hook.stdin, environment)
		if code != 0 || stdout != "" {
			t.Fatalf("%s: exit %d stdout %q stderr %q; want exit 0 and no output", hook.name, code, stdout, stderr)
		}
	}

	t.Run("pre-publish allows without a record", func(t *testing.T) {
		// R-E2E: no record, exit 0 and empty stdout.
		for _, hook := range pre {
			expectAllow(t, hook, env(withCW))
		}
	})

	plan := filepath.Join(hooksCanonicalTempDir(t), "plan.json")
	if err := os.WriteFile(plan, []byte(`{"title":"T","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":[],"allow_non_execution":true}],"checks":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	start := exec.CommandContext(t.Context(), binary, "task", "start", "--repo", repo, "--plan", plan,
		"--policy", filepath.Join(root, "skills", "task-orchestration", "model-policy.yaml"), "--summary", "--store", store)
	start.Env = []string{"HOME=" + home, "PATH=" + withoutCW}
	start.Dir = t.TempDir()
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("cw task start: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	record, err := taskflow.Load(filepath.Join(store, entries[0].Name()))
	if err != nil {
		t.Fatalf("load started record: %v", err)
	}

	t.Run("pre-publish denies with an open record", func(t *testing.T) {
		// R-E2E: an open record yields exit 0 and the deny JSON naming the task.
		for _, hook := range pre {
			code, stdout, stderr := hooksSh(t, hook.command, hook.stdin, env(withCW))
			if code != 0 {
				t.Fatalf("%s: exit %d stderr %q", hook.name, code, stderr)
			}
			var decision struct {
				HookSpecificOutput struct {
					HookEventName            string `json:"hookEventName"`
					PermissionDecision       string `json:"permissionDecision"`
					PermissionDecisionReason string `json:"permissionDecisionReason"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal([]byte(stdout), &decision); err != nil {
				t.Fatalf("%s: stdout %q is not the deny JSON: %v", hook.name, stdout, err)
			}
			output := decision.HookSpecificOutput
			if output.HookEventName != "PreToolUse" || output.PermissionDecision != "deny" || !strings.Contains(output.PermissionDecisionReason, record.ID) {
				t.Fatalf("%s: decision %+v, want a PreToolUse deny naming task %s", hook.name, output, record.ID)
			}
		}
	})

	t.Run("pre-publish honours enforce_verify=false", func(t *testing.T) {
		// R-E2E: CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false allows.
		for _, hook := range pre {
			expectAllow(t, hook, env(withCW, "CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false"))
		}
	})

	t.Run("pre-publish allows without cw on PATH", func(t *testing.T) {
		// R-E2E: without cw the command exits 0 with no output.
		for _, hook := range pre {
			expectAllow(t, hook, env(withoutCW))
		}
	})

	withOldCW := hooksStubCW(t, hooksOldCW)
	withSilentCW := hooksStubCW(t, hooksSilentCW)

	t.Run("pre-publish allows with a cw that has no hook command", func(t *testing.T) {
		// R-E2E: a cw that exits 2 (older than the hook subcommand) gives
		// exit 0 and no output.
		for _, hook := range pre {
			expectAllow(t, hook, env(withOldCW))
		}
	})

	session := file.Hooks["SessionStart"]
	if len(session) != 1 || len(session[0].Hooks) != 1 {
		t.Fatalf("SessionStart groups %+v", session)
	}
	sessionCommand := session[0].Hooks[0].Command
	agents, err := os.ReadFile(filepath.Join(root, "global", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	sessionStdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "startup", "cwd": repo})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("session-start with cw emits the instructions", func(t *testing.T) {
		// R-E2E: with cw on PATH, one SessionStart JSON line carrying
		// global/AGENTS.md without trailing newlines.
		code, stdout, stderr := hooksSh(t, sessionCommand, sessionStdin, env(withCW))
		if code != 0 {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
			t.Fatalf("stdout %q is not one JSON line", stdout)
		}
		var output struct {
			HookSpecificOutput struct {
				HookEventName     string `json:"hookEventName"`
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(stdout), &output); err != nil {
			t.Fatalf("stdout is not JSON: %v", err)
		}
		if output.HookSpecificOutput.HookEventName != "SessionStart" {
			t.Fatalf("hookEventName %q", output.HookSpecificOutput.HookEventName)
		}
		if want := strings.TrimRight(string(agents), "\n"); output.HookSpecificOutput.AdditionalContext != want {
			t.Fatalf("additionalContext differs from global/AGENTS.md without trailing newlines (got %d bytes, want %d)", len(output.HookSpecificOutput.AdditionalContext), len(want))
		}
	})

	t.Run("session-start falls back to the raw file", func(t *testing.T) {
		// R-E2E: without cw on PATH, with a cw that exits 2, and with a cw
		// that prints nothing, the command exits 0 and prints the raw file.
		tests := []struct {
			name string
			path string
		}{
			{"cw absent from PATH", withoutCW},
			{"cw exits 2", withOldCW},
			{"cw prints nothing", withSilentCW},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				code, stdout, stderr := hooksSh(t, sessionCommand, sessionStdin, env(test.path))
				if code != 0 || stdout != string(agents) {
					t.Fatalf("exit %d stderr %q stdout %d bytes; want exit 0 and the %d-byte file verbatim (equal: %v)", code, stderr, len(stdout), len(agents), stdout == string(agents))
				}
			})
		}
	})
}
