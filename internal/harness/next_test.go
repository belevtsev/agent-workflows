package harness

import (
	"testing"
	"time"
)

// The next action of a daily task, a bug ticket and a feature is decided from
// the saved task and what the checkout shows. Each row is one such decision.
// Configured workflows take their own path and are not covered here.

var (
	nextNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	// first is the candidate every event of a row saw. edited has other
	// content and staged has the same content with another index.
	first  = Snapshot{Tree: "tree-1", Raw: "raw-1", Index: "index-1", Head: "head-1"}
	edited = Snapshot{Tree: "tree-2", Raw: "raw-2", Index: "index-2", Head: "head-1"}
	staged = Snapshot{Tree: "tree-1", Raw: "raw-1", Index: "index-9", Head: "head-1"}

	implement = Scope{Entry: "implement", Until: "verify"}
	reviewed  = Scope{Entry: "implement", Until: "verify", RequireReview: true}
	deliver   = Scope{Entry: "implement", Until: "deliver", Commit: true}
	verify    = Scope{Entry: "verify", Until: "verify"}
	bug       = Scope{Entry: "verify", Until: "deliver", Repair: true, Commit: true}
	publish   = Scope{Entry: "implement", Until: "deliver", Publish: "origin/feature"}

	failed   = &Proof{Outcome: "failed", Candidate: first}
	brief    = &FeatureBrief{Version: 1, Goal: "Export", Scope: "One format", Criteria: []Criterion{{ID: "a", Expected: "A", Check: "check:unit"}}}
	findings = Event{Action: "review", Findings: []string{"The export is empty"}, Candidate: first}
	past     = nextNow.Add(-time.Minute)
)

func events(actions ...string) []Event {
	out := make([]Event, len(actions))
	for i, action := range actions {
		out[i] = Event{Action: action, Candidate: first}
	}
	return out
}

type nextCase struct {
	name  string
	task  Task
	state Readiness
	want  string
	pause Pause // of a paused task
}

