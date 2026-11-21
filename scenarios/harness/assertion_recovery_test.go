package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestRepairCannotSilentlyReplaceItsFailingAssertion(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("app.sh", "printf 'broken\\n'\n")
	r.Git("add", "app.sh")
	r.Git("commit", "-qm", "Pin the failing public command")
	f := flow{t: t, repo: r, id: "public-output"}
	f.call(0, "open", "--require-review", "--request", "Repair and deliver the public command so it prints ready", "--entry", "verify", "--until", "deliver", "--repair", "--commit")
	f.record("intake", "host", "Exercise the user-visible output and the required suite")
	test := []string{"test", "--ticket", f.state.Ticket, "--expect", "the public command prints ready", "--", "sh", "-c", "test \"$(sh app.sh)\" = ready"}
	f.call(5, test...)
	f.want("repair")
	r.Write("notes.txt", "first attempted repair")
	f.record("repair", "host", "Candidate repair")
	f.review("reviewer-1", "Candidate reviewed")
	f.want("verify")
	first := f.state.Proof.Run
	substitute := r.Kitsu("task", "test", f.id, "--ticket", f.state.Ticket, "--brief", "--expect", "suite passes", "--", "sh", "unit.sh")
	if substitute.Code != 1 || !strings.Contains(substitute.Output(), "--retry") || !strings.Contains(substitute.Output(), first) {
		t.Fatalf("different green suite silently displaced failed public behavior: %s", substitute.Output())
	}
	if len(r.RecordFiles("unit")) != 0 {
		t.Fatal("unexpected substitute execution")
	}
	f.call(5, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("repair")
	r.Write("app.sh", "printf 'ready\\n'\n")
	f.record("repair", "host", "Corrected the actual output")
	f.review("reviewer-2", "Verified public output change")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("commit")
	if f.state.Proof.Run == first || !f.state.Verified {
		t.Fatal("the repaired command needs fresh evidence", f.state)
	}
	f.call(0, "next")
	f.want("commit")
	r.Git("add", "app.sh", "notes.txt")
	f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Correct the public output")
	f.call(0, "next")
	f.want("done")
	if f.state.Delivery != "committed" || r.Git("status", "--porcelain") != "" || !strings.Contains(r.Git("show", "HEAD:app.sh"), "ready") {
		t.Fatal("delivered commit does not contain the verified repair", f.state)
	}
}

func TestAssertionCorrectionRetainsOriginalFailure(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "correct-fixture"}
	f.call(0, "open", "--request", "Verify the app fixture", "--until", "verify", "--repair")
	f.record("intake", "host", "Exercise the actual fixture")
	f.call(1, "test", "--ticket", f.state.Ticket, "--retry")
	f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "fixture is ok", "--", "git", "grep", "incorrect-pattern", "--", "src/app.txt")
	original := f.state.Proof.Run
	f.want("repair")
	// A reserved repair owns the candidate until the host reconciles it.
	f.call(0, "claim", "--actor", "owner", "--ticket", f.state.Ticket)
	f.call(1, "test", "--ticket", f.state.Ticket, "--retry")
	f.call(0, "release", "--ticket", f.state.Ticket)
	r.Write("other.txt", "unreviewed edit")
	f.call(1, "test", "--ticket", f.state.Ticket, "--retry")
	r.Remove("other.txt")
	reason := "The first command matched a typo. The fixture contract requires ok."
	correct := func(tool string, args ...string) []string {
		return append([]string{"test", "--ticket", f.state.Ticket, "--replace-reason", reason, "--expect", "fixture is ok", "--", tool}, args...)
	}
	f.call(5, correct("kitsu-missing-fixture-tool")...)
	f.call(1, "test", "--ticket", f.state.Ticket, "--expect", "suite passes", "--", "sh", "unit.sh")
	f.call(0, correct("git", "grep", "ok", "--", "src/app.txt")...)
	f.want("done")
	var saved struct {
		Events []struct {
			Replaced *struct {
				Run  string `json:"run_id"`
				Argv []string
			} `json:"replaced_assertion"`
			Reason string `json:"replacement_reason"`
		}
	}
	shown := r.Kitsu("task", "show", f.id, "--json")
	if err := json.Unmarshal([]byte(shown.Stdout), &saved); err != nil || shown.Code != 0 || len(saved.Events) == 0 {
		t.Fatal("correction history unavailable", shown.Output(), err)
	}
	last := saved.Events[len(saved.Events)-1]
	if last.Reason != reason || last.Replaced == nil || last.Replaced.Run != original || !strings.Contains(strings.Join(last.Replaced.Argv, " "), "incorrect-pattern") {
		t.Fatal("the correction lost its origin or reason", last)
	}
	body := readReceiptMarkdown(f)
	markdownContains(t, body, "A failing assertion, run ` "+original+" `, was replaced by another command.")
	markdownOmits(t, body, reason, "incorrect-pattern", "Authority added")
}
