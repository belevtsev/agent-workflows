package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

func bindReviewRangeFlags(f *flag.FlagSet, scope *harness.Scope) {
	for name, dest := range map[string]*string{"review-base": &scope.ReviewBase, "review-head": &scope.ReviewHead} {
		f.Func(name, "", func(rev string) error {
			if strings.TrimSpace(rev) == "" {
				return errors.New("review revision must be nonempty")
			}
			*dest = rev
			return nil
		})
	}
}

func resolveReviewRange(ctx context.Context, s *session, scope harness.Scope) (harness.Scope, harness.Snapshot, error) {
	base, _, err := s.repo.ReviewCommit(ctx, scope.ReviewBase)
	if err != nil {
		return scope, harness.Snapshot{}, failf("review base: %v", err)
	}
	head, tree, err := s.repo.ReviewCommit(ctx, scope.ReviewHead)
	if err != nil {
		return scope, harness.Snapshot{}, failf("review head: %v", err)
	}
	scope.ReviewBase, scope.ReviewHead = base, head
	return scope, harness.Snapshot{Head: head, Tree: tree}, nil
}

func harnessCandidate(ctx context.Context, s *session, t *harness.Task) (harness.Snapshot, error) {
	if t.Scope.ReviewBase == "" {
		return harnessSnapshot(ctx, s)
	}
	scope, candidate, err := resolveReviewRange(ctx, s, t.Scope)
	if err == nil && (scope != t.Scope || candidate != t.Start) {
		err = failf("recorded committed review range is unavailable")
	}
	return candidate, err
}

// Read the reservation as recorded. A read-only reviewer can open this view
// with its entire checkout mounted read-only. Recording the review checks the live candidate.
func printReservedReview(ctx context.Context, cfg Config, id, ticket string, brief bool) error {
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	l, err := readOnlyStageLedger(s.repo.GitDir)
	if err != nil {
		return err
	}
	defer l.close()
	var t harness.Task
	if err := l.read(harnessFile(id), &t); err != nil {
		return err
	}
	if !t.Valid(id) || t.Claim == nil || t.Claim.Candidate == nil || t.Claim.Ticket != ticket {
		return failf("no matching pinned review reservation; recover the claim with task next %s", id)
	}
	c := *t.Claim.Candidate
	if actionTicket(&t, "review", c) != ticket {
		return failf("review reservation does not match the recorded candidate")
	}
	assignment := "Review the reserved candidate read-only. Check the requested behavior and affected callers. " +
		"Return approval or each unresolved finding with severity, location and a reproducible case to the coordinator."
	if brief {
		assignment = "Review the reserved candidate read-only against the request and criteria. Check affected callers and report unresolved findings with severity, location and a reproducer."
	}
	input := map[string]any{
		"task": id, "request": t.Request, "scope": t.Scope,
		"reserved_candidate": c, "review_round": t.StartedReviews(),
	}
	prior := t.Latest("review")
	guidance, err := reservedReviewComparison(ctx, s, t.Scope, prior, c, input)
	if err != nil {
		return err
	}

	input["assignment"] = assignment + guidance
	if t.Claim.Reviewer != nil {
		input["reviewer"] = t.Claim.Reviewer
	}
	addNativeReviewInput(input, &t, c, assignment+guidance)
	if !t.Deadline.IsZero() {
		input["review_return_by"] = t.Deadline.Add(-t.FinishReserve)
	}
	if prior != nil {
		input["prior_findings"] = prior.Findings
		addReviewChanges(input, prior, t.Scope.Until, c)
	}
	if brief {
		input["details"] = []string{"kitsu", "task", "show", id, "--review-ticket", ticket}
	} else {
		addReviewWorkHistory(input, &t, prior)
	}
	addReviewFeatureEvidence(ctx, s, &t, c, input)
	return json.NewEncoder(cfg.Out).Encode(input)
}

