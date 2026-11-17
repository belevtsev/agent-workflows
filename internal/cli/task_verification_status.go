package cli

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

type taskVerificationView struct {
	Status    string                `json:"status"`
	Ready     bool                  `json:"ready"`
	BaseTree  string                `json:"base_tree"`
	AttemptID string                `json:"attempt_id"`
	SourceKey string                `json:"source_key"`
	Required  []string              `json:"required"`
	Results   []verification.Result `json:"results"`
	Missing   []string              `json:"missing"`
	Reference []string              `json:"reference"`
	Scope     string                `json:"scope"`
}

const taskVerificationScope = "Current local execution evidence for repository-declared assertions only. Shared summaries are historical; skill selection, worker claims, reviews, approvals and task completion are not certified."

func newTaskVerificationView(task string) *taskVerificationView {
	return &taskVerificationView{Status: "pending", Required: []string{}, Results: []verification.Result{}, Missing: []string{},
		Reference: []string{"kitsu", "task", "verify", task}, Scope: taskVerificationScope}
}

func observeTaskVerification(ctx context.Context, s *session, task string) (*taskVerificationView, error) {
	first, err := taskVerificationObservation(ctx, s, task)
	if err != nil || first == nil {
		return first, err
	}
	second, err := taskVerificationObservation(ctx, s, task)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(first, second) {
		return nil, errors.New("verification state changed while observing; retry")
	}
	return second, nil
}

func taskVerificationObservation(ctx context.Context, s *session, task string) (*taskVerificationView, error) {
	history, historyErr := verification.Load(s.repo.Root, task)
	if errors.Is(historyErr, verification.ErrPromotionPending) {
		v := newTaskVerificationView(task)
		v.Status, v.AttemptID, v.BaseTree = "unavailable", history.ID, history.Attempt.BaseTree
		v.Missing = append(v.Missing, "Interrupted small-to-full promotion; rerun task verify to finish publication.")
		return v, nil
	}
	if historyErr != nil && !errors.Is(historyErr, verification.ErrNoHistory) {
		return nil, historyErr
	}
	verdict, err := s.judgeWorktree(ctx, nil)
	if err != nil {
		if errors.Is(err, evidence.ErrNoDeclaration) && errors.Is(historyErr, verification.ErrNoHistory) {
			return nil, nil
		}
		return nil, err
	}
	enabled := slices.ContainsFunc(verdict.Checks, func(c evidence.Check) bool { return c.Verify })
	if historyErr != nil && !enabled {
		return nil, nil
	}
	v := newTaskVerificationView(task)
	raw, err := verificationRawKey(ctx, s, verdict.Tree)
	if err != nil {
		return nil, err
	}
	v.SourceKey = contextpack.Key([]string{verdict.Tree, raw})
	if historyErr != nil {
		v.Missing = append(v.Missing, "Pin the task's starting revision with task verify --from <revision>.")
		return v, nil
	}
	v.AttemptID, v.BaseTree = history.ID, history.Attempt.BaseTree
	plan, err := verification.Build(ctx, s.repo, verdict.Tree, v.BaseTree)
	if err != nil {
		v.Status, v.Missing = "unavailable", append(v.Missing, "Pinned verification base or current plan is unavailable: "+err.Error())
		return v, nil
	}
	v.Required = plan.RequiredIDs()
	if len(v.Required) == 0 {
		v.Status, v.Ready = "not_required", true
		return v, nil
	}
	v.Results = currentVerificationResults(s, verdict, plan, v.Required)
	return boundTaskVerification(ctx, s, task, verdict, plan, history, v, raw)
}

