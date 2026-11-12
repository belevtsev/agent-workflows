//go:build unix

package cli

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Tests for cw gate install, uninstall and status, driven through Execute
// against real repositories. cw runs git with this process's environment, so
// each test first points git at a global configuration of its own.

// gtScript is the hook cw gate install must write, spelled out here as the
// oracle rather than taken from the code under test.
func gtScript(name string) string {
	return "#!/bin/sh\n# agent-workflows: written by cw gate install; cw gate uninstall deletes this file\n" +
		"command -v cw >/dev/null 2>&1 || exit 0; exec cw hook git " + name + " \"$@\"\n"
}

// gtLine is the line cw gate install tells a person to add to a foreign hook.
func gtLine(name string) string {
	return "if command -v cw >/dev/null 2>&1; then cw hook git " + name + " \"$@\" </dev/null || exit $?; fi"
}

// gtIsolate makes git read global, or no global configuration when global
// is os.DevNull, and no system configuration.
func gtIsolate(t *testing.T, global string) {
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// gtRun runs `cw gate ARGS` and returns its exit status, stdout and stderr.
func gtRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	config, out := testConfig(t)
	var stderr bytes.Buffer
	config.Err = &stderr
	err := Execute(t.Context(), append([]string{"gate"}, args...), config)
	return ExitCode(err), out.String(), stderr.String()
}

// gtEntry is how one directory entry looks: its mode, its link target and
// the bytes it leads to.
func gtEntry(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "absent"
	} else if err != nil {
		t.Fatal(err)
	}
	target, _ := os.Readlink(path)
	data, _ := os.ReadFile(path)
	return info.Mode().String() + " -> " + target + "\n" + string(data)
}

