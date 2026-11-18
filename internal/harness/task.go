// Package harness determines the next action for an outcome-scoped coding task.
// Hosts supply judgments. Verification and delivery facts come from the CLI.
package harness

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

var Outcomes = []string{"plan", "implement", "review", "verify", "deliver"}

type Scope struct {
	Publish    string `json:"publish_to,omitempty"`
	PRBase     string `json:"pr_base,omitempty"`
	ReviewBase string `json:"review_base,omitempty"`
	ReviewHead string `json:"review_head,omitempty"`
	Entry      string `json:"entry"`
	Until      string `json:"until"`
	Repair     bool   `json:"repair"`
	Commit     bool   `json:"commit"`
}

func (s Scope) Validate() error {
	if s.ReviewBase != "" || s.ReviewHead != "" {
		return s.validateCommittedReview()
	}
	entry, end := slices.Index(Outcomes, s.Entry), slices.Index(Outcomes, s.Until)
	if entry < 0 || entry > end || s.Entry == "deliver" {
		return fmt.Errorf("choose an entry of plan, implement, review or verify at or before --until")
	}
	if s.Repair && end < slices.Index(Outcomes, "verify") {
		return fmt.Errorf("--repair requires a verify or deliver outcome")
	}
	if s.PRBase != "" && s.Publish == "" {
		return fmt.Errorf("--pr-base requires --publish-to REMOTE/BRANCH")
	}
	if s.Publish != "" {
		remote, branch, ok := strings.Cut(s.Publish, "/")
		if !ok || remote == "" || branch == "" || strings.HasPrefix(remote, "-") || strings.ContainsAny(s.Publish, " :*?[]\\\t\r\n") {
			return fmt.Errorf("invalid --publish-to REMOTE/BRANCH")
		}
	}
	if (s.Commit || s.Publish != "") && s.Until != "deliver" {
		return fmt.Errorf("commit and publication require --until deliver")
	}
	return nil
}

func (s Scope) validateCommittedReview() error {
	if s.ReviewBase == "" || s.ReviewHead == "" {
		return fmt.Errorf("--review-base and --review-head are required together")
	}
	if s.Entry != "review" || s.Until != "review" || s.Repair || s.Commit || s.Publish != "" || s.PRBase != "" {
		return fmt.Errorf("a committed review range requires a read-only review outcome; open a new task for another scope")
	}
	return nil
}

// Snapshot captures the worktree independently from what is staged for commit.
type Snapshot struct {
	Tree       string `json:"tree"`
	Raw        string `json:"raw"`
	Index      string `json:"index"`
	Head       string `json:"head,omitempty"`
	MergeHeads string `json:"merge_heads,omitempty"`
}

func (s Snapshot) SameSource(other Snapshot) bool { return s.Tree == other.Tree && s.Raw == other.Raw }

func (s Snapshot) SameBase(other Snapshot) bool {
	return s.Head == other.Head && s.MergeHeads == other.MergeHeads
}

// Parents is the ordered parent list Git would use for the candidate.
func (s Snapshot) Parents() string { return strings.TrimSpace(s.Head + " " + s.MergeHeads) }

type Event struct {
	PriorRepositoryFailures []RunResult               `json:"prior_repository_failures,omitempty"`
	ReconfirmedAt           time.Time                 `json:"reconfirmed_at,omitzero"`
	Replaced                *Assertion                `json:"replaced_assertion,omitempty"`
	ReplacementReason       string                    `json:"replacement_reason,omitempty"`
	Reviewer                *roles.SpecialistSettings `json:"reviewer,omitempty"`
	Developer               *DeveloperAssignment      `json:"developer,omitempty"`
	ReviewIndex             bool                      `json:"reviewed_index,omitempty"`
	Link                    string                    `json:"pull_request,omitempty"`
	ObservedCommit          string                    `json:"observed_commit,omitempty"`
	Action                  string                    `json:"action"`
	Actor                   string                    `json:"actor,omitempty"`
	Note                    string                    `json:"note"`
	Findings                []string                  `json:"findings,omitempty"`
	Candidate               Snapshot                  `json:"candidate"`
	At                      time.Time                 `json:"at"`
	Ticket                  string                    `json:"ticket"`
}

