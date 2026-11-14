//go:build windows

package platform_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// codexWindowsCommands are the commandWindows of hooks/codex-hooks.json by
// event, read from the file of this commit.
func codexWindowsCommands(t *testing.T) map[string][]string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		if filepath.Dir(dir) == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = filepath.Dir(dir)
	}
	data, err := os.ReadFile(filepath.Join(dir, "hooks", "codex-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				CommandWindows string `json:"commandWindows"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	commands := map[string][]string{}
	for event, groups := range file.Hooks {
		for _, group := range groups {
			for _, hook := range group.Hooks {
				if hook.CommandWindows != "" {
					commands[event] = append(commands[event], hook.CommandWindows)
				}
			}
		}
	}
	for _, event := range []string{"SessionStart", "PreToolUse", "SubagentStop", "SessionEnd"} {
		if len(commands[event]) == 0 {
			t.Fatalf("hooks/codex-hooks.json has no commandWindows for %s", event)
		}
	}
	return commands
}

// cmdWays are the two ways a command line reaches cmd.exe: as an argument
// that Go quotes, and as the raw command line `cmd.exe /C <command>`.
var cmdWays = []string{"argument", "raw command line"}

// runCmd runs command with cmd.exe /C in dir, with PATH set to path and the
// notice on standard input, in one of cmdWays.
func runCmd(t *testing.T, way, dir, path, command string, notice map[string]any) cwtest.Result {
	t.Helper()
	input, err := json.Marshal(notice)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/C", command)
	if way == "raw command line" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe /C " + command}
	}
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); !strings.EqualFold(name, "PATH") && !strings.EqualFold(name, "NoDefaultCurrentDirectoryInExePath") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+path)
	cmd.Stdin = strings.NewReader(string(input))
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	result := cwtest.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		result.Code = exit.ExitCode()
	default:
		t.Fatalf("cmd.exe /C %s: %v", command, err)
	}
	return result
}

// codexNotice is a notice of event for a session in dir; a PreToolUse notice
// holds a shell call that skips the hooks.
func codexNotice(event, dir string) map[string]any {
	notice := map[string]any{"session_id": "test-session", "cwd": dir, "hook_event_name": event, "source": "startup"}
	if event == "PreToolUse" {
		notice["tool_name"] = "Bash"
		notice["tool_input"] = map[string]any{"command": "git commit --no-verify -m change"}
	}
	return notice
}

// systemPath is a PATH with the folder of cmd.exe and nothing that holds cw.
func systemPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32")
}

// Requirement "Hooks on Windows", scenario "A cw of the working directory".
func TestCodexWindowsCommandsRunNoCWOfTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	marker := filepath.Join(t.TempDir(), "folder-cw-ran")
	repo.Write("cw.bat", "@echo off\r\necho ran> \""+marker+"\"\r\necho {\"hookSpecificOutput\":{}}\r\n")
	for event, commands := range codexWindowsCommands(t) {
		for _, command := range commands {
			for _, way := range cmdWays {
				t.Run(event+"/"+way, func(t *testing.T) {
					result := runCmd(t, way, repo.Dir, systemPath(), command, codexNotice(event, repo.Dir))
					if result.Code != 0 {
						t.Errorf("exit status %d, want 0:\n%s", result.Code, result.Output())
					}
					if exists(marker) {
						t.Errorf("the command ran cw.bat of the working directory")
					}
					if strings.Contains(result.Stdout, "hookSpecificOutput") {
						t.Errorf("the command printed what cw.bat of the working directory prints:\n%s", result.Stdout)
					}
				})
			}
		}
	}
}

