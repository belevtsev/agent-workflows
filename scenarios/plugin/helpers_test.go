// Package plugin_test holds the scenarios of the specification
// agent-integration
// (openspec/changes/reduce-cw-to-evidence-gate/specs/agent-integration/spec.md).
//
// The hook commands are taken from hooks/hooks.json (Claude Code) and
// hooks/codex-hooks.json (Codex) and run the way the hosts document: with
// sh -c in the session's directory and a JSON notice on standard input.
package plugin_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// The hook files of the plugin, relative to the module root.
const (
	claudeHooks = "hooks/hooks.json"
	codexHooks  = "hooks/codex-hooks.json"
)

// hostFiles names each host with its hook file.
var hostFiles = []struct{ host, file string }{
	{"Claude Code", claudeHooks},
	{"Codex", codexHooks},
}

// hookFile is the part of a hook file that the hosts read.
type hookFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			// CommandWindows is what Codex runs with cmd.exe /C on Windows.
			CommandWindows string `json:"commandWindows"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// needShell skips a test on Windows, where the hosts run hook commands with
// Git Bash, which these tests do not set up.
func needShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the hook commands are run with sh -c; Git Bash is not set up on Windows")
	}
}

// moduleRoot is the directory of go.mod, found from the test's directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// readHookFile returns the raw content of a hook file of the plugin.
func readHookFile(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// matches says whether a matcher, a list of exact tool names separated by
// |, selects a tool. An empty matcher or * selects every tool.
func matches(matcher, tool string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	for _, name := range strings.Split(matcher, "|") {
		if name == tool {
			return true
		}
	}
	return false
}

// hookCommands returns the command strings that a host runs for an event,
// for the given tool (PreToolUse) or session source (SessionStart). A test
// fails when the file selects none.
func hookCommands(t *testing.T, file, event, selector string) []string {
	t.Helper()
	var hooks hookFile
	if err := json.Unmarshal([]byte(readHookFile(t, file)), &hooks); err != nil {
		t.Fatalf("%s is not valid JSON: %v", file, err)
	}
	var commands []string
	for _, group := range hooks.Hooks[event] {
		if !matches(group.Matcher, selector) {
			continue
		}
		for _, hook := range group.Hooks {
			if hook.Type == "command" && hook.Command != "" {
				commands = append(commands, hook.Command)
			}
		}
	}
	if len(commands) == 0 {
		t.Fatalf("%s runs no command for %s with %q", file, event, selector)
	}
	return commands
}

// runHooks runs every command with sh -c in dir, with env and the notice on
// standard input, and returns their standard output joined and the first
// exit status that is not 0.
func runHooks(t *testing.T, dir string, env []string, commands []string, notice any) kitsutest.Result {
	t.Helper()
	return runHooksIn(t, "sh", dir, env, commands, notice)
}

// runHooksIn is runHooks with another shell, such as bash, which Claude Code
// names in its hook file.
func runHooksIn(t *testing.T, shell, dir string, env []string, commands []string, notice any) kitsutest.Result {
	t.Helper()
	input, err := json.Marshal(notice)
	if err != nil {
		t.Fatal(err)
	}
	var all kitsutest.Result
	for _, command := range commands {
		cmd := exec.Command(shell, "-c", command)
		cmd.Dir = dir
		cmd.Env = env
		cmd.Stdin = strings.NewReader(string(input))
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		all.Stdout += stdout.String()
		all.Stderr += stderr.String()
		var exit *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exit):
			if all.Code == 0 {
				all.Code = exit.ExitCode()
			}
		default:
			t.Fatalf("%s -c %q: %v", shell, command, err)
		}
	}
	return all
}

// sessionStart runs the SessionStart commands of a hook file in the
// repository's directory, as a host does when a session starts there.
func sessionStart(t *testing.T, repo *kitsutest.Repo, env []string, file string) kitsutest.Result {
	t.Helper()
	notice := map[string]any{
		"session_id":      "test-session",
		"transcript_path": filepath.Join(repo.Home, "transcript.jsonl"),
		"cwd":             repo.Dir,
		"hook_event_name": "SessionStart",
		"source":          "startup",
	}
	return runHooks(t, repo.Dir, env, hookCommands(t, file, "SessionStart", "startup"), notice)
}

// preToolUse runs the PreToolUse commands of a hook file for one tool call
// in the repository's directory.
func preToolUse(t *testing.T, repo *kitsutest.Repo, file, tool string, input map[string]any) kitsutest.Result {
	t.Helper()
	notice := map[string]any{
		"session_id":      "test-session",
		"cwd":             repo.Dir,
		"hook_event_name": "PreToolUse",
		"tool_name":       tool,
		"tool_input":      input,
	}
	return runHooks(t, repo.Dir, repo.Env(), hookCommands(t, file, "PreToolUse", tool), notice)
}

// shellInput is the tool input of the shell tool for a command line.
func shellInput(command string) map[string]any {
	return map[string]any{"command": command}
}

// decision is what a PreToolUse command printed, read as the host reads it.
type decision struct {
	Permission string
	Reason     string
}

// decisionOf reads the one JSON object a PreToolUse command prints when it
// asks or denies. A test fails when the command did not exit 0 or printed
// anything else.
func decisionOf(t *testing.T, result kitsutest.Result) decision {
	t.Helper()
	if result.Code != 0 {
		t.Fatalf("the hook command exited %d, want 0:\n%s", result.Code, result.Output())
	}
	var out struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	decoder := json.NewDecoder(strings.NewReader(result.Stdout))
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("the hook command did not print a JSON object: %v\n%s", err, result.Output())
	}
	if decoder.More() {
		t.Fatalf("the hook command printed more than one JSON object:\n%s", result.Stdout)
	}
	if got := out.HookSpecificOutput.HookEventName; got != "PreToolUse" {
		t.Errorf("hookEventName is %q, want PreToolUse", got)
	}
	return decision{out.HookSpecificOutput.PermissionDecision, out.HookSpecificOutput.PermissionDecisionReason}
}

// assertNoDecision fails the test unless the hook command exited 0 and
// printed nothing on standard output, so the host neither asks nor denies.
func assertNoDecision(t *testing.T, result kitsutest.Result, what string) {
	t.Helper()
	if result.Code != 0 || strings.TrimSpace(result.Stdout) != "" {
		t.Errorf("for %s the hook command exited %d and printed, want exit 0 and nothing:\n%s", what, result.Code, result.Output())
	}
}

// gatedDemo is the shared fixture: demo with the git hooks installed.
func gatedDemo(t *testing.T) *kitsutest.Repo {
	t.Helper()
	repo := kitsutest.Demo(t)
	if result := repo.Kitsu("init"); result.Code != 0 {
		t.Fatalf("kitsu init exited %d:\n%s", result.Code, result.Output())
	}
	return repo
}

// withPath is env with PATH replaced.
func withPath(env []string, path string) []string {
	var out []string
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); strings.EqualFold(name, "PATH") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "PATH="+path)
}

// pathOf is the value of PATH in env.
func pathOf(env []string) string {
	path := ""
	for _, kv := range env {
		if name, value, _ := strings.Cut(kv, "="); strings.EqualFold(name, "PATH") {
			path = value
		}
	}
	return path
}

// head is the commit HEAD names.
func head(repo *kitsutest.Repo) string { return repo.Git("rev-parse", "HEAD") }
