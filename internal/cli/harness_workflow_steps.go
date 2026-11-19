package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func advanceWorkflow(ctx context.Context, s *session, o workflowOptions) (workflowStep, error) {
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return workflowStep{}, err
	}
	defer l.close()
	task, err := readWorkflow(l, o.id)
	if err != nil {
		return workflowStep{}, err
	}
	v, err := observeHarness(ctx, s, task, false)
	if err != nil {
		return workflowStep{}, err
	}
	step := workflowStep{view: v, deadline: task.Deadline}
	if err := saveObservedDelivery(l, task, v); err != nil {
		return step, err
	}
	w := task.Workflow
	if v.Next == "done" {
		return step, nil
	}
	if w.Active != nil {
		return recoverNativeWorkflow(ctx, s, l, task, step)
	}
	busy, err := workflowWorktreeOwner(l, task.ID)
	if err != nil {
		return step, err
	}
	if busy != "" {
		return blockedWorkflowStep(step, "Finish the outstanding assignment for "+busy+" before continuing."), nil
	}
	if w.Problem != "" {
		hint := fmt.Sprintf("%s\nResolve the problem, then run kitsu task run --resume %s --retry.", w.Problem, task.ID)
		return blockedWorkflowStep(step, hint), nil
	}
	if err := workflowCheckCandidate(task, v); err != nil {
		return blockedWorkflowStep(step, err.Error()), nil
	}
	if role := harness.WorkflowRole(v.Next); role != "" {
		return reserveWorkflowAttempt(ctx, s, l, task, step, o, role)
	}
	if !task.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, task.Deadline)
		defer cancel()
	}
	switch v.Next {
	case "baseline":
		err = runWorkflowBaseline(ctx, s, l, task, v)
	case "verify":
		err = runWorkflowVerification(ctx, s, l, task, v)
	case "commit":
		err = workflowCommit(ctx, s, l, task, v)
	default:
		return step, nil
	}
	if save := l.write(harnessFile(task.ID), task); save != nil {
		return step, errors.Join(err, save)
	}
	step.again = err == nil
	return step, err
}

func readWorkflow(l *stageLedger, id string) (*harness.Task, error) {
	var task harness.Task
	if err := l.read(harnessFile(id), &task); err != nil {
		return nil, err
	}
	if !task.Valid(id) || task.Workflow == nil {
		return nil, failf("invalid configured task %s", id)
	}
	return &task, nil
}

func workflowWorktreeOwner(l *stageLedger, exceptID string) (string, error) {
	if current, err := l.active(); current != "" || err != nil {
		return current, err
	}
	return workflowActiveTask(l, exceptID)
}

func blockedWorkflowStep(step workflowStep, message string) workflowStep {
	step.view.Next = "blocked"
	step.message = message
	return step
}

func workflowCheckCandidate(task *harness.Task, v harnessView) error {
	accepted := task.Workflow.Candidate
	if accepted == nil || *accepted == v.Candidate || v.Delivery == "committed" {
		return nil
	}
	if task.Scope.Commit && accepted.SameSource(v.Candidate) && accepted.SameBase(v.Candidate) &&
		v.Candidate.Index == v.Candidate.Tree {
		// An explicitly authorized candidate may have been staged by Kitsu
		// before interruption, or by the developer prior to resuming.
		task.Workflow.Candidate = &v.Candidate
		return nil
	}
	return failf("observed worktree, index, or Git parents differ from the last accepted candidate; inspect the changes before resuming")
}

func runWorkflowBaseline(ctx context.Context, s *session, l *stageLedger, task *harness.Task, v harnessView) error {
	w := task.Workflow
	if err := prepareHarnessVerification(ctx, s, task, v.Candidate); err != nil {
		return err
	}
	o := harnessOptions{id: task.ID, ticket: v.Ticket, argv: w.Test.Argv, expect: w.Test.Expect}
	out := workflowCheckOutput(s.cfg, task.ID, "baseline")
	proof, err := workflowBaselineProof(ctx, s, task, o, v.Candidate, out)
	if err != nil {
		return err
	}
	w.Baselines = append(w.Baselines, *proof)
	ok, err := expectedWorkflowBaseline(s, w, proof)
	if err != nil {
		return err
	}
	if !ok {
		w.Problem = fmt.Sprintf("baseline returned %s or failed for a different reason; inspect run %s", proof.Outcome, proof.Run)
		if proof.ExitStatus != nil && (proof.Outcome == store.Passed || proof.Outcome == store.Failed) &&
			len(w.Baselines) == 1 && w.Rework < w.ReworkMax {
			w.Problem = ""
			w.Pending = append(w.Pending, "tests")
		}
		return nil
	}
	w.BaselineReady = true
	// Green before coding is historical. For test-only work it is on the
	// final candidate; retain the result and run the declared checks once.
	if task.Scope.Entry == "tests" && proof.Candidate == v.Candidate {
		task.RecordAssertion(proof, "test-only baseline")
		proof.CheckedAt = time.Time{} // finish records the declared checks.
	}
	task.Generation++
	return l.write(harnessFile(task.ID), task)
}

func expectedWorkflowBaseline(s *session, w *harness.Workflow, proof *harness.Proof) (bool, error) {
	if w.Plan.Baseline == "pass" {
		return proof.Outcome == store.Passed, nil
	}
	if proof.Outcome != store.Failed || proof.ExitStatus == nil || *proof.ExitStatus == 0 {
		return false, nil
	}
	file, err := os.Open(s.store.LogPath(proof.Run))
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4<<20))
	return bytes.Contains(data, []byte(w.Test.Failure)), err
}

