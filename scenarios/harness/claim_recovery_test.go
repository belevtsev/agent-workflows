package harness_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestOutstandingReviewerSurvivesCandidateRollback(t *testing.T) {
	for _, until := range []string{"review", "deliver"} {
		t.Run(until, func(t *testing.T) {
			r := kitsutest.Demo(t)
			f := flow{t: t, repo: r, id: "rolled-back"}
			args := []string{"open", "--request", "Review the public behavior", "--entry", "review", "--until", until}
			if until == "deliver" {
				args = append(args, "--commit")
			}
			f.call(0, args...)
			f.record("intake", "coordinator", "Inspect and check the requested fixture")
			r.Write("change", "approved\n")
			r.Git("add", "change")
			f.call(0, "next")
			f.review("reviewer-one", "Approved original candidate")
			if until == "deliver" {
				f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "approved change", "--", "git", "grep", "approved", "--", "change")
				f.want("commit")
			}
			previousTicket := f.state.Ticket
			r.Write("change", "superseded\n")
			f.call(0, "next")
			f.want("review")
			reserved := f.state.Ticket
			f.call(0, "claim", "--ticket", reserved, "--actor", "coordinator", "--host", "codex")
			r.Write("change", "approved\n")
			result := r.Kitsu("task", "next", f.id, "--brief")
			var current struct {
				Next, Ticket string
				Claim        struct{ Owner, Ticket string }
				ReviewRound  int `json:"review_round"`
			}
			if err := json.Unmarshal([]byte(result.Stdout), &current); result.Code != 0 || err != nil || current.Next != "reconcile" || current.Ticket != "" || current.Claim.Ticket != reserved || current.Claim.Owner != "coordinator" || current.ReviewRound != 2 {
				t.Fatalf("another candidate has an outstanding reviewer: %s", result.Output())
			}
			f.call(1, "record", "review", "--actor", "reviewer-two", "--ticket", reserved, "--note", "Approved superseded candidate")
			if until == "deliver" {
				f.call(1, "commit", "--ticket", previousTicket, "--message", "Must wait for reconciliation")
			}
			f.call(0, "release", "--ticket", reserved)
			if until == "deliver" {
				f.want("commit")
				f.call(0, "commit", "--ticket", f.state.Ticket, "--message", "Deliver original verified change")
			}
			f.want("done")
			shown := r.Kitsu("task", "show", f.id, "--json")
			if shown.Code != 0 || strings.Contains(shown.Stdout, "Approved superseded candidate") {
				t.Fatal(shown.Output())
			}
		})
	}
}

func TestRecoverExactWorkerAcrossReplacementReservations(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			r := kitsutest.Demo(t)
			f := flow{t: t, repo: r, id: "review-worker"}
			f.call(0, "open", "--request", "Review this candidate", "--until", "review", "--review-limit", "2")
			f.record("intake", "parent", "Inspect the working diff", "--claim-review", host)
			first := f.state.Ticket
			bind := func(ticket, owner, worker string) kitsutest.Result {
				return r.Kitsu("task", "claim", f.id, "--brief", "--ticket", ticket, "--actor", owner, "--worker", worker)
			}
			assertWorker := func(result kitsutest.Result, worker string, round int) {
				t.Helper()
				var v struct {
					Claim   struct{ Worker string }
					Round   int  `json:"review_round"`
					Created bool `json:"claim_created"`
				}
				if err := json.Unmarshal([]byte(result.Stdout), &v); result.Code != 0 || err != nil || v.Claim.Worker != worker || v.Round != round || v.Created {
					t.Fatalf("worker recovery: %s", result.Output())
				}
			}
			if got := bind(first, "other", "native-first"); got.Code != 1 {
				t.Fatal("non-owner linked a worker", got.Output())
			}
			assertWorker(bind(first, "parent", "native-first"), "native-first", 1)
			assertWorker(bind(first, "parent", "native-first"), "native-first", 1)
			if got := bind(first, "parent", "another"); got.Code != 1 {
				t.Fatal("overwrote a linked worker", got.Output())
			}
			assertWorker(r.Kitsu("task", "next", f.id, "--brief"), "native-first", 1)
			f.call(1, "record", "review", "--actor", "another", "--ticket", first, "--note", "Wrong worker")
			f.call(0, "release", "--ticket", first)
			second := f.state.Ticket
			f.call(0, "claim", "--ticket", second, "--actor", "parent", "--host", host)
			assertWorker(bind(second, "parent", "native-second"), "native-second", 2)
			if got := bind(first, "parent", "native-first"); got.Code != 1 {
				t.Fatal("old ticket rebound a cancelled worker", got.Output())
			}
			f.call(1, "record", "review", "--actor", "native-first", "--ticket", second, "--note", "Late first result")
			f.record("review", "native-second", "Reviewed replacement reservation")
			f.want("done")
		})
	}
}
