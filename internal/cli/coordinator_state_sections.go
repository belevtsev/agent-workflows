package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/gate"
)

var coordinatorSectionNames = []string{"source", "task", "interval", "checks", "findings", "policy", "prerequisites", "git"}

func coordinatorSections(ctx context.Context, s *session, e *contextpack.Engine, l *stageLedger, task string) (map[string]json.RawMessage, error) {
	state, err := e.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := stageRawSources(ctx, s, state.Tree)
	if err != nil {
		return nil, err
	}
	records, err := coordinatorRecordKey(ctx, s.repo.Root, task)
	if err != nil {
		return nil, err
	}
	views, err := uxTaskViews(ctx, s, task)
	if err != nil {
		return nil, err
	}
	view := views[0]
	policy, err := coordinatorPolicy(ctx, s, task, state)
	if err != nil {
		return nil, err
	}
	checks := stageReceipt{State: state, Checks: []stageCheck{}, Missing: []string{}, Warnings: []string{}}
	if err := observeStageChecks(ctx, s, &checks); err != nil {
		return nil, err
	}
	interval, err := coordinatorInterval(ctx, s, l, task, state, checks.EvidenceKey)
	if err != nil {
		return nil, err
	}
	git, err := coordinatorGit(ctx, s, state)
	if err != nil {
		return nil, err
	}
	findings, prerequisites, err := coordinatorRecords(ctx, s, view, state)
	if err != nil {
		return nil, err
	}
	prerequisites = coordinatorPrerequisites(prerequisites, task, interval, checks, git, policy)
	taskState := map[string]any{"workflow": view.Workflow, "stage": view.Stage, "reference": []string{"kitsu", "task", "show", task, "--json"}}
	if view.Report != nil {
		taskState["content"], taskState["broken_rules"], taskState["not_automated"] = view.Report.Content, view.Report.BrokenRules, view.Report.NotAutomated
	}
	values := map[string]any{
		"source": map[string]any{"state": state, "write_content_key": contextpack.Key(raw), "records_key": records},
		"task":   taskState, "interval": interval, "policy": policy, "git": git, "findings": findings,
		"checks":        map[string]any{"verified": checks.Verified, "items": checks.Checks, "warnings": checks.Warnings, "error": checks.EvidenceError, "evidence_key": checks.EvidenceKey, "log_directory": s.store.Abs("logs"), "reference": []string{"kitsu", "gate", "status", "--json"}},
		"prerequisites": map[string]any{"completion": "not_certified", "missing": prerequisites, "scope": "Recorded blockers and next-stage requirements only; later stages and semantic/native execution obligations still require original sources and independent verification."},
	}
	sections := make(map[string]json.RawMessage, len(values))
	for name, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		sections[name] = data
	}
	after, err := e.Snapshot(ctx)
	if err != nil || after != state {
		return nil, fmt.Errorf("source changed during coordinator observation: %v", err)
	}
	afterRaw, err := stageRawSources(ctx, s, state.Tree)
	if err != nil || contextpack.Key(afterRaw) != contextpack.Key(raw) {
		return nil, fmt.Errorf("raw source changed during coordinator observation: %v", err)
	}
	afterRecords, err := coordinatorRecordKey(ctx, s.repo.Root, task)
	if err != nil || afterRecords != records {
		return nil, fmt.Errorf("task records changed during coordinator observation: %v", err)
	}
	return sections, nil
}

