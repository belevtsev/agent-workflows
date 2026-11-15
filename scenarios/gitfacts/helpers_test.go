// Package gitfacts_test holds the git behaviour that the design of gatecub relies
// on, as tests. Each test names the observation it holds, from the proposal
// (openspec/changes/reduce-cw-to-evidence-gate/proposal.md), sections
// "Sources" and "Known limits". The observations were first made by hand on
// Linux; these tests make them on every system CI runs. They drive git only.
package gitfacts_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// newRepo is an empty repository on main whose editor does nothing, so that
// a continued cherry-pick, merge or revert never waits for input.
func newRepo(t *testing.T) *gatecubtest.Repo {
	t.Helper()
	repo := gatecubtest.NewRepo(t)
	repo.Setenv("GIT_EDITOR", ":")
	return repo
}

// withFile is newRepo with f.txt holding "a" in a first commit.
func withFile(t *testing.T) *gatecubtest.Repo {
	t.Helper()
	repo := newRepo(t)
	commitFile(repo, "f.txt", "a\n", "init")
	return repo
}

// commitFile writes a file, stages it and commits it without the hooks.
func commitFile(repo *gatecubtest.Repo, rel, content, message string) {
	repo.Write(rel, content)
	repo.Git("add", "--", rel)
	repo.Git("commit", "-q", "--no-verify", "-m", message)
}

// treeOf is the tree of a commit.
func treeOf(repo *gatecubtest.Repo, rev string) string {
	return repo.Git("rev-parse", rev+"^{tree}")
}

// parents is the number of parents of HEAD.
func parents(repo *gatecubtest.Repo) int {
	return len(strings.Fields(repo.Git("rev-list", "--parents", "-1", "HEAD"))) - 1
}

// gitPath is the absolute path of a file in the git directory, as
// `git rev-parse --git-path` names it.
func gitPath(repo *gatecubtest.Repo, name string) string {
	path := repo.Git("rev-parse", "--git-path", name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo.Dir, path)
	}
	return filepath.Clean(path)
}

// exists says whether a path exists.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// runEnv runs git in the repository with extra variables.
func runEnv(t *testing.T, repo *gatecubtest.Repo, extra []string, args ...string) gatecubtest.Result {
	t.Helper()
	command := repo.Command("git", args...)
	command.Env = append(command.Env, extra...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	result := gatecubtest.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		result.Code = exit.ExitCode()
	default:
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return result
}

// snapshot is the tree that `git add -A` would stage, made in a copy of the
// index so that the real index is not touched. This is how the design takes
// its snapshot. When `git add -A` fails, the tree is empty and the result
// is that of `git add -A`.
func snapshot(t *testing.T, repo *gatecubtest.Repo) (string, gatecubtest.Result) {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "index")
	data, err := os.ReadFile(gitPath(repo, "index"))
	switch {
	case err == nil:
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			t.Fatal(err)
		}
	case !os.IsNotExist(err):
		t.Fatal(err)
	}
	env := []string{"GIT_INDEX_FILE=" + tmp}
	if add := runEnv(t, repo, env, "add", "-A"); add.Code != 0 {
		return "", add
	}
	tree := runEnv(t, repo, env, "write-tree")
	if tree.Code != 0 {
		t.Fatalf("git write-tree: exit status %d\n%s", tree.Code, tree.Output())
	}
	return strings.TrimSpace(tree.Stdout), tree
}

// mustSnapshot is snapshot that fails the test when `git add -A` fails.
func mustSnapshot(t *testing.T, repo *gatecubtest.Repo) string {
	t.Helper()
	tree, result := snapshot(t, repo)
	if tree == "" {
		t.Fatalf("git add -A in a copy of the index: exit status %d\n%s", result.Code, result.Output())
	}
	return tree
}

// hookLog is a file that hooks append lines to.
type hookLog struct {
	t    *testing.T
	path string
}

// newHookLog creates an empty log outside the repository.
func newHookLog(t *testing.T) *hookLog {
	t.Helper()
	log := &hookLog{t: t, path: filepath.Join(t.TempDir(), "hook.log")}
	log.take()
	return log
}

// sh is the path of the log as a quoted word of a sh script. Git for
// Windows' sh reads C:/x as a path; a backslash would need escaping.
func (l *hookLog) sh() string { return shQuote(l.path) }

// take returns the lines logged so far and empties the log.
func (l *hookLog) take() []string {
	l.t.Helper()
	data, err := os.ReadFile(l.path)
	if err != nil && !os.IsNotExist(err) {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(l.path, nil, 0o644); err != nil {
		l.t.Fatal(err)
	}
	text := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// runs is the number of lines logged so far; it empties the log.
func (l *hookLog) runs() int { return len(l.take()) }

// shQuote quotes a path for a sh script, with forward slashes.
func shQuote(path string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", `'\''`) + "'"
}

// writeHook writes an executable sh script. Git for Windows runs a hook
// that starts with #!, whatever its mode.
func writeHook(t *testing.T, path, body string) {
	t.Helper()
	writeProgram(t, path, "#!/bin/sh\n"+body)
}

// installHook writes a hook into the hooks directory of the repository.
func installHook(t *testing.T, repo *gatecubtest.Repo, name, body string) {
	t.Helper()
	writeHook(t, gitPath(repo, "hooks/"+name), body)
}

// logTreeHook installs a hook that logs its name and the tree that
// `git write-tree` gives inside it, and exits 0.
func logTreeHook(t *testing.T, repo *gatecubtest.Repo, name string, log *hookLog) {
	t.Helper()
	installHook(t, repo, name, "echo \""+name+" $(git write-tree)\" >> "+log.sh()+"\n")
}
