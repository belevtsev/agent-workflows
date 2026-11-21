package cli

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/workflowrunner"
)

func workflowCheckResults(ids []string, result workflowrunner.Receipt) bool {
	if result.Role != "" || len(result.Changes) != 0 || len(result.Checks) != len(ids) {
		return false
	}
	for i, check := range result.Checks {
		if check.ID != ids[i] || len(check.Log) > 128<<10 || !validWorkflowCheckResult(check) {
			return false
		}
	}
	return true
}

func validWorkflowCheckResult(check workflowrunner.CheckResult) bool {
	switch check.Outcome {
	case "passed":
		return check.Exit != nil && *check.Exit == 0
	case "test_failed":
		return check.TestID != "" && check.Exit != nil && *check.Exit > 0
	case "failed":
		return check.Exit != nil && *check.Exit > 0
	case "error":
		return true
	}
	return false
}

func acceptWorkflowChecks(ctx context.Context, s *session, l *stageLedger, task *harness.Task, got workflowrunner.Receipt) error {
	w, a := task.Workflow, task.Workflow.Active
	if !workflowCheckResults(a.Checks, got) {
		return completeWorkflowFailure(l, task, "runner did not return all required check outcomes")
	}
	c, err := applyWorkflowFiles(ctx, s, l, task, nil)
	if err != nil {
		return err
	}
	if err := checkHarnessPolicy(ctx, s, task, c.Tree); err != nil {
		return err
	}
	if err := s.checkHarnessCommitTarget(ctx, task.CommitTarget); err != nil {
		return err
	}
	if a.Action == "baseline" {
		return acceptWorkflowBaseline(ctx, s, l, task, got.Checks[0])
	}

	var proof, failed *harness.Proof
	passing := map[string]harness.CheckRun{}
	for _, check := range got.Checks {
		p, declared, err := retainWorkflowCheck(ctx, s, task, check, task.Scope.Commit)
		if err != nil {
			return err
		}
		if proof == nil {
			proof = p
		}
		if declared && p.Outcome == store.Passed {
			passing[check.ID] = harness.CheckRun{ID: p.Run, Digest: p.LogDigest}
		}
		if p.Outcome != store.Passed && failed == nil {
			failed = p
		}
	}
	proof.GateRuns = passing
	if failed != nil && failed != proof {
		proof.FailedCheck = failed.CheckID
		proof.GateFailure = &harness.RunResult{
			CheckRun: harness.CheckRun{ID: failed.Run, Digest: failed.LogDigest}, CheckID: failed.CheckID,
			Outcome: failed.Outcome, ExitStatus: failed.ExitStatus,
		}
	}
	task.RecordAssertion(proof, "additional reviewed assertion")
	w.Verification = &harness.WorkflowEvidence{Attempt: a.ID, Job: a.Job, Candidate: a.Candidate, Checks: a.Checks}
	if err := workflowVerificationOutcome(task); err != nil {
		return err
	}
	finishWorkflowAttempt(task, "complete")
	return l.write(harnessFile(task.ID), task)
}

func acceptWorkflowBaseline(ctx context.Context, s *session, l *stageLedger, t *harness.Task, got workflowrunner.CheckResult) error {
	w, a := t.Workflow, t.Workflow.Active
	p, _, err := retainWorkflowCheck(ctx, s, t, got, false)
	if err != nil {
		return err
	}
	w.Baselines = append(w.Baselines, *p)
	w.BaselineReady = w.Plan.Baseline == "pass" && got.Outcome == "passed" ||
		w.Plan.Baseline == "fail" && got.Outcome == "test_failed" && got.TestID == w.Test.TestID
	if !w.BaselineReady {
		return completeWorkflowFailure(l, t, "baseline did not produce the specified behavioral test outcome; inspect the retained run")
	}
	t.Events = append(t.Events, workflowEvent(a, a.Candidate, "Baseline confirmed by the reviewed test reporter."))
	finishWorkflowAttempt(t, "complete")
	return l.write(harnessFile(t.ID), t)
}