func TestGateInstallLeavesAForeignHookUnchanged(t *testing.T) {
	// Oracle (plan G4b): a hook file cw gate did not write keeps its bytes,
	// mode and link target through install and uninstall; install exits 4
	// (refused) and prints the line to add, while the other hook is written
	// with mode 0755 and the script above, and uninstall removes only it.
	gtIsolate(t, os.DevNull)
	for _, row := range []struct {
		name, foreign, other string
		make                 func(t *testing.T, path string)
	}{
		{"executable script", "pre-commit", "pre-push", func(t *testing.T, path string) {
			hkWriteFile(t, filepath.Dir(path), filepath.Base(path), "#!/bin/sh\nnpx lint-staged\n")
			if err := os.Chmod(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"empty file", "pre-push", "pre-commit", func(t *testing.T, path string) {
			hkWriteFile(t, filepath.Dir(path), filepath.Base(path), "")
		}},
		{"symbolic link to a file with the marker", "pre-commit", "pre-push", func(t *testing.T, path string) {
			target := filepath.Join(hkCanonicalTempDir(t), "shared-hook")
			hkWriteFile(t, filepath.Dir(target), filepath.Base(target), gtScript("pre-commit"))
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			repo := hkRepo(t)
			hooks := filepath.Join(repo, ".git", "hooks")
			foreign, other := filepath.Join(hooks, row.foreign), filepath.Join(hooks, row.other)
			row.make(t, foreign)
			before := gtEntry(t, foreign)

			code, stdout, stderr := gtRun(t, "install", "--repo", repo)
			if code != ExitRefused {
				t.Fatalf("install exit %d, want %d (refused)\n%s%s", code, ExitRefused, stdout, stderr)
			}
			if !strings.Contains(stdout, row.foreign+": refused\n") || !strings.Contains(stdout, row.other+": installed\n") {
				t.Fatalf("install stdout:\n%s", stdout)
			}
			if !strings.Contains(stderr, foreign) || !strings.Contains(stderr, "\n"+gtLine(row.foreign)+"\n") {
				t.Fatalf("install stderr does not name %s and give the line to add:\n%s", foreign, stderr)
			}
			if got := gtEntry(t, foreign); got != before {
				t.Fatalf("install changed the foreign %s:\n%s\nwant\n%s", row.foreign, got, before)
			}
			if got, want := gtEntry(t, other), "-rwxr-xr-x -> \n"+gtScript(row.other); got != want {
				t.Fatalf("install wrote %s as\n%s\nwant\n%s", row.other, got, want)
			}

			code, stdout, stderr = gtRun(t, "uninstall", "--repo", repo)
			if code != ExitOK || !strings.Contains(stdout, row.foreign+": left unchanged") || !strings.Contains(stdout, row.other+": removed\n") {
				t.Fatalf("uninstall exit %d\n%s%s", code, stdout, stderr)
			}
			if got := gtEntry(t, foreign); got != before {
				t.Fatalf("uninstall changed the foreign %s:\n%s\nwant\n%s", row.foreign, got, before)
			}
			if got := gtEntry(t, other); got != "absent" {
				t.Fatalf("uninstall left %s:\n%s", row.other, got)
			}
		})
	}
}

func TestGateStatusAndInstallTrackTheirOwnHooks(t *testing.T) {
	// Oracle: status reports absent, installed and foreign per hook, as text
	// and with --json; install rewrites a marked hook that differs from its
	// script, writes nothing when both are current, and writes no other file.
	gtIsolate(t, os.DevNull)
	repo := hkRepo(t)
	hooks := filepath.Join(repo, ".git", "hooks")
	status := func(t *testing.T, want map[string]string) {
		t.Helper()
		code, stdout, stderr := gtRun(t, "status", "--repo", repo, "--json")
		var report struct {
			HooksPath string `json:"hooks_path"`
			Hooks     []struct {
				Name  string `json:"name"`
				Path  string `json:"path"`
				State string `json:"state"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal([]byte(stdout), &report); code != ExitOK || err != nil {
			t.Fatalf("status --json exit %d (%v)\n%s%s", code, err, stdout, stderr)
		}
		got := map[string]string{}
		for _, hook := range report.Hooks {
			if hook.Path != filepath.Join(hooks, hook.Name) {
				t.Fatalf("status --json path %s for %s", hook.Path, hook.Name)
			}
			got[hook.Name] = hook.State
		}
		if report.HooksPath != hooks || !reflect.DeepEqual(got, want) {
			t.Fatalf("status --json = %s %v, want %s %v", report.HooksPath, got, hooks, want)
		}
		code, stdout, _ = gtRun(t, "status", "--repo", repo)
		text := "git hooks directory: " + hooks + "\npre-commit: " + want["pre-commit"] + "\npre-push: " + want["pre-push"]
		if code != ExitOK || !strings.HasPrefix(stdout, text) {
			t.Fatalf("status exit %d:\n%s\nwant it to start with\n%s", code, stdout, text)
		}
	}
	entries := func(t *testing.T) []string {
		t.Helper()
		names, err := filepath.Glob(filepath.Join(hooks, "*"))
		if err != nil {
			t.Fatal(err)
		}
		dot, _ := filepath.Glob(filepath.Join(hooks, ".*"))
		return append(names, dot...)
	}
	before := entries(t)
	status(t, map[string]string{"pre-commit": "absent", "pre-push": "absent"})

	hkWriteFile(t, hooks, "pre-commit", "#!/bin/sh\n# agent-workflows: written by cw gate install; cw gate uninstall deletes this file\nexit 1\n")
	code, stdout, _ := gtRun(t, "install", "--repo", repo)
	if code != ExitOK || !strings.Contains(stdout, "pre-commit: updated\npre-push: installed\n") {
		t.Fatalf("install over an older own hook: exit %d\n%s", code, stdout)
	}
	for _, name := range []string{"pre-commit", "pre-push"} {
		if got, want := gtEntry(t, filepath.Join(hooks, name)), "-rwxr-xr-x -> \n"+gtScript(name); got != want {
			t.Fatalf("%s is\n%s\nwant\n%s", name, got, want)
		}
	}
	if got, want := len(entries(t)), len(before)+2; got != want {
		t.Fatalf("hooks directory holds %d entries after install, want %d: %v", got, want, entries(t))
	}
	status(t, map[string]string{"pre-commit": "installed", "pre-push": "installed"})
	if code, stdout, _ := gtRun(t, "install", "--repo", repo); code != ExitOK || !strings.Contains(stdout, "pre-commit: unchanged\npre-push: unchanged\n") {
		t.Fatalf("second install: exit %d\n%s", code, stdout)
	}

	hkWriteFile(t, hooks, "pre-push", "#!/bin/sh\nexit 0\n")
	status(t, map[string]string{"pre-commit": "installed", "pre-push": "foreign"})
}

func TestGateUsesTheHooksDirectoryGitRuns(t *testing.T) {
	// Oracle: `git rev-parse --git-path hooks`. A relative core.hooksPath
	// is resolved from the worktree root, also when --repo names a
	// subdirectory; a linked worktree shares the main repository's hooks;
	// core.hooksPath from the global configuration is written to, with a
	// note that other repositories use those hooks too.
	gtIsolate(t, os.DevNull)
	installed := func(t *testing.T, repo, hooks string, note bool) {
		t.Helper()
		code, stdout, stderr := gtRun(t, "install", "--repo", repo)
		if code != ExitOK || !strings.HasPrefix(stdout, "git hooks directory: "+hooks+"\n") {
			t.Fatalf("install --repo %s: exit %d\n%s%s\nwant hooks directory %s", repo, code, stdout, stderr, hooks)
		}
		if got := strings.Contains(stdout, "\nnote: core.hooksPath is set in the global git configuration"); got != note {
			t.Fatalf("install note present = %v, want %v:\n%s", got, note, stdout)
		}
		for _, name := range []string{"pre-commit", "pre-push"} {
			if got := gtEntry(t, filepath.Join(hooks, name)); got != "-rwxr-xr-x -> \n"+gtScript(name) {
				t.Fatalf("%s/%s is\n%s", hooks, name, got)
			}
		}
	}

	t.Run("relative core.hooksPath", func(t *testing.T) {
		repo := hkRepo(t)
		hkGit(t, repo, "config", "core.hooksPath", "tracked-hooks")
		hkWriteFile(t, repo, "sub/file", "x")
		installed(t, filepath.Join(repo, "sub"), filepath.Join(repo, "tracked-hooks"), false)
		if got := gtEntry(t, filepath.Join(repo, ".git", "hooks", "pre-commit")); got != "absent" {
			t.Fatalf(".git/hooks/pre-commit is %s, want absent", got)
		}
	})
	t.Run("linked worktree", func(t *testing.T) {
		repo := hkRepo(t)
		hkGit(t, repo, "commit", "-q", "--allow-empty", "-m", "initial")
		linked := filepath.Join(hkCanonicalTempDir(t), "linked")
		hkGit(t, repo, "worktree", "add", "-q", linked)
		installed(t, linked, filepath.Join(repo, ".git", "hooks"), false)
	})
	t.Run("global core.hooksPath", func(t *testing.T) {
		shared := filepath.Join(hkCanonicalTempDir(t), "shared-hooks")
		global := filepath.Join(hkCanonicalTempDir(t), "gitconfig")
		hkWriteFile(t, filepath.Dir(global), filepath.Base(global), "[core]\n\thooksPath = "+shared+"\n")
		repo := hkRepo(t)
		gtIsolate(t, global)
		installed(t, repo, shared, true)
	})
}

func TestGateRefusesDryRunAndADirectoryOutsideGit(t *testing.T) {
	// Oracle: the exit table. --dry-run on a command that writes is a usage
	// error (2) and writes nothing; a directory git does not recognise as a
	// repository is a failure (1) that says so.
	gtIsolate(t, os.DevNull)
	repo := hkRepo(t)
	for _, command := range []string{"install", "uninstall"} {
		config, _ := testConfig(t)
		err := Execute(t.Context(), []string{"--dry-run", "gate", command, "--repo", repo}, config)
		if ExitCode(err) != ExitUsage || err.Error() != "cw gate "+command+" rejects --dry-run" {
			t.Fatalf("--dry-run gate %s: %v (exit %d)", command, err, ExitCode(err))
		}
	}
	if got := gtEntry(t, filepath.Join(repo, ".git", "hooks", "pre-commit")); got != "absent" {
		t.Fatalf("--dry-run install wrote pre-commit:\n%s", got)
	}
	outside := hkCanonicalTempDir(t)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(outside))
	config, _ := testConfig(t)
	err := Execute(t.Context(), []string{"gate", "status", "--repo", outside}, config)
	if ExitCode(err) != ExitFailure || !strings.HasPrefix(err.Error(), "cannot locate the git hooks directory of "+outside+": fatal: not a git repository") {
		t.Fatalf("gate status outside git: %v (exit %d)", err, ExitCode(err))
	}
}

func TestGateHookLetsGitGoAheadWithoutCw(t *testing.T) {
	// Oracle (the accepted fail-open): the installed hook, run by /bin/sh
	// with no cw on PATH, exits 0 without output, so git goes ahead.
	gtIsolate(t, os.DevNull)
	repo := hkRepo(t)
	if code, stdout, stderr := gtRun(t, "install", "--repo", repo); code != ExitOK {
		t.Fatalf("install exit %d\n%s%s", code, stdout, stderr)
	}
	for _, name := range []string{"pre-commit", "pre-push"} {
		hook := exec.CommandContext(t.Context(), "/bin/sh", filepath.Join(repo, ".git", "hooks", name), "origin", "url")
		hook.Env, hook.Dir = []string{"PATH=" + t.TempDir()}, repo
		output, err := hook.CombinedOutput()
		if err != nil || len(output) != 0 {
			t.Fatalf("%s without cw on PATH: %v, output %q; want exit 0 without output", name, err, output)
		}
	}
}
