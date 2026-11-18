package harness_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

type developerAssignment struct {
	Host, Owner, Action string
	Model               struct{ Model, Effort string }
	Skills              []struct{ Path, SHA256 string }
	Scope               struct {
		Entry, Until string
		Repair       bool
	}
	Start stageCandidate `json:"starting_candidate"`
	Label string         `json:"launch_label"`
}

type developerReceipt struct {
	Next, Ticket string
	Deadline     time.Time
	Launch       string `json:"developer_launch"`
	Created      bool   `json:"claim_created"`
	Claim        struct {
		Owner, Worker, Ticket string
		Developer             *developerAssignment
	}
}

func TestDelegatedWorkRecovery(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			r := newStageRepo(t)
			path := map[string]string{"codex": ".agents", "claude": ".claude"}[host] + "/skills/implement/SKILL.md"
			const source = "---\nname: implement\ndescription: Implement the expected response.\n---\nCheck the exact response file.\n"
			r.Write(path, source)
			if result := r.Kitsu("role", "set", "developer", "--host", host, "--model", "custom-developer", "--reasoning", "max", "--skills", "implement"); result.Code != 0 {
				t.Fatal(result.Output())
			}
			r.Write("app.txt", "initial\n")
			r.Write("notes.txt", "Original developer notes.\n")
			r.Git("add", "-A")
			r.Git("commit", "-qm", "Start response fixture")
			r.Write("notes.txt", "Staged developer notes.\n")
			r.Git("add", "notes.txt")
			r.Write("notes.txt", "Unstaged developer notes.\n")
			r.Write("app.txt", "partial fix\n")
			const owner = "assigning-coordinator"
			const worker = "native-implementer"
			const repairer = "native-repairer"
			f := flow{t: t, repo: r, id: "delegated-response"}
			f.call(0, "open", "--require-review", "--entry", "implement", "--until", "verify", "--repair", "--request", "Have a developer agent finish this partial fix and verify the exact ready response.")
			f.record("intake", owner, "Finish app.txt and preserve both sets of notes.")
			f.want("implement")
			prior := stageNext(&f).Candidate
			reserved := reserveDeveloperWork(&f, owner, host)
			checkDeveloperAssignment(t, reserved.Claim.Developer, owner, host, "implement", r.Path(path), source, prior)
			if !reserved.Created || reserved.Launch != "unresolved" || reserved.Claim.Worker != "" {
				t.Fatalf("missing developer reservation: %+v", reserved)
			}
			launches := 1
			f.call(1, "claim", "--ticket", reserved.Ticket, "--actor", "replacement-coordinator", "--host", host)
			f.call(1, "record", "implement", "--ticket", reserved.Ticket, "--actor", worker, "--note", "Unlinked work")
			recovered := stageJSON[developerReceipt](t, r.Kitsu("task", "next", f.id, "--brief"))
			if recovered.Created || recovered.Launch != "unresolved" || recovered.Claim.Worker != "" || recovered.Ticket != reserved.Ticket || !reflect.DeepEqual(recovered.Claim.Developer, reserved.Claim.Developer) {
				t.Fatalf("reservation recovery changed the unresolved launch: %+v", recovered)
			}
			f.call(1, "claim", "--ticket", reserved.Ticket, "--actor", "other", "--worker", worker)
			linkDeveloperWork(&f, owner, worker, reserved)
			f.call(1, "claim", "--ticket", reserved.Ticket, "--actor", owner, "--worker", "different-worker")

			// Implement remains the correct stage, but the old worker's base is gone.
			r.Git("commit", "--allow-empty", "--only", "-qm", "Advance base")
			index, head := r.Read(".git/index"), r.Git("rev-parse", "HEAD")
			f.call(0, "next")
			f.want("reconcile")
			f.call(1, "record", "implement", "--ticket", reserved.Ticket, "--actor", worker, "--note", "Result on the old base")
			f.call(0, "release", "--ticket", reserved.Ticket)
			f.want("implement")
			prior = stageNext(&f).Candidate
			current := reserveDeveloperWork(&f, owner, host)
			launches++
			checkDeveloperAssignment(t, current.Claim.Developer, owner, host, "implement", r.Path(path), source, prior)
			if current.Ticket == reserved.Ticket || current.Claim.Developer.Label == reserved.Claim.Developer.Label {
				t.Fatal("replacement reservation reused a ticket or native history label")
			}
			f.call(1, "claim", "--ticket", reserved.Ticket, "--actor", owner, "--worker", worker)
			linkDeveloperWork(&f, owner, worker+"-replacement", current)
			r.Write("app.txt", "almost ready\n")
			f.call(0, "next")
			f.want("implement")
			if f.state.Ticket != current.Ticket {
				t.Fatal("expected implementation edits changed the delegated ticket")
			}
			first := stageNext(&f).Candidate
			f.call(1, "record", "implement", "--ticket", current.Ticket, "--actor", owner, "--note", "Claim owner's result")
			firstReview := recordDelegatedWork(&f, "implement", worker+"-replacement", host, current, first, "Implemented the response.")
			f.call(1, "claim", "--ticket", firstReview, "--actor", owner, "--worker", worker+"-replacement")
			f.call(1, "claim", "--ticket", firstReview, "--actor", owner, "--worker", worker)
			f.call(0, "claim", "--ticket", firstReview, "--actor", owner, "--worker", "independent-one")
			f.record("review", "independent-one", "A partial fix remains.", "--finding", "app.txt:1 exact ready response is missing")
			f.want("repair")

			prior = stageNext(&f).Candidate
			repair := reserveDeveloperWork(&f, owner, host)
			launches++
			checkDeveloperAssignment(t, repair.Claim.Developer, owner, host, "repair", r.Path(path), source, prior)
			linkDeveloperWork(&f, owner, repairer, repair)
			r.Write("app.txt", "ready\n")
			second := stageNext(&f).Candidate
			secondReview := recordDelegatedWork(&f, "repair", repairer, host, repair, second, "Corrected the exact response.")
			f.call(1, "record", "review", "--ticket", secondReview, "--actor", repairer, "--note", "Self review")
			f.call(0, "claim", "--ticket", secondReview, "--actor", owner, "--worker", "independent-two")
			f.record("review", "independent-two", "The exact ready response now matches.")
			f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "the exact ready response", "--", "git", "grep", "^ready$", "--", "app.txt")
			f.want("done")
			stageHistory(&f, "intake", "implement", "review", "repair", "review", "test")
			if !f.state.Verified || r.Git("rev-parse", "HEAD") != head || r.Read(".git/index") != index || r.Read("notes.txt") != "Unstaged developer notes.\n" || r.Read("app.txt") != "ready\n" {
				t.Fatal("delegation modified unrelated work or lost verification")
			}
			checkDeveloperUsage(t, host, []string{reserved.Claim.Developer.Label, current.Claim.Developer.Label, repair.Claim.Developer.Label}, []string{"implement", "implement", "repair"})
			t.Logf("%s: %d scripted developer launches, two accepted returns and two fresh review sessions, verified candidate %s", host, launches, second.Tree)
		})
	}
}

