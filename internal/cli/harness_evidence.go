package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/runner"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

type verificationLogRef struct {
	harness.RunResult
	Path string `json:"path"`
}

type harnessView struct {
	TicketMarkdown    string                `json:"ticket_markdown,omitempty"`
	TicketSource      *harness.TicketSource `json:"ticket_source,omitempty"`
	ActorSessions     []tokens.Session      `json:"actor_session_usage,omitempty"`
	ReviewPolicyError string                `json:"review_policy_error,omitempty"`
	RepositoryReview  bool                  `json:"repository_requires_review,omitempty"`
	VerificationLog   *verificationLogRef   `json:"verification_log,omitempty"`
	ManagedInterval   string                `json:"managed_interval,omitempty"`
	PendingDaily      string                `json:"pending_daily_action,omitempty"`
	PriorNotes        map[string]string     `json:"prior_notes,omitempty"`
	Publication       *publicationReceipt   `json:"last_publication,omitempty"`
	Completion        *harness.Completion   `json:"completed_delivery,omitempty"`
	DeveloperLaunch   string                `json:"developer_launch,omitempty"`
	ReviewCurrent     bool                  `json:"review_current"`
	store             *store.Store
	ClaimCreated      bool           `json:"claim_created,omitempty"`
	Pause             *harness.Pause `json:"pause,omitempty"`
	Deadline          time.Time      `json:"deadline,omitzero"`
	ReviewLimit       int            `json:"review_limit,omitempty"`
	ReviewStarts      int            `json:"review_starts,omitempty"`
	FinishReserve     time.Duration  `json:"finish_reserve_ns,omitempty"`
	Request           string         `json:"request"`
	Claim             *harness.Claim `json:"claim,omitempty"`
	Behavior          string         `json:"current_behavior"`
	Coverage          string         `json:"token_coverage"`
	Captured          *int64         `json:"captured_tokens"`
	UsageError        string         `json:"usage_error,omitempty"`

	CommitTarget        string           `json:"commit_target,omitempty"`
	CurrentCommitTarget string           `json:"current_commit_target,omitempty"`
	CommitEvidence      string           `json:"commit_evidence,omitempty"`
	Task                string           `json:"task"`
	Scope               harness.Scope    `json:"scope"`
	Next                string           `json:"next"`
	Candidate           harness.Snapshot `json:"candidate"`
	Ticket              string           `json:"ticket,omitempty"`
	Verified            bool             `json:"verified"`
	Gate                string           `json:"repository_checks"`
	Delivery            string           `json:"delivery"`
	Review              *harness.Event   `json:"latest_review,omitempty"`
	Proof               *harness.Proof   `json:"behavioral_assertion,omitempty"`
	Last                *harness.Event   `json:"last_result,omitempty"`
	Elapsed             float64          `json:"elapsed_seconds"`
	FirstResult         *float64         `json:"first_result_seconds,omitempty"`
	Actors              int              `json:"recorded_sessions"`
	Retries             int              `json:"repeated_actions"`
	Repairs             int              `json:"repairs"`
	Usage               []string         `json:"usage_reference"`
}

func observeHarness(ctx context.Context, s *session, t *harness.Task, withUsage bool) (harnessView, error) {
	v := harnessView{store: s.store, Claim: t.Claim, Request: t.Request, Task: t.ID, Scope: t.Scope, Behavior: "pending", Gate: "unchecked", Delivery: "working tree", Proof: t.Proof, Review: t.Latest("review"), Usage: []string{"kitsu", "usage", "show", "--task", t.ID, "--json"}}
	v.Publication = lastPublication(t)
	v.Deadline, v.FinishReserve, v.ReviewLimit, v.ReviewStarts = t.Deadline, t.FinishReserve, t.ReviewLimit, t.StartedReviews()
	if completion := t.CompletedDelivery(); completion != nil {
		v.Completion, v.Candidate, v.CommitTarget = completion, completion.Candidate, completion.Target
		v.Next, v.Delivery, v.Verified = "done", "committed", true
		v.Claim, v.ReviewCurrent = nil, completion.Reviewed
		v.Behavior, v.Gate, v.CommitEvidence = "passed", completion.RepositoryChecks, completion.CommitEvidence
		if v.Proof != nil && (v.Proof.Outcome != store.Passed || v.Proof.Candidate.Tree != completion.Candidate.Tree) {
			v.Proof = nil
		}
		v.describeBug(t)
	} else if err := v.observeCurrentHarness(ctx, s, t); err != nil {
		return v, err
	}
	v.timing(t)
	if withUsage {
		v.loadUsage(s.repo, t.Events)
	}
	return v, nil
}

