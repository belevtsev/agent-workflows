package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

type taskVerificationReport struct {
	Version        int                   `json:"version"`
	Task           string                `json:"task"`
	Workflow       string                `json:"workflow"`
	Verification   *taskVerificationView `json:"verification"`
	ChecksVerified bool                  `json:"checks_verified"`
	Checks         []verification.Result `json:"checks"`
	Executed       int                   `json:"recorded_run_attempts"`
	Reused         int                   `json:"reused_checks"`
	PlanReference  []string              `json:"plan_reference"`
	ObservationKey string                `json:"observation_key"`
	EvidenceKey    string                `json:"evidence_key"`
	Problem        string                `json:"problem,omitempty"`
}

func (r *taskVerificationRun) output(ctx context.Context, cfg Config, current *taskVerificationView) error {
	verdict, err := r.s.judgeWorktree(ctx, nil)
	if err != nil {
		return compactFailure(err)
	}
	report := taskVerificationReport{Version: 1, Task: r.options.task, Workflow: r.options.workflow, Verification: current,
		ChecksVerified: verdict.Verified, Checks: currentVerificationResults(r.s, verdict, r.plan, r.plan.AllIDs()),
		Reused: r.reused, PlanReference: []string{"kitsu", "check", "plan", "--from", r.plan.BaseTree}, ObservationKey: contextpack.Key(current)}
	report.ChecksVerified = verdict.Verified && verificationResultsPassed(report.Checks)
	records, _, err := r.s.store.AllRecords()
	if err != nil {
		return compactFailure(err)
	}
	for _, record := range records {
		if !r.before[record.RunID] {
			report.Executed++
		}
	}
	report.EvidenceKey, err = compactEvidenceKey(r.s)
	if err != nil {
		return compactFailure(err)
	}
	if r.runErr != nil {
		report.Problem = r.runErr.Error()
	}
	if err := validateTaskVerificationReport(ctx, r.s, report); err != nil {
		return compactFailure(err)
	}
	if err := r.print(ctx, cfg, report); err != nil {
		return err
	}
	if r.runErr != nil || current == nil || !current.Ready || !report.ChecksVerified {
		return exit(ExitNotVerified)
	}
	return nil
}

func (r *taskVerificationRun) print(ctx context.Context, cfg Config, report taskVerificationReport) error {
	if r.options.compact {
		return printTaskVerificationCompact(ctx, cfg, r.s, report, r.options.budget)
	}
	if r.options.json {
		return printJSON(cfg, report)
	}
	fmt.Fprintf(cfg.Out, "%s: verification %s; required gate checks verified: %t\n", report.Task, report.Verification.Status, report.ChecksVerified)
	fmt.Fprintf(cfg.Out, "Recorded run attempts: %d; reused checks: %d\n", report.Executed, report.Reused)
	for _, missing := range report.Verification.Missing {
		fmt.Fprintln(cfg.Out, "  "+missing)
	}
	if report.Problem != "" {
		fmt.Fprintln(cfg.Out, report.Problem)
	}
	fmt.Fprintf(cfg.Out, "Inspect current evidence: kitsu task show %s --json\n", report.Task)
	return nil
}

func printTaskVerificationCompact(ctx context.Context, cfg Config, s *session, report taskVerificationReport, budget int) error {
	e, err := contextpack.Open(s.repo)
	if err != nil {
		return compactFailure(err)
	}
	state, err := e.Snapshot(ctx)
	if err != nil {
		return compactFailure(err)
	}
	a := contextpack.Artifact{Kind: "verification", State: state, Items: []contextpack.Item{}}
	a.Metadata, err = json.Marshal(report)
	if err != nil {
		return err
	}
	remaining := budget
	for _, result := range report.Checks {
		item := contextpack.Item{ID: result.ID, Status: result.State}
		if result.RunID != "" {
			item.LogRunID = result.RunID
			limit := remaining
			if item.Status == "passed" || item.Status == "satisfied" {
				limit = 0
			}
			lines, total, err := compactLogCandidate(s, result.RunID, limit)
			if err != nil {
				return compactFailure(err)
			}
			item.LogChunks = total
			if limit > 0 && len(lines) == total {
				remaining = max(0, remaining-compactLogBytes(lines))
			}
			item.Lines = lines
		}
		a.Items = append(a.Items, item)
	}
	if err := e.Save(ctx, &a); err != nil {
		return compactFailure(err)
	}
	if err := validateTaskVerificationArtifact(ctx, s, a); err != nil {
		return compactFailure(err)
	}
	return printCompact(cfg, a, "", "", budget, false)
}

func validateTaskVerificationArtifact(ctx context.Context, s *session, a contextpack.Artifact) error {
	var report taskVerificationReport
	if err := json.Unmarshal(a.Metadata, &report); err != nil {
		return err
	}
	return validateTaskVerificationReport(ctx, s, report)
}

func validateTaskVerificationReport(ctx context.Context, s *session, report taskVerificationReport) error {
	current, err := observeTaskVerification(ctx, s, report.Task)
	if err != nil {
		return err
	}
	key, err := compactEvidenceKey(s)
	if err != nil {
		return err
	}
	if report.Version != 1 || report.ObservationKey != contextpack.Key(current) || report.EvidenceKey != key {
		return fmt.Errorf("artifact stale: verification source, policy, local records or retained logs changed")
	}
	verdict, err := s.judgeWorktree(ctx, nil)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("artifact stale: task verification is unavailable")
	}
	plan, err := verification.Build(ctx, s.repo, verdict.Tree, current.BaseTree)
	if err != nil {
		return err
	}
	checks := currentVerificationResults(s, verdict, plan, plan.AllIDs())
	if report.ChecksVerified != (verdict.Verified && verificationResultsPassed(checks)) || !reflect.DeepEqual(report.Checks, checks) {
		return fmt.Errorf("artifact stale: required gate outcomes or retained check logs changed")
	}
	confirmed, err := observeTaskVerification(ctx, s, report.Task)
	if err != nil {
		return err
	}
	confirmedEvidence, err := compactEvidenceKey(s)
	if err != nil {
		return err
	}
	if report.ObservationKey != contextpack.Key(confirmed) || report.EvidenceKey != confirmedEvidence {
		return fmt.Errorf("artifact stale: source or local evidence changed during verification reporting")
	}
	return nil
}
