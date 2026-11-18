package harness_test

import (
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestCommitReceiptCanBeReplayedWhilePublicationIsPending(t *testing.T) {
	for _, mode := range []string{"recorded commit", "interrupted after Git"} {
		t.Run(mode, func(t *testing.T) {
			r := kitsutest.Demo(t)
			r.Git("checkout", "-qb", "delivery")
			r.Git("remote", "add", "fixture", ".") // observe this disposable repository through Git's local transport
			base := r.Git("rev-parse", "HEAD")
			r.Write("answer.txt", "42\n")
			f := flow{t: t, repo: r, id: "commit-and-publish"}
			f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--publish-to", "fixture/delivery",
				"--request", "Verify, commit and confirm the answer")
			f.record("intake", "coordinator", "The answer must be 42")
			r.Git("add", "answer.txt")
			f.call(0, "next")
			f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
			f.want("commit")
			proof, ticket := f.state.Proof.Run, f.state.Ticket
			const message = "Deliver the checked answer"
			interrupted := mode == "interrupted after Git"
			if interrupted {
				// Git has finished, but Kitsu's commit result was never saved.
				r.Git("commit", "-qm", message)
			} else {
				f.call(0, "commit", "--ticket", ticket, "--message", message, "--include", "answer.txt")
			}
			head := r.Git("rev-parse", "HEAD")
			f.call(0, "next")
			f.want("publish")
			publication := f.state.Ticket
			f.call(1, "commit", "--ticket", publication, "--message", message)
			for range 2 {
				f.call(0, "commit", "--ticket", ticket, "--message", message, "--include", "answer.txt")
				f.want("publish")
				if f.state.Delivery != "committed" || !f.state.Verified || f.state.Proof.Run != proof || f.state.Ticket != publication {
					t.Fatal("acknowledgement changed verification or the publication ticket", f.state)
				}
			}
			if r.Git("rev-parse", "HEAD") != head || r.Git("rev-list", "--count", base+"..HEAD") != "1" ||
				r.Git("status", "--porcelain") != "" || len(r.RecordFiles("task-"+f.id)) != 1 {
				t.Fatal("acknowledging the receipt repeated work or changed Git state")
			}
			var history struct {
				Events []struct {
					Action, Note, Actor string
					ObservedCommit      string `json:"observed_commit"`
				}
			}
			result := r.Kitsu("task", "show", f.id, "--json")
			if err := json.Unmarshal([]byte(result.Stdout), &history); result.Code != 0 || err != nil {
				t.Fatal(result.Output(), err)
			}
			if interrupted {
				if len(history.Events) != 2 || history.Events[1].Action != "test" || history.Events[1].ObservedCommit != head ||
					history.Events[1].Actor != "" || !strings.Contains(history.Events[1].Note, proof) {
					t.Fatal("missing Git observation on the original assertion", result.Output())
				}
			} else if len(history.Events) != 3 || history.Events[2].Action != "commit" || history.Events[2].Note != message {
				t.Fatal("changes to the original commit event", result.Output())
			}
			r.Git("checkout", "-qb", "other-work")
			r.Git("commit", "--allow-empty", "-qm", "Unrelated later work")
			f.call(0, "next")
			f.want("blocked")
			if f.state.Ticket != "" {
				t.Fatal("issued a publication ticket for later work", f.state)
			}
			r.Git("checkout", "-q", "delivery")
			f.call(0, "next")
			f.want("publish")
			if f.state.Ticket != publication {
				t.Fatal("lost the publication ticket on return to its candidate", f.state)
			}
			f.call(0, "confirm", "--ticket", publication)
			f.want("done")
		})
	}
}