func (v *harnessView) observeCurrentHarness(ctx context.Context, s *session, t *harness.Task) error {
	c, err := harnessCandidate(ctx, s, t)
	if err != nil {
		return err
	}
	v.Candidate = c
	if t.Scope.ReviewBase != "" {
		v.Delivery = "committed range"
	}
	if p := t.Proof; p != nil && (p.GateFailure != nil || p.Outcome != store.Passed) {
		v.VerificationLog = v.verificationLog()
	}
	var headTree, parents string
	movedReview := v.Review != nil && !v.Review.Candidate.SameBase(c) && v.Review.Candidate.SameSource(c)
	passingRun := t.Proof != nil && t.Proof.Outcome == store.Passed && c.SameSource(t.Proof.Candidate)
	if c.Head != "" && c.MergeHeads == "" && (movedReview || passingRun) {
		headTree, parents, err = s.repo.CommitContents(ctx, c.Head)
		if err != nil {
			return err
		}
	}
	v.ReviewCurrent = v.Review != nil && v.Review.ReviewMatches(c, t.Scope.Until, headTree, parents)
	if err := v.observeDelivery(ctx, s, t, headTree, parents); err != nil {
		return err
	}
	v.describeBug(t)
	return v.routeCurrentDelivery(ctx, s, t)
}

func (v *harnessView) routeCurrentDelivery(ctx context.Context, s *session, t *harness.Task) error {
	recovering, err := v.checkCommitAttempt(ctx, s, t)
	if err != nil || v.Next == "blocked" {
		return err
	}
	if err := v.routeAction(ctx, s, t); err != nil {
		return err
	}
	if recovering && slices.Contains([]string{"commit", "implement", "repair"}, v.Next) {
		v.Next, v.Ticket, v.Delivery = "blocked", "", "Git already created this commit. Open a new task for further edits."
	}
	if v.Next == "done" && v.Delivery == "committed" && t.Scope.Until == "deliver" && (t.Scope.Commit || t.Scope.Publish != "") {
		v.Completion = v.deliveryCompletion(time.Now().UTC())
	}
	return nil
}

func (v *harnessView) describeBug(t *harness.Task) {
	if t.Bug != nil {
		v.TicketSource = &t.Bug.Source
	}
	if t.NeedsReproduction() && v.Behavior == "passed" {
		v.Verified = false
		v.Delivery = "bug not reproduced; run an assertion that demonstrates the reported behavior"
	}
}

func (v *harnessView) setActionTicket(ctx context.Context, s *session, t *harness.Task) error {
	c := v.Candidate
	pending := v.Next
	if v.Pause != nil {
		pending = v.Pause.Pending
	}
	if t.Scope.Until == "plan" && pending == "plan" && c != t.Start {
		v.Next, v.Pause = "blocked", nil
		v.Delivery = planInputChange(t.Start, c)
		return nil
	}
	ticket := actionTicket(t, pending, c)
	claim := v.Claim
	developerBaseMoved := false
	if claim != nil && claim.Developer != nil {
		v.DeveloperLaunch = "unresolved"
		if claim.Worker != "" {
			v.DeveloperLaunch = "linked"
		}
		developerBaseMoved = !claim.Developer.Start.SameBase(c)
	}
	if err := v.checkCommitCheckout(ctx, s, t, pending); err != nil || v.CurrentCommitTarget != "" {
		return err
	}
	if pending == "commit" {
		ticket = contextpack.Key([]string{ticket, v.CommitTarget})
	}
	if claim != nil && (claim.Ticket != ticket || developerBaseMoved) {
		v.Next, v.Pause = "reconcile", nil
	} else if v.Next != "done" && v.Next != "blocked" && v.Next != "paused" {
		v.Ticket = ticket
	}
	return nil
}

