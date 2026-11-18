package harness_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestReviewFindingOnOwnImplementation(t *testing.T) {
	for _, tt := range []struct{ entry, until, next string }{
		{"implement", "deliver", "implement"},
		{"plan", "implement", "implement"},
		{"implement", "review", "done"},
		{"review", "review", "done"},
		{"review", "verify", "blocked"},
		{"verify", "verify", "blocked"},
	} {
		t.Run(tt.entry+"-to-"+tt.until, func(t *testing.T) {
			r := kitsutest.Demo(t)
			f := flow{t: t, repo: r, id: "ready"}
			args := []string{"open", "--entry", tt.entry, "--until", tt.until, "--request", "Handle the requested ready response", "--require-review"}
			if tt.until == "deliver" {
				args = append(args, "--commit")
			}
			if tt.entry == "review" || tt.entry == "verify" {
				r.Write("response.txt", "pending\n")
			}
			f.call(0, args...)
			f.record("intake", "host", "The response must be ready")
			if tt.entry == "plan" {
				f.record("plan", "host", "Write ready and check the line exactly")
			}
			if tt.entry == "verify" {
				f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "response file exists", "--", "test", "-f", "response.txt")
			} else if tt.entry != "review" {
				r.Write("response.txt", "pending\n")
				f.record("implement", "host", "Wrote the response")
			}
			before := freezeStageWorkspace(r, "response.txt")
			f.review("reviewer-one", "Read the response", "--finding", "response.txt contains pending instead of ready")
			f.call(0, "next")
			f.want(tt.next)
			before.check(t, r)
			if tt.next != "implement" {
				if f.state.Ticket != "" {
					t.Fatal("terminal or blocked review granted a ticket")
				}
				return
			}
			firstTicket := f.state.Ticket
			f.call(1, "record", "implement", "--actor", "host", "--ticket", firstTicket, "--note", "Pretend to have corrected it")
			r.Write("response.txt", "ready\n")
			f.call(0, "next")
			f.want("implement")
			if f.state.Ticket != firstTicket {
				t.Fatal("the in-progress correction lost its ticket")
			}
			f.record("implement", "host", "Corrected the reviewed response")
			f.want("review")
			f.review("reviewer-two", "The changed file now contains exactly ready")
			f.want("verify")
			f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "response is exactly ready", "--", "grep", "-qx", "ready", "response.txt")
			if tt.until == "deliver" {
				f.want("commit")
				r.Git("add", "response.txt")
				f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Return ready")
			}
			f.want("done")
			history := r.Kitsu("task", "show", f.id, "--json")
			if history.Code != 0 || !strings.Contains(history.Stdout, "contains pending instead of ready") {
				t.Fatal("lost the original finding", history.Output())
			}
		})
	}
}

func TestStagedReviewFindingNeedsWholeCandidateAfterExtension(t *testing.T) {
	r := kitsutest.Demo(t)
	r.Write("response.txt", "pending\n")
	r.Git("add", "response.txt")
	r.Write("response.txt", "ready\n")
	f := flow{t: t, repo: r, id: "staged-finding"}
	f.call(0, "open", "--entry", "review", "--until", "review", "--request", "Review the staged response")
	f.record("intake", "host", "Staged output must be ready")
	f.review("index-reviewer", "Checked staged response", "--finding", "staged output is pending")
	f.want("done")
	before := freezeStageWorkspace(r, "response.txt")
	f.call(0, "extend", "--until", "verify", "--repair")
	f.want("review")
	f.review("working-reviewer", "The working tree already corrects the staged defect")
	f.want("verify")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "working response is ready", "--", "grep", "-qx", "ready", "response.txt")
	f.want("done")
	before.check(t, r)
}
