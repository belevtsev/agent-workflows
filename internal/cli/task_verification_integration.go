package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/skills"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func uxSavedTasks(root string) ([]string, error) {
	tasks, err := skills.Tasks(root)
	if err != nil {
		return nil, err
	}
	small, err := smallAdviserTasks(root)
	if err != nil {
		return nil, err
	}
	verified, err := verification.Tasks(root)
	if err != nil {
		return nil, err
	}
	tasks = append(append(tasks, small...), verified...)
	tasks, err = appendUsageBudgetTasks(root, tasks)
	if err != nil {
		return nil, err
	}
	slices.Sort(tasks)
	return slices.Compact(tasks), nil
}

func taskVerificationPolicyKey(ctx context.Context, s *session, tree string) (string, error) {
	file, err := evidence.Declaration(ctx, s.repo, tree)
	if errors.Is(err, evidence.ErrNoDeclaration) {
		return contextpack.Key(nil), nil
	}
	if err != nil {
		return "", err
	}
	return verification.PolicyKey(file), nil
}

func uxSmallTaskView(ctx context.Context, s *session, task string) (uxTaskView, error) {
	view := uxTaskView{Task: task, Workflow: "small", Stage: "unknown"}
	has, err := skills.HasSelections(s.repo.Root, task)
	if err != nil {
		return view, err
	}
	_, adviceErr := advisers.Location(s.repo.Root, task)
	if adviceErr != nil && !errors.Is(adviceErr, advisers.ErrNoBinding) {
		return view, adviceErr
	}
	hasVerification, err := hasVerificationHistory(s.repo.Root, task)
	if err != nil {
		return view, err
	}
	hasBudget, err := hasUsageBudget(s.repo.Root, task)
	if err != nil {
		return view, err
	}
	if !has && !hasVerification && !hasBudget && errors.Is(adviceErr, advisers.ErrNoBinding) {
		return view, change.ErrNoFolder
	}
	view.Verification, err = observeTaskVerification(ctx, s, task)
	return view, err
}

func printTaskVerificationSummary(cfg Config, v *taskVerificationView) {
	if v == nil {
		return
	}
	fmt.Fprintf(cfg.Out, "  Behavioral verification: %s; ready: %t\n", v.Status, v.Ready)
	for _, missing := range v.Missing {
		fmt.Fprintln(cfg.Out, "  "+missing)
	}
}

func coordinatorTaskVerification(taskState map[string]any, view uxTaskView, prerequisites []map[string]any) []map[string]any {
	if len(view.UsageBudget) > 0 {
		taskState["usage_budget_reference"] = view.UsageBudget
	}
	if view.Verification == nil {
		return prerequisites
	}
	taskState["verification"] = view.Verification
	if !view.Verification.Ready {
		prerequisites = append(prerequisites, statePrerequisite("behavioral_verification", view.Verification.Reference))
	}
	return prerequisites
}
