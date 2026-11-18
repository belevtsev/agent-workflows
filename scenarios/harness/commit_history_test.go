package harness_test

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

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

func TestCommitHookChangingIndexCannotRecordVerifiedDelivery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable hook")
	}
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "hook-changes-index"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and commit the change")
	f.record("intake", "host", "Check the candidate")
	r.Write("change.txt", "before hook\n")
	f.call(0, "next")
	f.call(0, "test", "--expect", "fixture behavior", "--ticket", f.state.Ticket, "--", "sh", "unit.sh")
	f.want("commit")
	r.Git("add", "change.txt")
	r.Git("config", "core.hooksPath", r.Path(".git/local-hooks"))
	path := ".git/local-hooks/pre-commit"
	r.Write(path, "#!/bin/sh\nprintf 'written by hook\\n' > change.txt\ngit add change.txt\n")
	if err := os.Chmod(r.Path(path), 0755); err != nil {
		t.Fatal(err)
	}
	result := r.Kitsu("task", "commit", f.id, "--ticket", f.state.Ticket, "--message", "Commit verified change", "--brief")
	if result.Code != 1 || !strings.Contains(result.Output(), "changed") {
		t.Fatal("unexpected commit result", result.Output())
	}
	if got := r.Git("show", "HEAD:change.txt"); got != "written by hook" {
		t.Fatal("fixture failed to exercise hook", got)
	}
	f.call(0, "next")
	f.want("verify")
	// A post-commit hook can change the checkout while leaving the actual
	// committed tree correct. Report the new pending verification in the receipt.
	r.Remove(path)
	r.Write("change.txt", "second verified change\n")
	f.call(0, "next")
	f.call(0, "test", "--expect", "fixture behavior", "--ticket", f.state.Ticket, "--", "sh", "unit.sh")
	f.want("commit")
	r.Git("add", "change.txt")
	post := ".git/local-hooks/post-commit"
	r.Write(post, "#!/bin/sh\nprintf 'later work\\n' > change.txt\n")
	if err := os.Chmod(r.Path(post), 0755); err != nil {
		t.Fatal(err)
	}
	f.call(5, "commit", "--ticket", f.state.Ticket, "--message", "Commit second verified change")
	f.want("verify")
	if got := r.Git("show", "HEAD:change.txt"); got != "second verified change" {
		t.Fatal(got)
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
