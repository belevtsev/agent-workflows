package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func advanceWorkflow(ctx context.Context, s *session, o workflowOptions) (workflowStep, error) {
	l, err := openStageLedgerWait(ctx, s, ledgerWait)
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
	if w.Active != nil {
		step.attempt = w.Active
		return step, nil
	}
	if v.Next == "done" {
		return step, nil
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
	switch v.Next {
	case "plan", "tests", "implement", "repair", "review", "baseline", "verify":
		return reserveWorkflowAttempt(ctx, s, l, task, step)
	case "commit":
		err := workflowCommit(ctx, s, l, task, v)
		save := l.write(harnessFile(task.ID), task)
		step.again = err == nil && save == nil
		return step, errors.Join(err, save)
	default:
		return step, nil
	}
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

func stageWorkflowVerification(ctx context.Context, s *session, task *harness.Task, v harnessView) (harnessView, error) {
	if !task.Scope.Commit || v.Candidate.Index == v.Candidate.Tree {
		return v, nil
	}
	if err := checkHarnessPolicy(ctx, s, task, v.Candidate.Tree); err != nil {
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
	if err := checkWorkflowCommitHooks(ctx, s); err != nil {
		return err
	}
	note := strings.Join(strings.Fields(task.Request), " ")
	runes := []rune(note)
	note = string(runes[:min(len(runes), 72)])
	return commitVerifiedHarness(ctx, s, l, task, harnessOptions{id: task.ID, ticket: v.Ticket, note: note, includes: task.Workflow.Includes, brief: true}, v)
}
