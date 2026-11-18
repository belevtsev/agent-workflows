package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/runner"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// Run the saved assertion with the delivered commit checked out. A saved
// receipt describes that revision even if the developer subsequently moves on.
func bugReceipt(ctx context.Context, s *session, l *stageLedger, t *harness.Task, o harnessOptions) error {
	if t.Bug == nil {
		return failf("task %s has no imported bug ticket", t.ID)
	}
	r := t.Bug.Receipt
	if r == nil {
		view, err := observeHarness(ctx, s, t, false)
		if err != nil {
			return err
		}
		if view.Next != "receipt" || view.Delivery != "committed" || !view.Verified {
			return failf("no delivery receipt yet: next action is %s (%s)", view.Next, view.Delivery)
		}
		r, err = verifyBugCommit(ctx, s, t, view)
		if err != nil {
			return err
		}
		t.Bug.Receipt = r
		t.Events = append(t.Events, harness.Event{Action: "receipt", Note: "Verified local delivery", ObservedCommit: r.Commit, Candidate: view.Candidate, At: r.At})
		if err := l.write(harnessFile(t.ID), t); err != nil {
			return err
		}
	}
	if o.json || o.brief {
		return printJSON(s.cfg, r)
	}
	fmt.Fprintf(s.cfg.Out, "Verified local commit %s\nTicket: %s\nSource SHA-256: %s\nBehavior: %s\nReproduced: %s\nVerified on commit: %s\nRepository checks: %s\n", r.Commit, decl.Plain(r.Source.URL), r.Source.SHA256, decl.Plain(r.Expect), r.Reproduction.ID, r.OnCommit.ID, r.RepositoryChecks)
	return nil
}

func verifyBugCommit(ctx context.Context, s *session, t *harness.Task, v harnessView) (*harness.DeliveryReceipt, error) {
	red, p := t.Bug.Reproduction, t.Proof
	if delivery := t.Latest("commit"); delivery != nil && delivery.ObservedCommit != v.Candidate.Head {
		return nil, failf("return to delivered commit %s to issue its receipt", delivery.ObservedCommit)
	}
	if p.CommitTarget == "" || p.CommitTarget != t.CommitTarget || p.Candidate.Head == v.Candidate.Head {
		return nil, failf("no verified commit operation for this ticket on the current revision")
	}
	valid, err := validAssertionRecord(s, red, store.Failed)
	if err != nil {
		return nil, err
	}
	if !valid || red.ExitStatus == nil || *red.ExitStatus == 0 {
		return nil, failf("the original failing assertion evidence is unavailable")
	}
	if !t.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, t.Deadline)
		defer cancel()
	}
	key := contextpack.Key([]string{"ticket-delivery", t.ID, v.Candidate.Head, p.Run})
	check := decl.Check{ID: "receipt-" + t.ID, Run: p.Argv, Timeout: 10 * time.Minute}
	job := runner.Job{Check: check, Tree: v.Candidate.Tree, Key: key, Out: io.Discard, Err: io.Discard, CheckInputs: s.checkHarnessInputs(v.Candidate, t.CommitTarget)}
	if err := job.CheckInputs(ctx); err != nil {
		return nil, err
	}
	record, err := s.executeJob(ctx, job, false)
	if err != nil {
		return nil, err
	}
	if record.Outcome != store.Passed {
		return nil, failf("verification on commit %s was %s; inspect %s", v.Candidate.Head, record.Outcome, s.store.LogPath(record.RunID))
	}
	digest, err := taskVerificationLog(s, record.RunID)
	if err != nil {
		return nil, err
	}
	return &harness.DeliveryReceipt{
		Task: t.ID, Source: t.Bug.Source, Target: t.CommitTarget,
		Commit: v.Candidate.Head, Tree: v.Candidate.Tree, Expect: p.Expect, Argv: p.Argv,
		Reproduction: harness.FailureEvidence{
			CheckRun: harness.CheckRun{ID: red.Run, Digest: red.LogDigest},
			Tree:     red.Candidate.Tree, Head: red.Candidate.Head, ExitStatus: *red.ExitStatus,
		},
		BeforeCommit:     harness.CheckRun{ID: p.Run, Digest: p.LogDigest},
		OnCommit:         harness.CheckRun{ID: record.RunID, Digest: digest},
		RepositoryChecks: v.Gate, RepositoryRuns: p.GateRuns, At: time.Now().UTC(),
	}, nil
}

func checkBugExtension(task *harness.Task, scope harness.Scope) error {
	if task.Bug != nil && task.Bug.Receipt != nil {
		return failf("ticket delivery already has a final receipt; open a new task for further work")
	}
	if task.Bug != nil && (scope.Publish != "" || scope.PRBase != "") {
		return usagef("bug ticket delivery ends at the verified local commit; use a separate task to publish")
	}

	return nil
}