func TestLinkedDeveloperCanReturnAfterDeadline(t *testing.T) {
	r := newStageRepo(t)
	r.Write("app.txt", "partial repair\n")
	r.Git("add", "app.txt")
	r.Git("commit", "-qm", "Starting response")
	f := flow{t: t, repo: r, id: "late-developer"}
	f.call(0, "open", "--require-review", "--entry", "implement", "--until", "verify", "--request", "Finish the exact response.", "--time-limit", "1h")
	f.record("intake", "coordinator", "App responds with ready.")
	claim := reserveDeveloperWork(&f, "coordinator", "codex")
	if claim.Deadline.IsZero() {
		t.Fatal("developer reservation lacks its deadline")
	}
	linkDeveloperWork(&f, "coordinator", "linked-developer", claim)
	r.Write("app.txt", "ready\n")
	path := ".git/kitsu-stages/task-harness-late-developer.json"
	var saved map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.Read(path)), &saved); err != nil {
		t.Fatal(err)
	}
	saved["deadline"] = json.RawMessage(`"2000-01-01T00:00:00Z"`)
	expired, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(path, string(expired))
	f.call(0, "next")
	f.want("paused")
	f.call(1, "extend", "--time-limit", "1h")
	f.call(1, "record", "implement", "--ticket", claim.Ticket, "--actor", "another", "--note", "Unrelated result")
	args := []string{"task", "record", f.id, "implement", "--ticket", claim.Ticket, "--actor", "linked-developer", "--note", "Finished after the deadline.", "--claim-review", "codex", "--brief"}
	for i := 0; i < 2; i++ {
		late := stageJSON[developerReceipt](t, r.Kitsu(args...))
		if late.Next != "paused" || late.Created || late.Ticket != "" || late.Claim.Ticket != "" {
			t.Fatalf("late return started more work: %+v", late)
		}
	}
	event := stageHistory(&f, "intake", "implement")[1]
	if event.Actor != "linked-developer" || r.Git("show", event.Candidate.Tree+":app.txt") != "ready" {
		t.Fatalf("late return lost its accepted candidate: %+v", event)
	}
	f.call(0, "extend", "--time-limit", "1h")
	f.want("review")
	resumed := stageJSON[developerReceipt](t, r.Kitsu(args...))
	if resumed.Created || resumed.Claim.Ticket != "" || resumed.Next != "review" {
		t.Fatalf("replay created a reviewer after renewing the deadline: %+v", resumed)
	}
}