func addReviewWorkHistory(input map[string]any, t *harness.Task, prior *harness.Event) {
	for _, action := range []string{"intake", "plan"} {
		if e := t.Latest(action); e != nil {
			input[action] = e.Note
		}
	}
	if work := t.Latest("implement", "repair"); work != nil {
		input["reported_"+work.Action] = work.Note
		input["reported_work_tree"] = work.Candidate.Tree
	}
	if prior != nil {
		input["prior_review"] = prior.Note
	}
	if p := t.Proof; p != nil && (p.Outcome != "passed" || p.GateFailure != nil || p.FailedCheck != "") {
		input["prior_assertion"] = p
	}
}

func addReviewFeatureEvidence(ctx context.Context, s *session, t *harness.Task, c harness.Snapshot, input map[string]any) {
	feature := t.Feature
	if feature == nil || feature.Draft {
		return
	}
	input["feature_revision"] = feature.Revision()
	input["feature_goal"], input["feature_scope"] = feature.Goal, feature.Scope
	input["acceptance_criteria"] = feature.Criteria
	input["verification"] = reservedFeatureVerification(ctx, s, t, c)
}

func reservedFeatureVerification(ctx context.Context, s *session, t *harness.Task, c harness.Snapshot) map[string]any {
	result := map[string]any{"status": "unverified"}
	p := t.Proof
	if p == nil || p.Candidate != c || p.CheckedAt.IsZero() || checkHarnessPolicy(ctx, s, t, c.Tree) != nil {
		return result
	}
	valid, err := harnessProofValid(s, t)
	if err != nil || !valid {
		return result
	}
	view := harnessView{Candidate: c}
	if err := view.observeDailyGate(ctx, s, t); err != nil || !view.Verified {
		return result
	}
	runs := make(map[string]string, len(p.GateRuns))
	for id, run := range p.GateRuns {
		runs[id] = run.ID
	}
	return map[string]any{"status": "passed", "assertion_run": p.Run, "repository_runs": runs}
}

// Supply a full comparison on first review or when an incremental comparison
// alone cannot establish the new scope. All endpoints belong to the reservation.
func reservedReviewComparison(ctx context.Context, s *session, scope harness.Scope, prior *harness.Event, c harness.Snapshot, input map[string]any) (string, error) {
	if base, head := scope.ReviewBase, scope.ReviewHead; base != "" {
		input["review_log"] = []string{"git", "--no-replace-objects", "log", "--format=%H", "--no-notes", "--no-show-signature", base + ".." + head, "--"}
		input["review_diff"] = reservedDiff(base, head)
		return " Follow review_log and review_diff and read affected files from the reserved candidate tree.", nil
	}
	if prior != nil && prior.Candidate.SameBase(c) && (!prior.ReviewIndex || scope.Until == "review") {
		return "", nil
	}
	base := c.Head
	if base == "" {
		var err error
		base, err = s.repo.EmptyTreeID(ctx)
		if err != nil {
			return "", err
		}
	}
	input["review_diff"] = reservedDiff(base, c.Tree)
	if scope.Until == "review" && c.Tree != c.Index {
		input["review_staged_diff"] = reservedDiff(base, c.Index)
		return " Use review_staged_diff for the staged patch or review_diff for the working tree, according to the request.", nil
	}
	return " Use review_diff for the reserved candidate.", nil
}

func reservedDiff(before, after string) []string {
	return []string{"git", "--no-replace-objects", "diff", "--no-ext-diff", "--no-textconv", before, after, "--"}
}

func addReviewChanges(reply map[string]any, prior *harness.Event, until string, candidate harness.Snapshot) {
	before, after := prior.Candidate.Tree, candidate.Tree
	if prior.ReviewIndex && until != "review" {
		before = prior.Candidate.Index
	}
	if until == "review" && prior.Candidate.Index != candidate.Index {
		reply["review_index_changes"] = reservedDiff(prior.Candidate.Index, candidate.Index)
	}
	reply["review_changes"] = reservedDiff(before, after)
	reply["review_scope_changed"] = prior.ReviewIndex && until != "review"
	reply["review_base_changed"] = !prior.Candidate.SameBase(candidate)
	if prior.Candidate.Tree == candidate.Tree && prior.Candidate.Raw != candidate.Raw {
		reply["review_raw_only_changes"] = true
	}
}
