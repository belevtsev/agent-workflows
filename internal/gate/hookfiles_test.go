package gate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestIsHook(t *testing.T) {
	for _, name := range []string{"pre-commit", "pre-merge-commit", "pre-push"} {
		if !IsHook(name) {
			t.Errorf("IsHook(%q) = false", name)
		}
	}
	for _, name := range []string{"", "commit-msg", "post-commit", "pre-rebase", "Pre-commit"} {
		if IsHook(name) {
			t.Errorf("IsHook(%q) = true", name)
		}
	}
}

func TestLine(t *testing.T) {
	if got := Line("pre-commit"); got != `kitsu hook pre-commit "$@"` {
		t.Errorf("Line = %q", got)
	}
}

func TestScript(t *testing.T) {
	for _, name := range Hooks {
		script := Script(name)
		if !strings.HasPrefix(script, "#!/bin/sh\n") {
			t.Errorf("%s does not begin with #!/bin/sh", name)
		}
		for _, want := range []string{marker, "exit 3", InstallURL, "command -v kitsu", "absolute path"} {
			if !strings.Contains(script, want) {
				t.Errorf("the %s script lacks %q:\n%s", name, want, script)
			}
		}
		if !strings.Contains(script, `case "$kitsu" in /*) exec "$kitsu" hook `+name+` "$@" ;; esac`+"\n") || callLine(script, name) == "" {
			t.Errorf("the %s script does not run kitsu hook by an absolute path:\n%s", name, script)
		}
		if strings.Contains(script, "exec kitsu ") {
			t.Errorf("the %s script runs kitsu by its bare name:\n%s", name, script)
		}
		for _, banned := range []string{"go install", "go get", "--no-verify"} {
			if strings.Contains(script, banned) {
				t.Errorf("the %s script holds %q", name, banned)
			}
		}
	}
}

func hookPaths(dir string) []string {
	var paths []string
	for _, name := range Hooks {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}

func TestInstall(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	foreign, err := Install(t.Context(), e.repo)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("Install = %v, %v", foreign, err)
	}
	dir := filepath.Join(e.repo.CommonDir, "hooks")
	first := map[string]string{}
	for i, path := range hookPaths(dir) {
		content := readFile(t, path)
		if content != Script(Hooks[i]) {
			t.Errorf("%s holds:\n%s", path, content)
		}
		first[path] = content
		if runtime.GOOS != "windows" {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm()&0o111 != 0o111 {
				t.Errorf("%s is not executable: %v %v", path, info.Mode(), err)
			}
		}
	}
	if State(t.Context(), e.repo) != Installed {
		t.Error("State after Install is not installed")
	}

	foreign, err = Install(t.Context(), e.repo)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("second Install = %v, %v", foreign, err)
	}
	for path, content := range first {
		if readFile(t, path) != content {
			t.Errorf("the second Install changed %s", path)
		}
	}
}

func TestInstallRepairsOwnFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes do not exist on Windows")
	}
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	if _, err := Install(t.Context(), e.repo); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.repo.CommonDir, "hooks", "pre-push")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if State(t.Context(), e.repo) != Missing {
		t.Error("State with a hook that cannot be executed is not missing")
	}
	if err := os.WriteFile(path, []byte(marker+"\nold version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if foreign, err := Install(t.Context(), e.repo); err != nil || len(foreign) != 0 {
		t.Fatalf("Install = %v, %v", foreign, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o111 == 0 || readFile(t, path) != Script("pre-push") {
		t.Errorf("Install did not rewrite its own outdated file")
	}
}

func TestInstallLeavesForeignFile(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	dir := filepath.Join(e.repo.CommonDir, "hooks")
	other := "#!/bin/sh\necho another tool\n"
	path := filepath.Join(dir, "pre-commit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProgram(t, path, other)
	foreign, err := Install(t.Context(), e.repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Foreign{{Name: "pre-commit", Path: path}}; !slices.Equal(foreign, want) {
		t.Errorf("foreign = %v, want %v", foreign, want)
	}
	if readFile(t, path) != other {
		t.Error("Install changed a file it did not write")
	}
	for _, name := range []string{"pre-merge-commit", "pre-push"} {
		if readFile(t, filepath.Join(dir, name)) != Script(name) {
			t.Errorf("%s was not installed", name)
		}
	}
	if State(t.Context(), e.repo) != Missing {
		t.Error("State with a foreign pre-commit that does not call kitsu is not missing")
	}
}

func TestInstallWritesNothingWithHooksPath(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	e.git("config", "core.hooksPath", ".githooks")
	own := filepath.Join(e.repo.CommonDir, "hooks")
	before, _ := os.ReadDir(own)
	foreign, err := Install(t.Context(), e.repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []Foreign{}
	for _, name := range Hooks {
		want = append(want, Foreign{Name: name, Path: filepath.Join(e.dir, ".githooks", name)})
	}
	if !slices.Equal(foreign, want) {
		t.Errorf("foreign = %v, want %v", foreign, want)
	}
	if exists(filepath.Join(e.dir, ".githooks")) {
		t.Error("Install created the configured hooks directory")
	}
	after, _ := os.ReadDir(own)
	if len(after) != len(before) {
		t.Errorf("Install wrote into %s: %d entries before, %d after", own, len(before), len(after))
	}
	for _, path := range hookPaths(own) {
		if exists(path) {
			t.Errorf("Install wrote %s", path)
		}
	}
}

func TestInstallInLinkedWorktreeUsesCommonDir(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	wt := filepath.Join(e.base, "wt")
	e.git("worktree", "add", "-q", wt)
	linked := newRepoAt(t, wt)
	if _, err := Install(t.Context(), linked); err != nil {
		t.Fatal(err)
	}
	for _, path := range hookPaths(filepath.Join(e.repo.CommonDir, "hooks")) {
		if !exists(path) {
			t.Errorf("%s is missing", path)
		}
	}
	if State(t.Context(), e.repo) != Installed || State(t.Context(), linked) != Installed {
		t.Error("the hooks of a linked worktree are not the main checkout's")
	}
}

func TestState(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	if State(t.Context(), e.repo) != Missing {
		t.Error("State without hooks is not missing")
	}
	if _, err := Install(t.Context(), e.repo); err != nil {
		t.Fatal(err)
	}
	// A hook of another tool that calls kitsu counts.
	path := filepath.Join(e.repo.CommonDir, "hooks", "pre-commit")
	writeProgram(t, path, "#!/bin/sh\nformat\n"+Line("pre-commit")+"\n")
	if State(t.Context(), e.repo) != Installed {
		t.Error("State with a shared hook that calls kitsu hook is not installed")
	}
	if err := os.Remove(filepath.Join(e.repo.CommonDir, "hooks", "pre-push")); err != nil {
		t.Fatal(err)
	}
	if State(t.Context(), e.repo) != Missing {
		t.Error("State with pre-push removed is not missing")
	}
	if _, err := Install(t.Context(), e.repo); err != nil {
		t.Fatal(err)
	}
	e.git("config", "core.hooksPath", ".githooks")
	if err := os.MkdirAll(filepath.Join(e.dir, ".githooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if State(t.Context(), e.repo) != Missing {
		t.Error("State with an empty core.hooksPath directory is not missing")
	}
	for _, name := range Hooks {
		writeProgram(t, filepath.Join(e.dir, ".githooks", name), Script(name))
	}
	if State(t.Context(), e.repo) != Installed {
		t.Error("State with kitsu hooks in core.hooksPath is not installed")
	}
}

func TestUninstall(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	dir := filepath.Join(e.repo.CommonDir, "hooks")
	if _, err := Install(t.Context(), e.repo); err != nil {
		t.Fatal(err)
	}
	shared := "#!/bin/sh\nformat\n" + Line("pre-commit") + "\n"
	sharedPath := filepath.Join(dir, "pre-commit")
	writeProgram(t, sharedPath, shared)
	unrelated := filepath.Join(dir, "commit-msg")
	writeProgram(t, unrelated, "#!/bin/sh\n")
	removed, foreign, err := Uninstall(t.Context(), e.repo)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("Uninstall removed %d hooks, want the 2 that kitsu wrote", removed)
	}
	if want := []Foreign{{Name: "pre-commit", Path: sharedPath, Line: Line("pre-commit")}}; !slices.Equal(foreign, want) {
		t.Errorf("foreign = %v, want %v", foreign, want)
	}
	if own := Own(t.Context(), e.repo); len(own) != 0 {
		t.Errorf("after Uninstall kitsu still owns %v", own)
	}
	if readFile(t, sharedPath) != shared {
		t.Error("Uninstall changed a hook it did not write")
	}
	if !exists(unrelated) {
		t.Error("Uninstall removed a hook that is not kitsu's")
	}
	for _, name := range []string{"pre-merge-commit", "pre-push"} {
		if exists(filepath.Join(dir, name)) {
			t.Errorf("Uninstall left %s", name)
		}
	}
	if removed, foreign, err := Uninstall(t.Context(), e.repo); err != nil || len(foreign) != 1 || removed != 0 {
		t.Errorf("a second Uninstall = %d, %v, %v", removed, foreign, err)
	}
}

func TestUninstallWithHooksPath(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	if _, err := Install(t.Context(), e.repo); err != nil {
		t.Fatal(err)
	}
	e.git("config", "core.hooksPath", ".githooks")
	configured := filepath.Join(e.dir, ".githooks")
	if err := os.MkdirAll(configured, 0o755); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(configured, "pre-push")
	writeProgram(t, own, Script("pre-push"))
	if _, _, err := Uninstall(t.Context(), e.repo); err != nil {
		t.Fatal(err)
	}
	if exists(own) {
		t.Error("Uninstall left kitsu's own file in core.hooksPath")
	}
	for _, path := range hookPaths(filepath.Join(e.repo.CommonDir, "hooks")) {
		if exists(path) {
			t.Errorf("Uninstall left %s in the common hooks directory", path)
		}
	}
}

// A directory that core.hooksPath names belongs to its owner: a file there
// that holds more than the script of kitsu is not removed, also when it holds
// the marker line. It is named for the user to edit.
func TestUninstallLeavesAnotherToolsFileInHooksPath(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	e.git("config", "core.hooksPath", ".githooks")
	team := filepath.Join(e.dir, ".githooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(team), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "#!/bin/sh\n" + marker + "\nsh team-lint.sh || exit 1\n" + Line("pre-commit") + "\n"
	writeProgram(t, team, content)
	removed, foreign, err := Uninstall(t.Context(), e.repo)
	if err != nil || removed != 0 || len(foreign) != 1 || foreign[0].Path != team {
		t.Errorf("Uninstall = %d removed, %v, %v; want none removed and the team's file named", removed, foreign, err)
	}
	if got, _ := os.ReadFile(team); string(got) != content {
		t.Errorf("Uninstall changed the team's hook:\n%s", got)
	}
}

// A hook that only mentions kitsu in a comment does not call it.
func TestACommentDoesNotCallKitsu(t *testing.T) {
	for script, want := range map[string]bool{
		"#!/bin/sh\n# TODO: enable: kitsu hook pre-commit \"$@\"\nexit 0\n": false,
		"#!/bin/sh\n  # kitsu hook pre-commit\n":                            false,
		"#!/bin/sh\nlint || exit 1\nkitsu hook pre-commit \"$@\"\n":         true,
		"#!/bin/sh\nexec kitsu hook pre-push \"$@\"\n":                      false,
		Script("pre-commit"): true,
	} {
		if got := callLine(script, "pre-commit"); (got != "") != want {
			t.Errorf("callLine = %q, want a line: %v, for:\n%s", got, want, script)
		}
	}
}

// The line Uninstall names is the one the file holds: a copy of kitsu's own
// script that was changed since calls kitsu in another form than Line gives.
func TestUninstallNamesTheLineTheFileHolds(t *testing.T) {
	e := newEnv(t, map[string]string{"a.txt": "a\n"})
	e.git("config", "core.hooksPath", ".githooks")
	edited := filepath.Join(e.dir, ".githooks", "pre-push")
	if err := os.MkdirAll(filepath.Dir(edited), 0o755); err != nil {
		t.Fatal(err)
	}
	writeProgram(t, edited, Script("pre-push")+"# a note of the team\n")
	_, foreign, err := Uninstall(t.Context(), e.repo)
	if err != nil || len(foreign) != 1 {
		t.Fatalf("Uninstall = %v, %v; want the changed file named", foreign, err)
	}
	if want := `case "$kitsu" in /*) exec "$kitsu" hook pre-push "$@" ;; esac`; foreign[0].Line != want || !strings.Contains(readFile(t, edited), want) {
		t.Errorf("Uninstall names the line %q, want %q, which the file holds", foreign[0].Line, want)
	}
}

// The hook file runs kitsu only from an entry of PATH that is an absolute path,
// in every shell: bash started as sh, which is the sh of macOS, gives an
// absolute path also for a file it found through an empty or relative entry.
func TestScriptRunsNoKitsuOfAnEmptyOrRelativeEntryOfPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runs the hook file with a POSIX shell and plants an executable script")
	}
	shells := map[string]string{}
	if sh, err := exec.LookPath("sh"); err == nil {
		shells["sh"] = sh
	}
	if bash, err := exec.LookPath("bash"); err == nil {
		shells["bash"] = bash
		shells["bash as sh"] = filepath.Join(t.TempDir(), "sh")
		if err := os.Symlink(bash, shells["bash as sh"]); err != nil {
			t.Fatal(err)
		}
	}
	work := t.TempDir()
	planted := filepath.Join(work, "planted-ran")
	for _, dir := range []string{work, filepath.Join(work, "rel")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeProgram(t, filepath.Join(dir, "kitsu"), "#!/bin/sh\necho ran > '"+planted+"'\n")
	}
	installed := t.TempDir()
	ran := filepath.Join(installed, "installed-ran")
	writeProgram(t, filepath.Join(installed, "kitsu"), "#!/bin/sh\necho \"$@\" > '"+ran+"'\n")
	hook := filepath.Join(t.TempDir(), "pre-commit")
	writeProgram(t, hook, Script("pre-commit"))
	run := func(shell, path string) (int, string) {
		command := exec.Command(shell, hook, "an argument")
		command.Dir, command.Env = work, []string{"PATH=" + path}
		output, err := command.CombinedOutput()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Fatal(err)
		}
		return command.ProcessState.ExitCode(), string(output)
	}
	for label, shell := range shells {
		for _, path := range []string{"", ":", ".", "rel", ":/nowhere", ".:/nowhere", "rel:/nowhere", "/nowhere:"} {
			code, output := run(shell, path)
			if code != 3 || !strings.Contains(output, "absolute path") || !strings.Contains(output, InstallURL) {
				t.Errorf("%s, PATH=%q: exit status %d, want 3 and the reason:\n%s", label, path, code, output)
			}
			if exists(planted) {
				t.Fatalf("%s, PATH=%q: the hook ran a kitsu that no absolute entry of PATH holds", label, path)
			}
		}
		for _, path := range []string{installed, ":" + installed, ".:rel:" + installed} {
			if err := os.RemoveAll(ran); err != nil {
				t.Fatal(err)
			}
			code, output := run(shell, path)
			if code != 0 || strings.TrimSpace(readFile(t, ran)) != "hook pre-commit an argument" || exists(planted) {
				t.Errorf("%s, PATH=%q: exit status %d; want the installed kitsu run with `hook pre-commit an argument` and no other:\n%s", label, path, code, output)
			}
		}
	}
}
