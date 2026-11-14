package gitfacts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// lastTree is the tree logged by the last hook run, after its name.
func lastTree(t *testing.T, log *hookLog) string {
	t.Helper()
	lines := log.take()
	if len(lines) == 0 {
		t.Fatalf("the hook did not run")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) != 2 {
		t.Fatalf("bad hook log line %q", lines[len(lines)-1])
	}
	return fields[1]
}

// Sources, first script: "Inside `pre-commit`, `git write-tree` gives the
// tree the commit records, for a plain commit, `git commit -a` and
// `git commit <path>`."
func TestPreCommitSeesTheTreeTheCommitRecords(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	commitFile(repo, "new.txt", "n\n", "new")
	log := newHookLog(t)
	logTreeHook(t, repo, "pre-commit", log)

	repo.Write("f.txt", "plain\n")
	repo.Git("add", "f.txt")
	repo.Git("commit", "-q", "-m", "plain")
	if got, want := lastTree(t, log), treeOf(repo, "HEAD"); got != want {
		t.Errorf("git commit: the hook saw %s, the commit recorded %s", got, want)
	}

	repo.Write("f.txt", "all\n")
	repo.Write("untracked.txt", "u\n")
	repo.Git("commit", "-q", "-a", "-m", "all")
	if got, want := lastTree(t, log), treeOf(repo, "HEAD"); got != want {
		t.Errorf("git commit -a: the hook saw %s, the commit recorded %s", got, want)
	}

	repo.Write("f.txt", "path\n")
	repo.Write("new.txt", "pending\n")
	repo.Git("commit", "-q", "-m", "path", "f.txt")
	if got, want := lastTree(t, log), treeOf(repo, "HEAD"); got != want {
		t.Errorf("git commit f.txt: the hook saw %s, the commit recorded %s", got, want)
	}
	if got := repo.Git("show", "HEAD:new.txt"); got != "n" {
		t.Errorf("git commit f.txt recorded new.txt as %q, want the committed %q", got, "n")
	}
}

// Sources, first script: "`pre-commit` ran for `git commit`, `--amend` and
// `--allow-empty`. It did not run for a clean `git cherry-pick`, a clean
// `git revert`, `git rebase`, a fast-forward merge or `--no-verify`."
// Known limits says the same.
func TestWhichCommandsRunPreCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	commitFile(repo, "g.txt", "g\n", "second")
	log := newHookLog(t)
	logTreeHook(t, repo, "pre-commit", log)
	expect := func(what string, want int) {
		t.Helper()
		if got := log.runs(); got != want {
			t.Errorf("%s: pre-commit ran %d time(s), want %d", what, got, want)
		}
	}

	repo.Write("h.txt", "h\n")
	repo.Git("add", "h.txt")
	repo.Git("commit", "-q", "-m", "plain")
	expect("git commit", 1)
	repo.Git("commit", "-q", "--amend", "-m", "amended")
	expect("git commit --amend", 1)
	repo.Git("commit", "-q", "--allow-empty", "-m", "empty")
	expect("git commit --allow-empty", 1)

	repo.Git("checkout", "-q", "-b", "side", "HEAD~2")
	commitFile(repo, "side.txt", "s\n", "side")
	repo.Git("checkout", "-q", "main")
	log.take()
	repo.Git("cherry-pick", "side")
	expect("a clean git cherry-pick", 0)
	repo.Git("revert", "--no-edit", "HEAD")
	expect("a clean git revert", 0)

	repo.Git("checkout", "-q", "-b", "reb", "side")
	commitFile(repo, "r.txt", "r\n", "r")
	repo.Git("rebase", "-q", "main")
	expect("git rebase", 0)
	repo.Git("checkout", "-q", "main")
	repo.Git("merge", "-q", "--ff-only", "reb")
	expect("a fast-forward git merge", 0)
	repo.Git("commit", "-q", "--no-verify", "--allow-empty", "-m", "nv")
	expect("git commit --no-verify", 0)
}

// Sources, second script: "When a `pre-commit` hook stages a change after
// reading the tree, git records the changed tree, not the one the hook
// read." Known limits: cw must therefore be the last command of a shared
// hook.
func TestAHookThatStagesAfterReadingChangesTheRecordedTree(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	log := newHookLog(t)
	installHook(t, repo, "pre-commit",
		"echo \"pre-commit $(git write-tree)\" >> "+log.sh()+"\n"+
			"printf 'formatted\\n' > f.txt\n"+
			"git add f.txt\n")

	repo.Write("f.txt", "b\n")
	repo.Git("add", "f.txt")
	repo.Git("commit", "-q", "-m", "c")
	judged := lastTree(t, log)
	if recorded := treeOf(repo, "HEAD"); recorded == judged {
		t.Errorf("git recorded the tree the hook read (%s), want the changed one", judged)
	}
	if got := repo.Git("show", "HEAD:f.txt"); got != "formatted" {
		t.Errorf("HEAD:f.txt is %q, want %q", got, "formatted")
	}
}