func (v *harnessView) checkCommitCheckout(ctx context.Context, s *session, t *harness.Task, pending string) error {
	target := t.AuthorizedCommitTarget()
	v.CommitTarget = target
	if pending == "done" || pending == "publish" || target == "" && pending != "commit" {
		return nil
	}
	current, err := s.repo.CommitTarget(ctx)
	if err != nil {
		return err
	}
	if target == "" {
		v.CommitTarget = current
		return nil
	}
	if current == target {
		return nil
	}
	v.Next, v.Pause, v.Verified = "blocked", nil, false
	v.CurrentCommitTarget = current
	v.Delivery = fmt.Sprintf("checkout is %s. Return to commit target %s to resume", current, target)
	if pending != "commit" || target == "HEAD" {
		return nil
	}
	proof := t.Proof.Candidate
	head, found, err := s.repo.CommitOf(ctx, target)
	if err != nil || !found || head == proof.Head {
		return err
	}
	tree, parents, err := s.repo.CommitContents(ctx, head)
	if err == nil && tree == v.Candidate.Tree && parents == proof.Parents() {
		v.Delivery = "verified commit " + head + " already exists on " + target + ". Return to that checkout to resume"
	}
	return err
}

// An active managed interval owns the worktree. Keep the saved daily state
// readable while withholding an action that would be refused on execution.
func (v *harnessView) checkManagedInterval(id string) {
	if id != "" && v.Next != "done" {
		v.ManagedInterval, v.PendingDaily = id, v.Next
		v.Next, v.Ticket = "blocked", ""
	}
}

func (v *harnessView) routeAction(ctx context.Context, s *session, t *harness.Task) error {
	required, err := repositoryRequiresReview(ctx, s, t, v.Candidate)
	var invalid *decl.Error
	if err != nil && !errors.As(err, &invalid) {
		return err
	}
	v.RepositoryReview = required
	v.Next, v.Pause = t.Next(time.Now(), v.readiness())
	if invalid != nil {
		v.ReviewPolicyError = "Cannot determine independent-review policy: " + invalid.Error() + ". Correct kitsu.toml or explicitly require review with kitsu task extend " + t.ID + " --require-review."
		switch v.Next {
		case "review", "verify", "commit", "publish", "done":
			v.Next = "blocked"
			return nil
		}
	}
	return v.setActionTicket(ctx, s, t)
}

// Retain the policy present at intake even when the candidate edits it.
func repositoryRequiresReview(ctx context.Context, s *session, task *harness.Task, current harness.Snapshot) (bool, error) {
	if task.Version == 1 || task.PendingScope || task.Scope.Until == "plan" || task.Scope.RequireReview || task.Scope.Entry == "review" || task.Scope.Until == "review" {
		return false, nil
	}
	seen := map[string]bool{}
	var invalidPolicy error
	for _, tree := range []string{task.Start.Head, task.Start.Tree, current.Tree, current.Head} {
		if tree == "" || seen[tree] {
			continue
		}
		seen[tree] = true
		cfg, err := evidence.Declaration(ctx, s.repo, tree)
		if errors.Is(err, evidence.ErrNoDeclaration) {
			continue
		}
		if err != nil {
			var invalid *decl.Error
			if errors.As(err, &invalid) {
				invalidPolicy = err
				continue
			}
			return false, err
		}
		if cfg.RequireReview {
			return true, nil
		}
	}
	return false, invalidPolicy
}

func (v harnessView) readiness() harness.Readiness {
	return harness.Readiness{RepositoryReview: v.RepositoryReview, Candidate: v.Candidate, Reviewed: v.ReviewCurrent, Verified: v.Verified, Committed: v.Delivery == "committed"}
}