// Requirement "Hooks on Windows", scenario "A relative entry of `PATH` under
// Codex on Windows".
func TestCodexWindowsCommandsRunNoProgramOfARelativeEntryOfPATH(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	marker := filepath.Join(t.TempDir(), "repository-program-ran")
	planted := "@echo off\r\necho ran> \"" + marker + "\"\r\necho {\"hookSpecificOutput\":{}}\r\n"
	for _, name := range []string{"cw.bat", "git.bat", "where.bat", "tools/cw.bat", "tools/git.bat"} {
		repo.Write(name, planted)
	}
	for _, first := range []string{".", "tools", `.\tools`, ""} {
		path := first + string(os.PathListSeparator) + systemPath()
		// cmd.exe finds the planted file through such an entry, so a command
		// that stayed in the repository would run it.
		if first != "" {
			probe := runCmd(t, cmdWays[0], repo.Dir, path, "set NoDefaultCurrentDirectoryInExePath=1& cw", codexNotice("SessionStart", repo.Dir))
			if !strings.Contains(probe.Stdout, "hookSpecificOutput") {
				t.Fatalf("with PATH=%s cmd.exe does not find the planted cw.bat, so the test proves nothing:\n%s", path, probe.Output())
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
		}
		for event, commands := range codexWindowsCommands(t) {
			for _, command := range commands {
				for _, way := range cmdWays {
					t.Run(event+"/"+way+"/PATH="+first, func(t *testing.T) {
						result := runCmd(t, way, repo.Dir, path, command, codexNotice(event, repo.Dir))
						if result.Code != 0 {
							t.Errorf("exit status %d, want 0:\n%s", result.Code, result.Output())
						}
						if exists(marker) {
							t.Errorf("the command ran a program of the repository")
						}
						if strings.Contains(result.Stdout, "hookSpecificOutput") {
							t.Errorf("the command printed what a program of the repository prints:\n%s", result.Stdout)
						}
					})
				}
			}
		}
		// With the installed cw in a later entry the guard still decides.
		installed := first + string(os.PathListSeparator) + pathOf(repo)
		for _, command := range codexWindowsCommands(t)["PreToolUse"] {
			for _, way := range cmdWays {
				t.Run("PreToolUse with the installed cw/"+way+"/PATH="+first, func(t *testing.T) {
					result := runCmd(t, way, repo.Dir, installed, command, codexNotice("PreToolUse", repo.Dir))
					if result.Code != 0 || !strings.Contains(result.Stdout, "deny") {
						t.Errorf("exit status %d, want 0 and the decision deny:\n%s", result.Code, result.Output())
					}
					if exists(marker) {
						t.Errorf("the command ran a program of the repository")
					}
				})
			}
		}
	}
}

// substDrive gives folder a drive letter of its own with subst until the
// test ends. When the machine has none to give, the test is skipped, and
// fails in CI.
func substDrive(t *testing.T, folder string) string {
	t.Helper()
	for letter := 'Z'; letter >= 'G'; letter-- {
		drive := string(letter) + ":"
		if exists(drive + `\`) {
			continue
		}
		if output, err := exec.Command("subst", drive, folder).CombinedOutput(); err != nil {
			t.Logf("subst %s %s: %v\n%s", drive, folder, err, output)
			continue
		}
		t.Cleanup(func() { _ = exec.Command("subst", drive, "/d").Run() })
		return drive
	}
	// In CI the test must run: a skip there would read as a pass.
	if os.Getenv("CI") != "" {
		t.Fatal("subst gave the test folder no drive letter")
	}
	t.Skip("subst gave the test folder no drive letter")
	return ""
}

// Requirement "Hooks on Windows", scenario "A drive-relative entry of `PATH`
// under Codex on Windows". The repository gets a drive letter of its own, so
// the test needs no second disk. It is not parallel: a drive letter is the
// machine's.
func TestCodexWindowsCommandsRunNoProgramOfADriveRelativeEntryOfPATH(t *testing.T) {
	repo := portable(t, "")
	marker := filepath.Join(t.TempDir(), "repository-program-ran")
	planted := "@echo off\r\necho ran> \"" + marker + "\"\r\necho {\"hookSpecificOutput\":{}}\r\n"
	for _, name := range []string{"cw.bat", "tools/cw.bat"} {
		repo.Write(name, planted)
	}
	drive := substDrive(t, filepath.Dir(repo.Dir))
	session := drive + `\` + filepath.Base(repo.Dir)
	if !exists(filepath.Join(session, "cw.toml")) {
		t.Fatalf("%s does not show the repository", session)
	}
	// From another drive cmd.exe still finds the planted file through an
	// entry such as Z:tools: it leads into the directory left on drive Z.
	probe := runCmd(t, cmdWays[0], session, drive+"tools"+string(os.PathListSeparator)+systemPath(), "set NoDefaultCurrentDirectoryInExePath=1& cd /d %SystemRoot% && cw", codexNotice("SessionStart", session))
	if !strings.Contains(probe.Stdout, "hookSpecificOutput") {
		t.Fatalf("with PATH=%stools cmd.exe does not find the planted cw.bat from another drive, so the test proves nothing:\n%s", drive, probe.Output())
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	for _, first := range []string{drive + "tools", drive, drive + "."} {
		path := first + string(os.PathListSeparator) + systemPath()
		for event, commands := range codexWindowsCommands(t) {
			for _, command := range commands {
				for _, way := range cmdWays {
					t.Run(event+"/"+way+"/PATH="+first, func(t *testing.T) {
						result := runCmd(t, way, session, path, command, codexNotice(event, session))
						if result.Code != 0 {
							t.Errorf("exit status %d, want 0:\n%s", result.Code, result.Output())
						}
						if exists(marker) {
							t.Errorf("the command ran a program of the repository")
						}
						if strings.Contains(result.Stdout, "hookSpecificOutput") {
							t.Errorf("the command printed what a program of the repository prints:\n%s", result.Stdout)
						}
					})
				}
			}
		}
	}
}

// Requirement "Hooks on Windows", scenario "A file named `cw` that Windows
// cannot run".
func TestCodexWindowsSessionStartSaysWhenCWCannotBeRun(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "cw"), []byte("#!/bin/sh\necho ran\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := folder + string(os.PathListSeparator) + systemPath()
	for _, command := range codexWindowsCommands(t)["SessionStart"] {
		for _, way := range cmdWays {
			t.Run(way, func(t *testing.T) {
				result := runCmd(t, way, repo.Dir, path, command, codexNotice("SessionStart", repo.Dir))
				if result.Code != 0 || !strings.Contains(result.Stdout, "cw is not installed") {
					t.Errorf("exit status %d, want 0 and the line that says cw is not installed:\n%s", result.Code, result.Output())
				}
			})
		}
	}
}

// Requirement "Hooks on Windows", scenario "The guard of Codex on Windows".
func TestCodexWindowsGuardDenies(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	path := pathOf(repo) // cw under test first, then git and the rest
	for _, command := range codexWindowsCommands(t)["PreToolUse"] {
		for _, way := range cmdWays {
			t.Run(way, func(t *testing.T) {
				result := runCmd(t, way, repo.Dir, path, command, codexNotice("PreToolUse", repo.Dir))
				var out struct {
					HookSpecificOutput struct {
						PermissionDecision string `json:"permissionDecision"`
					} `json:"hookSpecificOutput"`
				}
				if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &out); err != nil || out.HookSpecificOutput.PermissionDecision != "deny" {
					t.Errorf("the command (exit status %d) printed no decision deny (%v):\n%s", result.Code, err, result.Output())
				}
				if result.Code != 0 {
					t.Errorf("exit status %d, want 0", result.Code)
				}
			})
		}
	}
}

// Requirement "Hooks on Windows", scenario "An older cw on Windows".
func TestCodexWindowsCommandsExitZeroWithAnOlderCW(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	older := t.TempDir()
	if err := os.WriteFile(filepath.Join(older, "cw.bat"), []byte("@echo off\r\necho usage: cw ^<command^> 1>&2\r\nexit /b 2\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := older + string(os.PathListSeparator) + systemPath()
	for event, commands := range codexWindowsCommands(t) {
		for _, command := range commands {
			for _, way := range cmdWays {
				t.Run(event+"/"+way, func(t *testing.T) {
					result := runCmd(t, way, repo.Dir, path, command, codexNotice(event, repo.Dir))
					if result.Code != 0 || strings.TrimSpace(result.Stdout) != "" {
						t.Errorf("exit status %d and printed, want 0 and nothing on standard output:\n%s", result.Code, result.Output())
					}
				})
			}
		}
	}
}

// Requirement "Hooks on Windows", scenario "Codex starts a session on Windows
// without `cw`".
func TestCodexWindowsSessionStartSaysWhenCWIsMissing(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	withoutCW := filepath.Dir(git) + string(os.PathListSeparator) + systemPath()
	outside := t.TempDir()
	for _, command := range codexWindowsCommands(t)["SessionStart"] {
		for _, way := range cmdWays {
			t.Run(way, func(t *testing.T) {
				missing := runCmd(t, way, repo.Dir, withoutCW, command, codexNotice("SessionStart", repo.Dir))
				if missing.Code != 0 || !strings.Contains(missing.Stdout, "cw is not installed") || !strings.Contains(missing.Stdout, "#install") {
					t.Errorf("without cw in a repository with cw.toml: exit status %d, want 0 and the line that says cw is not installed:\n%s", missing.Code, missing.Output())
				}
				elsewhere := runCmd(t, way, outside, withoutCW, command, codexNotice("SessionStart", outside))
				if elsewhere.Code != 0 || strings.TrimSpace(elsewhere.Stdout) != "" {
					t.Errorf("without cw outside a repository: exit status %d and printed, want 0 and nothing:\n%s", elsewhere.Code, elsewhere.Output())
				}
				present := runCmd(t, way, repo.Dir, pathOf(repo), command, codexNotice("SessionStart", repo.Dir))
				if present.Code != 0 || strings.TrimSpace(present.Stdout) == "" || strings.Contains(present.Stdout, "cw is not installed") {
					t.Errorf("with cw on PATH: exit status %d, want 0 and what `cw context` prints:\n%s", present.Code, present.Output())
				}
			})
		}
	}
}
