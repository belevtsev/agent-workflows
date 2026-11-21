package harness_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// plainTask verifies one staged file under a task that may commit, and leaves
// the commit to the caller.
func plainTask(t *testing.T, r *kitsutest.Repo, id, file string, more ...string) *turns {
	t.Helper()
	r.Write(file, file+"\n")
	r.Git("add", file)
	m := &turns{t: t, repo: r, id: id}
	m.do(0, append(more, "--commit", "--request", "Track "+file, "--expect", file+" is tracked", "--", "git", "ls-files", "--error-unmatch", "--", file)...)
	return m
}

func plainRepo(t *testing.T) *kitsutest.Repo {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Git("commit", "--allow-empty", "-qm", "Base")
	return r
}

func receiptOf(t *testing.T, r *kitsutest.Repo, id string) string {
	t.Helper()
	return readReceiptMarkdown(flow{t: t, repo: r, id: id})
}

const precededCommit = "Verification preceded the observed commit with the matching tree and parents."

// A plain git commit of the verified candidate is the delivery. The receipt
// names it without another Kitsu command, also after the branch moved on, and
// the next task command records it.
func TestPlainGitCommitOfTheVerifiedCandidateIsTheDelivery(t *testing.T) {
	r := plainRepo(t)
	m := plainTask(t, r, "plain", "a.txt")
	m.want("commit")
	r.Git("commit", "-qm", "Add a")
	delivered := r.Git("rev-parse", "HEAD")
	markdownContains(t, receiptOf(t, r, m.id), "Verified commit `"+delivered+"`", precededCommit)

	later := plainTask(t, r, "later", "b.txt")
	r.Git("commit", "-qm", "Add b")
	markdownContains(t, receiptOf(t, r, m.id), "Verified commit `"+delivered+"`", precededCommit)

	later.do(0)
	later.want("done")
	if show := r.Kitsu("task", "next", later.id, "--brief"); !strings.Contains(show.Stdout, `"commit_evidence":"commit_matches_verified_tree"`) ||
		!strings.Contains(show.Stdout, r.Git("rev-parse", "HEAD")) {
		t.Fatalf("the next command did not record the plain commit: %s", show.Output())
	}
	if r.Git("rev-list", "--count", "HEAD") != "3" {
		t.Fatal("recording the delivery created another commit")
	}
}

// Only the verified tree on the verified parents counts. Another tree or
// another parent is a commit that still has to be verified as it is.
func TestPlainGitCommitOfAnotherCandidateIsNotTheVerifiedDelivery(t *testing.T) {
	for name, change := range map[string]func(r *kitsutest.Repo){
		"another tree": func(r *kitsutest.Repo) {
			r.Write("extra.txt", "unverified\n")
			r.Git("add", "extra.txt")
		},
		"another parent": func(r *kitsutest.Repo) { r.Git("commit", "-q", "--allow-empty", "--only", "-m", "Between") },
	} {
		t.Run(name, func(t *testing.T) {
			r := plainRepo(t)
			m := plainTask(t, r, "other", "a.txt")
			change(r)
			r.Git("commit", "-qm", "Add a")
			markdownOmits(t, receiptOf(t, r, m.id), "Verified commit", precededCommit)
			if next := r.Kitsu("task", "next", m.id, "--brief"); !strings.Contains(next.Stdout, `"next":"verify"`) || strings.Contains(next.Stdout, `"verified":true`) {
				t.Fatalf("an unverified commit counted as the delivery: %s", next.Output())
			}
			// The saved assertion runs again on the commit that exists.
			m.do(0)
			m.want("done")
			markdownContains(t, receiptOf(t, r, m.id), "Verified commit `"+r.Git("rev-parse", "HEAD")+"`", "The commit was already checked out when verification ran.")
		})
	}
}