func printHarness(cfg Config, o harnessOptions, v harnessView) error {
	v.describeCommitReadiness()
	if o.brief {
		reply := v.brief()
		if v.VerificationLog != nil {
			reply["verification_log"] = v.VerificationLog
		}
		return json.NewEncoder(cfg.Out).Encode(reply)
	}
	if o.json {
		return printJSON(cfg, v)
	}
	fmt.Fprintf(cfg.Out, "%s · goal %s · next %s\n", v.Task, v.Scope.Until, v.Next)
	if v.ReviewPolicyError != "" {
		fmt.Fprintln(cfg.Out, decl.Plain(v.ReviewPolicyError))
	}
	if v.TicketMarkdown != "" {
		fmt.Fprintln(cfg.Out, "Ticket Markdown:", decl.Plain(v.TicketMarkdown))
	}
	if v.TicketSource != nil {
		fmt.Fprintln(cfg.Out, "Source ticket:", decl.Plain(v.TicketSource.URL))
		if v.Next == "receipt" || v.Next == "done" {
			fmt.Fprintln(cfg.Out, "Delivery receipt: kitsu task receipt", v.Task)
		}
	}
	v.printResumeNotes(cfg.Out)
	if v.Scope.ReviewBase != "" {
		fmt.Fprintf(cfg.Out, "Review range %s..%s\n", v.Scope.ReviewBase, v.Scope.ReviewHead)
	}
	if v.Pause != nil {
		fmt.Fprintf(cfg.Out, "Paused before %s (%s). Resume with kitsu task next %s after extending the limit.\n", v.Pause.Pending, v.Pause.Reason, v.Task)
	}
	if v.Last != nil && (v.Last.Action != "review" || v.ReviewCurrent) {
		fmt.Fprintf(cfg.Out, "%s: %s\n", v.Last.Action, decl.Plain(v.Last.Note))
		if v.Last.Link != "" {
			fmt.Fprintln(cfg.Out, decl.Plain(v.Last.Link))
		}
	}
	if v.ManagedInterval != "" {
		fmt.Fprintf(cfg.Out, "Finish managed interval %s to resume daily action %s.\n", v.ManagedInterval, v.PendingDaily)
	}
	v.printReview(cfg.Out)
	v.printVerification(cfg.Out)
	if p := v.Publication; p != nil {
		fmt.Fprintf(cfg.Out, "Last publication check: %s · %s · %s · %s\n", decl.Plain(p.Target), p.Commit, p.Outcome, p.ObservedAt.Format(time.RFC3339))
	}
	if v.Next == "reconcile" {
		fmt.Fprintln(cfg.Out, "Recover the claimed worker. After it finishes or is cancelled, release its reservation with kitsu task release", v.Task, "--ticket", v.Claim.Ticket)
	}
	v.printClaim(cfg.Out)
	if v.CommitTarget != "" {
		fmt.Fprintln(cfg.Out, "Commit target:", decl.Plain(v.CommitTarget))
	}
	if v.Ticket != "" {
		fmt.Fprintln(cfg.Out, "Action ticket:", v.Ticket)
	}
	return nil
}

func (v harnessView) printReview(out io.Writer) {
	if v.Review == nil {
		return
	}
	label := "Review finding:"
	if v.PriorNotes != nil && v.ReviewCurrent && v.Last != v.Review {
		fmt.Fprintln(out, "Current review:", decl.Plain(v.Review.Note))
	}
	if !v.ReviewCurrent {
		fmt.Fprintf(out, "The latest review covers an earlier candidate. See kitsu task show %s --json for that review.\n", v.Task)
		label = "Earlier finding:"
	}
	for _, item := range v.Review.Findings {
		fmt.Fprintln(out, label, decl.Plain(item))
	}
}

func (v harnessView) printClaim(out io.Writer) {
	if v.ClaimCreated {
		fmt.Fprintln(out, "New reservation created.")
	}
	claim := v.Claim
	if claim == nil {
		return
	}
	role, settings := "Reviewer", claim.Reviewer
	fmt.Fprintln(out, "Claimed by:", decl.Plain(claim.Owner))
	if d := claim.Developer; d != nil {
		role, settings = "Developer", &d.SpecialistSettings
		fmt.Fprintf(out, "Developer launch %s. Recovery label %s.\n", v.DeveloperLaunch, d.LaunchLabel)
	}
	if claim.Worker != "" {
		fmt.Fprintln(out, "Native "+strings.ToLower(role)+":", decl.Plain(claim.Worker))
	}
	if settings != nil {
		writeRoleLine(out, fmt.Sprintf("%s (%s): %s, reasoning %s", role, settings.Host, decl.Plain(settings.Model.Model), decl.Plain(settings.Model.Effort)))
		for _, skill := range settings.Skills {
			writeRoleLine(out, "  "+decl.Plain(skill.Path)+" sha256 "+skill.SHA256)
		}
	}
}