// ReviewMatches preserves staged-only review scope after a task is extended.
func (e *Event) ReviewMatches(s Snapshot, until, headTree, parents string) bool {
	if until == "review" {
		return e.Candidate == s
	}
	if e.ReviewIndex && e.Candidate.Index != s.Tree {
		return false
	}
	return e.Candidate.SameSource(s) && (e.Candidate.SameBase(s) || e.Candidate.CommittedAs(s, headTree, parents))
}

// CommittedAs recognizes the exact verified tree and ordered parent set,
// including a commit that already existed when the task began.
func (base Snapshot) CommittedAs(current Snapshot, headTree, parents string) bool {
	return headTree != "" && headTree == current.Tree && current.MergeHeads == "" &&
		(current.SameBase(base) || current.Head != base.Head && parents == base.Parents())
}

type CheckRun struct {
	ID     string `json:"run_id"`
	Digest string `json:"log_sha256"`
}
type RunResult struct {
	CheckRun
	CheckID    string `json:"check_id"`
	Outcome    string `json:"outcome"`
	ExitStatus *int   `json:"exit_status,omitempty"`
}

// Assertion identifies the command and run that exposed a behavioral failure.
type Assertion struct {
	Run    string   `json:"run_id"`
	Expect string   `json:"expected_behavior"`
	Argv   []string `json:"argv"`
}

type Proof struct {
	CompletionTicket string     `json:"completion_ticket,omitempty"`
	CommitTarget     string     `json:"commit_target,omitempty"` // Persisted before Git can advance the ref.
	PriorFailure     *Assertion `json:"prior_failure,omitempty"`
	PerRunInputs     bool       `json:"per_run_inputs,omitempty"`
	GateFailure      *RunResult `json:"repository_failure,omitempty"`
	ExitStatus       *int       `json:"exit_status,omitempty"`
	CheckID          string     `json:"check_id"`
	CheckedAt        time.Time  `json:"checked_at,omitzero"`
	FailedCheck      string     `json:"failed_check,omitempty"`

	GateRuns  map[string]CheckRun `json:"repository_runs,omitempty"`
	Ticket    string              `json:"ticket"`
	Expect    string              `json:"expected_behavior"`
	Argv      []string            `json:"argv"`
	Key       string              `json:"content_key"`
	Run       string              `json:"run_id"`
	LogDigest string              `json:"log_sha256"`
	Outcome   string              `json:"outcome"`
	Candidate Snapshot            `json:"candidate"`
}

// FailedAssertion keeps an observed failure available across inconclusive reruns.
// Older records already contain the command and run of their latest failure.
func (p *Proof) FailedAssertion() *Assertion {
	if p == nil {
		return nil
	}
	if p.Outcome == "failed" {
		return &Assertion{Run: p.Run, Expect: p.Expect, Argv: p.Argv}
	}
	return p.PriorFailure
}

type Claim struct {
	Worker    string                    `json:"worker,omitempty"`
	Candidate *Snapshot                 `json:"candidate,omitempty"`
	Reviewer  *roles.SpecialistSettings `json:"reviewer,omitempty"`
	Developer *DeveloperAssignment      `json:"developer,omitempty"`
	Ticket    string                    `json:"ticket"`
	Owner     string                    `json:"owner"`
}

// DeveloperAssignment retains the original scope and candidate so the result
// can be inspected independently of a subsequent extension or checkout change.
type DeveloperAssignment struct {
	roles.SpecialistSettings
	Scope       Scope    `json:"scope"`
	Start       Snapshot `json:"starting_candidate"`
	Owner       string   `json:"owner"`
	Action      string   `json:"action"`
	LaunchLabel string   `json:"launch_label"`
}