func coordinatorPrerequisites(prerequisites []map[string]any, task string, interval map[string]any, checks stageReceipt, git map[string]any, policy coordinatorPolicyKeys) []map[string]any {
	if interval["status"] != "none" {
		prerequisites = append(prerequisites, statePrerequisite("unfinished_interval", []string{"kitsu", "task", "finish", interval["dispatch_id"].(string)}))
		if len(interval["policy_changes"].([]string)) > 0 || interval["expired"] == true {
			prerequisites = append(prerequisites, statePrerequisite("interval_requires_abandon_and_reprepare", []string{"kitsu", "task", "finish", "--help"}))
		}
		if returned, ok := interval["return"].(map[string]any); ok && interval["task"] == task && (returned["findings"].(int) > 0 || returned["blockers"].(int) > 0) {
			prerequisites = append(prerequisites, statePrerequisite("inspect_untrusted_worker_claims", []string{returned["path"].(string)}))
		}
	}
	if !checks.Verified {
		prerequisites = append(prerequisites, statePrerequisite("current_passing_checks", []string{"kitsu", "gate", "status", "--json"}))
	}
	if git["worktree_matches_head"] != true {
		prerequisites = append(prerequisites, statePrerequisite("uncommitted_content", []string{"git", "status", "--short"}))
	}
	if git["hooks"] != gate.Installed || git["mode"] != "enforce" {
		prerequisites = append(prerequisites, statePrerequisite("enforcing_hooks", []string{"kitsu", "gate", "status", "--json"}))
	}
	if len(policy.Problems) > 0 {
		prerequisites = append(prerequisites, statePrerequisite("skill_policy_unavailable", []string{"kitsu", "role", "list", "--json"}))
	}
	return prerequisites
}

func statePrerequisite(code string, reference []string) map[string]any {
	return map[string]any{"code": code, "reference": reference}
}

func coordinatorRecords(ctx context.Context, s *session, view uxTaskView, state contextpack.State) (map[string]any, []map[string]any, error) {
	findings := map[string]any{"status": "unknown", "open": []any{}, "reference": []string{"kitsu", "task", "show", view.Task, "--json"}}
	missing := []map[string]any{}
	if view.Workflow == "small" {
		missing = append(missing, statePrerequisite("small_workflow_completion_unknown", []string{"kitsu", "task", "show", view.Task}))
	}
	folder, err := change.Read(s.repo.Root, view.Task)
	if errors.Is(err, change.ErrNoFolder) && view.Workflow == "small" {
		folder = &change.Folder{Task: view.Task}
		err = nil
	}
	if err != nil {
		return nil, nil, err
	}
	open := []any{}
	if folder.Reviews != nil {
		for _, f := range folder.Reviews.Findings {
			if f.Disposition == "open" {
				open = append(open, map[string]any{"id": f.ID, "stage": f.Stage, "severity": f.Severity, "path": folder.Reviews.File, "line": f.Line, "content_key": contextpack.Key(f)})
			}
		}
	}
	if view.Workflow == "full" {
		findings["status"], findings["open"] = "recorded_only", open
	}
	if len(open) > 0 {
		missing = append(missing, statePrerequisite("open_recorded_findings", []string{"kitsu", "task", "check", view.Task}))
	}
	if view.Report != nil && view.Report.Stage != change.StageDone {
		item := statePrerequisite("task_stage_requirements", []string{"kitsu", "task", "show", view.Task, "--json"})
		item["stage"], item["count"] = view.Report.Stage, len(view.Report.Missing)
		item["content_key"] = contextpack.Key(view.Report.Missing)
		missing = append(missing, item)
	}
	if view.Report != nil && view.Report.BrokenRules > 0 {
		missing = append(missing, statePrerequisite("invalid_task_records", []string{"kitsu", "task", "check", view.Task}))
	}
	advice, err := coordinatorAdviserPrerequisites(ctx, s, view, state, folder)
	return findings, append(missing, advice...), err
}

func coordinatorAdviserPrerequisites(ctx context.Context, s *session, view uxTaskView, state contextpack.State, folder *change.Folder) ([]map[string]any, error) {
	// Inspect every adviser checkpoint, including ones after the current stage.
	content, historical := change.Content{}, ""
	if view.Report != nil {
		content = view.Report.Content
		gitOf, err := s.changeGit(ctx)
		if err != nil {
			return nil, err
		}
		git, err := gitOf(folder)
		if err != nil {
			return nil, err
		}
		historical = git.Committed
	} else {
		code := change.NewCode()
		if err := s.repo.Entries(ctx, state.Tree, code.Add); err != nil {
			return nil, err
		}
		content.Code = code.ID()
	}
	status, err := advisers.Evaluate(s.repo.Root, view.Task, content, historical, "")
	if err != nil && !errors.Is(err, advisers.ErrNoBinding) {
		return nil, err
	}
	if err == nil && !status.Ready {
		item := statePrerequisite("adviser_checkpoints", []string{"kitsu", "adviser", "status", view.Task, "--json"})
		item["requirements"] = status.Requirements
		return []map[string]any{item}, nil
	}
	return nil, nil
}

