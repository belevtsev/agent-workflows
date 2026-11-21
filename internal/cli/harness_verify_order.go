package cli

import (
	"context"
	"fmt"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/runner"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

const (
	beforeReview = "before-review"
	reviewFirst  = "review-first"
)

func parseVerificationOrder(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	flags := newFlags("check trust")
	order := flags.String("verify-order", "", "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return "", err
	}
	if len(p.pos) != 0 || p.dash || (*order != beforeReview && *order != reviewFirst) {
		return "", usagef("--verify-order expects before-review or review-first")
	}
	return *order, nil
}

func verificationOrderFile(repo *gitx.Repo) (uxSnapshot, error) {
	return localTrustFile(repo.GitDir, ".verify-order")
}

func revokeVerificationOrder(ctx context.Context, cfg Config, repo *gitx.Repo) error {
	if !onboardingTerminal(cfg) {
		return usagef("change the verification order in your own terminal")
	}
	saved, err := verificationOrderFile(repo)
	if err != nil {
		return err
	}
	if err := uxSaveFile(ctx, saved, saved.path+".lock", nil, []byte(reviewFirst+"\n")); err != nil {
		return err
	}
	fmt.Fprintln(cfg.Out, "Restored review-first ordering in this checkout.")
	return nil
}

func verificationOrderApproved(s *session, policy string) bool {
	if !stageValidID(policy) {
		return false
	}
	saved, err := verificationOrderFile(s.repo)
	return err == nil && string(saved.data) == policy+"\n" && requireHarnessCheckApproval(s, policy) == nil
}

func needsEarlyCheckApproval(task *harness.Task, repositoryReview bool) bool {
	return (task.Scope.RequireReview || repositoryReview) && task.BeforeFirstFeatureReview()
}

func canVerifyBeforeReview(s *session, task *harness.Task, state harness.Readiness) bool {
	return !state.Verified && needsEarlyCheckApproval(task, state.RepositoryReview) &&
		verificationOrderApproved(s, task.RepositoryPolicy)
}

func executeHarnessCheck(ctx context.Context, s *session, t *harness.Task, early bool, job runner.Job, printStatus bool) (store.Record, error) {
	if early && !verificationOrderApproved(s, t.RepositoryPolicy) {
		return store.Record{}, failf("early verification needs current checkout approval from kitsu check trust --verify-order before-review")
	}
	return s.executeJob(ctx, job, printStatus)
}