type Task struct {
	CommitTarget       string        `json:"commit_target,omitempty"` // Captured at launch or when commit scope is granted.
	ReviewLimit        int           `json:"review_limit,omitempty"`
	ReviewStarts       int           `json:"review_starts,omitempty"`
	DeveloperStarts    int           `json:"developer_starts,omitempty"`
	ReleasedDevelopers []string      `json:"released_developer_workers,omitempty"`
	FinishReserve      time.Duration `json:"finish_reserve_ns,omitempty"`
	PendingScope       bool          `json:"pending_scope,omitempty"`
	Claim              *Claim        `json:"claim,omitempty"`
	Generation         int           `json:"generation,omitempty"`

	Version  int       `json:"version"`
	ID       string    `json:"task"`
	Request  string    `json:"request"`
	Scope    Scope     `json:"scope"`
	Start    Snapshot  `json:"starting_snapshot"`
	Created  time.Time `json:"created_at"`
	Deadline time.Time `json:"deadline,omitempty,omitzero"`
	Events   []Event   `json:"events"`
	Proof    *Proof    `json:"proof,omitempty"`
}

// AuthorizedCommitTarget recovers the destination from older verification receipts.
func (t *Task) AuthorizedCommitTarget() string {
	if !t.Scope.Commit {
		return ""
	}
	if t.CommitTarget != "" || t.Proof == nil {
		return t.CommitTarget
	}
	return t.Proof.CommitTarget
}

// RecordAssertion retains the link to a corrected command and keeps the original
// failure actionable when a retry cannot produce a behavioral verdict.
func (t *Task) RecordAssertion(p *Proof, reason string) {
	t.ArchiveGateFailure()
	e := Event{Action: "test", Note: p.Expect + " (" + p.Outcome + "), run " + p.Run, At: time.Now().UTC(), Candidate: p.Candidate, Ticket: p.Ticket}
	prior := t.Proof.FailedAssertion()
	if prior != nil && !slices.Equal(prior.Argv, p.Argv) {
		e.Replaced, e.ReplacementReason = prior, reason
	}
	if p.Outcome != "passed" && p.Outcome != "failed" {
		p.PriorFailure = prior
	}
	t.Proof = p
	t.Events = append(t.Events, e)
}

// ArchiveGateFailure moves a superseded repository result into the history of
// the assertion that led to it. Its run and digest locate the original log.
func (t *Task) ArchiveGateFailure() {
	p := t.Proof
	if p == nil {
		return
	}
	if p.GateFailure != nil {
		if assertion := t.Latest("test"); assertion != nil {
			assertion.PriorRepositoryFailures = append(assertion.PriorRepositoryFailures, *p.GateFailure)
		}
	}
	p.GateFailure, p.FailedCheck = nil, ""
}

func (t *Task) Latest(actions ...string) *Event {
	for i := len(t.Events) - 1; i >= 0; i-- {
		if slices.Contains(actions, t.Events[i].Action) {
			return &t.Events[i]
		}
	}
	return nil
}

// RepairBase identifies the most recent review finding or failed assertion.
func (t *Task) RepairBase() Snapshot {
	for i := len(t.Events) - 1; i >= 0; i-- {
		e := &t.Events[i]
		if e.Action == "test" || e.Action == "review" && len(e.Findings) > 0 {
			return e.Candidate
		}
	}
	return Snapshot{}
}

func (t *Task) UsedSession(id string) bool {
	return slices.Contains(t.ReleasedDevelopers, id) || slices.ContainsFunc(t.Events, func(e Event) bool { return e.Actor == id })
}

// StartedReviews includes claims released without a result. Older records
// provide a lower bound from completed reviews.
func (t *Task) StartedReviews() int {
	n := 0
	for _, e := range t.Events {
		if e.Action == "review" {
			n++
		}
	}
	return max(n, t.ReviewStarts)
}

type Pause struct {
	Reason  string `json:"reason"`
	Pending string `json:"pending_action"`
}

type Readiness struct {
	Candidate                     Snapshot
	Reviewed, Verified, Committed bool
}

