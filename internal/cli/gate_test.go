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

// gtHooks are the git hooks cw gate must manage, in the order it reports
// them: git runs pre-commit before a commit, pre-push before a push, and
// pre-merge-commit before a merge commit it makes without conflicts.
var gtHooks = []string{"pre-commit", "pre-push", "pre-merge-commit"}

// gtLines is how install, uninstall and status list the hooks, one line per
// hook in gtHooks' order with its word from words.
func gtLines(words map[string]string) string {
	var b strings.Builder
	for _, name := range gtHooks {
		b.WriteString(name + ": " + words[name] + "\n")
	}
	return b.String()
}

// gtInstalled is how gtEntry shows a hook cw gate install wrote.
func gtInstalled(name string) string { return "-rwxr-xr-x -> \n" + gtScript(name) }

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

// gtDir is how a directory looks: each entry, hidden ones included, or
// "missing".
func gtDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	} else if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, entry := range entries {
		b.WriteString(entry.Name() + " " + gtEntry(t, filepath.Join(dir, entry.Name())) + "\n")
	}
	return b.String()
}

func TestGateInstallLeavesAForeignHookUnchanged(t *testing.T) {
	// Oracle (plan G4b): a hook file whose bytes are not exactly cw's script
	// keeps its bytes, mode and link target through install and uninstall;
	// install exits 4 (refused) and prints the line to add, while the other
	// hooks are written with mode 0755 and the script above, and uninstall
	// removes only them.
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
		{"symbolic link to cw's script", "pre-commit", "pre-push", func(t *testing.T, path string) {
			target := filepath.Join(hkCanonicalTempDir(t), "shared-hook")
			hkWriteFile(t, filepath.Dir(target), filepath.Base(target), gtScript("pre-commit"))
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
		{"script of its own that carries cw's marker line", "pre-commit", "pre-push", func(t *testing.T, path string) {
			hkWriteFile(t, filepath.Dir(path), filepath.Base(path), gtScript("pre-commit")+"npm run lint\n")
		}},
		{"cw's script for the other hook", "pre-push", "pre-commit", func(t *testing.T, path string) {
			hkWriteFile(t, filepath.Dir(path), filepath.Base(path), gtScript("pre-commit"))
		}},
		// Git's sample pre-merge-commit runs pre-commit; a copy of cw's
		// pre-commit script there is not cw's pre-merge-commit.
		{"cw's pre-commit script as pre-merge-commit", "pre-merge-commit", "pre-commit", func(t *testing.T, path string) {
			hkWriteFile(t, filepath.Dir(path), filepath.Base(path), gtScript("pre-commit"))
			if err := os.Chmod(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			repo := hkRepo(t)
			hooks := filepath.Join(repo, ".git", "hooks")
			foreign := filepath.Join(hooks, row.foreign)
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
			for _, name := range gtHooks {
				if got, want := gtEntry(t, filepath.Join(hooks, name)), gtInstalled(name); name != row.foreign && got != want {
					t.Fatalf("install wrote %s as\n%s\nwant\n%s", name, got, want)
				}
			}

			code, stdout, stderr = gtRun(t, "uninstall", "--repo", repo)
			if code != ExitOK || !strings.Contains(stdout, row.foreign+": left unchanged") || !strings.Contains(stdout, row.other+": removed\n") {
				t.Fatalf("uninstall exit %d\n%s%s", code, stdout, stderr)
			}
			if got := gtEntry(t, foreign); got != before {
				t.Fatalf("uninstall changed the foreign %s:\n%s\nwant\n%s", row.foreign, got, before)
			}
			for _, name := range gtHooks {
				if got := gtEntry(t, filepath.Join(hooks, name)); name != row.foreign && got != "absent" {
					t.Fatalf("uninstall left %s:\n%s", name, got)
				}
			}
		})
	}
}

func TestGateStatusAndInstallTrackTheirOwnHooks(t *testing.T) {
	// Oracle: status reports absent, installed and foreign per hook, as text
	// and with --json; install restores the mode of its own script, writes
	// nothing when every hook is current, and leaves no other file behind.
	gtIsolate(t, os.DevNull)
	repo := hkRepo(t)
	hooks := filepath.Join(repo, ".git", "hooks")
	status := func(t *testing.T, want map[string]string) {
		t.Helper()
		code, stdout, stderr := gtRun(t, "status", "--repo", repo, "--json")
		var report struct {
			HooksPath string `json:"hooks_path"`
			Shared    bool   `json:"shared"`
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
		if report.HooksPath != hooks || report.Shared || !reflect.DeepEqual(got, want) {
			t.Fatalf("status --json = %s shared %v %v, want %s unshared %v", report.HooksPath, report.Shared, got, hooks, want)
		}
		code, stdout, _ = gtRun(t, "status", "--repo", repo)
		// A foreign hook's line goes on to say what to do.
		lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
		ok := code == ExitOK && len(lines) == 1+len(gtHooks) && lines[0] == "git hooks directory: "+hooks
		for i, name := range gtHooks {
			if ok {
				line := strings.TrimPrefix(lines[1+i], name+": "+want[name])
				ok = line == "" || want[name] == "foreign" && strings.HasPrefix(line, " (")
			}
		}
		if !ok {
			t.Fatalf("status exit %d:\n%s\nwant the hooks directory %s, then %v in the order %v", code, stdout, hooks, want, gtHooks)
		}
	}
	status(t, map[string]string{"pre-commit": "absent", "pre-push": "absent", "pre-merge-commit": "absent"})

	hkWriteFile(t, hooks, "pre-commit", gtScript("pre-commit"))
	if err := os.Chmod(filepath.Join(hooks, "pre-commit"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := func(t *testing.T) int {
		t.Helper()
		found, err := os.ReadDir(hooks)
		if err != nil {
			t.Fatal(err)
		}
		return len(found)
	}
	before := entries(t)
	code, stdout, _ := gtRun(t, "install", "--repo", repo)
	if code != ExitOK || !strings.HasSuffix(stdout, "\n"+gtLines(map[string]string{"pre-commit": "updated", "pre-push": "installed", "pre-merge-commit": "installed"})) {
		t.Fatalf("install over its own script without the executable bit: exit %d\n%s", code, stdout)
	}
	for _, name := range gtHooks {
		if got, want := gtEntry(t, filepath.Join(hooks, name)), gtInstalled(name); got != want {
			t.Fatalf("%s is\n%s\nwant\n%s", name, got, want)
		}
	}
	if got, want := entries(t), before+2; got != want {
		t.Fatalf("hooks directory holds %d entries after install, want %d:\n%s", got, want, gtDir(t, hooks))
	}
	status(t, map[string]string{"pre-commit": "installed", "pre-push": "installed", "pre-merge-commit": "installed"})
	if code, stdout, _ := gtRun(t, "install", "--repo", repo); code != ExitOK ||
		!strings.HasSuffix(stdout, "\n"+gtLines(map[string]string{"pre-commit": "unchanged", "pre-push": "unchanged", "pre-merge-commit": "unchanged"})) {
		t.Fatalf("second install: exit %d\n%s", code, stdout)
	}

	hkWriteFile(t, hooks, "pre-push", "#!/bin/sh\nexit 0\n")
	status(t, map[string]string{"pre-commit": "installed", "pre-push": "foreign", "pre-merge-commit": "installed"})
}

func TestGateStatusReportsItsOwnHookWithoutAnExecuteBitAsAbsent(t *testing.T) {
	// Oracle: git skips a hook it cannot execute, so status reports cw's
	// own script with mode 0644 as absent, with --json and as text, where
	// the line says why; install then restores mode 0755 (updated) and
	// status reports installed. The other hook, installed, is the control.
	gtIsolate(t, os.DevNull)
	repo := hkRepo(t)
	hooks := filepath.Join(repo, ".git", "hooks")
	if code, stdout, stderr := gtRun(t, "install", "--repo", repo); code != ExitOK {
		t.Fatalf("install: exit %d\n%s%s", code, stdout, stderr)
	}
	if err := os.Chmod(filepath.Join(hooks, "pre-commit"), 0o644); err != nil {
		t.Fatal(err)
	}
	states := func(t *testing.T) map[string]string {
		t.Helper()
		code, stdout, stderr := gtRun(t, "status", "--repo", repo, "--json")
		var report struct {
			Hooks []struct {
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal([]byte(stdout), &report); code != ExitOK || err != nil {
			t.Fatalf("status --json exit %d (%v)\n%s%s", code, err, stdout, stderr)
		}
		got := map[string]string{}
		for _, hook := range report.Hooks {
			got[hook.Name] = hook.State
		}
		return got
	}
	if got, want := states(t), map[string]string{"pre-commit": "absent", "pre-push": "installed", "pre-merge-commit": "installed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("status --json states %v, want %v", got, want)
	}
	code, stdout, _ := gtRun(t, "status", "--repo", repo)
	want := "\n" + gtLines(map[string]string{
		"pre-commit":       "absent (cw gate install wrote it, but it has no execute bit, so git skips it; cw gate install restores the mode)",
		"pre-push":         "installed",
		"pre-merge-commit": "installed",
	})
	if code != ExitOK || !strings.HasSuffix(stdout, want) {
		t.Fatalf("status exit %d:\n%s\nwant it to end with%s", code, stdout, want)
	}
	if code, stdout, _ := gtRun(t, "install", "--repo", repo); code != ExitOK ||
		!strings.HasSuffix(stdout, "\n"+gtLines(map[string]string{"pre-commit": "updated", "pre-push": "unchanged", "pre-merge-commit": "unchanged"})) {
		t.Fatalf("install over its own script without an execute bit: exit %d\n%s", code, stdout)
	}
	if got := gtEntry(t, filepath.Join(hooks, "pre-commit")); got != gtInstalled("pre-commit") {
		t.Fatalf("pre-commit after install is\n%s\nwant\n%s", got, gtInstalled("pre-commit"))
	}
	if got, want := states(t), map[string]string{"pre-commit": "installed", "pre-push": "installed", "pre-merge-commit": "installed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("status --json states after install %v, want %v", got, want)
	}
}

func TestGateUsesTheHooksDirectoryGitRuns(t *testing.T) {
	// Oracle: the directory git runs the repository's hooks from. A relative
	// core.hooksPath resolves from the worktree root, also when --repo names
	// a subdirectory; a linked worktree shares the main repository's hooks;
	// --repo reached through a symbolic link means the directory it points
	// to; GIT_DIR and GIT_WORK_TREE in cw's environment do not choose another
	// repository. No hook appears anywhere else.
	installed := func(t *testing.T, repo, hooks string) {
		t.Helper()
		code, stdout, stderr := gtRun(t, "install", "--repo", repo)
		if want := "git hooks directory: " + hooks + "\n" + gtLines(map[string]string{"pre-commit": "installed", "pre-push": "installed", "pre-merge-commit": "installed"}); code != ExitOK || stdout != want {
			t.Fatalf("install --repo %s: exit %d\n%s%s\nwant\n%s", repo, code, stdout, stderr, want)
		}
		for _, name := range gtHooks {
			if got := gtEntry(t, filepath.Join(hooks, name)); got != gtInstalled(name) {
				t.Fatalf("%s/%s is\n%s", hooks, name, got)
			}
		}
	}
	t.Run("relative core.hooksPath", func(t *testing.T) {
		gtIsolate(t, os.DevNull)
		repo := hkRepo(t)
		hkGit(t, repo, "config", "core.hooksPath", "tracked-hooks")
		hkWriteFile(t, repo, "tracked-hooks/post-merge", "#!/bin/sh\n")
		hkWriteFile(t, repo, "sub/file", "x")
		installed(t, filepath.Join(repo, "sub"), filepath.Join(repo, "tracked-hooks"))
		if got := gtDir(t, filepath.Join(repo, ".git", "hooks")); strings.Contains(got, "pre-commit ") {
			t.Fatalf(".git/hooks holds a pre-commit:\n%s", got)
		}
	})
	t.Run("linked worktree", func(t *testing.T) {
		gtIsolate(t, os.DevNull)
		repo := hkRepo(t)
		hkGit(t, repo, "commit", "-q", "--allow-empty", "-m", "initial")
		linked := filepath.Join(hkCanonicalTempDir(t), "linked")
		hkGit(t, repo, "worktree", "add", "-q", linked)
		installed(t, linked, filepath.Join(repo, ".git", "hooks"))
	})
	t.Run("symbolic link to a subdirectory", func(t *testing.T) {
		gtIsolate(t, os.DevNull)
		repo := hkRepo(t)
		hkWriteFile(t, repo, "lib/pkg/file", "x")
		// Joined to the link rather than to lib/pkg, git's ../../.git/hooks
		// would name outside/.git/hooks.
		outside := hkCanonicalTempDir(t)
		link := filepath.Join(outside, "a", "vendor-lib")
		if err := os.Mkdir(filepath.Dir(link), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(repo, "lib", "pkg"), link); err != nil {
			t.Fatal(err)
		}
		installed(t, link, filepath.Join(repo, ".git", "hooks"))
		if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
			t.Fatalf("%s holds %v (%v), want only a/", outside, entries, err)
		}
	})
	t.Run("GIT_DIR and GIT_WORK_TREE of another repository", func(t *testing.T) {
		gtIsolate(t, os.DevNull)
		repo, other := hkRepo(t), hkRepo(t)
		before := gtDir(t, filepath.Join(other, ".git", "hooks"))
		t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
		t.Setenv("GIT_WORK_TREE", other)
		installed(t, repo, filepath.Join(repo, ".git", "hooks"))
		if got := gtDir(t, filepath.Join(other, ".git", "hooks")); got != before {
			t.Fatalf("install changed the hooks of the repository GIT_DIR names:\n%s", got)
		}
	})
}

func TestGateRefusesASharedHooksDirectoryWithoutTheFlag(t *testing.T) {
	// Oracle (review decision): a hooks directory that other repositories use
	// too, through core.hooksPath in the global configuration or a directory
	// outside the repository's git directory and work tree, makes install
	// and uninstall exit 4 (refused) before they change anything, unless
	// --shared is given; status reports it as shared.
	type setup func(t *testing.T, repo string) (hooks string)
	outsideDir := func(t *testing.T) string {
		shared := filepath.Join(hkCanonicalTempDir(t), "shared-hooks")
		hkWriteFile(t, shared, "pre-commit", "#!/bin/sh\necho team hook\n")
		return shared
	}
	for _, row := range []struct {
		name, reason string
		setup        setup
	}{
		{"global core.hooksPath", "shared: core.hooksPath is set in the global git configuration", func(t *testing.T, repo string) string {
			shared := outsideDir(t)
			global := filepath.Join(hkCanonicalTempDir(t), "gitconfig")
			hkWriteFile(t, filepath.Dir(global), filepath.Base(global), "[core]\n\thooksPath = "+shared+"\n")
			gtIsolate(t, global)
			return shared
		}},
		{"local core.hooksPath outside the repository", "shared: it is outside this repository's git directory and work tree", func(t *testing.T, repo string) string {
			shared := outsideDir(t)
			hkGit(t, repo, "config", "core.hooksPath", shared)
			return shared
		}},
		{"hooks directory linked outside the repository", "shared: it is outside this repository's git directory and work tree", func(t *testing.T, repo string) string {
			shared := outsideDir(t)
			hooks := filepath.Join(repo, ".git", "hooks")
			if err := os.Rename(hooks, hooks+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(shared, hooks); err != nil {
				t.Fatal(err)
			}
			return shared
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			gtIsolate(t, os.DevNull)
			repo := hkRepo(t)
			hooks := row.setup(t, repo)
			before := gtDir(t, hooks)
			for _, command := range []string{"install", "uninstall"} {
				code, stdout, stderr := gtRun(t, command, "--repo", repo)
				if code != ExitRefused || !strings.Contains(stdout, row.reason) || !strings.Contains(stderr, "run cw gate "+command+" again with --shared") {
					t.Fatalf("%s without --shared: exit %d\n%s%s", command, code, stdout, stderr)
				}
				if got := gtDir(t, hooks); got != before {
					t.Fatalf("%s without --shared changed %s:\n%s", command, hooks, got)
				}
			}
			if code, stdout, _ := gtRun(t, "status", "--repo", repo, "--json"); code != ExitOK || !strings.Contains(stdout, `"shared":true`) {
				t.Fatalf("status --json: exit %d %s", code, stdout)
			}
			code, stdout, stderr := gtRun(t, "install", "--repo", repo, "--shared")
			if code != ExitRefused || !strings.HasSuffix(stdout, "\n"+gtLines(map[string]string{"pre-commit": "refused", "pre-push": "installed", "pre-merge-commit": "installed"})) ||
				gtEntry(t, filepath.Join(hooks, "pre-push")) != gtInstalled("pre-push") || gtEntry(t, filepath.Join(hooks, "pre-merge-commit")) != gtInstalled("pre-merge-commit") {
				t.Fatalf("install --shared: exit %d\n%s%s", code, stdout, stderr)
			}
			if code, stdout, _ := gtRun(t, "uninstall", "--repo", repo, "--shared"); code != ExitOK || gtDir(t, hooks) != before {
				t.Fatalf("uninstall --shared: exit %d\n%s\nleft\n%s", code, stdout, gtDir(t, hooks))
			}
		})
	}
}

func TestGateCreatesAHooksDirectoryOnlyInTheGitDirectory(t *testing.T) {
	// Oracle (review decision): install creates a missing hooks directory
	// inside the git directory, and uninstall removes it once it is empty, so
	// the round trip leaves the repository as it was; a missing hooks
	// directory anywhere else is refused (exit 4) and not created.
	gtIsolate(t, os.DevNull)
	repo := hkCanonicalTempDir(t)
	hkGit(t, repo, "init", "-q", "--template=")
	hooks := filepath.Join(repo, ".git", "hooks")
	if got := gtDir(t, hooks); got != "missing" {
		t.Fatalf("git init --template= made %s:\n%s", hooks, got)
	}
	if code, stdout, stderr := gtRun(t, "install", "--repo", repo); code != ExitOK || gtEntry(t, filepath.Join(hooks, "pre-push")) != gtInstalled("pre-push") {
		t.Fatalf("install: exit %d\n%s%s", code, stdout, stderr)
	}
	if code, stdout, stderr := gtRun(t, "uninstall", "--repo", repo); code != ExitOK || gtDir(t, hooks) != "missing" {
		t.Fatalf("uninstall: exit %d\n%s%s\nleft %s:\n%s", code, stdout, stderr, hooks, gtDir(t, hooks))
	}

	hkGit(t, repo, "config", "core.hooksPath", ".githooks")
	code, _, stderr := gtRun(t, "install", "--repo", repo)
	if code != ExitRefused || !strings.Contains(stderr, "the hooks directory "+filepath.Join(repo, ".githooks")+" does not exist") {
		t.Fatalf("install into a missing core.hooksPath: exit %d\n%s", code, stderr)
	}
	if got := gtDir(t, filepath.Join(repo, ".githooks")); got != "missing" {
		t.Fatalf("install created .githooks:\n%s", got)
	}
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
	for _, name := range gtHooks {
		hook := exec.CommandContext(t.Context(), "/bin/sh", filepath.Join(repo, ".git", "hooks", name), "origin", "url")
		hook.Env, hook.Dir = []string{"PATH=" + t.TempDir()}, repo
		output, err := hook.CombinedOutput()
		if err != nil || len(output) != 0 {
			t.Fatalf("%s without cw on PATH: %v, output %q; want exit 0 without output", name, err, output)
		}
	}
}