// Retain logs in the normal evidence store for diagnostics and the local
// commit hook. Only a matching runner receipt authorizes the configured FSM.
func retainWorkflowCheck(ctx context.Context, s *session, t *harness.Task, got workflowrunner.CheckResult, gate bool) (*harness.Proof, bool, error) {
	a := t.Workflow.Active
	policy := s.workflowRunner.Offer().Policy
	i := slices.IndexFunc(policy.Checks, func(c workflowrunner.Check) bool { return c.ID == got.ID })
	if i < 0 {
		return nil, false, failf("runner result names an unreviewed check")
	}
	argv := policy.Checks[i].Argv
	expect := "reviewed check " + got.ID
	for _, assertion := range []*harness.WorkflowAssertion{t.Workflow.Test, t.Workflow.Intake} {
		if assertion != nil && slices.Equal(argv, assertion.Argv) {
			expect = assertion.Expect
		}
	}
	p := &harness.Proof{
		Ticket: a.Ticket, Candidate: a.Candidate, Argv: argv, Expect: expect, CheckID: got.ID, ExitStatus: got.Exit,
		PerRunInputs: true, IndexChecked: true, CommitTarget: t.CommitTarget, RepositoryPolicy: t.RepositoryPolicy,
		CheckedAt: time.Now().UTC(), Key: contextpack.Key([]string{"runner", a.Job, got.ID}),
	}
	rec := store.Record{ID: got.ID, Run: argv, Tree: a.Candidate.Tree, ContentKey: p.Key, StartedAt: a.Started}
	declared := false
	if gate {
		v, err := evidence.Judge(ctx, s.repo, s.store, a.Candidate.Tree, time.Now(), nil)
		if err != nil && !errors.Is(err, evidence.ErrNoDeclaration) {
			return nil, false, err
		}
		if v != nil {
			for _, c := range v.Checks {
				if c.ID == got.ID && slices.Equal(c.Run, argv) {
					p.Key, rec.ContentKey, rec.Paths, declared = c.Key, c.Key, c.Paths, true
					break
				}
			}
		}
	}
	outcome := store.Interrupted
	switch got.Outcome {
	case "passed":
		outcome = store.Passed
	case "test_failed", "failed":
		outcome = store.Failed
	}
	id, err := store.NewRunID(a.Started)
	if err != nil {
		return nil, false, err
	}
	rec.RunID = id
	run, err := s.store.Begin(ctx, rec)
	if err != nil {
		return nil, false, err
	}
	logErr := store.WriteFile(s.store.LogPath(id), got.Log, 0o600)
	_, err = run.Finish(ctx, func(r *store.Record) {
		r.Outcome, r.ExitStatus, r.EndedAt = outcome, got.Exit, &p.CheckedAt
		if logErr != nil {
			r.Outcome = store.Interrupted
		}
	})
	if err := errors.Join(err, logErr); err != nil {
		return nil, false, err
	}
	p.Run, p.LogDigest, p.Outcome = id, workflowrunner.BytesDigest(got.Log), outcome
	return p, declared, nil
}

func savedWorkflowReceipt(s *session, task *harness.Task, proof *harness.WorkflowEvidence, kind string) (workflowrunner.Receipt, bool) {
	var zero workflowrunner.Receipt
	if proof == nil || !nativeSessionID.MatchString(proof.Attempt) {
		return zero, false
	}
	w := task.Workflow
	client := s.workflowRunner
	if client == nil {
		var err error
		client, err = workflowrunner.Load(s.repo.Root)
		if err != nil {
			return zero, false
		}
	}
	if client.Authority() != w.Authority {
		return zero, false
	}
	l, err := readOnlyStageLedger(s.repo.GitDir)
	if err != nil {
		return zero, false
	}
	defer l.close()
	var signed workflowrunner.Signed
	if err := l.read(workflowReceiptFile(proof.Attempt), &signed); err != nil {
		return zero, false
	}
	got, err := client.Verify(signed, proof.Attempt, workflowBinding(task, proof.Candidate, kind, proof.Checks))
	return got, err == nil && got.Job == proof.Job && got.Policy == w.Policy && got.Profile == w.Profile &&
		got.Stopped && got.Status == "returned"
}

func currentWorkflowProof(s *session, t *harness.Task) bool {
	w, proof := t.Workflow, t.Proof
	signed, ok := savedWorkflowReceipt(s, t, w.Verification, "verify")
	if !ok || proof == nil || proof.Candidate != w.Verification.Candidate || !workflowCheckResults(w.Verification.Checks, signed) {
		return false
	}
	return len(signed.Checks) > 0 && !slices.ContainsFunc(signed.Checks, func(check workflowrunner.CheckResult) bool {
		return check.Outcome != "passed"
	})
}

func currentWorkflowReview(s *session, t *harness.Task, event *harness.Event) bool {
	proof := t.Workflow.Reviewed
	signed, ok := savedWorkflowReceipt(s, t, proof, "review")
	if !ok || event == nil || event.Candidate != proof.Candidate || event.Actor != signed.Session {
		return false
	}
	settings := t.Workflow.Roles["reviewer"]
	var result nativeReviewReport
	return signed.AgentType == "kitsu-reviewer" && signed.Role == "reviewer" && signed.Host == settings.Host &&
		roles.ObservedModelMatches(settings.Host, settings.Model.Model, signed.Model) &&
		(settings.Model.Effort == "" || signed.Effort == settings.Model.Effort) &&
		len(signed.Changes) == 0 && decodeWorkflowJSON(signed.Result, &result) == nil && result.valid() &&
		event.Note == result.Note && slices.Equal(event.Findings, result.Findings)
}

func (v *harnessView) requireSignedWorkflowReview(s *session, t *harness.Task) {
	v.WorkflowReview = v.ReviewCurrent && currentWorkflowReview(s, t, v.Review)
	v.ReviewCurrent = v.WorkflowReview
}

func workflowVerificationStatus(s *session, t *harness.Task) (string, bool) {
	if !currentWorkflowProof(s, t) {
		return "runner evidence stale or unavailable", false
	}
	if t.RepositoryPolicy == undeclaredPolicy {
		return undeclaredPolicy, true
	}
	return "passed", true
}