func TestDelegatedCorrectionCountsAsRework(t *testing.T) {
	for _, entry := range []string{"review", "implement"} {
		t.Run(entry, func(t *testing.T) {
			r := newStageRepo(t)
			r.Write("app.txt", "almost ready\n")
			r.Git("add", "app.txt")
			r.Git("commit", "-qm", "Commit the existing partial fix")
			f := flow{t: t, repo: r, id: "first-delegated-correction"}
			open := []string{"open", "--entry", entry, "--request", "Observe and correct the exact response."}
			if entry == "review" {
				open = append(open, "--until", "verify", "--repair")
			} else {
				open = append(open, "--until", "implement")
			}
			f.call(0, open...)
			f.record("intake", "coordinator", "Check the exact ready response.")
			if entry == "review" {
				f.review("independent", "The exact response still fails.", "--finding", "app.txt:1 still returns almost ready")
				f.want("repair")
			} else {
				f.record("implement", "coordinator", "Prepared the new response.")
				f.call(5, "test", "--ticket", f.state.Ticket, "--expect", "the exact ready response", "--", "git", "grep", "^ready$", "--", "app.txt")
				f.want("implement")
			}
			reservation := reserveDeveloperWork(&f, "coordinator", "codex")
			checkDeveloperUsage(t, "codex", []string{reservation.Claim.Developer.Label}, []string{"repair"})
		})
	}
}

func checkDeveloperUsage(t *testing.T, host string, labels, stages []string) {
	t.Helper()
	const spent = 23
	var starts []tokens.Start
	var writing, rework int64
	for i, label := range labels {
		parsed, ok := tokens.ParseLabel(label)
		if !ok || parsed.Stage != stages[i] || parsed.Round != i+1 || tokens.RoleOf(parsed.Stage) != "developer" {
			t.Errorf("unexpected developer launch label %q, want %s attempt %d", label, stages[i], i+1)
		}
		if stages[i] == "implement" {
			writing += spent
		} else {
			rework += spent
		}
		starts = append(starts, tokens.Start{
			Host: host, Session: "coordinator", Agent: fmt.Sprintf("developer-%d", i+1),
			Task: parsed.Task, Stage: parsed.Stage, Round: parsed.Round, Known: true,
			Models: map[string]tokens.Counts{"fixture-model": {Output: spent}},
		})
	}
	report := tokens.Build(starts, nil)
	if len(report.Tasks) != 1 || report.Tasks[0].Writing != writing || report.Tasks[0].Flow.Rework != rework {
		t.Fatalf("developer starts should account for %d writing and %d rework tokens, got %+v", writing, rework, report.Tasks)
	}
	t.Logf("%s assigns %d scripted tokens to writing and %d to rework for %v", host, writing, rework, labels)
}

