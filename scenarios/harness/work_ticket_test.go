package harness_test

import (
	"encoding/json"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestWorkTicketSurvivesEditsButNotANewBase(t *testing.T) {
	for _, action := range []string{"implement", "repair"} {
		t.Run(action, func(t *testing.T) {
			r := kitsutest.Demo(t)
			f := flow{t: t, repo: r, id: "ready"}
			if action == "implement" {
				f.call(0, "open", "--entry", action, "--until", "verify", "--request", "Implement ready and verify it")
			} else {
				f.call(0, "open", "--entry", "verify", "--until", "verify", "--repair", "--request", "Check ready and repair it if missing")
			}
			f.record("intake", "coordinator", "ready.txt must contain ready")
			if action == "repair" {
				f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "ready.txt contains ready", "--", "grep", "-qx", "ready", "ready.txt")
			}
			f.want(action)
			original := f.state.Ticket
			if action == "implement" {
				r.Git("commit", "--allow-empty", "-qm", "Advance HEAD")
			} else {
				base := r.Git("rev-parse", "HEAD")
				side := r.Git("commit-tree", r.Git("rev-parse", "HEAD^{tree}"), "-p", base, "-m", "Side history")
				r.Git("merge", "--no-ff", "--no-commit", side)
			}
			r.Write("ready.txt", "ready\n")
			rejected := r.Kitsu("task", "record", f.id, action, "--actor", "coordinator", "--ticket", original, "--note", "Based on earlier history")
			if rejected.Code != 1 {
				t.Fatalf("old work ticket accepted after Git history changed: %s", rejected.Output())
			}
			f.call(0, "next")
			f.want(action)
			if f.state.Ticket == original {
				t.Fatal("the old ticket was reissued on a different base")
			}
			refreshed := f.state.Ticket
			r.Git("add", "ready.txt")
			f.call(0, "next")
			if refreshed != f.state.Ticket {
				t.Fatal("staging invalidated the work ticket")
			}
			f.record(action, "coordinator", "Reviewed the new base and the completed work")
			f.want("verify")
		})
	}
}

func TestLegacyDeveloperTicketUsesReservedBase(t *testing.T) {
	r := kitsutest.Demo(t)
	f := flow{t: t, repo: r, id: "legacy-work"}
	f.call(0, "open", "--entry", "implement", "--until", "verify", "--request", "Implement ready")
	f.record("intake", "coordinator", "Check the ready response")
	f.call(0, "claim", "--ticket", f.state.Ticket, "--actor", "coordinator", "--host", "codex")
	f.call(0, "claim", "--ticket", f.state.Ticket, "--actor", "coordinator", "--worker", "developer")
	path := ".git/kitsu-stages/task-harness-legacy-work.json"
	var saved harness.Task
	if err := json.Unmarshal([]byte(r.Read(path)), &saved); err != nil {
		t.Fatal(err)
	}
	legacy := contextpack.Key([]any{saved.ID, saved.Created, saved.Scope, saved.Generation, len(saved.Events), "implement", harness.Snapshot{}})
	saved.Claim.Ticket = legacy
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(path, string(data))
	head := r.Git("rev-parse", "HEAD")
	r.Git("commit", "--allow-empty", "-qm", "Advance the checkout")
	f.call(0, "next")
	f.want("reconcile")
	f.call(1, "record", "implement", "--actor", "developer", "--ticket", legacy, "--note", "Old result")
	r.Git("checkout", "-q", "--detach", head)
	r.Write("ready.txt", "ready\n")
	f.call(0, "next")
	f.want("implement")
	if f.state.Ticket != legacy {
		t.Fatal("a linked developer lost the original reservation")
	}
	f.record("implement", "developer", "Finished against the reserved base")
	f.want("verify")
}