func runWorkflowVerification(ctx context.Context, s *session, l *stageLedger, task *harness.Task, v harnessView) error {
	w := task.Workflow
	if w.Test == nil {
		return failf("the configured workflow has no saved assertion")
	}
	v, err := stageWorkflowVerification(ctx, s, task, v)
	if err != nil {
		return err
	}
	prior := task.Proof
	run := harnessOptions{id: task.ID, ticket: v.Ticket, argv: w.Test.Argv, expect: w.Test.Expect, brief: true}
	if prior != nil && !slices.Equal(prior.Argv, run.argv) {
		run.replaceReason = "the configured test writer revised the assertion"
	}
	workflowCheckOutput(s.cfg, task.ID, "verify")
	if err := runHarnessTest(ctx, s, l, task, run, v); err != nil {
		return err
	}
	if err := workflowVerificationOutcome(task); err != nil || w.Problem != "" {
		return err
	}
	if task.Proof.Outcome == store.Passed && task.Proof.FailedCheck == "" {
		current, err := observeHarness(ctx, s, task, false)
		if err != nil {
			return err
		}
		if !current.Verified {
			w.Problem = "the passing assertion did not establish current verification evidence; inspect retained checks before retrying"
		}
	}
	return nil
}

func stageWorkflowVerification(ctx context.Context, s *session, task *harness.Task, v harnessView) (harnessView, error) {
	if !task.Scope.Commit || v.Candidate.Index == v.Candidate.Tree {
		return v, nil
	}
	if err := prepareHarnessVerification(ctx, s, task, v.Candidate); err != nil {
		return v, err
	}
	if err := checkCommitIncludes(ctx, s, task, v.Candidate.Tree, task.Workflow.Includes); err != nil {
		return v, err
	}
	if err := s.repo.StageWorkflowCandidate(ctx, s.store.Tmp(), task.CommitTarget, v.Candidate.Head, v.Candidate.Index, v.Candidate.Tree); err != nil {
		return v, err
	}
	v, err := observeHarness(ctx, s, task, false)
	if err == nil {
		task.Workflow.Candidate = &v.Candidate
	}
	return v, err
}

func workflowVerificationOutcome(task *harness.Task) error {
	w := task.Workflow
	p := task.Proof
	if p == nil {
		return failf("verification produced no retained proof")
	}
	switch {
	case p.Outcome != store.Passed && (p.Outcome != store.Failed || p.ExitStatus == nil):
		w.Problem = fmt.Sprintf("verification ended with %s; inspect run %s", p.Outcome, p.Run)
	case p.GateFailure != nil && (p.GateFailure.Outcome != store.Failed || p.GateFailure.ExitStatus == nil):
		w.Problem = fmt.Sprintf("repository check %s ended without a behavioral verdict; inspect run %s", p.FailedCheck, p.GateFailure.ID)
	case p.Outcome != store.Passed || p.FailedCheck != "":
		if (task.Scope.Entry == "verify" || task.Scope.Entry == "review") && !task.Scope.Repair {
			w.Problem = "verification failed on the original candidate; retry or explicitly authorize repair with task extend"
		} else if task.Scope.Entry == "tests" && !w.HasCorrection("tests") {
			w.Pending = append(w.Pending, "tests")
			w.BaselineReady = false
		}
	}
	return nil
}

func workflowCommit(ctx context.Context, s *session, l *stageLedger, task *harness.Task, v harnessView) error {
	note := strings.Join(strings.Fields(task.Request), " ")
	runes := []rune(note)
	note = string(runes[:min(len(runes), 72)])
	return commitHarness(ctx, s, l, task, harnessOptions{id: task.ID, ticket: v.Ticket, note: note, includes: task.Workflow.Includes, brief: true}, v)
}

func recoverNativeWorkflow(ctx context.Context, s *session, l *stageLedger, task *harness.Task, step workflowStep) (workflowStep, error) {
	attempt := task.Workflow.Active
	if attempt.Native && attempt.Role == "reviewer" {
		var observation nativeReviewObservation
		err := l.read(nativeReviewFile(task.Claim.ReviewID), &observation)
		if err == nil && observation.Result != nil {
			err = acceptWorkflowReview(ctx, s, l, task, step.view)
			step.again = err == nil
			return step, err
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return step, err
		}
	} else if attempt.Native {
		var observed workflowNativeResult
		err := l.read(workflowNativeFile(attempt.ID), &observed)
		if err == nil && observed.Conflict {
			return blockedWorkflowStep(step, "Multiple native workers reported this assignment; reconcile them before continuing."), nil
		}
		if err == nil && observed.Stopped && len(observed.Payload) == 0 {
			return step, completeWorkflowFailure(l, task, "the native worker stopped without a matching role result")
		}
		if err == nil && observed.Stopped && observed.Worker != "" && len(observed.Payload) > 0 {
			attempt.ObservedModel, attempt.ObservedEffort = observed.Model, observed.Effort
			if !observedWorkflowSettingsMatch(task.Workflow.Roles[attempt.Role], observed.Model, observed.Effort) {
				return step, completeWorkflowFailure(l, task, "native host reported different settings for the assigned role")
			}
			err = acceptWorkflowReturn(ctx, s, l, task, observed.Worker, observed.Payload)
			step.again = err == nil
			return step, err
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return step, err
		}
	}
	return blockedWorkflowStep(step, fmt.Sprintf("Attempt %s on %s is outstanding. Recover the worker or confirm it stopped before releasing the assignment.", attempt.ID, attempt.Host)), nil
}
