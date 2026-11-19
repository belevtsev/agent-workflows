package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/harness"
)

// An argv proposed by a sandboxed model does not inherit permission to run
// outside its sandbox. Use an exact approved project check or the developer's
// explicit argv approval before starting the Go-owned verifier.
func requireWorkflowAssertion(ctx context.Context, s *session, task *harness.Task, candidate harness.Snapshot) error {
	w := task.Workflow
	if w == nil {
		return nil
	}
	if reason := guardCheckTrust(ctx, s.repo, "kitsu task test"); reason != "" {
		return failf("%s", reason)
	}
	if w.Test == nil || len(w.Test.Argv) == 0 {
		return failf("configured task has no saved assertion")
	}
	if w.ApprovedArgv == workflowArgvKey(w.Test.Argv) {
		return nil
	}
	declared, err := evidence.Declaration(ctx, s.repo, candidate.Tree)
	if err != nil && !errors.Is(err, evidence.ErrNoDeclaration) {
		return err
	}
	if declared != nil {
		for _, check := range declared.Checks {
			if slices.Equal(check.Run, w.Test.Argv) {
				return nil
			}
		}
	}
	return failf("the test writer proposed %q. Review and approve this assertion with kitsu task run --resume %s --approve-assertion in your terminal", w.Test.Argv, task.ID)
}

func workflowArgvKey(argv []string) string { return contextpack.Key(argv) }

func approveWorkflowAssertion(ctx context.Context, s *session, o workflowOptions) error {
	if o.nativeHost != "" || !onboardingTerminal(s.cfg) {
		return usagef("approve the assertion in your own terminal")
	}
	argv, err := workflowApprovalArgv(ctx, s, o.id)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.cfg.Err, "Assertion in %q for task %s\n  %q\nType yes to run these arguments. Press Ctrl+C to cancel.\n",
		s.repo.Root, o.id, argv)
	if err := readCheckApproval(ctx, s.cfg.In); err != nil {
		return err
	}
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return err
	}
	defer l.close()
	task, err := readWorkflow(l, o.id)
	if err != nil {
		return err
	}
	if err := workflowApprovalReady(task); err != nil {
		return err
	}
	if !slices.Equal(task.Workflow.Test.Argv, argv) {
		return failf("the assertion changed while you were approving it; review the current command before continuing")
	}
	task.Workflow.ApprovedArgv = workflowArgvKey(argv)
	return l.write(harnessFile(task.ID), task)
}

func workflowApprovalArgv(ctx context.Context, s *session, id string) ([]string, error) {
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return nil, err
	}
	defer l.close()
	task, err := readWorkflow(l, id)
	if err != nil {
		return nil, err
	}
	if err := workflowApprovalReady(task); err != nil {
		return nil, err
	}
	return task.Workflow.Test.Argv, nil
}

func workflowApprovalReady(task *harness.Task) error {
	w := task.Workflow
	if task.CompletedDelivery() != nil || w.Active != nil || w.Test == nil || len(w.Test.Argv) == 0 {
		return failf("the task has no pending assertion to approve; inspect task next first")
	}
	return nil
}
