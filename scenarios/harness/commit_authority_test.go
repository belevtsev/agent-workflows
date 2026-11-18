package harness_test

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestCommitAuthorityPinsBranchBeforeVerification(t *testing.T) {
	for _, mode := range []string{"open", "extend"} {
		t.Run(mode, func(t *testing.T) {
			r := kitsutest.Demo(t)
			r.Git("checkout", "-qb", "delivery")
			r.Git("branch", "elsewhere")
			f := flow{t: t, repo: r, id: "conditional"}
			const request = "Check the change and commit it on this branch only if it passes."
			if mode == "open" {
				f.call(0, "open", "--request", request, "--entry", "verify", "--until", "deliver", "--commit")
			} else {
				f.call(0, "open", "--request", request, "--entry", "verify", "--until", "verify")
				f.call(0, "extend", "--until", "deliver", "--commit")
			}
			f.record("intake", "coordinator", "The answer should equal 42.")
			r.Write("answer.txt", "42\n")
			r.Git("add", "answer.txt")
			f.call(0, "next")
			f.want("verify")
			ticket := f.state.Ticket
			before := freezeStageWorkspace(r, "answer.txt")

			r.Git("symbolic-ref", "HEAD", "refs/heads/elsewhere")
			f.call(0, "next")
			f.want("blocked")
			if f.state.Ticket != "" || f.state.CommitTarget != "refs/heads/delivery" || !strings.Contains(f.state.Delivery, "refs/heads/elsewhere") {
				t.Fatalf("wrong checkout receipt = %+v", f.state)
			}
			f.call(0, "open", "--request", request, "--entry", "verify", "--until", "deliver", "--commit")
			f.want("blocked")
			f.call(0, "extend", "--commit")
			f.want("blocked")
			args := []string{"test", "--ticket", ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt"}
			f.call(1, args...)
			if len(r.RecordFiles("task-"+f.id)) != 0 {
				t.Fatal("verification started on the other checkout")
			}
			before.check(t, r)
			r.Git("symbolic-ref", "HEAD", "refs/heads/delivery")
			f.call(0, args...)
			f.want("commit")
			f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver verified answer")
			f.want("done")
			if r.Git("rev-parse", "elsewhere") != before.head || r.Git("rev-list", "--count", before.head+"..delivery") != "1" || r.Git("show", "delivery:answer.txt") != "42" {
				t.Fatal("the scoped commit did not land exactly once on the original branch")
			}
		})
	}
}

func TestCommitAuthorityRejectsBranchChangesDuringVerification(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	r.Git("branch", "elsewhere")
	f := flow{t: t, repo: r, id: "branch-switch"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify the change and commit here.")
	f.record("intake", "coordinator", "Verify only the authorized checkout.")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "stable checkout", "--", "git", "symbolic-ref", "HEAD", "refs/heads/elsewhere")
	f.want("blocked")
	if f.state.Proof == nil || f.state.Proof.Outcome != "source_changed" || f.state.Verified || f.state.Ticket != "" {
		t.Fatalf("branch move was accepted as verification: %+v", f.state)
	}
	r.Git("symbolic-ref", "HEAD", "refs/heads/delivery")
	f.call(0, "next")
	f.want("verify")
}

func TestCommitAuthorityRecoversAfterPostCommitBranchSwitch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable hook")
	}
	r := kitsutest.Demo(t)
	r.Git("checkout", "-qb", "delivery")
	r.Git("branch", "elsewhere")
	parent := r.Git("rev-parse", "HEAD")
	f := flow{t: t, repo: r, id: "hook-switch"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Verify the change and commit here.")
	f.record("intake", "coordinator", "The answer should equal 42.")
	r.Write("answer.txt", "42\n")
	r.Git("add", "answer.txt")
	f.call(0, "next")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
	f.want("commit")
	ticket := f.state.Ticket
	r.Git("config", "core.hooksPath", r.Path(".git/test-hooks"))
	hook := ".git/test-hooks/post-commit"
	r.Write(hook, "#!/bin/sh\ngit symbolic-ref HEAD refs/heads/elsewhere\n")
	if err := os.Chmod(r.Path(hook), 0755); err != nil {
		t.Fatal(err)
	}
	result := r.Kitsu("task", "commit", f.id, "--ticket", ticket, "--message", "Deliver answer")
	if result.Code != 1 || !strings.Contains(result.Output(), "checkout changed during commit") {
		t.Fatal("Git hook changed the checkout without a diagnostic", result.Output())
	}
	delivered := r.Git("rev-parse", "delivery")
	f.call(0, "next")
	f.want("blocked")
	if f.state.Ticket != "" || !strings.Contains(f.state.Delivery, delivered) || r.Git("rev-parse", "elsewhere") != parent {
		t.Fatalf("commit recovery can repeat work or lost its destination: %+v", f.state)
	}
	r.Git("symbolic-ref", "HEAD", "refs/heads/delivery")
	f.call(0, "next")
	f.want("done")
	f.call(0, "commit", "--ticket", ticket, "--message", "Deliver answer")
	if r.Git("rev-list", "--count", parent+"..delivery") != "1" || r.Git("show", "delivery:answer.txt") != "42" {
		t.Fatal("recovery did not preserve the one authorized commit")
	}
}

func TestConditionalCommitAuthorityPreservesFailedCandidate(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("answer.txt", "13\n")
	r.Git("add", "answer.txt")
	before := freezeStageWorkspace(r, "answer.txt")
	f := flow{t: t, repo: r, id: "conditional-failure"}
	const request = "Check the change and commit it on this branch only if it passes."
	f.call(0, "open", "--request", request, "--entry", "verify", "--until", "deliver", "--commit")
	f.record("intake", "coordinator", "The answer should equal 42.")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "answer is 42", "--", "git", "grep", "--no-index", "-e", "^42$", "--", "answer.txt")
	f.call(0, "next")
	f.want("verify")
	if f.state.Proof == nil || f.state.Proof.Outcome != "failed" || f.state.Verified {
		t.Fatalf("failed behavior was lost: %+v", f.state)
	}
	f.call(1, "open", "--request", request, "--entry", "verify", "--until", "deliver", "--commit", "--repair")
	f.call(1, "open", "--request", "Repair the answer", "--entry", "verify", "--until", "deliver", "--commit")
	f.call(1, "record", "repair", "--ticket", f.state.Ticket, "--actor", "coordinator", "--note", "Replace the failed answer")
	f.call(1, "commit", "--ticket", f.state.Ticket, "--message", "Premature answer", "--include", "answer.txt")
	stageHistory(&f, "intake", "test")
	before.check(t, r)
}
