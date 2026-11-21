package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/workflowrunner"
)

func workflowReceiptFile(attempt string) string {
	return "runner-result-" + attempt + ".json"
}

func workflowIntakeKey(t *harness.Task) string {
	return workflowrunner.Digest([]any{t.ID, t.Created, t.Request, t.Start, t.Workflow.Intake, t.Workflow.Roles})
}

func workflowBinding(t *harness.Task, c harness.Snapshot, kind string, ids []string) string {
	w := t.Workflow
	return workflowrunner.Digest([]any{workflowIntakeKey(t), t.Scope, t.RepositoryPolicy, w.Test, w.Plan, w.Policy, kind, ids, c})
}

func reserveWorkflowAttempt(ctx context.Context, s *session, l *stageLedger, t *harness.Task, step workflowStep) (workflowStep, error) {
	w, v := t.Workflow, step.view
	kind, role := v.Next, harness.WorkflowRole(v.Next)
	settings := w.Roles[role]
	if err := validateWorkflowSkills(settings); err != nil {
		return blockedWorkflowStep(step, err.Error()), nil
	}
	rework := w.IsRework(kind)
	if rework && w.Rework >= w.ReworkMax {
		return blockedWorkflowStep(step, "Rework limit reached. Inspect the findings and explicitly extend the rework limit if needed."), nil
	}
	if err := checkHarnessPolicy(ctx, s, t, v.Candidate.Tree); err != nil {
		return blockedWorkflowStep(step, err.Error()), nil
	}
	if role == "quality-engineer" && len(w.TestPolicy) == 0 || role == "developer" && len(s.workflowRunner.Policy.CodePaths) == 0 {
		return blockedWorkflowStep(step, "The runner policy must approve this role's writable paths."), nil
	}
	var err error
	if kind == "verify" {
		v, err = stageWorkflowVerification(ctx, s, t, v)
		if err != nil {
			return step, err
		}
	}
	files, err := workflowFiles(ctx, s, t, v.Candidate, kind == "verify" && t.Scope.Commit)
	if err != nil {
		return step, err
	}
	a, err := prepareWorkflowAttempt(l, s, t, v, settings, role, rework, files)
	if err != nil {
		return step, err
	}
	w.Active = a
	job, err := prepareWorkflowJob(ctx, s, l, t, v, settings, files)
	if err != nil {
		return step, err
	}
	a.Checks = job.Checks
	a.Binding = workflowBinding(t, a.Candidate, kind, a.Checks)
	job.Binding = a.Binding
	a.Job = workflowrunner.Digest(job)
	if rework {
		w.Rework++
	}
	if role == "reviewer" {
		t.ReviewStarts = t.StartedReviews() + 1
	}
	if err := l.write(workflowActiveFile, workflowWorktreeClaim{Task: t.ID, Attempt: a.ID}); err != nil {
		return step, err
	}
	if err := l.write(harnessFile(t.ID), t); err != nil {
		return step, err
	}
	step.attempt, step.job, step.deadline = a, job, job.Deadline
	return step, nil
}

func prepareWorkflowAttempt(l *stageLedger, s *session, t *harness.Task, v harnessView,
	settings roles.SpecialistSettings, role string, rework bool, files []workflowrunner.File,
) (*harness.WorkflowAttempt, error) {
	w := t.Workflow
	var err error
	a := &harness.WorkflowAttempt{ID: rand.Text(), Role: role, Host: settings.Host, Action: v.Next, Ticket: v.Ticket,
		Candidate: v.Candidate, Rework: rework, Started: time.Now().UTC()}
	if role == "developer" || role == "quality-engineer" {
		a.Shared, err = workflowSharedFingerprints(s.repo.Root, w.Plan.Shared)
		if err != nil {
			return nil, err
		}
	}
	if role == "developer" && w.PreCode == nil {
		if err := l.write(workflowBaselineFile(t.ID), files); err != nil {
			return nil, err
		}
		w.PreCode, w.PreCodeSources = &a.Candidate, map[string]string{"bundle": workflowFilesKey(files)}
	}
	return a, nil
}

