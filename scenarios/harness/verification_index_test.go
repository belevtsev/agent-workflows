package harness_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestVerifyStagedContentsAfterIndexOnlyChange(t *testing.T) {
	for _, extend := range []bool{false, true} {
		name := "verify"
		if extend {
			name = "extend-to-delivery"
		}
		t.Run(name, func(t *testing.T) {
			r := kitsutest.NewRepo(t)
			r.Write("answer.txt", "wrong\n")
			r.Git("add", "answer.txt")
			r.Git("commit", "-qm", "Original staged content")
			r.Write("answer.txt", "ready\n")
			r.Git("add", "answer.txt")
			f := flow{t: t, repo: r, id: "staged"}
			f.call(0, "open", "--entry", "verify", "--until", "verify", "--request", "Check whether the staged answer is ready")
			f.record("intake", "host", "Check the staged bytes")
			f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "staged answer is ready", "--", "git", "grep", "--cached", "-e", "^ready$", "--", "answer.txt")
			f.want("done")
			run := f.state.Proof.Run
			r.Git("restore", "--staged", "--", "answer.txt")
			if extend {
				f.call(0, "extend", "--until", "deliver", "--commit")
			}
			f.call(0, "next")
			f.want("verify")
			f.call(5, "test", "--retry", "--ticket", f.state.Ticket)
			if f.state.Verified || f.state.Proof.Run == run || f.state.Proof.Outcome != "failed" || r.Read("answer.txt") != "ready\n" {
				t.Fatalf("new staged contents did not get an independent result: %+v", f.state)
			}
		})
	}
}

func TestCheckCannotCertifyIndexItChanges(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("answer.txt", "wrong\n")
	r.Git("add", "answer.txt")
	r.Git("commit", "-qm", "Original answer")
	r.Write("answer.txt", "ready\n")
	f := flow{t: t, repo: r, id: "restage"}
	f.call(0, "open", "--entry", "verify", "--until", "verify", "--request", "Verify staged contents")
	f.record("intake", "host", "Check without changing the staged candidate")
	result := r.Kitsu("task", "test", f.id, "--brief", "--ticket", f.state.Ticket, "--expect", "stable staged candidate", "--", "git", "add", "answer.txt")
	if result.Code != 5 || !strings.Contains(result.Stdout, "source_changed") {
		t.Fatalf("index changed during assertion: exit %d, %s", result.Code, result.Output())
	}
}

func TestCommitRechecksPreviouslyVerifiedStagedContents(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("answer.txt", "before\n")
	r.Git("add", "answer.txt")
	r.Git("commit", "-qm", "Original answer")
	head := r.Git("rev-parse", "HEAD")
	r.Write("answer.txt", "ready\n")
	r.Git("add", "answer.txt")
	r.Write("answer.txt", "wrong\n")
	f := flow{t: t, repo: r, id: "verify-delivery"}
	f.call(0, "open", "--entry", "verify", "--until", "deliver", "--commit", "--request", "Commit only if the staged answer is ready")
	f.record("intake", "host", "Inspect the staged answer")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "staged answer is ready", "--", "git", "grep", "--cached", "-e", "^ready$", "--", "answer.txt")
	f.want("commit")
	ticket := f.state.Ticket
	r.Git("add", "answer.txt")
	index := r.Git("write-tree")
	f.call(5, "commit", "--ticket", ticket, "--include", "answer.txt", "--message", "Deliver checked answer")
	if f.state.Verified || f.state.Proof.Outcome != "failed" || r.Git("rev-parse", "HEAD") != head || r.Git("write-tree") != index {
		t.Fatal("failed staged assertion did not preserve checkout and failure receipt", f.state)
	}
}
