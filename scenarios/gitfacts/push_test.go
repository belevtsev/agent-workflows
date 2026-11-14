package gitfacts_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// pushing is a repository with a bare remote origin that holds main, and a
// pre-push hook that logs its first argument and each line it reads. The
// hook uses only sh builtins.
func pushing(t *testing.T) (*cwtest.Repo, *hookLog) {
	t.Helper()
	repo := withFile(t)
	remote := filepath.Join(filepath.Dir(repo.Dir), "origin.git")
	repo.Git("init", "-q", "--bare", "-b", "main", remote)
	repo.Git("remote", "add", "origin", remote)
	repo.Git("push", "-q", "origin", "main")
	log := newHookLog(t)
	installHook(t, repo, "pre-push",
		"echo \"args $1\" >> "+log.sh()+"\n"+
			"while read -r lref lsha rref rsha; do echo \"$lref $lsha $rref $rsha\" >> "+log.sh()+"; done\n")
	return repo, log
}

// Sources, first script: "`pre-push` receives one line per ref with both
// commit IDs; a delete has the local ID all zeros; a new ref has the remote
// ID all zeros."
func TestPrePushGetsOneLinePerRef(t *testing.T) {
	t.Parallel()
	repo, log := pushing(t)
	old := repo.Git("rev-parse", "HEAD")
	zero := strings.Repeat("0", len(old))
	commitFile(repo, "g.txt", "g\n", "more")
	head := repo.Git("rev-parse", "HEAD")
	repo.Git("branch", "side")

	repo.Git("push", "-q", "origin", "main", "side")
	want := []string{
		"args origin",
		"refs/heads/main " + head + " refs/heads/main " + old,
		"refs/heads/side " + head + " refs/heads/side " + zero,
	}
	if got := log.take(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("pre-push read, for an update and a new branch:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	repo.Git("push", "-q", "origin", "--delete", "side")
	want = []string{"args origin", "(delete) " + zero + " refs/heads/side " + head}
	if got := log.take(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("pre-push read, for a delete:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Sources, first script: "It runs for `--dry-run` too." Known limits: git
// "gives the hook no sign of it": the hook reads the same as for the push.
func TestPrePushRunsForADryRunWithoutASign(t *testing.T) {
	t.Parallel()
	repo, log := pushing(t)
	old := repo.Git("rev-parse", "HEAD")
	commitFile(repo, "g.txt", "g\n", "more")

	repo.Git("push", "-q", "--dry-run", "origin", "main")
	dry := log.take()
	if got := repo.Git("ls-remote", "origin", "refs/heads/main"); !strings.HasPrefix(got, old) {
		t.Fatalf("the dry run moved origin/main: %s", got)
	}
	repo.Git("push", "-q", "origin", "main")
	pushed := log.take()
	if len(dry) == 0 {
		t.Fatalf("pre-push did not run for git push --dry-run")
	}
	if strings.Join(dry, "\n") != strings.Join(pushed, "\n") {
		t.Errorf("pre-push read for the dry run:\n%s\nand for the push:\n%s",
			strings.Join(dry, "\n"), strings.Join(pushed, "\n"))
	}
}