func (v harnessView) verificationLog() *verificationLogRef {
	p := v.Proof
	if p == nil || p.FailedCheck != "" && p.GateFailure == nil {
		return nil
	}
	log := harness.RunResult{CheckRun: harness.CheckRun{ID: p.Run, Digest: p.LogDigest}, CheckID: p.CheckID, Outcome: p.Outcome, ExitStatus: p.ExitStatus, Detail: p.Detail}
	if p.GateFailure != nil {
		log = *p.GateFailure
	}
	if filepath.IsLocal(log.ID) && filepath.Base(log.ID) == log.ID {
		return &verificationLogRef{RunResult: log, Path: v.store.LogPath(log.ID)}
	}
	return nil
}

// Routine worker returns need a ticket and deciding evidence references.
// Full results, findings and inputs stay in task show across interruptions.
func (v harnessView) brief() map[string]any {
	reply := map[string]any{"task": v.Task, "next": v.Next, "verified": v.Verified, "repository_checks": v.Gate, "delivery": v.Delivery, "details": []string{"kitsu", "task", "show", v.Task, "--json"}}
	if v.TicketMarkdown != "" {
		reply["ticket_markdown"] = v.TicketMarkdown
	}
	if v.TicketSource != nil && (v.Next == "receipt" || v.Next == "done") {
		reply["delivery_receipt"] = []string{"kitsu", "task", "receipt", v.Task, "--json"}
	}
	if v.ReviewPolicyError != "" {
		reply["review_policy_error"] = v.ReviewPolicyError
	}
	if v.ManagedInterval != "" {
		reply["managed_interval"], reply["pending_daily_action"] = v.ManagedInterval, v.PendingDaily
	}
	if v.CommitTarget != "" {
		reply["commit_target"] = v.CommitTarget
	}
	if v.CurrentCommitTarget != "" {
		reply["current_commit_target"] = v.CurrentCommitTarget
	}
	if v.CommitEvidence != "" {
		reply["commit_evidence"] = v.CommitEvidence
	}
	if v.Publication != nil {
		reply["last_publication"] = v.Publication
	}
	if v.Completion != nil {
		reply["completed_delivery"] = v.Completion
	}
	v.addBriefLimits(reply)
	if v.Ticket != "" {
		reply["ticket"] = v.Ticket
	}
	v.addBriefClaim(reply)
	if v.Review != nil {
		reply["review_findings"] = len(v.Review.Findings)
		if v.Next == "review" && v.Claim != nil {
			addReviewChanges(reply, v.Review, v.Scope.Until, v.Candidate)
		}
		reply["review_current"] = v.ReviewCurrent
	}
	if v.Proof != nil {
		reply["behavior"] = v.Behavior
		reply["run_id"] = v.Proof.Run
		if v.Proof.FailedCheck != "" {
			reply["failed_check"] = v.Proof.FailedCheck
		}
	}
	return reply
}

func (v harnessView) addBriefLimits(reply map[string]any) {
	if v.Pause != nil || v.Claim != nil && (v.ReviewLimit > 0 || !v.Deadline.IsZero()) {
		reply["review_starts"], reply["review_limit"] = v.ReviewStarts, v.ReviewLimit
		if !v.Deadline.IsZero() {
			reply["deadline"] = v.Deadline
			if v.DeveloperLaunch == "" {
				reply["review_return_by"] = v.Deadline.Add(-v.FinishReserve)
			}
		}
	}
	if v.Pause != nil {
		reply["pause"], reply["candidate"], reply["resume"] = v.Pause, v.Candidate, []string{"kitsu", "task", "next", v.Task, "--brief"}
	}
}

func (v harnessView) addBriefClaim(reply map[string]any) {
	if v.ClaimCreated {
		reply["claim_created"] = true
	}
	if v.DeveloperLaunch != "" {
		reply["developer_launch"] = v.DeveloperLaunch
	}
	if v.Claim != nil {
		claim := *v.Claim
		if claim.Candidate != nil {
			reply["review_round"] = v.ReviewStarts
		}
		if claim.Candidate != nil && claim.Ticket == v.Ticket && v.Next == "review" {
			reply["review_input"] = []string{"kitsu", "task", "show", v.Task, "--review-ticket", v.Ticket}
		}
		claim.Candidate = nil
		reply["claim"] = claim
	}
}

func harnessProofValid(s *session, t *harness.Task) (bool, error) {
	return validAssertionRecord(s, t.Proof, store.Passed)
}

