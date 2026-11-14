package gitfacts_test

import (
	"strings"
	"testing"
)

// Sources, first script: "A `pre-merge-commit` hook that exits non-zero
// leaves the merge in progress ("Not committing merge; use 'git commit' to
// complete the merge"), and `git commit` then concludes it and runs
// `pre-commit`."
func TestADeniedMergeIsConcludedByGitCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	repo.Git("checkout", "-q", "-b", "m1")
	commitFile(repo, "m1.txt", "m1\n", "m1")
	repo.Git("checkout", "-q", "main")
	commitFile(repo, "m0.txt", "m0\n", "m0")
	log := newHookLog(t)
	logTreeHook(t, repo, "pre-commit", log)
	installHook(t, repo, "pre-merge-commit",
		"echo \"pre-merge-commit $(git write-tree)\" >> "+log.sh()+"\n"+
			"echo 'denied (test)' >&2\nexit 3\n")

	merge := repo.TryGit("merge", "--no-edit", "m1")
	if merge.Code == 0 {
		t.Fatalf("git merge exited 0 although pre-merge-commit denied\n%s", merge.Output())
	}
	if !strings.Contains(merge.Output(), "Not committing merge; use 'git commit' to complete the merge") {
		t.Errorf("git merge did not say how to complete the merge:\n%s", merge.Output())
	}
	if !exists(gitPath(repo, "MERGE_HEAD")) {
		t.Errorf("MERGE_HEAD is missing: the merge is not in progress")
	}
	if n := parents(repo); n != 1 {
		t.Errorf("HEAD has %d parents after the denial, want 1", n)
	}
	if got := log.take(); len(got) != 1 || !strings.HasPrefix(got[0], "pre-merge-commit ") {
		t.Errorf("hook runs during git merge: %q, want one run of pre-merge-commit", got)
	}

	repo.Git("commit", "-q", "--no-edit")
	if n := parents(repo); n != 2 {
		t.Errorf("HEAD has %d parents after git commit, want 2", n)
	}
	got := log.take()
	if len(got) != 1 || !strings.HasPrefix(got[0], "pre-commit ") {
		t.Errorf("hook runs during git commit: %q, want one run of pre-commit", got)
	} else if tree := strings.Fields(got[0])[1]; tree != treeOf(repo, "HEAD") {
		t.Errorf("pre-commit saw %s, the merge commit recorded %s", tree, treeOf(repo, "HEAD"))
	}
}
