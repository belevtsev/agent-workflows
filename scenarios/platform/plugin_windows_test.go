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

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
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
func runCmd(t *testing.T, way, dir, path, command string, notice map[string]any) kitsutest.Result {
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
	result := kitsutest.Result{Stdout: stdout.String(), Stderr: stderr.String()}
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

// systemPath is a PATH with the folder of cmd.exe and nothing that holds kitsu.
func systemPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32")
}

func assertMissingKitsuGuidance(t *testing.T, result kitsutest.Result) {
	t.Helper()
	const want = "kitsu is not installed or is missing from absolute PATH. Install it: https://github.com/belevtsev/kitsu#install"
	text := strings.TrimSuffix(strings.TrimSuffix(result.Stdout, "\n"), "\r")
	if result.Code != 0 || text != want {
		t.Errorf("SessionStart exited %d and printed %q, want exit 0 and only the install/PATH guidance %q; stderr:\n%s", result.Code, result.Stdout, want, result.Stderr)
	}
}

// Requirement "Hooks on Windows", scenario "A kitsu of the working directory".
func TestCodexWindowsCommandsRunNoKitsuOfTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	marker := filepath.Join(t.TempDir(), "folder-kitsu-ran")
	repo.Write("kitsu.bat", "@echo off\r\necho ran> \""+marker+"\"\r\necho {\"hookSpecificOutput\":{}}\r\n")
	for event, commands := range codexWindowsCommands(t) {
		for _, command := range commands {
			for _, way := range cmdWays {
				t.Run(event+"/"+way, func(t *testing.T) {
					result := runCmd(t, way, repo.Dir, systemPath(), command, codexNotice(event, repo.Dir))
					if result.Code != 0 {
						t.Errorf("exit status %d, want 0:\n%s", result.Code, result.Output())
					}
					if exists(marker) {
						t.Errorf("the command ran kitsu.bat of the working directory")
					}
					if strings.Contains(result.Stdout, "hookSpecificOutput") {
						t.Errorf("the command printed what kitsu.bat of the working directory prints:\n%s", result.Stdout)
					}
					if event == "SessionStart" {
						assertMissingKitsuGuidance(t, result)
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
	for _, name := range []string{"kitsu.bat", "git.bat", "where.bat", "tools/kitsu.bat", "tools/git.bat"} {
		repo.Write(name, planted)
	}
	// The control used below works: through the PATH of the repository
	// cmd.exe runs the installed kitsu from a folder that holds nothing.
	const askCmd = "set NoDefaultCurrentDirectoryInExePath=1& kitsu version"
	if control := runCmd(t, cmdWays[0], t.TempDir(), pathOf(repo), askCmd, codexNotice("PreToolUse", repo.Dir)); !strings.HasPrefix(strings.TrimSpace(control.Stdout), "kitsu ") {
		t.Fatalf("cmd.exe does not run `kitsu version` through the PATH of the repository, so the control proves nothing:\n%s", control.Output())
	}
	for _, first := range []string{".", "tools", `.\tools`, ""} {
		path := first + string(os.PathListSeparator) + systemPath()
		// cmd.exe finds the planted file through such an entry, so a command
		// that stayed in the repository would run it.
		if first != "" {
			probe := runCmd(t, cmdWays[0], repo.Dir, path, "set NoDefaultCurrentDirectoryInExePath=1& kitsu", codexNotice("SessionStart", repo.Dir))
			if !strings.Contains(probe.Stdout, "hookSpecificOutput") {
				t.Fatalf("with PATH=%s cmd.exe does not find the planted kitsu.bat, so the test proves nothing:\n%s", path, probe.Output())
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
						if event == "SessionStart" {
							assertMissingKitsuGuidance(t, result)
						}
					})
				}
			}
		}
		// With the installed kitsu in a later entry the guard still decides.
		installed := first + string(os.PathListSeparator) + pathOf(repo)
		neutral := t.TempDir()
		for _, command := range codexWindowsCommands(t)["PreToolUse"] {
			for _, way := range cmdWays {
				t.Run("PreToolUse with the installed kitsu/"+way+"/PATH="+first, func(t *testing.T) {
					notice := codexNotice("PreToolUse", repo.Dir)
					result := runCmd(t, way, repo.Dir, installed, command, notice)
					if result.Code != 0 {
						t.Errorf("exit status %d, want 0:\n%s", result.Code, result.Output())
					}
					if exists(marker) {
						t.Errorf("the command ran a program of the repository")
					}
					if strings.Contains(result.Stdout, "deny") {
						return
					}
					// Behind an empty first entry cmd.exe may find no program
					// at all. Then the command has no kitsu to run, and that is
					// the system's doing: the control asks cmd.exe for kitsu
					// from a folder that holds nothing.
					control := runCmd(t, way, neutral, installed, askCmd, notice)
					if first == "" && !strings.HasPrefix(strings.TrimSpace(control.Stdout), "kitsu ") {
						if strings.TrimSpace(result.Stdout) != "" {
							t.Errorf("cmd.exe finds no kitsu through this PATH, and the command printed, want nothing:\n%s", result.Output())
						}
						return
					}
					// What follows only says more about the failure.
					loud := runCmd(t, way, repo.Dir, installed, strings.ReplaceAll(command, " 2>nul", ""), notice)
					git := runCmd(t, way, neutral, installed, "set NoDefaultCurrentDirectoryInExePath=1& git --version", notice)
					status := runCmd(t, way, neutral, installed, "set NoDefaultCurrentDirectoryInExePath=1& cd /d %SystemRoot% && kitsu context --host codex", codexNotice("SessionStart", repo.Dir))
					t.Errorf("the command printed no decision deny:\n%s\n`kitsu version` through the same PATH (exit status %d):\n%s\nthe command without 2>nul (exit status %d):\n%s\n`git --version` (exit status %d):\n%s\n`kitsu context` from the Windows folder (exit status %d):\n%s",
						result.Output(), control.Code, control.Output(), loud.Code, loud.Output(), git.Code, git.Output(), status.Code, status.Output())
				})
			}
		}
		// And the session still gets what `kitsu context` prints. Where cmd.exe
		// finds no kitsu behind an empty first entry, the session is told that
		// kitsu is not installed, as for a missing kitsu.
		for _, command := range codexWindowsCommands(t)["SessionStart"] {
			for _, way := range cmdWays {
				t.Run("SessionStart with the installed kitsu/"+way+"/PATH="+first, func(t *testing.T) {
					notice := codexNotice("SessionStart", repo.Dir)
					result := runCmd(t, way, repo.Dir, installed, command, notice)
					if result.Code != 0 {
						t.Errorf("exit status %d, want 0:\n%s", result.Code, result.Output())
					}
					if exists(marker) {
						t.Errorf("the command ran a program of the repository")
					}
					said := strings.Contains(result.Stdout, "kitsu is not installed")
					// The control asks from the Windows folder, where the
					// command itself runs kitsu.
					control := runCmd(t, way, filepath.Dir(systemPath()), installed, askCmd, notice)
					switch found := strings.HasPrefix(strings.TrimSpace(control.Stdout), "kitsu "); {
					case found && (said || !strings.Contains(result.Stdout, "declared in kitsu.toml")):
						t.Errorf("cmd.exe finds kitsu through this PATH, and the command did not print what `kitsu context` prints:\n%s", result.Output())
					case !found && first != "":
						t.Errorf("cmd.exe finds no kitsu through this PATH:\n%s", control.Output())
					case !found:
						assertMissingKitsuGuidance(t, result)
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
	for _, name := range []string{"kitsu.bat", "tools/kitsu.bat"} {
		repo.Write(name, planted)
	}
	drive := substDrive(t, filepath.Dir(repo.Dir))
	session := drive + `\` + filepath.Base(repo.Dir)
	if !exists(filepath.Join(session, "kitsu.toml")) {
		t.Fatalf("%s does not show the repository", session)
	}
	// From another drive cmd.exe still finds the planted file through an
	// entry such as Z:tools: it leads into the directory left on drive Z.
	probe := runCmd(t, cmdWays[0], session, drive+"tools"+string(os.PathListSeparator)+systemPath(), "set NoDefaultCurrentDirectoryInExePath=1& cd /d %SystemRoot% && kitsu", codexNotice("SessionStart", session))
	if !strings.Contains(probe.Stdout, "hookSpecificOutput") {
		t.Fatalf("with PATH=%stools cmd.exe does not find the planted kitsu.bat from another drive, so the test proves nothing:\n%s", drive, probe.Output())
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
						if event == "SessionStart" {
							assertMissingKitsuGuidance(t, result)
						}
					})
				}
			}
		}
	}
}

// Requirement "Hooks on Windows", scenario "A file named `kitsu` that Windows
// cannot run".
func TestCodexWindowsSessionStartSaysWhenKitsuCannotBeRun(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "kitsu"), []byte("#!/bin/sh\necho ran\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := folder + string(os.PathListSeparator) + systemPath()
	for _, command := range codexWindowsCommands(t)["SessionStart"] {
		for _, way := range cmdWays {
			t.Run(way, func(t *testing.T) {
				result := runCmd(t, way, repo.Dir, path, command, codexNotice("SessionStart", repo.Dir))
				assertMissingKitsuGuidance(t, result)
			})
		}
	}
}

// Requirement "Hooks on Windows", scenario "The guard of Codex on Windows".
func TestCodexWindowsGuardDenies(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	path := pathOf(repo) // kitsu under test first, then git and the rest
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

// Requirement "Hooks on Windows", scenario "An older kitsu on Windows".
func TestCodexWindowsCommandsExitZeroWithAnOlderKitsu(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	older := t.TempDir()
	if err := os.WriteFile(filepath.Join(older, "kitsu.bat"), []byte("@echo off\r\necho usage: kitsu ^<command^> 1>&2\r\nexit /b 2\r\n"), 0o644); err != nil {
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
// without `kitsu`".
func TestCodexWindowsSessionStartSaysWhenKitsuIsMissing(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	plain := kitsutest.NewRepo(t)
	plain.Write("nested/README.md", "# no gate\n")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	paths := []struct{ name, path string }{
		{"with Git", filepath.Dir(git) + string(os.PathListSeparator) + systemPath()},
		{"without Git", systemPath()},
	}
	sessions := []struct{ name, dir string }{
		{"repository with kitsu.toml", repo.Dir},
		{"repository without kitsu.toml", plain.Dir},
		{"subfolder without kitsu.toml", filepath.Join(plain.Dir, "nested")},
		{"outside Git", t.TempDir()},
	}
	for _, command := range codexWindowsCommands(t)["SessionStart"] {
		for _, way := range cmdWays {
			t.Run(way, func(t *testing.T) {
				for _, path := range paths {
					for _, session := range sessions {
						t.Run(path.name+"/"+session.name, func(t *testing.T) {
							missing := runCmd(t, way, session.dir, path.path, command, codexNotice("SessionStart", session.dir))
							assertMissingKitsuGuidance(t, missing)
						})
					}
				}
				present := runCmd(t, way, repo.Dir, pathOf(repo), command, codexNotice("SessionStart", repo.Dir))
				if present.Code != 0 || strings.TrimSpace(present.Stdout) == "" || strings.Contains(present.Stdout, "kitsu is not installed") {
					t.Errorf("with kitsu on PATH: exit status %d, want 0 and what `kitsu context` prints:\n%s", present.Code, present.Output())
				}
			})
		}
	}
}