func validAssertionRecord(s *session, p *harness.Proof, outcome string) (bool, error) {
	if p == nil || (!p.PerRunInputs || !p.IndexChecked) || p.Outcome != outcome || !validProofLocation(p) || !filepath.IsLocal(p.Run) || filepath.Base(p.Run) != p.Run {
		return false, nil
	}
	records, damaged, err := s.store.Records(p.CheckID, p.Key)
	if err != nil || len(damaged) > 0 {
		return false, err
	}
	for _, r := range records {
		if r.RunID != p.Run {
			continue
		}
		if r.ID != p.CheckID || r.ContentKey != p.Key || r.Outcome != outcome || r.Tree != p.Candidate.Tree || !slices.Equal(r.Run, p.Argv) {
			return false, nil
		}
		digest, err := taskVerificationLog(s, r.RunID)
		return err == nil && digest == p.LogDigest, nil
	}
	return false, nil
}

func harnessGate(ctx context.Context, s *session, tree string, p *harness.Proof) (string, bool, error) {
	v, err := evidence.Judge(ctx, s.repo, s.store, tree, time.Now(), nil)
	if errors.Is(err, evidence.ErrNoDeclaration) {
		return "undeclared", true, nil
	}
	if err != nil {
		return "", false, err
	}
	if len(v.Checks) == 0 {
		return "none declared", true, nil
	}
	if !v.Verified || len(p.GateRuns) != len(v.Checks) {
		return "pending", false, nil
	}
	for _, check := range v.Checks {
		if !currentHarnessCheck(s, check, p.GateRuns[check.ID]) {
			return "pending", false, nil
		}
	}
	return "passed", true, nil
}

func runHarnessTest(ctx context.Context, s *session, l *stageLedger, t *harness.Task, o harnessOptions, v harnessView) error {
	if err := pinHarnessCommitTarget(ctx, s, t); err != nil {
		return err
	}
	if err := s.checkHarnessCommitTarget(ctx, t.CommitTarget); err != nil {
		return err
	}
	out := s.cfg.Err
	if o.brief {
		out = io.Discard
	}
	c := v.Candidate
	p := t.Proof
	reuse := false
	if p != nil && c.Index == p.Candidate.Index && c.SameBase(p.Candidate) && c.SameSource(p.Candidate) && slices.Equal(o.argv, p.Argv) && o.expect == p.Expect {
		var err error
		reuse, err = harnessProofValid(s, t)
		if err != nil {
			return err
		}
	}
	if !reuse {
		var err error
		p, err = executeHarnessAssertion(ctx, s, t, o, c, out)
		if err != nil {
			return err
		}
		t.RecordAssertion(p, o.replaceReason)
		// Keep the deciding run even when interrupted during the remaining checks.
		if err := l.write(harnessFile(t.ID), t); err != nil {
			return err
		}
	}
	if p.Outcome != store.Passed || t.NeedsReproduction() {
		return nil
	}
	_, found, err := s.repo.Object(ctx, c.Tree, decl.FileName)
	if err != nil {
		return err
	}
	if found {
		if passed, err := runHarnessGate(ctx, s, l, t, out); err != nil || !passed {
			return err
		}
	}

	return finishHarnessTest(ctx, s, t, c, o.ticket)
}

func finishHarnessTest(ctx context.Context, s *session, t *harness.Task, c harness.Snapshot, ticket string) error {
	p := t.Proof
	after, err := harnessSnapshot(ctx, s)
	if err != nil {
		return err
	}
	if c.Index != after.Index || !c.SameSource(after) || !c.SameBase(after) {
		return failf("candidate changed during verification; run task next")
	}
	if err := s.checkHarnessCommitTarget(ctx, t.CommitTarget); err != nil {
		return err
	}
	p.CommitTarget = t.CommitTarget
	if ticket != p.Ticket {
		p.CompletionTicket = ticket
		// A reused assertion leaves the event count unchanged. Consume its retry ticket.
		t.Generation++
	}
	t.ArchiveGateFailure()
	p.CheckedAt = time.Now().UTC()
	return nil
}

