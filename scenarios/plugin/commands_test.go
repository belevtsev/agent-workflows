package plugin_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// hookEvents are the events both hook files handle, with the tool or the
// source a host gives for each.
var hookEvents = []struct{ event, selector string }{
	{"SessionStart", "startup"},
	{"PreToolUse", "Bash"},
	{"SubagentStop", ""},
	{"SessionEnd", ""},
}

// noticeFor is a notice of event for a session in dir. A PreToolUse notice
// holds a shell call that the guard would speak up for.
func noticeFor(event, dir string) map[string]any {
	notice := map[string]any{
		"session_id":      "test-session",
		"transcript_path": filepath.Join(dir, "no-transcript.jsonl"),
		"cwd":             dir,
		"hook_event_name": event,
		"source":          "startup",
		"reason":          "other",
	}
	if event == "PreToolUse" {
		notice["tool_name"] = "Bash"
		notice["tool_input"] = shellInput("git commit --no-verify -m change")
	}
	return notice
}

// shells are the shells a hook command is run with here: sh, and bash where
// it is installed, which Claude Code names in its hook file.
func shells(t *testing.T) []string {
	t.Helper()
	found := []string{"sh"}
	if _, err := exec.LookPath("bash"); err == nil {
		found = append(found, "bash")
	}
	return found
}

