// Package pushgate_test holds the scenario tests of the specification
// publish-gate
// (openspec/changes/reduce-cw-to-evidence-gate/specs/publish-gate/spec.md)
// for the requirements "Push rule", "Updates that add nothing", "Linked
// worktrees" and "Removal".
package pushgate_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// gated is the shared fixture of publish-gate: the demo repository with a
// bare remote origin whose main is at the first commit of demo, and with the
// hooks that `cw init` installs. It returns the repository and the remote.
func gated(t *testing.T) (*cwtest.Repo, *cwtest.Repo) {
	t.Helper()
	repo, origin := withOrigin(t)
	mustCW(t, repo, 0, "init")
	return repo, origin
}

// withOrigin is the demo repository with the bare remote origin, before
// `cw init`.
func withOrigin(t *testing.T) (*cwtest.Repo, *cwtest.Repo) {
	t.Helper()
	repo := cwtest.Demo(t)
	bare := filepath.Join(filepath.Dir(repo.Dir), "origin.git")
	repo.Git("init", "-q", "--bare", "-b", "main", bare)
	repo.Git("remote", "add", "origin", bare)
	repo.Git("push", "-q", "origin", "main")
	return repo, repo.In(bare)
}

// mustCW runs cw and fails the test when it does not exit with code.
func mustCW(t *testing.T, repo *cwtest.Repo, code int, args ...string) cwtest.Result {
	t.Helper()
	result := repo.CW(args...)
	if result.Code != code {
		t.Fatalf("cw %s: exit status %d, want %d\n%s", strings.Join(args, " "), result.Code, code, result.Output())
	}
	return result
}

// checked runs cw run and fails the test unless both checks end passed.
func checked(t *testing.T, repo *cwtest.Repo) {
	t.Helper()
	mustCW(t, repo, 0, "run")
}

// refOn is the commit a ref points at in a repository, or "" when the ref
// does not exist.
func refOn(repo *cwtest.Repo, ref string) string {
	result := repo.TryGit("rev-parse", "--verify", "-q", ref+"^{commit}")
	if result.Code != 0 {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// commitAll commits everything in the working tree through the hooks and
// fails the test when the commit is not recorded.
func commitAll(t *testing.T, repo *cwtest.Repo, message string) string {
	t.Helper()
	repo.Git("add", "-A")
	result := repo.TryGit("commit", "-q", "-m", message)
	if result.Code != 0 {
		t.Fatalf("git commit -m %s: exit status %d\n%s", message, result.Code, result.Output())
	}
	return refOn(repo, "HEAD")
}

// commitAround commits everything in the working tree with --no-verify.
func commitAround(repo *cwtest.Repo, message string) string {
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "--no-verify", "-m", message)
	return refOn(repo, "HEAD")
}

// assertSent fails the test unless git push exited 0 and each ref on the
// remote now points at the commit given for it ("" for a deleted ref).
func assertSent(t *testing.T, push cwtest.Result, origin *cwtest.Repo, want map[string]string) {
	t.Helper()
	if push.Code != 0 {
		t.Errorf("git push exited %d, want 0\n%s", push.Code, push.Output())
	}
	for ref, commit := range want {
		if got := refOn(origin, ref); got != commit {
			t.Errorf("%s on origin is %q, want %q", ref, got, commit)
		}
	}
}

// assertDenied fails the test unless git push failed, each ref on the remote
// still points where it did before ("" for a ref that did not exist), and
// standard error holds a reason. It returns standard error.
func assertDenied(t *testing.T, push cwtest.Result, origin *cwtest.Repo, before map[string]string) string {
	t.Helper()
	if push.Code == 0 {
		t.Errorf("git push exited 0, want a denial\n%s", push.Output())
	}
	for ref, commit := range before {
		if got := refOn(origin, ref); got != commit {
			t.Errorf("%s on origin moved from %q to %q", ref, commit, got)
		}
	}
	if strings.TrimSpace(push.Stderr) == "" {
		t.Errorf("the denial printed nothing on standard error")
	}
	return push.Stderr
}

// holdsWord says whether text holds word as a word of its own.
func holdsWord(text, word string) bool {
	return regexp.MustCompile(`(^|[^\w./-])` + regexp.QuoteMeta(word) + `($|[^\w./-])`).MatchString(text)
}

// sha256Of is the SHA-256 of a file.
func sha256Of(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// exists says whether a path exists.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// hooksDir is the hooks directory of the common git directory.
func hooksDir(t *testing.T, repo *cwtest.Repo) string {
	t.Helper()
	return filepath.Join(filepath.Dir(repo.StoreDir()), "hooks")
}