func executeHarnessAssertion(ctx context.Context, s *session, t *harness.Task, o harnessOptions, c harness.Snapshot, out io.Writer) (*harness.Proof, error) {
	check, key, shared, err := assertionCheck(ctx, s, t, o, c)
	if err != nil {
		return nil, err
	}
	record, err := s.executeJob(ctx, runner.Job{Check: check, Tree: c.Tree, Key: key, Out: out, Err: out, CheckInputs: s.checkHarnessInputs(c, t.CommitTarget)}, false)
	if err != nil {
		return nil, err
	}
	p := &harness.Proof{IndexChecked: true, PerRunInputs: true, CheckID: check.ID, Ticket: o.ticket, Candidate: c, Key: key, Run: record.RunID, Outcome: record.Outcome, ExitStatus: record.ExitStatus, Detail: sourceChangeSummary(record), Expect: o.expect, Argv: o.argv}
	p.LogDigest, err = taskVerificationLog(s, p.Run)
	if err != nil {
		return nil, err
	}
	if shared && p.Outcome == store.Passed {
		p.GateRuns = map[string]harness.CheckRun{check.ID: {ID: p.Run, Digest: p.LogDigest}}
	}
	if p.Outcome == store.Failed && record.ExitStatus == nil {
		p.Outcome = "unavailable"
	}
	return p, nil
}

func currentHarnessCheck(s *session, check evidence.Check, receipt harness.CheckRun) bool {
	if check.State != evidence.Satisfied || check.Deciding == nil || check.Deciding.RunID != receipt.ID {
		return false
	}
	digest, err := taskVerificationLog(s, receipt.ID)
	return err == nil && digest == receipt.Digest
}

func (v *harnessView) timing(t *harness.Task) {
	seen := map[string]bool{}
	actions := map[string]bool{}
	end := time.Now().UTC()
	for i := range t.Events {
		e := &t.Events[i]
		if actions[e.Action] {
			v.Retries++
		}
		actions[e.Action] = true
		v.Last = e
		if e.Actor != "" {
			seen[e.Actor] = true
		}
		if e.Action == "repair" {
			v.Repairs++
		}
		if e.Action != "intake" && v.FirstResult == nil {
			d := e.At.Sub(t.Created).Seconds()
			v.FirstResult = &d
		}
	}
	if v.Completion != nil {
		end = v.Completion.At
	} else if v.Next == "done" && v.Last != nil {
		end = v.Last.At
		if t.Proof != nil && t.Proof.CheckedAt.After(end) {
			end = t.Proof.CheckedAt
		}
	}
	v.Actors = len(seen)
	v.Elapsed = end.Sub(t.Created).Seconds()
}

func (v *harnessView) observeDelivery(ctx context.Context, s *session, t *harness.Task, headTree, parents string) error {
	c, p := v.Candidate, t.Proof
	if p == nil {
		return nil
	}
	if !c.SameSource(p.Candidate) || p.Candidate.Index != c.Index {
		v.Behavior = "stale"
		return nil
	}
	v.Behavior = p.Outcome
	if p.Outcome == store.Passed && (!p.PerRunInputs || !p.IndexChecked) {
		v.Behavior = "fresh per-run evidence required"
		return nil
	}
	passed, err := harnessProofValid(s, t)
	if err != nil || !passed {
		if v.Behavior == "passed" {
			v.Behavior = "evidence unavailable"
		}
		return err
	}
	v.Gate, v.Verified, err = harnessGate(ctx, s, c.Tree, p)
	if p.CheckedAt.IsZero() {
		v.Verified = false
	}
	if err != nil || !v.Verified {
		return err
	}
	v.Delivery = "verified working tree"
	if p.Candidate.CommittedAs(c, headTree, parents) {
		if target := t.AuthorizedCommitTarget(); target != "" {
			delivered, err := s.repo.DeliveredAt(ctx, target, c.Head)
			if err != nil {
				return err
			}
			if !delivered {
				v.Verified = false
				v.Delivery = "verified commit absent from " + target + "; inspect the checkout before verifying again"
				return nil
			}
		}
		v.Delivery = "committed"
		v.CommitEvidence = "commit_matches_verified_tree"
		if p.Candidate.Head == c.Head {
			v.CommitEvidence = "verified_existing_commit"
		}
		return nil
	}
	if !c.SameBase(p.Candidate) {
		v.Verified = false
		v.Delivery = "HEAD changed; verify before committing"
	}
	return nil
}