func TestUnreviewedMergeParentCannotCompleteDelivery(t *testing.T) {
	r := kitsutest.Demo(t)
	base := r.Git("rev-parse", "HEAD")
	tree := r.Git("rev-parse", "HEAD^{tree}")
	side := r.Git("commit-tree", tree, "-p", base, "-m", "Side history")
	f := flow{t: t, repo: r, id: "unrelated-history"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and commit the fix")
	f.record("intake", "host", "Check the unit behavior")
	r.Write("fix.txt", "the verified change\n")
	f.call(0, "next")
	f.call(0, "test", "--expect", "fixture behavior", "--ticket", f.state.Ticket, "--", "sh", "unit.sh")
	f.want("commit")
	r.Git("add", "fix.txt")
	merge := r.Git("commit-tree", r.Git("write-tree"), "-p", base, "-p", side, "-m", "Unexpected merge")
	r.Git("update-ref", "HEAD", merge, base)
	f.call(0, "next")
	f.want("verify")
	if f.state.Verified {
		t.Fatal("accepted a merge with an extra parent as the interrupted commit")
	}
}

func TestPendingMergeMustActuallyBeCommitted(t *testing.T) {
	r := kitsutest.Demo(t)
	base := r.Git("rev-parse", "HEAD")
	tree := r.Git("rev-parse", "HEAD^{tree}")
	side := r.Git("commit-tree", tree, "-p", base, "-m", "Side history")
	r.Git("merge", "--no-ff", "--no-commit", side)
	f := flow{t: t, repo: r, id: "pending-merge"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and finish the existing merge")
	f.record("intake", "host", "Preserve the agreed merge parents")
	f.call(0, "test", "--expect", "fixture behavior", "--ticket", f.state.Ticket, "--", "sh", "unit.sh")
	f.want("commit")
	f.call(0, "commit", "--message", "Finish reviewed merge", "--ticket", f.state.Ticket)
	f.want("done")
	if parents := r.Git("show", "-s", "--format=%P", "HEAD"); parents != base+" "+side {
		t.Fatalf("unexpected parents %s", parents)
	}
}

func prepareHookCommit(t *testing.T) flow {
	t.Helper()
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "hook-changes-index"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and commit the change")
	f.record("intake", "host", "Check the candidate")
	r.Write("change.txt", "before hook\n")
	r.Git("add", "change.txt")
	f.call(0, "next")
	f.call(0, "test", "--expect", "candidate contains the expected text", "--ticket", f.state.Ticket, "--", "git", "grep", "-q", "hook", "--", "change.txt")
	f.want("commit")
	r.Git("config", "core.hooksPath", r.Path(".git/local-hooks"))
	return f
}

func writeCommitHook(t *testing.T, r *kitsutest.Repo, name, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable hook")
	}
	path := ".git/local-hooks/" + name
	r.Write(path, "#!/bin/sh\n"+body)
	if err := os.Chmod(r.Path(path), 0755); err != nil {
		t.Fatal(err)
	}
}

const formatBeforeCommit = "printf 'written by hook\\n' > change.txt\ngit add change.txt\n"

func TestCommitHookTreeRequiresFreshEvidenceAndKeepsTheCommit(t *testing.T) {
	f := prepareHookCommit(t)
	r := f.repo
	proof, ticket := f.state.Proof.Run, f.state.Ticket
	writeCommitHook(t, r, "pre-commit", formatBeforeCommit)
	result := r.Kitsu("task", "commit", f.id, "--ticket", f.state.Ticket, "--message", "Commit verified change", "--brief")
	if result.Code != 1 || !strings.Contains(result.Output(), "changed") {
		t.Fatal("unexpected commit result", result.Output())
	}
	if got := r.Git("show", "HEAD:change.txt"); got != "written by hook" {
		t.Fatal("fixture failed to exercise hook", got)
	}
	head := r.Git("rev-parse", "HEAD")
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Fatal("clean hook commit left changes in the real index", got)
	}
	f.call(0, "next")
	f.want("verify")
	f.call(1, "commit", "--ticket", ticket, "--message", "Do not duplicate the commit")
	if f.state.Verified || f.state.Completion != nil {
		t.Fatal("formatter output inherited stale evidence")
	}
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("done")
	if f.state.Proof.Run == proof || f.state.Completion == nil || f.state.Completion.Candidate.Head != head ||
		r.Git("rev-parse", "HEAD") != head {
		t.Fatal("did not freshly verify the already created commit", f.state)
	}
}

func TestCommitHookPreservesStagingChangedDuringCommit(t *testing.T) {
	f := prepareHookCommit(t)
	r := f.repo
	writeCommitHook(t, r, "pre-commit", formatBeforeCommit+"printf 'unrelated\\n' > preserved.txt\n(unset GIT_INDEX_FILE; git add preserved.txt)\n")
	result := r.Kitsu("task", "commit", f.id, "--ticket", f.state.Ticket, "--message", "Commit verified change")
	if result.Code != 1 || !strings.Contains(result.Output(), "staging was preserved") {
		t.Fatal("did not report concurrent staging", result.Output())
	}
	if r.Git("show", ":preserved.txt") != "unrelated" || r.Git("show", ":change.txt") != "before hook" ||
		r.Git("show", "HEAD:change.txt") != "written by hook" {
		t.Fatal("hook recovery overwrote staging or the created commit")
	}
	f.call(0, "next")
	f.want("blocked")
	if f.state.Ticket != "" || f.state.Verified {
		t.Fatal("issued a ticket covering someone else's staged work", f.state)
	}
}

