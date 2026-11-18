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
func printReservedReview(ctx context.Context, cfg Config, id, ticket string) error {
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
	input := map[string]any{
		"task": id, "request": t.Request, "scope": t.Scope,
		"reserved_candidate": c, "review_round": t.StartedReviews(),
	}
	if base, head := t.Scope.ReviewBase, t.Scope.ReviewHead; base != "" {
		assignment += " Follow review_log and review_diff and read affected files from the reserved candidate tree."
		input["review_log"] = []string{"git", "--no-replace-objects", "log", "--format=%H", "--no-notes", "--no-show-signature", base + ".." + head, "--"}
		input["review_diff"] = []string{"git", "--no-replace-objects", "diff", "--no-ext-diff", "--no-textconv", base, head, "--"}
	}
	input["assignment"] = assignment
	if t.Claim.Reviewer != nil {
		input["reviewer"] = t.Claim.Reviewer
	}
	if !t.Deadline.IsZero() {
		input["review_return_by"] = t.Deadline.Add(-t.FinishReserve)
	}
	for _, action := range []string{"intake", "plan"} {
		if e := t.Latest(action); e != nil {
			input[action] = e.Note
		}
	}
	if work := t.Latest("implement", "repair"); work != nil {
		input["reported_"+work.Action] = work.Note
		input["reported_work_tree"] = work.Candidate.Tree
	}
	if prior := t.Latest("review"); prior != nil {
		input["prior_review"], input["prior_findings"] = prior.Note, prior.Findings
		addReviewChanges(input, prior, t.Scope.Until, c)
	}
	if p := t.Proof; p != nil && (p.Outcome != "passed" || p.GateFailure != nil || p.FailedCheck != "") {
		input["prior_assertion"] = p
	}
	return json.NewEncoder(cfg.Out).Encode(input)
}

func addReviewChanges(reply map[string]any, prior *harness.Event, until string, candidate harness.Snapshot) {
	before, after := prior.Candidate.Tree, candidate.Tree
	if prior.ReviewIndex && until != "review" {
		before = prior.Candidate.Index
	}
	if until == "review" && prior.Candidate.Index != candidate.Index {
		reply["review_index_changes"] = []string{"git", "diff", prior.Candidate.Index, candidate.Index, "--"}
	}
	reply["review_changes"] = []string{"git", "diff", before, after, "--"}
	reply["review_scope_changed"] = prior.ReviewIndex && until != "review"
	reply["review_base_changed"] = !prior.Candidate.SameBase(candidate)
	if prior.Candidate.Tree == candidate.Tree && prior.Candidate.Raw != candidate.Raw {
		reply["review_raw_only_changes"] = true
	}
}
