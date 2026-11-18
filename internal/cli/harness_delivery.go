package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

func performHarnessCommit(ctx context.Context, s *session, l *stageLedger, t *harness.Task, o harnessOptions, v harnessView) error {
	c, p := v.Candidate, t.Proof
	t.CommitTarget, p.CommitTarget = v.CommitTarget, v.CommitTarget
	t.CommitAttempt = &harness.CommitAttempt{Before: c, Assertion: harness.CheckRun{ID: p.Run, Digest: p.LogDigest}}
	if err := l.write(harnessFile(t.ID), t); err != nil {
		return err
	}
	result, err := s.repo.CommitStaged(ctx, s.store.Tmp(), c.Tree, c.Head, c.MergeHeads, v.CommitTarget, o.note)
	if result.Commit == "" {
		return err
	}
	t.CommitAttempt.Commit, t.CommitAttempt.Tree = result.Commit, result.Tree
	if err != nil {
		return errors.Join(err, l.write(harnessFile(t.ID), t))
	}
	now := time.Now().UTC()
	t.Events = append(t.Events, harness.Event{Action: "commit", Note: o.note, Ticket: o.ticket, Candidate: c, ObservedCommit: result.Commit, At: now})
	if t.Bug == nil && t.Scope.Publish == "" {
		v.Candidate.Head, v.Candidate.MergeHeads = result.Commit, ""
		v.CommitEvidence = "commit_matches_verified_tree"
		t.Completion = v.deliveryCompletion(now)
	}
	return nil
}

func (v harnessView) deliveryCompletion(at time.Time) *harness.Completion {
	return &harness.Completion{
		Candidate: v.Candidate, Target: v.CommitTarget, At: at,
		CommitEvidence: v.CommitEvidence, RepositoryChecks: v.Gate, Reviewed: v.ReviewCurrent,
	}
}

func saveObservedDelivery(l *stageLedger, t *harness.Task, v harnessView) error {
	if t.Completion != nil || v.Completion == nil || v.Completion.Candidate.Head == "" {
		return nil
	}
	t.Completion = v.Completion
	return l.write(harnessFile(t.ID), t)
}

// After Git advances, any remaining work is tied to the resulting commit.
// An interrupted attempt without a recorded result can be matched only
// against its original verified tree and ordered parents.
func (v *harnessView) checkCommitAttempt(ctx context.Context, s *session, t *harness.Task) (bool, error) {
	a, recorded := t.AttemptedCommit()
	candidate := v.Candidate
	if a == nil || !recorded && candidate.SameBase(a.Before) {
		return false, nil
	}
	v.CommitTarget = t.AuthorizedCommitTarget()
	target, err := s.repo.CommitTarget(ctx)
	if err != nil {
		return false, err
	}
	if candidate.Head != "" && target == v.CommitTarget && candidate.MergeHeads == "" {
		tree, parents, err := s.repo.CommitContents(ctx, candidate.Head)
		if err != nil {
			return false, err
		}
		want := a.Before.Tree
		if a.Commit != "" {
			want = a.Tree
		}
		if (a.Commit == "" || a.Commit == candidate.Head) && tree == want && parents == a.Before.Parents() && tree == candidate.Tree && tree == candidate.Index {
			return true, nil
		}
	}
	v.Next, v.Verified = "blocked", false
	v.Delivery = "Git advanced during delivery. Inspect the authorized commit target before continuing."
	if a.Commit != "" {
		v.Delivery = fmt.Sprintf("Git created %s. Return to this commit with matching staged and working contents, or open a new task.", a.Commit)
	}
	return false, nil
}