func TestPostCommitWorkDoesNotReopenCompletedDelivery(t *testing.T) {
	f := prepareHookCommit(t)
	r := f.repo
	writeCommitHook(t, r, "post-commit", "printf 'later work\\n' > change.txt\n")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Commit verified change")
	f.want("done")
	if f.state.Completion == nil || f.state.Completion.Candidate.Head != r.Git("rev-parse", "HEAD") ||
		r.Git("show", "HEAD:change.txt") != "before hook" || !strings.Contains(r.Git("diff", "--", "change.txt"), "later work") {
		t.Fatal("did not preserve the committed tree, receipt and later edits", f.state)
	}
}

func rewriteDeliveryLedger(f flow, edit func(*harness.Task)) {
	f.t.Helper()
	path := f.repo.Path(".git/kitsu-stages/task-harness-" + f.id + ".json")
	data, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	var task harness.Task
	if err := json.Unmarshal(data, &task); err != nil {
		f.t.Fatal(err)
	}
	edit(&task)
	data, err = json.Marshal(task)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		f.t.Fatal(err)
	}
}

func TestLegacyCommitDeliveryStaysComplete(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
			f := prepareHookCommit(t)
			r := f.repo
			f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver verified change")
			tree := r.Git("rev-parse", "HEAD^{tree}")
			rewriteDeliveryLedger(f, func(task *harness.Task) {
				task.Version = version
				task.Completion, task.CommitAttempt = nil, nil
				task.Latest("commit").ObservedCommit = ""
			})
			r.Write("change.txt", "later work\n")
			r.Git("add", "change.txt")
			r.Git("commit", "-qm", "Unrelated later work")
			f.call(0, "next")
			f.want("done")
			got := f.state.Completion
			if got == nil || got.Candidate.Tree != tree || got.Candidate.Head != "" || f.state.Ticket != "" {
				t.Fatal("legacy receipt inherited the new HEAD or issued a ticket", f.state)
			}
		})
	}
}

func TestInterruptedCommitIntentRecoversExactCommit(t *testing.T) {
	f := prepareHookCommit(t)
	r := f.repo
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver verified change")
	delivered := r.Git("rev-parse", "HEAD")
	rewriteDeliveryLedger(f, func(task *harness.Task) {
		task.Completion = nil
		task.CommitAttempt.Commit, task.CommitAttempt.Tree = "", ""
		task.Events = task.Events[:len(task.Events)-1]
	})
	f.call(0, "next")
	f.want("done")
	r.Git("commit", "--allow-empty", "-qm", "Unrelated later work")
	f.call(0, "next")
	f.want("done")
	if f.state.Completion == nil || f.state.Completion.Candidate.Head != delivered || f.state.Ticket != "" {
		t.Fatal("did not retain the interrupted delivery", f.state)
	}
}

func TestChangingMergeParentsDuringVerificationRequiresAnotherAssertion(t *testing.T) {
	r := kitsutest.Demo(t)
	base := r.Git("rev-parse", "HEAD")
	side := r.Git("commit-tree", r.Git("rev-parse", "HEAD^{tree}"), "-p", base, "-m", "Side history")
	f := flow{t: t, repo: r, id: "merge-during-check"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify the candidate before delivery")
	f.record("intake", "host", "Check the committed history as well as the code")
	f.call(5, "test", "--expect", "assertion using the starting history", "--ticket", f.state.Ticket, "--", "git", "merge", "--no-ff", "--no-commit", side)
	f.call(0, "next")
	f.want("verify")
	if f.state.Verified {
		t.Fatal("an assertion was accepted after its parent set changed")
	}
	f.call(0, "test", "--expect", "check the current merge", "--ticket", f.state.Ticket, "--", "sh", "unit.sh")
	f.want("commit")
}

func TestDeliveryDistinguishesTheCommitAlreadyPresentAtVerification(t *testing.T) {
	for _, early := range []bool{false, true} {
		name := "commit_matches_verified_tree"
		if early {
			name = "verified_existing_commit"
		}
		t.Run(name, func(t *testing.T) {
			r := kitsutest.Demo(t)
			r.Write("answer.txt", "42\n")
			f := flow{t: t, repo: r, id: "observe-commit"}
			f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify the answer and commit")
			f.record("intake", "coordinator", "The answer must be 42")
			r.Git("add", "answer.txt")
			if early {
				r.Git("commit", "-qm", "Commit before proof")
			}
			f.call(0, "next")
			f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
			if !early {
				f.want("commit")
				f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Commit after proof", "--include", "answer.txt")
			}
			f.want("done")
			for _, format := range []string{"--json", "--brief"} {
				got := stageJSON[struct {
					Evidence string `json:"commit_evidence"`
				}](t, r.Kitsu("task", "next", f.id, format))
				if got.Evidence != name {
					t.Errorf("%s commit evidence = %q, want %q", format, got.Evidence, name)
				}
			}
		})
	}
}