// The task says where its commit goes. The verified tree on another branch is
// not that delivery until the authorized branch holds it.
func TestPlainGitCommitOnAnotherBranchIsNotTheAuthorizedDelivery(t *testing.T) {
	r := plainRepo(t)
	target := r.Git("symbolic-ref", "--short", "HEAD")
	m := plainTask(t, r, "branch", "a.txt")
	r.Git("checkout", "-qb", "feature")
	r.Git("commit", "-qm", "Add a")
	markdownOmits(t, receiptOf(t, r, m.id), "Verified commit")
	if next := r.Kitsu("task", "next", m.id, "--brief"); !strings.Contains(next.Stdout, `"next":"blocked"`) || !strings.Contains(next.Stdout, "Return to commit target refs/heads/"+target) {
		t.Fatalf("a commit on another branch counted as the delivery: %s", next.Output())
	}
	r.Git("checkout", "-q", target)
	r.Git("merge", "-q", "--ff-only", "feature")
	m.do(0)
	m.want("done")
	markdownContains(t, receiptOf(t, r, m.id), "Verified commit `"+r.Git("rev-parse", "HEAD")+"`", precededCommit)
}

// A hook that refuses Kitsu's commit leaves nothing to recover. The commit that
// follows is judged like any other: the same tree is the delivery, and another
// tree is verified as it is.
func TestPlainGitCommitAfterAHookRefusedTheCommitOfKitsu(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable hook")
	}
	for name, reformat := range map[string]bool{"the same tree": false, "another tree": true} {
		t.Run(name, func(t *testing.T) {
			r := plainRepo(t)
			r.Write(".git/hooks/commit-msg", "#!/bin/sh\ngrep -q TICKET \"$1\" || { echo 'the message needs a TICKET' >&2; exit 1; }\n")
			if err := os.Chmod(r.Path(".git/hooks/commit-msg"), 0o755); err != nil {
				t.Fatal(err)
			}
			m := plainTask(t, r, "hooked", "a.txt")
			refused := r.Kitsu("task", "do", m.id, "--message", "Add a", "--include", "a.txt")
			if refused.Code != 1 || !strings.Contains(refused.Stderr, "the message needs a TICKET") {
				t.Fatalf("the hook's refusal was not reported: %s", refused.Output())
			}
			m.do(0)
			m.want("commit")
			if reformat {
				r.Write("a.txt", "a.txt formatted\n")
				r.Git("add", "a.txt")
			}
			r.Git("commit", "-qm", "TICKET-1 Add a")
			m.do(0)
			m.want("done")
			evidence := precededCommit
			if reformat {
				evidence = "The commit was already checked out when verification ran."
			}
			markdownContains(t, receiptOf(t, r, m.id), "Verified commit `"+r.Git("rev-parse", "HEAD")+"`", evidence)
		})
	}
}

func TestPlainGitCommitDeliversAReviewedTaskAndABugTicket(t *testing.T) {
	r := plainRepo(t)
	review := []string{"--require-review", "--actor", "coordinator", "--claim-review", "claude"}
	m := plainTask(t, r, "reviewed", "a.txt", review...)
	m.want("review")
	sendNativeReviewEvent(t, r, "claude", "SubagentStart", "coordinator", "worker", "")
	finishNativeFixture(t, r, m.id, m.ticket, "claude", "coordinator", "worker", "Verified the tracked file.")
	m.do(0, append(review, "--expect", "a.txt is tracked", "--", "git", "ls-files", "--error-unmatch", "--", "a.txt")...)
	m.want("commit")
	r.Git("commit", "-qm", "Add a")
	markdownContains(t, receiptOf(t, r, m.id), "Verified commit `"+r.Git("rev-parse", "HEAD")+"`", "Host-observed review (Claude Code) has no open findings.")
	m.do(0)
	m.want("done")

	r.Write("output.txt", "broken\n")
	r.Git("add", "output.txt")
	r.Git("commit", "-qm", "Baseline")
	source := filepath.Join(t.TempDir(), "export-bug.md")
	if err := os.WriteFile(source, []byte("# Export is broken\nReturn ready.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bug := &turns{t: t, repo: r, id: "bug"}
	bug.do(5, "--from", source, "--expect", "output is ready", "--", "git", "grep", "-q", "-e", "^ready$", "--", "output.txt")
	r.Write("output.txt", "ready\n")
	r.Git("add", "output.txt")
	bug.do(0)
	bug.want("commit")
	r.Git("commit", "-qm", "Fix export")
	bug.do(0)
	bug.want("done")
	if receipt := r.Kitsu("task", "receipt", bug.id, "--json"); receipt.Code != 0 || !strings.Contains(receipt.Stdout, r.Git("rev-parse", "HEAD")) {
		t.Fatalf("the bug delivery has no receipt for the plain commit:\n%s", receipt.Output())
	}
}