// Sources, first script: "A hook that calls a program missing from `PATH`
// makes the commit fail."
func TestAHookThatCallsAMissingProgramFailsTheCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	installHook(t, repo, "pre-commit", "exec cw-not-installed-anywhere hook pre-commit \"$@\"\n")
	before := repo.Git("rev-parse", "HEAD")
	if result := repo.TryGit("commit", "-q", "--allow-empty", "-m", "x"); result.Code == 0 {
		t.Errorf("git commit exited 0 with a hook whose program is missing\n%s", result.Output())
	}
	if after := repo.Git("rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s", before, after)
	}
}

// conflicted is a repository whose branches side and main both changed
// f.txt, with a pre-commit hook that logs each run.
func conflicted(t *testing.T) (*cwtest.Repo, *hookLog) {
	t.Helper()
	repo := withFile(t)
	repo.Git("checkout", "-q", "-b", "side")
	commitFile(repo, "f.txt", "side\n", "side")
	repo.Git("checkout", "-q", "main")
	commitFile(repo, "f.txt", "main\n", "main")
	log := newHookLog(t)
	logTreeHook(t, repo, "pre-commit", log)
	return repo, log
}

// mustConflict runs a git command that must stop on a conflict, then
// resolves f.txt and stages it.
func mustConflict(t *testing.T, repo *cwtest.Repo, args ...string) {
	t.Helper()
	if result := repo.TryGit(args...); result.Code == 0 {
		t.Fatalf("git %s did not stop on a conflict\n%s", strings.Join(args, " "), result.Output())
	}
	repo.Write("f.txt", "resolved\n")
	repo.Git("add", "f.txt")
}

// Sources, second script and Known limits: "`pre-commit` ran when a
// conflicted `git cherry-pick`, `git merge` and `git revert` were
// continued, and did not run for `git rebase --continue`."
func TestPreCommitRunsWhenAConflictedOperationIsContinued(t *testing.T) {
	t.Parallel()
	repo, log := conflicted(t)
	expect := func(what string, want int) {
		t.Helper()
		if got := log.runs(); got != want {
			t.Errorf("%s: pre-commit ran %d time(s), want %d", what, got, want)
		}
	}

	mustConflict(t, repo, "cherry-pick", "side")
	repo.Git("cherry-pick", "--continue")
	expect("git cherry-pick --continue", 1)
	repo.Git("reset", "-q", "--hard", "HEAD~1")

	mustConflict(t, repo, "merge", "side")
	repo.Git("merge", "--continue")
	expect("git merge --continue", 1)
	repo.Git("reset", "-q", "--hard", "HEAD~1")

	commitFile(repo, "f.txt", "main2\n", "main2")
	mustConflict(t, repo, "revert", "--no-edit", "HEAD~1")
	repo.Git("revert", "--continue")
	expect("git revert --continue", 1)

	repo.Git("checkout", "-q", "side")
	mustConflict(t, repo, "rebase", "main")
	repo.Git("rebase", "--continue")
	expect("git rebase --continue", 0)
}

// Sources, second script: "A `pre-commit` that denies during
// `git cherry-pick --continue` leaves the cherry-pick in progress, and a
// later `--continue` concludes it."
func TestADeniedCherryPickContinueStaysInProgress(t *testing.T) {
	t.Parallel()
	repo, _ := conflicted(t)
	allow := filepath.Join(t.TempDir(), "allow")
	installHook(t, repo, "pre-commit",
		"test -e "+shQuote(allow)+" || { echo 'denied (test)' >&2; exit 3; }\n")
	pick := gitPath(repo, "CHERRY_PICK_HEAD")

	mustConflict(t, repo, "cherry-pick", "side")
	if result := repo.TryGit("cherry-pick", "--continue"); result.Code == 0 {
		t.Fatalf("git cherry-pick --continue exited 0 although pre-commit denied\n%s", result.Output())
	}
	if !exists(pick) {
		t.Errorf("CHERRY_PICK_HEAD is gone after the denial: the cherry-pick is not in progress")
	}
	if got := repo.Git("log", "-1", "--format=%s"); got != "main" {
		t.Errorf("HEAD is %q after the denial, want the commit main", got)
	}

	if err := os.WriteFile(allow, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git("cherry-pick", "--continue")
	if got := repo.Git("log", "-1", "--format=%s"); got != "side" {
		t.Errorf("HEAD is %q after the second --continue, want the picked commit side", got)
	}
	if exists(pick) {
		t.Errorf("CHERRY_PICK_HEAD is still there after the second --continue")
	}
}