// Requirement "Instruction text comes from `cw`", scenario "A program named
// `cw` in the repository".
func TestHookCommandsRunNoCWOfTheRepository(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	marker := filepath.Join(t.TempDir(), "repository-cw-ran")
	repo.Write("cw", "#!/bin/sh\necho ran > '"+marker+"'\n")
	if err := os.Chmod(repo.Path("cw"), 0o755); err != nil { //nolint:gosec // the planted cw must be executable
		t.Fatal(err)
	}
	// A folder that holds what the commands need and no cw.
	bin := t.TempDir()
	for _, program := range []string{"git", "sh", "bash"} {
		path, err := exec.LookPath(program)
		if err != nil {
			continue
		}
		if err := os.Symlink(path, filepath.Join(bin, program)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{":" + bin, "." + string(os.PathListSeparator) + bin} {
		env := withPath(repo.Env(), path)
		// The shell finds the planted file, so a command that ran what
		// `command -v` gives would run it.
		probe := exec.Command("sh", "-c", "command -v cw")
		probe.Dir, probe.Env = repo.Dir, env
		if out, _ := probe.Output(); strings.TrimSpace(string(out)) == "" {
			t.Fatalf("with PATH=%s the shell does not find the planted cw, so the test proves nothing", path)
		}
		for _, h := range hostFiles {
			for _, shell := range shells(t) {
				for _, e := range hookEvents[:2] {
					t.Run(h.host+"/"+shell+"/"+e.event+"/PATH="+strings.ReplaceAll(path, bin, "bin"), func(t *testing.T) {
						result := runHooksIn(t, shell, repo.Dir, env, hookCommands(t, h.file, e.event, e.selector), noticeFor(e.event, repo.Dir))
						if result.Code != 0 {
							t.Errorf("the %s command exited %d, want 0:\n%s", e.event, result.Code, result.Output())
						}
						if _, err := os.Stat(marker); err == nil {
							t.Errorf("the %s command ran the file cw of the repository", e.event)
						}
						if e.event == "PreToolUse" && strings.TrimSpace(result.Stdout) != "" {
							t.Errorf("the PreToolUse command printed, want nothing:\n%s", result.Stdout)
						}
					})
				}
			}
		}
	}
}

// Requirement "Instruction text comes from `cw`", scenario "An older `cw`".
func TestOlderCWBlocksNoToolCall(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	older := t.TempDir()
	script := "#!/bin/sh\necho 'usage: cw <command> [arguments]' >&2\nexit 2\n"
	if err := os.WriteFile(filepath.Join(older, "cw"), []byte(script), 0o755); err != nil { //nolint:gosec // the older cw must be executable
		t.Fatal(err)
	}
	env := withPath(repo.Env(), older+string(os.PathListSeparator)+pathOf(repo.Env()))
	for _, h := range hostFiles {
		for _, e := range hookEvents {
			t.Run(h.host+"/"+e.event, func(t *testing.T) {
				result := runHooks(t, repo.Dir, env, hookCommands(t, h.file, e.event, e.selector), noticeFor(e.event, repo.Dir))
				if result.Code != 0 || strings.TrimSpace(result.Stdout) != "" {
					t.Errorf("the %s command exited %d and printed, want exit 0 and nothing on standard output:\n%s", e.event, result.Code, result.Output())
				}
			})
		}
	}
}

// Requirements "Instruction text comes from `cw`" and "Ask before skipping":
// each command runs cw only by an absolute path and ends with exit status 0;
// a command for Codex runs in sh whatever the user's shell is, and its
// Windows form searches no working directory and ends with exit status 0.
// Claude Code matches its PowerShell tool.
func TestHookCommandsHaveTheirSafeShape(t *testing.T) {
	t.Parallel()
	for _, h := range hostFiles {
		t.Run(h.host, func(t *testing.T) {
			var hooks hookFile
			if err := json.Unmarshal([]byte(readHookFile(t, h.file)), &hooks); err != nil {
				t.Fatalf("%s is not valid JSON: %v", h.file, err)
			}
			for _, e := range hookEvents {
				if len(hooks.Hooks[e.event]) == 0 {
					t.Errorf("%s handles no %s", h.file, e.event)
				}
			}
			for event, groups := range hooks.Hooks {
				if event == "PreToolUse" && h.file == claudeHooks {
					for _, group := range groups {
						if !matches(group.Matcher, "PowerShell") {
							t.Errorf("the PreToolUse matcher %q does not select PowerShell", group.Matcher)
						}
					}
				}
				for _, group := range groups {
					for _, hook := range group.Hooks {
						checkCommandShape(t, h.file, event, hook.Command, hook.CommandWindows)
					}
				}
			}
		})
	}
}

// checkCommandShape checks one hook of a file; see
// TestHookCommandsHaveTheirSafeShape.
func checkCommandShape(t *testing.T, file, event, command, windows string) {
	t.Helper()
	if !strings.Contains(command, `case "$c" in /*)`) {
		t.Errorf("%s %s does not run cw only by an absolute path: %s", file, event, command)
	}
	if file == claudeHooks {
		if !strings.HasSuffix(command, "; true") {
			t.Errorf("%s %s does not end with ; true: %s", file, event, command)
		}
		return
	}
	if !strings.HasPrefix(command, "sh -c '") || !strings.HasSuffix(command, "; true'") || strings.Count(command, "'") != 2 {
		t.Errorf("%s %s is not one sh -c '…; true' with no other single quote: %s", file, event, command)
	}
	if !strings.HasPrefix(windows, "set NoDefaultCurrentDirectoryInExePath=1&") || !strings.HasSuffix(windows, "& exit /b 0") {
		t.Errorf("%s %s has no commandWindows that searches no working directory and exits 0: %q", file, event, windows)
	}
}

// Requirement "Session context", scenario "The host names the session's
// directory".
func TestSessionContextUsesTheDirectoryOfTheNotice(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	outside := t.TempDir()
	for _, h := range hostFiles {
		commands := hookCommands(t, h.file, "SessionStart", "startup")
		t.Run(h.host+"/started outside, session in demo", func(t *testing.T) {
			result := runHooks(t, outside, repo.Env(), commands, noticeFor("SessionStart", repo.Dir))
			if result.Code != 0 || !strings.Contains(result.Stdout, "cw run") {
				t.Errorf("the SessionStart command exited %d and printed no context of demo:\n%s", result.Code, result.Output())
			}
		})
		t.Run(h.host+"/started in demo, session outside", func(t *testing.T) {
			result := runHooks(t, repo.Dir, repo.Env(), commands, noticeFor("SessionStart", outside))
			if result.Code != 0 || result.Stdout != "" {
				t.Errorf("the SessionStart command exited %d and printed, want exit 0 and nothing:\n%s", result.Code, result.Output())
			}
		})
	}
}

// Requirement "Session context", scenario "`cw context` in a terminal".
func TestContextWithoutAHostReadsNoInput(t *testing.T) {
	t.Parallel()
	repo := gatedDemo(t)
	program := filepath.Join(cwtest.Binary(t), "cw")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	closed, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closed.Close() })
	for name, input := range map[string]*os.File{"an input that never ends": reader, "a closed input": closed} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, program, "context")
			cmd.Dir, cmd.Env, cmd.Stdin = repo.Dir, repo.Env(), input
			out, err := cmd.Output()
			if ctx.Err() != nil {
				t.Fatalf("cw context did not return within 30 seconds: it waits for its input")
			}
			if err != nil || !strings.Contains(string(out), "cw run") {
				t.Errorf("cw context ended with %v and printed no context:\n%s", err, out)
			}
		})
	}
}