func coordinatorGit(ctx context.Context, s *session, state contextpack.State) (map[string]any, error) {
	index, err := s.repo.IndexTree(ctx, s.store.Tmp())
	if err != nil {
		return nil, err
	}
	head, _, err := s.repo.TreeOf(ctx, "HEAD")
	if err != nil {
		return nil, err
	}
	mode, err := s.store.Mode()
	if err != nil {
		return nil, err
	}
	return map[string]any{"mode": mode, "hooks": gate.State(ctx, s.repo), "index_tree": index, "index_matches_worktree": index == state.Tree, "worktree_matches_head": head != "" && head == state.Tree, "reference": []string{"kitsu", "gate", "status", "--json"}}, nil
}

func coordinatorInterval(ctx context.Context, s *session, l *stageLedger, task string, state contextpack.State, evidenceKey string) (map[string]any, error) {
	id, err := l.active()
	if err != nil || id == "" {
		return map[string]any{"status": "none"}, err
	}
	record, err := l.record(id, s.repo.Root)
	if err != nil {
		return nil, err
	}
	b := record.Prepared.Brief
	changes := []string{}
	current, _, policyErr := briefInputs(s, b)
	if policyErr != nil {
		changes = append(changes, "worker_policy_unavailable")
	} else if current.Policy != b.Policy {
		changes = append(changes, "worker_policy")
	}
	if err := validateStageCheckPolicy(ctx, s, record, state.Tree); err != nil {
		changes = append(changes, "check_policy")
	}
	if state.SourcePolicy != record.Prepared.State.SourcePolicy {
		changes = append(changes, "source_visibility")
	}
	status := "active"
	if b.Task != task {
		status = "other_task"
	}
	result := map[string]any{"status": status, "dispatch_id": id, "task": b.Task, "stage": b.Stage, "role": b.Role, "round": b.Round, "host": b.Host, "expired": time.Since(record.Created) > contextpack.Lifetime, "policy_changes": changes, "packet_path": record.Prepared.Dispatch.Path, "packet_sha256": record.Prepared.Dispatch.SHA256, "write_bounds": "not_validated_by_view", "return": nil}
	returned, err := coordinatorReturn(s, l, record, id, changes, state, evidenceKey)
	if returned != nil {
		result["return"] = returned
	}
	return result, err
}

func coordinatorReturn(s *session, l *stageLedger, record stageRecord, id string, changes []string, state contextpack.State, evidenceKey string) (map[string]any, error) {
	var returned workerReturnReceipt
	if err := l.read(id+".return.json", &returned); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	returnID := returned.ID
	returned.ID = ""
	if returnID != contextpack.Key(returned) || returned.Version != 1 || returned.Scope != workerReturnScope || returned.Authority != "untrusted-worker-claims" || returned.Created.Before(record.Created) || returned.Created.After(time.Now().Add(time.Minute)) {
		return nil, fmt.Errorf("saved worker return was tampered with")
	}
	if err := validateWorkerReturn(returned.Claims); err != nil {
		return nil, err
	}
	if err := matchWorkerReturn(record, returned.Claims); err != nil {
		return nil, err
	}
	// A reference retains every claim; no claim is promoted to a disposition.
	severities := map[string]int{"blocker": 0, "major": 0, "minor": 0}
	for _, finding := range returned.Claims.Findings {
		severities[finding.Severity]++
	}
	return map[string]any{"return_id": returnID, "authority": returned.Authority, "path": filepath.Join(s.repo.GitDir, "kitsu-stages", id+".return.json"), "findings": len(returned.Claims.Findings), "finding_severities": severities, "blockers": len(returned.Claims.Blockers), "content_key": contextpack.Key(returned.Claims), "observations_current": len(changes) == 0 && returned.Observed.State == state && returned.Observed.EvidenceKey == evidenceKey}, nil
}