func reserveDeveloperWork(f *flow, owner, host string) developerReceipt {
	f.t.Helper()
	result := f.repo.Kitsu("task", "claim", f.id, "--actor", owner, "--ticket", f.state.Ticket, "--host", host, "--brief")
	claimed := stageJSON[developerReceipt](f.t, result)
	if !claimed.Created || claimed.Claim.Owner != owner || claimed.Claim.Ticket != f.state.Ticket || claimed.Ticket != f.state.Ticket {
		f.t.Fatalf("expected one new worker reservation: %+v", claimed)
	}
	return claimed
}

func checkDeveloperAssignment(t *testing.T, d *developerAssignment, owner, host, action, path, source string, start stageCandidate) {
	t.Helper()
	if d == nil || d.Owner != owner || d.Host != host || d.Action != action || d.Model.Model != "custom-developer" || d.Model.Effort != "max" || d.Label == "" || d.Start != start || d.Scope.Entry != "implement" || d.Scope.Until != "verify" || !d.Scope.Repair {
		t.Fatalf("wrong assigned scope or settings: %+v", d)
	}
	if len(d.Skills) != 1 || d.Skills[0].Path != path || d.Skills[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(source))) {
		t.Fatalf("wrong selected skill: %+v", d.Skills)
	}
}

func linkDeveloperWork(f *flow, owner, worker string, reserved developerReceipt) {
	f.t.Helper()
	f.call(0, "claim", "--actor", owner, "--ticket", reserved.Ticket, "--worker", worker)
	f.call(0, "claim", "--actor", owner, "--ticket", reserved.Ticket, "--worker", worker)
	linked := stageJSON[developerReceipt](f.t, f.repo.Kitsu("task", "next", f.id, "--brief"))
	if linked.Created || linked.Launch != "linked" || linked.Claim.Worker != worker || linked.Ticket != reserved.Ticket || !reflect.DeepEqual(linked.Claim.Developer, reserved.Claim.Developer) {
		f.t.Fatalf("wrong linked worker on recovery: %+v", linked)
	}
}

func recordDelegatedWork(f *flow, action, worker, host string, reserved developerReceipt, work stageCandidate, note string) string {
	f.t.Helper()
	args := []string{"task", "record", f.id, action, "--actor", worker, "--ticket", reserved.Ticket, "--note", note, "--claim-review", host, "--brief"}
	ret := stageJSON[developerReceipt](f.t, f.repo.Kitsu(args...))
	if !ret.Created || ret.Next != "review" || ret.Claim.Owner != reserved.Claim.Owner || ret.Claim.Developer != nil {
		f.t.Fatalf("worker return did not reserve independent review: %+v", ret)
	}
	replayed := stageJSON[developerReceipt](f.t, f.repo.Kitsu(args...))
	if replayed.Created || replayed.Claim.Ticket != ret.Claim.Ticket {
		f.t.Fatalf("worker return replay changed the next reservation: %+v", replayed)
	}
	// Replaying the worker's response against a later unrelated edit must
	// recover the already accepted work and the reviewer it spawned.
	source := f.repo.Read("app.txt")
	f.repo.Write("app.txt", "Edited after the developer return.\n")
	moved := stageJSON[developerReceipt](f.t, f.repo.Kitsu(args...))
	if moved.Created || moved.Next != "reconcile" || moved.Claim.Ticket != ret.Claim.Ticket {
		f.t.Fatalf("changed source let a replay replace the reviewer: %+v", moved)
	}
	f.repo.Write("app.txt", source)
	view := f.repo.Kitsu("task", "show", f.id, "--json")
	shown := stageJSON[struct {
		Events []struct {
			stageEvent
			Developer *developerAssignment
		}
	}](f.t, view)
	event := shown.Events[len(shown.Events)-1]
	if event.Actor != worker || event.Action != action || event.Candidate != work || !reflect.DeepEqual(event.Developer, reserved.Claim.Developer) || f.repo.Git("show", event.Candidate.Tree+":app.txt") != strings.TrimSpace(source) {
		f.t.Fatalf("worker return lost its assigned scope or candidate: %+v", event)
	}
	if err := os.WriteFile(filepath.Join(f.t.ArtifactDir(), action+"-receipt.json"), []byte(view.Stdout), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return ret.Claim.Ticket
}