func runHarnessGate(ctx context.Context, s *session, l *stageLedger, t *harness.Task, out io.Writer) (bool, error) {
	p := t.Proof
	base := t.Start.Head
	if base == "" {
		var err error
		base, err = s.repo.EmptyTree(ctx)
		if err != nil {
			return false, err
		}
	}
	plan, err := verification.Build(ctx, s.repo, p.Candidate.Tree, base)
	if err != nil {
		return false, err
	}
	if p.GateRuns == nil {
		p.GateRuns = map[string]harness.CheckRun{}
	}
	p.CheckedAt = time.Time{}
	ids := plan.PriorityIDs()
	for _, id := range plan.AllIDs() {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		passed, err := runHarnessGateCheck(ctx, s, t, id, out)
		if err != nil {
			return false, err
		}
		if err := l.write(harnessFile(t.ID), t); err != nil || !passed {
			return false, err
		}
	}
	return true, nil
}

func runHarnessGateCheck(ctx context.Context, s *session, t *harness.Task, id string, out io.Writer) (bool, error) {
	p := t.Proof
	verdict, err := s.judgeWorktree(ctx, nil)
	if err != nil {
		return false, err
	}
	idx := slices.IndexFunc(verdict.Checks, func(check evidence.Check) bool { return check.ID == id })
	if idx < 0 || verdict.Tree != p.Candidate.Tree {
		return false, failf("declared check changed during verification")
	}
	s.warn(verdict.Warnings)
	if currentHarnessCheck(s, verdict.Checks[idx], p.GateRuns[id]) {
		return true, nil
	}
	checkInputs := s.checkHarnessInputs(p.Candidate, t.CommitTarget)
	if err := checkInputs(ctx); err != nil {
		return false, err
	}
	check := verdict.Checks[idx]
	record, err := s.executeJob(ctx, runner.Job{Check: check.Check, Key: check.Key, Tree: verdict.Tree, Out: out, Err: out, CheckInputs: checkInputs}, true)
	if err != nil {
		return false, err
	}
	digest, err := taskVerificationLog(s, record.RunID)
	if err != nil {
		return false, err
	}
	run := harness.CheckRun{ID: record.RunID, Digest: digest}
	if record.Outcome == store.Passed {
		p.GateRuns[id] = run
		if p.FailedCheck == id || p.GateFailure != nil && p.GateFailure.CheckID == id {
			t.ArchiveGateFailure()
		}
		return true, nil
	}
	t.ArchiveGateFailure()
	delete(p.GateRuns, id)
	p.GateFailure = &harness.RunResult{CheckRun: run, CheckID: id, Outcome: record.Outcome, ExitStatus: record.ExitStatus, Detail: sourceChangeSummary(record)}
	if record.Outcome == store.Failed && record.ExitStatus != nil {
		p.FailedCheck = id
	}
	return false, nil
}

func validProofLocation(p *harness.Proof) bool {
	if !filepath.IsLocal(p.CheckID) || filepath.Base(p.CheckID) != p.CheckID || strings.ContainsAny(p.CheckID, `/\`) {
		return false
	}
	if len(p.Key) != 40 && len(p.Key) != 64 {
		return false
	}
	_, err := hex.DecodeString(p.Key)
	return err == nil
}

// When the assertion is an existing check, execute its declared job once.
// That same observed run can satisfy both the task assertion and the gate.
func assertionCheck(ctx context.Context, s *session, t *harness.Task, o harnessOptions, c harness.Snapshot) (decl.Check, string, bool, error) {
	check := decl.Check{ID: "task-" + t.ID, Run: o.argv, Timeout: 10 * time.Minute}
	key := contextpack.Key([]any{"task-assertion", t.ID, c.Tree, c.Raw, o.expect, o.argv})
	policy, err := evidence.Declaration(ctx, s.repo, c.Tree)
	if errors.Is(err, evidence.ErrNoDeclaration) {
		return check, key, false, nil
	}
	if err != nil {
		return check, "", false, err
	}
	for _, declared := range policy.Checks {
		if slices.Equal(declared.Run, o.argv) {
			keys, err := evidence.Keys(ctx, s.repo, c.Tree, []decl.Check{declared})
			return declared, keys[declared.ID], true, err
		}
	}
	return check, key, false, nil
}
