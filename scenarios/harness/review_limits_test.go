package harness_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestReviewDispatchLimitPreservesFindingsAndCanResume(t *testing.T) {
	r := kitsutest.NewRepo(t)
	f := flow{t: t, repo: r, id: "bounded"}
	f.call(2, "open", "--request", "Repair and verify", "--entry", "review", "--until", "verify", "--finish-reserve", "1m")
	f.call(0, "open", "--request", "Repair and verify", "--entry", "review", "--until", "verify", "--repair", "--review-limit", "2")
	f.record("intake", "host", "Inspect the patch")
	first := f.state.Ticket
	f.call(0, "claim", "--actor", "host", "--ticket", first)
	f.call(1, "claim", "--actor", "other", "--ticket", first)
	f.call(0, "release", "--ticket", first)
	f.review("reviewer", "Missing example", "--finding", "label.txt lacks expected value")
	r.Write("label.txt", "new value\n")
	f.record("repair", "host", "Handled the example", "--claim-review", "codex")
	f.want("paused")
	res := r.Kitsu("task", "next", f.id, "--brief")
	var stopped struct {
		Pause struct {
			Reason  string
			Pending string `json:"pending_action"`
		}
		Ticket    string
		Starts    int `json:"review_starts"`
		Findings  int `json:"review_findings"`
		Candidate struct{ Tree string }
		Resume    []string
	}
	if err := json.Unmarshal([]byte(res.Stdout), &stopped); res.Code != 0 || err != nil || stopped.Pause.Reason != "review_limit" || stopped.Pause.Pending != "review" || stopped.Starts != 2 || stopped.Findings != 1 || stopped.Ticket != "" || stopped.Candidate.Tree == "" || len(stopped.Resume) != 5 {
		t.Fatal(res.Output())
	}
	f.call(1, "claim", "--actor", "host", "--ticket", first)
	f.call(1, "test", "--ticket", first, "--expect", "correct label", "--", "git", "--version")
	f.call(0, "extend", "--review-limit", "3")
	f.review("second-reviewer", "Regression verified")
	f.call(0, "test", "--ticket", f.state.Ticket, "--expect", "expected label", "--", "git", "grep", "--no-index", "new value", "--", "label.txt")
	f.want("done")
}

func TestReviewFinishingAfterDeadlineCanReturnCurrentResult(t *testing.T) {
	r := kitsutest.NewRepo(t)
	f := flow{t: t, repo: r, id: "deadline-review"}
	f.call(0, "open", "--request", "Repair and verify the patch", "--entry", "review", "--until", "verify", "--time-limit", "5m", "--finish-reserve", "6m")
	f.record("intake", "host", "Inspect source")
	f.want("paused")
	if got := r.Kitsu("task", "next", f.id, "--brief"); !containsPauseReason(got.Stdout, "finish_reserve") {
		t.Fatal(got.Output())
	}
	f.call(0, "extend", "--time-limit", "1h")
	f.want("review")
	ticket := f.state.Ticket
	claim := r.Kitsu("task", "claim", f.id, "--actor", "host", "--ticket", ticket, "--brief")
	var dates struct {
		Deadline time.Time
		ReturnBy time.Time `json:"review_return_by"`
	}
	if err := json.Unmarshal([]byte(claim.Stdout), &dates); claim.Code != 0 || err != nil || dates.Deadline.Sub(dates.ReturnBy) != 6*time.Minute {
		t.Fatal("worker missing its return time", claim.Output())
	}
	// Advance the persisted deadline rather than making the test depend on sleep.
	file := ".git/kitsu-stages/task-harness-" + f.id + ".json"
	var record map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.Read(file)), &record); err != nil {
		t.Fatal(err)
	}
	record["deadline"] = json.RawMessage(`"2000-01-01T00:00:00Z"`)
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(file, string(data))
	f.call(0, "next")
	f.want("paused")
	r.Write("late.txt", "changed")
	f.call(1, "record", "review", "--actor", "reviewer", "--ticket", ticket, "--note", "Current source reviewed")
	r.Remove("late.txt")
	f.call(0, "record", "review", "--actor", "reviewer", "--ticket", ticket, "--note", "Current source reviewed")
	f.want("paused")
	f.call(0, "extend", "--time-limit", "1h")
	f.want("verify")
}

func containsPauseReason(output, reason string) bool {
	var v struct{ Pause struct{ Reason string } }
	return json.Unmarshal([]byte(output), &v) == nil && v.Pause.Reason == reason
}

func TestRecordAndClaimReviewSurvivesRetries(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			r := kitsutest.NewRepo(t)
			f := flow{t: t, repo: r, id: "combined"}
			f.call(0, "open", "--request", "Review the source", "--until", "review", "--review-limit", "2")
			args := []string{"task", "record", f.id, "intake", "--actor", "host", "--ticket", f.state.Ticket, "--note", "Inspect the current source", "--claim-review", host, "--brief"}
			receipt := func(wantCreated, wantClaim bool) {
				t.Helper()
				result := r.Kitsu(args...)
				var v struct {
					Created bool `json:"claim_created"`
					Claim   *struct {
						Owner    string
						Reviewer struct{ Host string }
					}
				}
				if err := json.Unmarshal([]byte(result.Stdout), &v); result.Code != 0 || err != nil || v.Created != wantCreated || (v.Claim != nil) != wantClaim {
					t.Fatal(result.Output())
				}
				if v.Claim != nil && (v.Claim.Owner != "host" || v.Claim.Reviewer.Host != host) {
					t.Fatal(result.Output())
				}
			}
			receipt(true, true)
			receipt(false, true)
			f.call(0, "next")
			f.call(0, "release", "--ticket", f.state.Ticket)
			receipt(false, false)
			f.review("worker", "Current candidate approved")
			f.want("done")
			receipt(false, false)
		})
	}
}

func TestResultAndReservationAreAtomicWhenSkillResolutionFails(t *testing.T) {
	r := kitsutest.Demo(t)
	const path = ".agents/skills/verify/SKILL.md"
	const source = "---\nname: verify\ndescription: Verify exports.\n---\nRead the regression.\n"
	r.Write(path, source)
	if saved := r.Kitsu("role", "set", "reviewer", "--host", "codex", "--skills", "verify"); saved.Code != 0 {
		t.Fatal(saved.Output())
	}
	r.Remove(path)
	f := flow{t: t, repo: r, id: "atomic"}
	f.call(0, "open", "--until", "review", "--request", "Review the export", "--review-limit", "1")
	f.call(1, "record", "intake", "--actor", "host", "--ticket", f.state.Ticket, "--note", "Inspect exports", "--claim-review", "codex")
	f.call(0, "next")
	f.want("intake")
	r.Write(path, source)
	f.call(0, "next")
	f.record("intake", "host", "Inspect exports", "--claim-review", "codex")
	f.record("review", "reviewer", "Current candidate approved")
	f.want("done")
}
