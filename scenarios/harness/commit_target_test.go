package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestCommitTicketAfterCheckoutSwitch(t *testing.T) {
	for _, mode := range []string{"branch", "detached"} {
		t.Run(mode, func(t *testing.T) {
			r := kitsutest.Demo(t)
			r.Git("checkout", "-qb", "delivery")
			parent := r.Git("rev-parse", "HEAD")
			f := flow{t: t, repo: r, id: "verified-delivery"}
			f.call(0, "open", "--entry", "review", "--until", "deliver", "--commit", "--request", "Review, verify and commit the change")
			r.Write("answer.txt", "42\n")
			f.call(0, "next")
			f.record("intake", "coordinator", "The answer must be 42")
			f.review("reviewer", "Checked the output")
			f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
			f.want("commit")
			ticket, proof := f.state.Ticket, f.state.Proof.Run
			r.Git("add", "answer.txt")
			target := "refs/heads/alternate"
			if mode == "detached" {
				r.Git("checkout", "-q", "--detach", "HEAD")
				target = "HEAD"
			} else {
				r.Git("checkout", "-qb", "alternate")
			}
			f.call(1, "commit", "--ticket", ticket, "--message", "Deliver answer")
			if got := r.Git("rev-parse", "HEAD"); got != parent {
				t.Fatal("stale ticket created commit", got)
			}
			f.call(0, "next")
			f.want("blocked")
			if f.state.Verified || f.state.Proof.Run != proof || f.state.Ticket != "" || f.state.CommitTarget != "refs/heads/delivery" || !strings.Contains(f.state.Delivery, target) {
				t.Fatal("expected the task to preserve its commit target and verification receipt", f.state)
			}
			r.Git("checkout", "-q", "delivery")
			f.call(0, "next")
			f.want("commit")
			if !f.state.Verified || f.state.Proof.Run != proof || f.state.Ticket != ticket {
				t.Fatal("returning to the authorized checkout did not restore the ticket and proof", f.state)
			}
			f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver answer")
			f.want("done")
			if r.Git("rev-list", "--count", parent+"..delivery") != "1" || r.Git("show", "delivery:answer.txt") != "42" {
				t.Fatal("commit did not reach the authorized target")
			}
			if mode == "branch" && r.Git("rev-parse", "alternate") != parent {
				t.Fatal("the other branch was advanced")
			}
		})
	}
}

func TestFailedCommitOnOneBranchCannotFinishOnAnother(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	parent := r.Git("rev-parse", "HEAD")
	f := flow{t: t, repo: r, id: "interrupted-delivery"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and commit an answer")
	f.record("intake", "host", "Check expected output")
	r.Write("answer.txt", "42\n")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
	r.Git("add", "answer.txt")
	// Fail inside Git, after Kitsu has validated the ticket and staged tree.
	r.Write(".git/refs/heads/delivery.lock", "another operation holds the branch ref")
	f.call(1, "commit", "--ticket", f.state.Ticket, "--message", "Deliver answer")
	r.Remove(".git/refs/heads/delivery.lock")
	r.Git("checkout", "-qb", "other")
	r.Git("commit", "-qm", "Same tree and parent on another branch")
	f.call(0, "next")
	f.want("blocked")
	if f.state.Verified || r.Git("rev-parse", "delivery") != parent {
		t.Fatal("attributed another branch's commit to the failed delivery")
	}
	// Recover the original branch and its work, then commit there for real.
	r.Git("checkout", "-q", "delivery")
	r.Git("checkout", "other", "--", "answer.txt")
	f.call(0, "next")
	f.want("commit")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver answer")
	f.want("done")
	r.Git("checkout", "-qb", "inspection")
	f.call(0, "next")
	f.want("done") // The intended branch still contains exactly this delivered commit.
}

func TestLegacyCommitScopeKeepsVerifiedDestination(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	r.Git("branch", "elsewhere")
	f := flow{t: t, repo: r, id: "legacy-delivery"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and commit here")
	f.record("intake", "coordinator", "Verify the answer")
	r.Write("answer.txt", "42\n")
	r.Git("add", "answer.txt")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
	f.want("commit")
	ticket, run := f.state.Ticket, f.state.Proof.Run

	file := ".git/kitsu-stages/task-harness-" + f.id + ".json"
	var saved map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.Read(file)), &saved); err != nil {
		t.Fatal(err)
	}
	delete(saved, "commit_target") // Older tasks stored this only on the proof.
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(file, string(data))
	r.Git("symbolic-ref", "HEAD", "refs/heads/elsewhere")
	f.call(0, "next")
	f.want("blocked")
	if f.state.CommitTarget != "refs/heads/delivery" || f.state.Ticket != "" {
		t.Fatalf("legacy destination was lost: %+v", f.state)
	}
	r.Git("symbolic-ref", "HEAD", "refs/heads/delivery")
	f.call(0, "next")
	f.want("commit")
	if f.state.Ticket != ticket || f.state.Proof.Run != run {
		t.Fatalf("legacy verification changed: %+v", f.state)
	}
	f.call(0, "commit", "--ticket", ticket, "--message", "Deliver verified answer")
	f.want("done")
}

func TestResumeDoesNotOfferDuplicateCommitOnDifferentBranch(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	r.Git("branch", "also-at-parent")
	f := flow{t: t, repo: r, id: "lost-receipt"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify and deliver the answer")
	f.record("intake", "host", "Check the answer")
	r.Write("answer.txt", "42\n")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
	r.Git("add", "answer.txt")
	r.Git("commit", "-qm", "Delivered before coordinator interruption")
	delivered := r.Git("rev-parse", "HEAD")
	// As in a hook or interrupted checkout, HEAD changes but the verified
	// files and index survive. The new branch looks ready to commit again.
	r.Git("symbolic-ref", "HEAD", "refs/heads/also-at-parent")
	f.call(0, "next")
	f.want("blocked")
	if f.state.Ticket != "" || !strings.Contains(f.state.Delivery, "refs/heads/delivery") || !strings.Contains(f.state.Delivery, delivered) {
		t.Fatal("missing recovery destination", f.state)
	}
	r.Git("symbolic-ref", "HEAD", "refs/heads/delivery")
	f.call(0, "next")
	f.want("done")
}
