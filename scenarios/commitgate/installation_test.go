package commitgate_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// hooksDir is the hooks directory of the repository's common git directory.
func hooksDir(t *testing.T, repo *kitsutest.Repo) string {
	t.Helper()
	common := repo.Git("rev-parse", "--git-common-dir")
	if !filepath.IsAbs(common) {
		common = filepath.Join(repo.Dir, common)
	}
	return filepath.Join(filepath.Clean(common), "hooks")
}

// listDir is the sorted list of the names in a directory; an absent
// directory lists nothing.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// fileSum is the SHA-256 of a file.
func fileSum(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Requirement "Hook installation", scenario "Hooks are installed once".
func TestHooksAreInstalledOnce(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	dir := hooksDir(t, repo)
	for _, name := range hookNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("the fixture already has a hook %s", name)
		}
	}

	if result := repo.Kitsu("init"); result.Code != 0 {
		t.Fatalf("the first kitsu init exited %d:\n%s", result.Code, result.Output())
	}
	first := map[string]string{}
	for _, name := range hookNames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("the hook %s is missing after kitsu init: %v", name, err)
			continue
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
			t.Errorf("the hook %s is not executable: %v", name, info.Mode())
		}
		first[name] = fileSum(t, path) + " " + info.Mode().String()
	}

	if result := repo.Kitsu("init"); result.Code != 0 {
		t.Fatalf("the second kitsu init exited %d:\n%s", result.Code, result.Output())
	}
	for _, name := range hookNames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("the hook %s is missing after the second kitsu init: %v", name, err)
			continue
		}
		if got := fileSum(t, path) + " " + info.Mode().String(); got != first[name] {
			t.Errorf("the hook %s changed with the second kitsu init: %s, then %s", name, first[name], got)
		}
	}
}

// Requirement "Hooks of other tools", scenario "An existing pre-commit hook".
func TestExistingPreCommitHookIsLeftAlone(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	dir := hooksDir(t, repo)
	own := filepath.Join(dir, "pre-commit")
	writeProgram(t, own, "#!/bin/sh\n# written by another tool\nexit 0\n")
	before := fileSum(t, own)

	result := repo.Kitsu("init")
	if result.Code != 4 {
		t.Errorf("kitsu init exited %d, want 4", result.Code)
	}
	if got := fileSum(t, own); got != before {
		t.Errorf("the SHA-256 of pre-commit changed from %s to %s", before, got)
	}
	for _, name := range []string{"pre-merge-commit", "pre-push"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("the hook %s is not installed: %v", name, err)
		}
	}
	if !holdsLine(result.Output(), `kitsu hook pre-commit "$@"`) {
		t.Errorf("the output does not hold the line `kitsu hook pre-commit \"$@\"`:\n%s", result.Output())
	}
	if !strings.Contains(result.Output(), "last") {
		t.Errorf("the output does not say that the line must come last:\n%s", result.Output())
	}
}

// holdsLine says whether one line of text is the given line, apart from the
// space around it.
func holdsLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// Requirement "Hooks of other tools", scenario "A configured hooks directory
// is left to its owner".
func TestConfiguredHooksDirectoryIsLeftToItsOwner(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	repo.Git("config", "core.hooksPath", ".githooks")
	dir := hooksDir(t, repo)
	before := listDir(t, dir)

	result := repo.Kitsu("init")
	if result.Code != 4 {
		t.Errorf("kitsu init exited %d, want 4", result.Code)
	}
	if names := listDir(t, repo.Path(".githooks")); len(names) > 0 {
		t.Errorf("kitsu init created files in .githooks/: %v", names)
	}
	if after := listDir(t, dir); strings.Join(after, " ") != strings.Join(before, " ") {
		t.Errorf("the hooks directory of the git directory changed from %v to %v", before, after)
	}
	for _, name := range hookNames {
		count := 0
		for _, line := range strings.Split(result.Output(), "\n") {
			if strings.Contains(line, "kitsu hook "+name+" ") || strings.HasSuffix(strings.TrimSpace(line), "kitsu hook "+name) {
				count++
			}
		}
		if count != 1 {
			t.Errorf("the output holds %d `kitsu hook %s` lines, want 1:\n%s", count, name, result.Output())
		}
	}
}

// Requirement "Hooks of other tools", scenario "A formatter that runs before
// `kitsu` in a shared hook".
func TestFormatterBeforeKitsuInSharedHook(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the formatter hook is a POSIX shell script")
	}
	// The fixture without kitsu init: the formatter's hook comes first.
	repo := kitsutest.Demo(t)
	g := &gate{Repo: repo, t: t}
	g.wrapKitsu()
	formatter := "#!/bin/sh\n" +
		"# another tool's pre-commit: it normalises src/app.txt\n" +
		"printf 'ok\\n\\n' > src/app.txt\n" +
		"git add src/app.txt\n" +
		"kitsu hook pre-commit \"$@\"\n"
	writeProgram(t, filepath.Join(hooksDir(t, repo), "pre-commit"), formatter)
	repo.Kitsu("init")

	repo.Write("README.md", "# demo\n\nchecked\n")
	g.checked()
	result := g.commitDenied("pre-commit", "commit", "-a", "-m", "change")
	if repo.Read("src/app.txt") != "ok\n\n" {
		t.Fatalf("the formatter did not rewrite src/app.txt:\n%s", result.Output())
	}

	g.mustRun(0)
	g.commitRecorded("commit", "-a", "-m", "change")
}