// Next keeps the outstanding action available for recovery while withholding
// the ticket for a new dispatch. A running reviewer may still return its result.
func (t *Task) Next(now time.Time, state Readiness) (string, *Pause) {
	action := t.next(state)
	switch {
	case action == "done":
	case !t.Deadline.IsZero() && !now.Before(t.Deadline):
		return "paused", &Pause{Reason: "deadline", Pending: action}
	case action == "review" && t.Claim == nil:
		if t.ReviewLimit > 0 && t.StartedReviews() >= t.ReviewLimit {
			return "paused", &Pause{Reason: "review_limit", Pending: action}
		}
		if t.FinishReserve > 0 && !now.Before(t.Deadline.Add(-t.FinishReserve)) {
			return "paused", &Pause{Reason: "finish_reserve", Pending: action}
		}
	}
	return action, nil
}

func (t *Task) next(state Readiness) string {
	if t.PendingScope {
		return "scope"
	}
	if t.Latest("intake") == nil {
		return "intake"
	}
	if t.Scope.Entry == "plan" && t.Latest("plan") == nil {
		return "plan"
	}
	if t.Scope.Until == "plan" {
		return "done"
	}
	implement := t.Scope.Entry == "implement" || t.Scope.Entry == "plan"
	if implement && t.Latest("implement") == nil {
		return "implement"
	}
	if t.Scope.Until == "implement" {
		if t.needsCorrection(state.Candidate) {
			return "implement"
		}
		if !state.Verified {
			return "verify"
		}
		return "done"
	}
	if next := t.reviewAction(state); next != "" {
		return next
	}
	if !state.Verified {
		return "verify"
	}
	return t.deliveryAction(state.Candidate, state.Committed)
}

func (t *Task) deliveryAction(current Snapshot, committed bool) string {
	if !committed && t.Scope.Commit {
		return "commit"
	}
	if t.Scope.Publish != "" {
		if !committed {
			return "blocked"
		}
		e := t.Latest("publish", "publication_unconfirmed")
		if e == nil || e.Action != "publish" || e.Candidate.Head != current.Head || !e.Candidate.SameSource(current) || e.Note != t.Scope.Publish || (t.Scope.PRBase != "" && e.Link == "") {
			return "publish"
		}
	}
	return "done"
}

func (t *Task) needsCorrection(candidate Snapshot) bool {
	p := t.Proof
	if p == nil || len(t.Events) == 0 || t.Events[len(t.Events)-1].Action != "test" {
		return false
	}
	// Recheck unchanged source after its base moves before asking for edits.
	return (p.Outcome == "failed" || p.FailedCheck != "") && (candidate.SameBase(p.Candidate) || !candidate.SameSource(p.Candidate))
}

func (t *Task) reviewAction(state Readiness) string {
	review := t.Latest("review")
	// Repair work always goes back to an independent reviewer.
	if t.Scope.Repair && t.needsCorrection(state.Candidate) {
		return "repair"
	}
	if t.Scope.Entry != "verify" || t.Latest("repair") != nil {
		// An in-progress repair can change the source before its result is saved.
		if review != nil && len(review.Findings) > 0 && t.Scope.Repair &&
			t.Events[len(t.Events)-1].Action == "review" && review.Candidate.SameBase(state.Candidate) {
			return "repair"
		}
		if review == nil || !state.Reviewed {
			return "review"
		}
		if t.Scope.Until == "review" {
			return "done"
		}
		if len(review.Findings) > 0 {
			if t.Scope.Repair {
				return "repair"
			}
			return "blocked"
		}
	}
	return ""
}

// Valid checks the record identity before the task can affect a checkout.
func (t *Task) Valid(id string) bool {
	if t.ID != id || t.Version != 1 || t.ReviewLimit < 0 || t.ReviewStarts < 0 || t.DeveloperStarts < 0 || t.FinishReserve < 0 || (t.FinishReserve > 0 && t.Deadline.IsZero()) {
		return false
	}
	if t.PendingScope {
		return t.Scope.Entry == "" && (t.Scope.Until == "" || slices.Contains(Outcomes, t.Scope.Until)) && len(t.Events) == 0 && t.Scope.ReviewBase == "" && t.Scope.ReviewHead == ""
	}
	return t.Scope.Validate() == nil && (t.Scope.ReviewBase == "" || commitID(t.Scope.ReviewBase) && commitID(t.Scope.ReviewHead))
}

func commitID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