var nextCases = []nextCase{
	{name: "a task without a scope asks for one", task: Task{PendingScope: true}, want: "scope"},
	{name: "a new task takes its request first", task: Task{Scope: implement}, want: "intake"},
	{name: "a plan entry plans before it implements", task: Task{Scope: Scope{Entry: "plan", Until: "verify"}, Events: events("intake")}, want: "plan"},
	{name: "a plan-only task ends with its plan", task: Task{Scope: Scope{Entry: "plan", Until: "plan"}, Events: events("intake", "plan")}, want: "done"},
	{name: "a draft feature brief is planned first", task: Task{Scope: implement, Feature: &FeatureBrief{Draft: true}, Events: events("intake")}, want: "plan"},

	{name: "an implementation is recorded before it is verified", task: Task{Scope: implement, Events: events("intake")}, want: "implement"},
	{name: "a recorded implementation is verified", task: Task{Scope: implement, Events: events("intake", "implement")}, want: "verify"},
	{name: "a revised feature brief asks for the implementation again",
		task: Task{Scope: implement, Feature: brief, Events: []Event{{Action: "intake"}, {Action: "implement", FeatureRevision: "an earlier revision"}}}, want: "implement"},
	{name: "a verified task with no commit in its scope is done", task: Task{Scope: implement, Events: events("intake", "implement")},
		state: Readiness{Verified: true}, want: "done"},
	{name: "a verified task with a commit in its scope commits", task: Task{Scope: deliver, Events: events("intake", "implement")},
		state: Readiness{Verified: true}, want: "commit"},
	{name: "a committed delivery is done", task: Task{Scope: deliver, Events: events("intake", "implement")},
		state: Readiness{Verified: true, Committed: true}, want: "done"},

	{name: "a failed assertion on an unchanged candidate returns to the author",
		task: Task{Scope: implement, Events: events("intake", "implement", "test"), Proof: failed}, state: Readiness{Candidate: first}, want: "implement"},
	{name: "a failed assertion on an edited candidate is recorded as implemented again",
		task: Task{Scope: implement, Events: events("intake", "implement", "test"), Proof: failed}, state: Readiness{Candidate: edited}, want: "implement"},
	{name: "a failed assertion is checked again once only the index changed",
		task: Task{Scope: implement, Events: events("intake", "implement", "test"), Proof: failed}, state: Readiness{Candidate: staged}, want: "verify"},
	{name: "a failed verify-only task retries its own candidate",
		task: Task{Scope: verify, Events: events("intake", "test"), Proof: failed, VerifyOnlyFailure: &first}, state: Readiness{Candidate: first}, want: "verify"},
	{name: "a failed verify-only task is blocked once the candidate changes",
		task: Task{Scope: verify, Events: events("intake", "test"), Proof: failed, VerifyOnlyFailure: &first}, state: Readiness{Candidate: edited}, want: "blocked"},
	{name: "a verify entry with repair authority repairs a failure",
		task:  Task{Scope: Scope{Entry: "verify", Until: "verify", Repair: true}, Events: events("intake", "test"), Proof: failed},
		state: Readiness{Candidate: first}, want: "repair"},

	{name: "a required review follows the implementation", task: Task{Scope: reviewed, Events: events("intake", "implement")}, want: "review"},
	{name: "verification comes first when the repository asks for it", task: Task{Scope: reviewed, Events: events("intake", "implement")},
		state: Readiness{VerifyBeforeReview: true}, want: "verify"},
	{name: "a repository review rule is a required review", task: Task{Scope: implement, Events: events("intake", "implement")},
		state: Readiness{RepositoryReview: true}, want: "review"},
	{name: "a supplied candidate is tested before its required review",
		task: Task{Scope: Scope{Entry: "verify", Until: "verify", RequireReview: true}, Events: events("intake")}, want: "verify"},
	{name: "a supplied candidate that passed is reviewed",
		task:  Task{Scope: Scope{Entry: "verify", Until: "verify", RequireReview: true}, Events: events("intake")},
		state: Readiness{Verified: true}, want: "review"},
	{name: "a clean review leads to verification", task: Task{Scope: reviewed, Events: events("intake", "implement", "review")},
		state: Readiness{Reviewed: true}, want: "verify"},
	{name: "a review of an earlier candidate is asked for again", task: Task{Scope: reviewed, Events: events("intake", "implement", "review")},
		want: "review"},
	{name: "review findings return to the author", task: Task{Scope: reviewed, Events: append(events("intake", "implement"), findings)},
		state: Readiness{Candidate: first}, want: "implement"},
	{name: "review findings go to repair on a task with repair authority",
		task:  Task{Scope: Scope{Entry: "implement", Until: "verify", RequireReview: true, Repair: true}, Events: append(events("intake", "implement"), findings)},
		state: Readiness{Candidate: first}, want: "repair"},
	{name: "an old record with findings and no repair authority is blocked",
		task:  Task{Version: 1, Scope: implement, Events: append(events("intake", "implement"), findings)},
		state: Readiness{Candidate: first, Reviewed: true}, want: "blocked"},
	{name: "a review-only task ends with its review, findings or not",
		task:  Task{Scope: Scope{Entry: "review", Until: "review"}, Events: append(events("intake"), findings)},
		state: Readiness{Candidate: first, Reviewed: true}, want: "done"},

	{name: "a bug ticket is reproduced first", task: Task{Scope: bug, Bug: &BugDelivery{}, Events: events("intake")}, want: "verify"},
	{name: "a reproduced bug is repaired", task: Task{Scope: bug, Bug: &BugDelivery{Reproduction: failed}, Events: events("intake", "test"), Proof: failed},
		state: Readiness{Candidate: first}, want: "repair"},
	{name: "a repaired bug is verified again",
		task:  Task{Scope: bug, Bug: &BugDelivery{Reproduction: failed}, Events: events("intake", "test", "repair"), Proof: failed},
		state: Readiness{Candidate: edited}, want: "verify"},
	{name: "a verified bug fix is committed",
		task:  Task{Scope: bug, Bug: &BugDelivery{Reproduction: failed}, Events: events("intake", "test", "repair")},
		state: Readiness{Candidate: edited, Verified: true}, want: "commit"},
	{name: "a committed bug fix takes its receipt",
		task:  Task{Scope: bug, Bug: &BugDelivery{Reproduction: failed}, Events: events("intake", "test", "repair")},
		state: Readiness{Candidate: edited, Verified: true, Committed: true}, want: "receipt"},
	{name: "a bug ticket with its receipt is done",
		task: Task{Scope: bug, Bug: &BugDelivery{Reproduction: failed, Receipt: &DeliveryReceipt{}}, Events: events("intake", "test", "repair")}, want: "done"},

	{name: "a publication waits for a commit", task: Task{Scope: publish, Events: events("intake", "implement")},
		state: Readiness{Candidate: first, Verified: true}, want: "blocked"},
	{name: "a committed candidate is published", task: Task{Scope: publish, Events: events("intake", "implement")},
		state: Readiness{Candidate: first, Verified: true, Committed: true}, want: "publish"},
	{name: "a confirmed publication is done",
		task:  Task{Scope: publish, Events: append(events("intake", "implement"), Event{Action: "publish", Note: "origin/feature", Candidate: first})},
		state: Readiness{Candidate: first, Verified: true, Committed: true}, want: "done"},
	{name: "an unconfirmed publication is published again",
		task:  Task{Scope: publish, Events: append(events("intake", "implement"), Event{Action: "publication_unconfirmed", Note: "origin/feature", Candidate: first})},
		state: Readiness{Candidate: first, Verified: true, Committed: true}, want: "publish"},
	{name: "a publication of an earlier candidate is published again",
		task:  Task{Scope: publish, Events: append(events("intake", "implement"), Event{Action: "publish", Note: "origin/feature", Candidate: first})},
		state: Readiness{Candidate: edited, Verified: true, Committed: true}, want: "publish"},
	{name: "a publication without its pull request is published again",
		task: Task{Scope: Scope{Entry: "implement", Until: "deliver", Publish: "origin/feature", PRBase: "main"},
			Events: append(events("intake", "implement"), Event{Action: "publish", Note: "origin/feature", Candidate: first})},
		state: Readiness{Candidate: first, Verified: true, Committed: true}, want: "publish"},

	{name: "a task past its deadline pauses and keeps its action", task: Task{Scope: implement, Events: events("intake", "implement"), Deadline: past},
		want: "paused", pause: Pause{Reason: "deadline", Pending: "verify"}},
	{name: "a finished task does not pause", task: Task{Scope: implement, Events: events("intake", "implement"), Deadline: past},
		state: Readiness{Verified: true}, want: "done"},
	{name: "no further review starts past the review limit", task: Task{Scope: reviewed, Events: events("intake", "implement"), ReviewLimit: 1, ReviewStarts: 1},
		want: "paused", pause: Pause{Reason: "review_limit", Pending: "review"}},
	{name: "a reserved review may still return past the limit",
		task: Task{Scope: reviewed, Events: events("intake", "implement"), ReviewLimit: 1, ReviewStarts: 1, Claim: &Claim{}}, want: "review"},
	{name: "no review starts inside the time kept for finishing",
		task: Task{Scope: reviewed, Events: events("intake", "implement"), Deadline: nextNow.Add(time.Minute), FinishReserve: 2 * time.Minute},
		want: "paused", pause: Pause{Reason: "finish_reserve", Pending: "review"}},
	{name: "a review starts before the time kept for finishing",
		task: Task{Scope: reviewed, Events: events("intake", "implement"), Deadline: nextNow.Add(time.Hour), FinishReserve: 2 * time.Minute}, want: "review"},
}

func TestNextActionOfATask(t *testing.T) {
	for _, c := range nextCases {
		t.Run(c.name, func(t *testing.T) {
			task := c.task
			if task.Version == 0 {
				task.Version = CurrentVersion
			}
			if !task.PendingScope {
				if err := task.Scope.Validate(); err != nil {
					t.Fatalf("the row has no valid scope: %v", err)
				}
			}
			got, pause := task.Next(nextNow, c.state)
			if got != c.want {
				t.Errorf("next = %q, want %q", got, c.want)
			}
			if want := c.pause; (pause == nil) != (want == Pause{}) || pause != nil && *pause != want {
				t.Errorf("pause = %+v, want %+v", pause, want)
			}
		})
	}
}
