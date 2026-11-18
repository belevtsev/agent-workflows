package harness_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestIncompleteImplementationReturnsToItsOwner(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("app.txt", "initial\n")
	r.Git("add", "app.txt")
	r.Git("commit", "-qm", "Baseline")
	head, index := r.Git("rev-parse", "HEAD"), r.Git("write-tree")
	f := flow{t: t, repo: r, id: "finish-design"}
	f.call(0, "open", "--request", "Implement the approved ready response", "--entry", "implement", "--until", "implement")
	f.record("intake", "coordinator", "The response must be exactly ready")
	r.Write("app.txt", "almost ready\n")
	f.record("implement", "developer", "Added a response")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "response is exactly ready", "--", "git", "grep", "^ready$", "--", "app.txt")
	f.call(0, "next")
	f.want("implement")
	f.call(1, "record", "implement", "--ticket", f.state.Ticket, "--actor", "developer", "--note", "No new work")
	f.call(0, "claim", "--ticket", f.state.Ticket, "--actor", "coordinator")
	f.call(1, "test", "--ticket", f.state.Ticket, "--retry")
	r.Write("app.txt", "ready\n")
	f.record("implement", "developer", "Corrected the response to the exact approved value")
	f.want("verify")
	f.call(1, "test", "--ticket", f.state.Ticket, "--expect", "file exists", "--", "git", "ls-files", "app.txt")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("done")
	shown := r.Kitsu("task", "show", f.id, "--json")
	var history struct{ Events []struct{ Action string } }
	if err := json.Unmarshal([]byte(shown.Stdout), &history); shown.Code != 0 || err != nil {
		t.Fatal(shown.Output(), err)
	}
	want := []string{"intake", "implement", "test", "implement", "test"}
	var actions []string
	for _, e := range history.Events {
		actions = append(actions, e.Action)
	}
	if !slices.Equal(actions, want) {
		t.Fatal(actions)
	}
	if !f.state.Verified || r.Git("rev-parse", "HEAD") != head || r.Git("write-tree") != index {
		t.Fatal("implementation finish must leave verification evidence and preserve the original branch and index")
	}
}

func TestImplementationCheckCanBeRetriedBeforeEditing(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "transient-implementation"}
	f.call(0, "open", "--request", "Implement the response", "--until", "implement")
	f.record("intake", "host", "Verify the response under normal conditions")
	r.Write("output.txt", "ready\n")
	f.record("implement", "host", "Wrote response")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "a working dependency accepts the response", "--", "sh", "-c", "test -f .git/available && git grep --no-index '^ready$' -- output.txt")
	f.want("implement")
	r.Write(".git/available", "yes\n")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("done")
}

func TestImplementationCorrectsDeclaredCheckAfterPassingBehavior(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("lint.sh", "if git grep --no-index -q ' $' -- output.txt; then exit 23; fi\n")
	f := flow{t: t, repo: r, id: "declared-implementation"}
	f.call(0, "open", "--request", "Implement a response that satisfies lint and the required example", "--until", "implement")
	f.record("intake", "host", "The observable example and declared checks must pass")
	r.Write("output.txt", "ok with response \n")
	f.record("implement", "host", "First version")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "output contains response", "--", "git", "grep", "--no-index", "response", "--", "output.txt")
	f.want("implement")
	if f.state.Proof.Outcome != "passed" {
		t.Fatal(f.state.Proof)
	}
	first := f.state.Proof.Run
	r.Write("output.txt", "ok with response\n")
	f.record("implement", "host", "Removed trailing whitespace from the response")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("done")
	if !f.state.Verified || f.state.Proof.Run == first {
		t.Fatal(f.state)
	}
}

func TestFailedAssertionRechecksUnchangedSourceAfterBaseMoves(t *testing.T) {
	for _, entry := range []string{"implement", "verify"} {
		t.Run(entry, func(t *testing.T) {
			r := kitsutest.NewRepo(t)
			r.Write("app.txt", "ready\n")
			r.Git("add", ".")
			r.Git("commit", "-qm", "Baseline")
			f := flow{t: t, repo: r, id: "new-base"}
			options := []string{"open", "--request", "Make sure app is ready", "--entry", entry, "--until", entry}
			if entry == "verify" {
				options = append(options, "--repair")
			}
			f.call(0, options...)
			f.record("intake", "host", "Run in the supported environment")
			if entry == "implement" {
				f.record("implement", "host", "Existing implementation supports the example")
			}
			f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "dependency confirms the response", "--", "sh", "-c", "test -f .git/available && git grep ready -- app.txt")
			old := f.state.Ticket
			r.Git("commit", "--allow-empty", "-qm", "Advance base")
			r.Write(".git/available", "yes\n")
			f.call(0, "next")
			f.want("verify")
			f.call(1, "test", "--ticket", old, "--retry")
			f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
			f.want("done")
		})
	}
}

func TestBlockingReviewOnOldBaseRequiresFreshReview(t *testing.T) {
	r := kitsutest.NewRepo(t)
	r.Write("app.txt", "initial\n")
	r.Git("add", "app.txt")
	r.Git("commit", "-qm", "Baseline")
	r.Write("app.txt", "almost ready\n")
	index := r.Git("write-tree")

	f := flow{t: t, repo: r, id: "review-new-base"}
	f.call(0, "open", "--request", "Review, repair, and verify the ready response", "--entry", "review", "--until", "verify", "--repair")
	f.record("intake", "host", "The response must be exactly ready")
	finding := "app.txt:1 still returns almost ready"
	f.review("original-reviewer", "The response still fails the exact match", "--finding", finding)
	f.want("repair")
	oldTicket := f.state.Ticket

	r.Git("commit", "--allow-empty", "-qm", "Advance the review base")
	f.call(0, "next")
	f.want("review")
	if f.state.Ticket == "" || f.state.Ticket == oldTicket {
		t.Fatal("new base must issue a fresh review ticket", f.state)
	}
	f.call(1, "record", "repair", "--ticket", oldTicket, "--actor", "host", "--note", "Old repair result")
	f.review("current-reviewer", "The same failure still reproduces on the new base", "--finding", finding)
	f.want("repair")
	if r.Read("app.txt") != "almost ready\n" || r.Git("write-tree") != index {
		t.Fatal("reopening review changed the candidate or staged files")
	}
}