func prepareWorkflowJob(ctx context.Context, s *session, l *stageLedger, t *harness.Task, v harnessView,
	settings roles.SpecialistSettings, files []workflowrunner.File,
) (*workflowrunner.Job, error) {
	w, a := t.Workflow, t.Workflow.Active
	kind, role := a.Action, a.Role
	var err error
	deadline := a.Started.Add(30 * time.Minute)
	if !t.Deadline.IsZero() && t.Deadline.Before(deadline) {
		deadline = t.Deadline
	}
	job := &workflowrunner.Job{Task: t.ID, Intake: workflowIntakeKey(t), Attempt: a.ID, Kind: kind,
		Policy: w.Policy, Profile: w.Profile, Deadline: deadline, Files: files,
		Role: role, Host: settings.Host, Model: settings.Model.Model, Effort: settings.Model.Effort}
	if role != "" {
		job.ReadOnly = role == "architect" || role == "reviewer"
		for _, ref := range settings.Skills {
			job.Skills = append(job.Skills, workflowrunner.Skill{ID: ref.ID, SHA256: ref.SHA256})
		}
		job.Prompt, err = workflowAssignment(ctx, s, t, v)
		job.Schema = workflowSchema(role)
		if len(job.Prompt) > 128<<10 {
			return nil, failf("role assignment exceeds 128 KiB")
		}
	} else {
		job.ReadOnly = true
		job.Checks, err = reviewedWorkflowChecks(ctx, s, t, v.Candidate, kind)
		if err == nil && kind == "baseline" {
			job.Files, err = workflowBaselineFiles(l, t, files)
		}
		if err == nil {
			err = workflowCheckInputs(s.workflowRunner.Policy, job.Checks, job.Files)
		}
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

func launchWorkflowAttempt(ctx context.Context, s *session, o workflowOptions, step workflowStep) error {
	a := step.attempt
	if !nativeSessionID.MatchString(a.ID) || a.Job == "" || a.Binding == "" {
		return failf("saved attempt has no supervised runner reservation")
	}
	var signed workflowrunner.Signed
	var got workflowrunner.Receipt
	var err error
	if step.job != nil {
		runCtx, cancel := context.WithDeadline(ctx, step.deadline)
		defer cancel()
		fmt.Fprintf(s.cfg.Err, "Kitsu task %s · %s\n", o.id, a.Action)
		signed, got, err = s.workflowRunner.Run(runCtx, *step.job)
	} else {
		signed, got, err = s.workflowRunner.Resume(ctx, a.ID, a.Binding, a.Job)
	}
	if err != nil {
		return failf("runner attempt %s remains reserved: %v. Resume to check its status.", a.ID, err)
	}
	l, err := openStageLedgerWait(ctx, s, ledgerWait)
	if err != nil {
		return err
	}
	defer l.close()
	t, err := readWorkflow(l, o.id)
	if err != nil {
		return err
	}
	w := t.Workflow
	if current := w.Active; current == nil || current.ID != a.ID || current.Job != got.Job ||
		got.Binding != workflowBinding(t, a.Candidate, a.Action, a.Checks) {
		return failf("runner result no longer matches the saved task reservation")
	}
	if got.Status != "returned" {
		if !got.Release {
			return failf("runner confirmed attempt %s stopped; the operator must authorize release before retrying", a.ID)
		}
		return completeWorkflowFailure(l, t, "runner stopped without a completed result")
	}
	if err := l.write(workflowReceiptFile(a.ID), signed); err != nil {
		return err
	}
	if a.Role == "" {
		return acceptWorkflowChecks(ctx, s, l, t, got)
	}
	return acceptWorkflowWorker(ctx, s, l, t, got)
}

func releaseWorkflowAttempt(ctx context.Context, s *session, o workflowOptions) error {
	l, err := readOnlyStageLedger(s.repo.GitDir)
	if err != nil {
		return err
	}
	t, err := readWorkflow(l, o.id)
	l.close()
	if err != nil {
		return err
	}
	if err := checkWorkflowAuthority(s.workflowRunner, t); err != nil {
		return err
	}
	a := t.Workflow.Active
	if a == nil || a.ID != o.release || a.Binding == "" {
		return failf("release must name a saved supervised attempt")
	}
	_, got, err := s.workflowRunner.Status(ctx, a.ID, a.Binding)
	if err != nil {
		return err
	}
	if got.Job != a.Job || !got.Stopped || !got.Release || got.Status != "stopped" && got.Status != "failed" && got.Status != "returned" {
		return failf("runner operator has not authorized release of this stopped attempt")
	}
	l, err = openStageLedgerWait(ctx, s, ledgerWait)
	if err != nil {
		return err
	}
	defer l.close()
	t, err = readWorkflow(l, o.id)
	if err != nil {
		return err
	}
	if current := t.Workflow.Active; current == nil || current.ID != a.ID || current.Binding != got.Binding || current.Job != got.Job {
		return failf("attempt changed while getting release authorization")
	}
	finishWorkflowAttempt(t, "released")
	t.Generation++
	t.Workflow.Problem = "Runner released the outstanding attempt. Inspect the candidate, then resume with --retry."
	return l.write(harnessFile(t.ID), t)
}

func acceptWorkflowWorker(ctx context.Context, s *session, l *stageLedger, t *harness.Task, got workflowrunner.Receipt) error {
	w, a := t.Workflow, t.Workflow.Active
	settings := w.Roles[a.Role]
	if !matchesWorkflowWorker(t, a, settings, got) {
		return completeWorkflowFailure(l, t, "runner did not establish a fresh worker with the assigned host, role, model and effort")
	}
	a.Session, a.ObservedModel, a.ObservedEffort, a.Usage = got.Session, got.Model, got.Effort, got.Usage
	if a.Role == "reviewer" {
		return acceptSupervisedReview(ctx, s, l, t, got)
	}
	var result workflowReturn
	if decodeWorkflowJSON(got.Result, &result) != nil || strings.TrimSpace(result.Note) == "" || len(result.Note) > 12<<10 {
		return completeWorkflowFailure(l, t, "worker did not return a bounded role result")
	}
	switch result.Status {
	case "blocked":
		return completeWorkflowFailure(l, t, result.Note)
	case "test_defect":
		if a.Role == "developer" && len(got.Changes) == 0 {
			w.Pending = workflowCorrections(w.Pending, "tests", "repair")
			finishWorkflowAttempt(t, "test_defect")
			t.Events = append(t.Events, workflowEvent(a, a.Candidate, result.Note))
			return l.write(harnessFile(t.ID), t)
		}
		fallthrough
	default:
		if result.Status != "complete" {
			return completeWorkflowFailure(l, t, "worker could not complete its role within the saved scope")
		}
	}
	if err := validateCompleteWorkerResult(s, t, result); err != nil {
		return completeWorkflowFailure(l, t, err.Error())
	}
	c, err := applyWorkflowFiles(ctx, s, l, t, got.Changes)
	if err != nil {
		if w.Active == nil {
			return err
		}
		return errors.Join(err, failf("runner result %s is retained; inspect the accepted checkout before resuming", a.ID))
	}
	if a.Role == "developer" && a.Rework && c.SameSource(a.Candidate) {
		return completeWorkflowFailure(l, t, "repair returned the same candidate")
	}
	if a.Role == "architect" {
		w.Plan = result.Plan
	} else if a.Role == "quality-engineer" {
		w.Test, w.BaselineReady = result.Assertion, false
	}
	w.Candidate = &c
	w.ResolveCorrection(a.Action)
	t.Events = append(t.Events, workflowEvent(a, c, result.Note))
	finishWorkflowAttempt(t, "complete")
	return l.write(harnessFile(t.ID), t)
}

func matchesWorkflowWorker(t *harness.Task, a *harness.WorkflowAttempt, settings roles.SpecialistSettings, got workflowrunner.Receipt) bool {
	return got.Host == a.Host && got.Role == a.Role && nativeSessionID.MatchString(got.Session) &&
		!workflowUsedSession(t, got.Session) && roles.ObservedModelMatches(a.Host, settings.Model.Model, got.Model) &&
		(settings.Model.Effort == "" || settings.Model.Effort == got.Effort)
}

func validateCompleteWorkerResult(s *session, t *harness.Task, r workflowReturn) error {
	w := t.Workflow
	switch w.Active.Role {
	case "architect":
		return validateWorkflowPlan(s.repo.Root, t.Scope, r.Plan)
	case "quality-engineer":
		if err := validateWorkflowAssertion(w.Plan, r.Assertion); err != nil {
			return err
		}
		_, err := approvedWorkflowCheck(s.workflowRunner.Policy, r.Assertion)
		return err
	}
	return nil
}

func acceptSupervisedReview(ctx context.Context, s *session, l *stageLedger, t *harness.Task, got workflowrunner.Receipt) error {
	a := t.Workflow.Active
	var report nativeReviewReport
	if got.AgentType != "kitsu-reviewer" || len(got.Changes) != 0 || decodeWorkflowJSON(got.Result, &report) != nil ||
		!report.valid() || len(report.Note) > 12<<10 {
		return completeWorkflowFailure(l, t, "runner did not return a fresh read-only reviewer verdict")
	}
	if _, err := applyWorkflowFiles(ctx, s, l, t, nil); err != nil {
		return err
	}
	event := workflowEvent(a, a.Candidate, report.Note)
	event.Findings = report.Findings
	t.Events = append(t.Events, event)
	t.Workflow.Reviewed = &harness.WorkflowEvidence{Attempt: a.ID, Job: a.Job, Candidate: a.Candidate}
	if err := routeWorkflowFindings(t, report.Findings); err != nil {
		t.Workflow.Problem = err.Error()
	}
	finishWorkflowAttempt(t, "complete")
	return l.write(harnessFile(t.ID), t)
}