func boundTaskVerification(ctx context.Context, s *session, task string, verdict *evidence.Verdict, plan verification.Plan, history verification.History, v *taskVerificationView, raw string) (*taskVerificationView, error) {
	skillsKey, _, err := taskVerificationSkills(s, task, history.Attempt.Workflow, false)
	if err != nil {
		v.Status, v.Missing = "unavailable", append(v.Missing, "Selected skill sources unavailable: "+err.Error())
		return v, nil
	}
	proof, proofErr := readLocalVerification(s, task)
	v.Status, v.Ready = "passed", true
	for _, result := range v.Results {
		if result.State == store.Passed {
			continue
		}
		v.Ready = false
		if v.Status != "failed" {
			v.Status = "unavailable"
		}
		if result.State == store.Failed || result.State == store.TimedOut {
			v.Status = "failed"
		}
		v.Missing = append(v.Missing, fmt.Sprintf("%s: %s", result.ID, result.State))
	}
	if !v.Ready {
		return v, nil
	}
	switch {
	case history.Attempt.PolicyKey != plan.PolicyKey || history.Attempt.SkillsKey != skillsKey:
		v.Status, v.Ready = "stale", false
		v.Missing = append(v.Missing, "Verification policy or selected original skill sources changed.")
	case proofErr != nil:
		v.Status, v.Ready = "unavailable", false
		v.Missing = append(v.Missing, "Current private verification observation unavailable; run task verify.")
	case proof.Attempt != history.ID || proof.Tree != verdict.Tree || proof.Policy != plan.PolicyKey || proof.Skills != skillsKey || proof.Raw != raw || !reflect.DeepEqual(proof.Results, v.Results):
		v.Status, v.Ready = "stale", false
		v.Missing = append(v.Missing, "Source or current local execution evidence changed; run task verify.")
	}
	return v, nil
}

func currentVerificationResults(s *session, verdict *evidence.Verdict, plan verification.Plan, ids []string) []verification.Result {
	results := make([]verification.Result, 0, len(ids))
	for _, id := range ids {
		result := verification.Result{ID: id, State: "pending"}
		for _, c := range plan.Checks {
			if c.ID == id {
				result.Expect = c.Expect
			}
		}
		for _, c := range verdict.Checks {
			if c.ID == id {
				result = verificationCheckResult(s, c, result)
			}
		}
		results = append(results, result)
	}
	return results
}

func verificationCheckResult(s *session, c evidence.Check, result verification.Result) verification.Result {
	record := c.Deciding
	if c.Last != nil && (record == nil || c.Last.Seq > record.Seq || c.Last.StartedAt.After(record.StartedAt)) {
		record = c.Last
	}
	if c.Implied || record == nil || record.Outcome == "" {
		return result
	}
	digest, err := taskVerificationLog(s, record.RunID)
	if err != nil {
		result.State = "unavailable"
		return result
	}
	result.State, result.RunID, result.Tree = record.Outcome, record.RunID, record.Tree
	result.ContentKey, result.LogSHA256, result.ExitStatus = record.ContentKey, digest, record.ExitStatus
	return result
}

func applyTaskVerification(report *change.Report, v *taskVerificationView) {
	if v == nil || v.Ready {
		return
	}
	if report.Stage == change.StageCommit || report.Stage == change.StageDone {
		report.Stage, report.Missing = change.StageReview, nil
	}
	if report.Stage == change.StageReview {
		report.Missing = append(report.Missing, "required behavioral verification: "+v.Status+" (kitsu task verify "+report.Task+")")
	}
}

func verificationWorkflow(s *session, task, explicit string, history verification.History, historyErr error) (string, error) {
	folder, err := change.Read(s.repo.Root, task)
	if err != nil && !errors.Is(err, change.ErrNoFolder) {
		return "", err
	}
	workflow := "small"
	if folder != nil {
		workflow = "full"
	}
	if explicit != "" && explicit != workflow {
		return "", fmt.Errorf("--workflow %s conflicts with existing %s task location", explicit, workflow)
	}
	if historyErr == nil && history.Attempt.Workflow == "full" && workflow == "small" {
		return "", errors.New("a full verification history cannot be demoted to small")
	}
	return workflow, nil
}

func hasVerificationHistory(root, task string) (bool, error) {
	_, err := verification.Load(root, task)
	if errors.Is(err, verification.ErrPromotionPending) {
		return true, nil
	}
	if errors.Is(err, verification.ErrNoHistory) {
		return false, nil
	}
	return err == nil, err
}
