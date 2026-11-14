package gate

import (
	"os"
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
	if got := Line("pre-commit"); got != `cw hook pre-commit "$@"` {
		t.Errorf("Line = %q", got)
	}
}

func TestScript(t *testing.T) {
	for _, name := range Hooks {
		script := Script(name)
		if !strings.HasPrefix(script, "#!/bin/sh\n") {
			t.Errorf("%s does not begin with #!/bin/sh", name)
		}
		for _, want := range []string{marker, Line(name), "exit 3", InstallURL, "command -v cw", "not found"} {
			if !strings.Contains(script, want) {
				t.Errorf("the %s script lacks %q:\n%s", name, want, script)
			}
		}
		if !strings.Contains(script, "exec "+Line(name)+"\n") {
			t.Errorf("the %s script does not end by running cw hook:\n%s", name, script)
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
	if err := os.WriteFile(path, []byte(other), 0o755); err != nil {
		t.Fatal(err)
	}
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
		t.Error("State with a foreign pre-commit that does not call cw is not missing")
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
	// A hook of another tool that calls cw counts.
	path := filepath.Join(e.repo.CommonDir, "hooks", "pre-commit")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nformat\n"+Line("pre-commit")+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if State(t.Context(), e.repo) != Installed {
		t.Error("State with a shared hook that calls cw hook is not installed")
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
		if err := os.WriteFile(filepath.Join(e.dir, ".githooks", name), []byte(Script(name)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if State(t.Context(), e.repo) != Installed {
		t.Error("State with cw hooks in core.hooksPath is not installed")
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
	if err := os.WriteFile(sharedPath, []byte(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "commit-msg")
	if err := os.WriteFile(unrelated, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign, err := Uninstall(t.Context(), e.repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Foreign{{Name: "pre-commit", Path: sharedPath}}; !slices.Equal(foreign, want) {
		t.Errorf("foreign = %v, want %v", foreign, want)
	}
	if readFile(t, sharedPath) != shared {
		t.Error("Uninstall changed a hook it did not write")
	}
	if !exists(unrelated) {
		t.Error("Uninstall removed a hook that is not cw's")
	}
	for _, name := range []string{"pre-merge-commit", "pre-push"} {
		if exists(filepath.Join(dir, name)) {
			t.Errorf("Uninstall left %s", name)
		}
	}
	if foreign, err := Uninstall(t.Context(), e.repo); err != nil || len(foreign) != 1 {
		t.Errorf("a second Uninstall = %v, %v", foreign, err)
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
	if err := os.WriteFile(own, []byte(Script("pre-push")), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(t.Context(), e.repo); err != nil {
		t.Fatal(err)
	}
	if exists(own) {
		t.Error("Uninstall left cw's own file in core.hooksPath")
	}
	for _, path := range hookPaths(filepath.Join(e.repo.CommonDir, "hooks")) {
		if exists(path) {
			t.Errorf("Uninstall left %s in the common hooks directory", path)
		}
	}
}
