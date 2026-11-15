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

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
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

// shells are the shells a hook command is run with here, by label: sh; bash
// where it is installed, which Claude Code names in its hook file; and bash
// started as sh, which is the sh of macOS. That one runs in POSIX mode,
// where `command -v` gives an absolute path also for a file it found through
// an empty or relative entry of PATH.
func shells(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{"sh": "sh"}
	if bash, err := exec.LookPath("bash"); err == nil {
		found["bash"] = "bash"
		asSh := filepath.Join(t.TempDir(), "sh")
		if err := os.Symlink(bash, asSh); err != nil {
			t.Fatal(err)
		}
		found["bash as sh"] = asSh
	}
	return found
}

// toolFolder is a folder that holds git, bash and sh, which the hook
// commands need, and no gatecub. sh is the program of that name on this machine,
// or bash, as on macOS.
func toolFolder(t *testing.T, sh string) string {
	t.Helper()
	bin := t.TempDir()
	for name, program := range map[string]string{"git": "git", "bash": "bash", "sh": sh} {
		path, err := exec.LookPath(program)
		if err != nil {
			continue
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

// Requirement "Instruction text comes from `gatecub`", scenario "A program named
// `gatecub` in the repository".
func TestHookCommandsRunNoGatecubOfTheRepository(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	marker := filepath.Join(t.TempDir(), "repository-gatecub-ran")
	gitMarker := filepath.Join(t.TempDir(), "repository-git-ran")
	for name, file := range map[string]string{"gatecub": marker, "git": gitMarker} {
		writeProgram(t, repo.Path(name), "#!/bin/sh\necho ran > '"+file+"'\n")
	}
	for _, innerSh := range []string{"sh", "bash"} {
		bin := toolFolder(t, innerSh)
		for _, first := range []string{"", "."} {
			env := withPath(repo.Env(), first+string(os.PathListSeparator)+bin)
			// The shell finds the planted file, so a command that ran what
			// `command -v` gives would run it.
			probe := exec.Command("sh", "-c", "command -v gatecub")
			probe.Dir, probe.Env = repo.Dir, env
			if out, _ := probe.Output(); strings.TrimSpace(string(out)) == "" {
				t.Fatalf("with PATH=%s%c<tools> the shell does not find the planted gatecub, so the test proves nothing", first, os.PathListSeparator)
			}
			for _, h := range hostFiles {
				for label, shell := range shells(t) {
					for _, e := range hookEvents[:2] {
						t.Run(h.host+"/"+label+"/"+e.event+"/PATH="+first+":tools with "+innerSh+" as sh", func(t *testing.T) {
							result := runHooksIn(t, shell, repo.Dir, env, hookCommands(t, h.file, e.event, e.selector), noticeFor(e.event, repo.Dir))
							if result.Code != 0 {
								t.Errorf("the %s command exited %d, want 0:\n%s", e.event, result.Code, result.Output())
							}
							if _, err := os.Stat(marker); err == nil {
								t.Errorf("the %s command ran the file gatecub of the repository", e.event)
							}
							if _, err := os.Stat(gitMarker); err == nil {
								t.Errorf("the %s command ran the file git of the repository", e.event)
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
}

// Requirement "Instruction text comes from `gatecub`", scenario "No absolute entry
// in `PATH`". Codex starts `sh` itself, so only the commands of Claude Code
// can run with such a PATH.
func TestHookCommandsRunNothingWithoutAnAbsoluteEntryOfPATH(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	marker := filepath.Join(t.TempDir(), "repository-program-ran")
	for _, name := range []string{"gatecub", "git"} {
		writeProgram(t, repo.Path(name), "#!/bin/sh\necho ran > '"+marker+"'\n")
	}
	for label, shell := range shells(t) {
		if !filepath.IsAbs(shell) {
			found, err := exec.LookPath(shell)
			if err != nil {
				t.Fatal(err)
			}
			shell = found
		}
		for _, value := range []string{"", ".", ":", ".:bin"} {
			for _, e := range hookEvents {
				t.Run(label+"/"+e.event+"/PATH="+value, func(t *testing.T) {
					result := runHooksIn(t, shell, repo.Dir, withPath(repo.Env(), value), hookCommands(t, claudeHooks, e.event, e.selector), noticeFor(e.event, repo.Dir))
					if result.Code != 0 || strings.TrimSpace(result.Stdout) != "" {
						t.Errorf("the %s command exited %d and printed, want exit 0 and nothing on standard output:\n%s", e.event, result.Code, result.Output())
					}
					if _, err := os.Stat(marker); err == nil {
						t.Errorf("the %s command ran a program of the repository", e.event)
					}
				})
			}
		}
	}
}

// Requirement "Instruction text comes from `gatecub`", scenario "A relative entry
// before the installed `gatecub`".
func TestHookCommandsRunTheInstalledGatecubAfterARelativeEntry(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	marker := filepath.Join(t.TempDir(), "repository-program-ran")
	for _, name := range []string{"gatecub", "git"} {
		writeProgram(t, repo.Path(name), "#!/bin/sh\necho ran > '"+marker+"'\n")
	}
	for _, first := range []string{"", "."} {
		env := withPath(repo.Env(), first+string(os.PathListSeparator)+pathOf(repo.Env()))
		for _, h := range hostFiles {
			for label, shell := range shells(t) {
				for _, e := range hookEvents[:2] {
					t.Run(h.host+"/"+label+"/"+e.event+"/PATH="+first+":installed", func(t *testing.T) {
						result := runHooksIn(t, shell, repo.Dir, env, hookCommands(t, h.file, e.event, e.selector), noticeFor(e.event, repo.Dir))
						if result.Code != 0 {
							t.Errorf("the %s command exited %d, want 0:\n%s", e.event, result.Code, result.Output())
						}
						if _, err := os.Stat(marker); err == nil {
							t.Errorf("the %s command ran a program of the repository", e.event)
						}
						switch e.event {
						case "SessionStart":
							if strings.TrimSpace(result.Stdout) == "" || strings.Contains(result.Stdout, "gatecub is not installed") {
								t.Errorf("the session did not get the context of the installed gatecub:\n%s", result.Output())
							}
						case "PreToolUse":
							if !strings.Contains(result.Stdout, "permissionDecision") {
								t.Errorf("the guard of the installed gatecub did not speak up:\n%s", result.Output())
							}
						}
					})
				}
			}
		}
	}
}

// Requirement "Instruction text comes from `gatecub`", scenario "A `gatecub` that
// cannot be run".
func TestSessionStartSaysWhenTheGatecubFoundCannotBeRun(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "gatecub"), []byte("#!/bin/sh\necho ran\n"), 0o644); err != nil { //nolint:gosec // the file must be readable and not executable
		t.Fatal(err)
	}
	env := withPath(repo.Env(), folder+string(os.PathListSeparator)+toolFolder(t, "sh"))
	for _, h := range hostFiles {
		for label, shell := range shells(t) {
			t.Run(h.host+"/"+label, func(t *testing.T) {
				result := runHooksIn(t, shell, repo.Dir, env, hookCommands(t, h.file, "SessionStart", "startup"), noticeFor("SessionStart", repo.Dir))
				if result.Code != 0 || !strings.Contains(result.Stdout, "gatecub is not installed") || strings.Contains(result.Stdout, "ran") {
					t.Errorf("exit status %d, want 0 and the line that says gatecub is not installed:\n%s", result.Code, result.Output())
				}
			})
		}
	}
}

// Requirement "Instruction text comes from `gatecub`", scenario "An older `gatecub`".
func TestOlderGatecubBlocksNoToolCall(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	older := t.TempDir()
	script := "#!/bin/sh\necho 'usage: gatecub <command> [arguments]' >&2\nexit 2\n"
	writeProgram(t, filepath.Join(older, "gatecub"), script)
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

// Requirements "Instruction text comes from `gatecub`" and "Ask before skipping":
// each command runs gatecub only by an absolute path and ends with exit status 0;
// a command for Codex runs in sh whatever the user's shell is, and its
// Windows form searches no working directory, leaves the folder of the
// session's drive and goes to the Windows folder, holds no double quote and
// ends with exit status 0. Claude Code matches its PowerShell tool.
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
		t.Errorf("%s %s does not run gatecub only by an absolute path: %s", file, event, command)
	}
	if !strings.Contains(command, `[ -n "$p" ] || exit 0; PATH=$p; c=$(command -v gatecub 2>/dev/null); [ -x "$c" ] || c=;`) {
		t.Errorf("%s %s does not keep only the absolute entries of PATH for all it runs: %s", file, event, command)
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
	if !strings.HasPrefix(windows, "set NoDefaultCurrentDirectoryInExePath=1& ") || !strings.HasSuffix(windows, " & exit /b 0") {
		t.Errorf("%s %s has no commandWindows that searches no working directory and exits 0: %q", file, event, windows)
	}
	// A relative entry of PATH leads into the directory cmd.exe is in, and
	// one such as D:tools into the directory it left on that drive. So the
	// command goes to the root of the session's drive and then to the
	// Windows folder before it looks for gatecub.
	rest := strings.TrimPrefix(strings.TrimPrefix(windows, "set NoDefaultCurrentDirectoryInExePath=1& "), "pushd .& ")
	if !strings.HasPrefix(rest, `cd \& cd /d %SystemRoot% && `) {
		t.Errorf("%s %s has a commandWindows that does not go to the Windows folder first: %q", file, event, windows)
	}
	// Codex gives the command to cmd.exe as one argument; a double quote in
	// it reaches cmd.exe with a backslash before it.
	if strings.Contains(windows, `"`) {
		t.Errorf("%s %s has a commandWindows with a double quote: %q", file, event, windows)
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
			if result.Code != 0 || !strings.Contains(result.Stdout, "gatecub run") {
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

// Requirement "Session context", scenario "`gatecub context` in a terminal".
func TestContextWithoutAHostReadsNoInput(t *testing.T) {
	t.Parallel()
	repo := gatedDemo(t)
	program := filepath.Join(gatecubtest.Binary(t), "gatecub")
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
				t.Fatalf("gatecub context did not return within 30 seconds: it waits for its input")
			}
			if err != nil || !strings.Contains(string(out), "gatecub run") {
				t.Errorf("gatecub context ended with %v and printed no context:\n%s", err, out)
			}
		})
	}
}
