package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// Pin the source before the host can change it. Outside a repository the
// existing interactive handoff is still useful for choosing a project.
func pinHostTask(ctx context.Context, cfg Config, o onboardingArgs, repo *gitx.Repo) (string, error) {
	if repo == nil {
		if o.resume != "" || o.reviewLimits != (reviewLimits{}) {
			return "", failf("enter the task's working tree before resuming or setting limits")
		}
		return "", nil
	}
	s := &session{cfg: cfg, repo: repo, store: store.Open(repo.CommonDir)}
	if o.resume != "" {
		// A lookup in an unused checkout must not initialize a ledger there.
		if _, err := os.Lstat(filepath.Join(repo.GitDir, "kitsu-stages")); err != nil {
			return "", resumeLookupError(o.resume, err)
		}
	}
	l, err := openStageLedgerWait(ctx, s, ledgerWait)
	if err != nil {
		return "", err
	}
	defer l.close()
	if id, err := l.active(); err != nil {
		return "", err
	} else if id != "" {
		return "", failf("resume the active managed interval %s first", id)
	}
	if o.resume != "" {
		task, err := loadHarness(ctx, s, l, harnessOptions{id: o.resume}, "next")
		if err != nil {
			return "", resumeLookupError(o.resume, err)
		}
		if err := checkHarnessResumeCheckout(ctx, s, &task); err != nil {
			return "", err
		}
		return task.ID, nil
	}
	target, err := repo.CommitTarget(ctx)
	if err != nil {
		return "", err
	}
	c, err := harnessSnapshot(ctx, s)
	if err != nil {
		return "", err
	}
	task := harness.Task{Version: harness.CurrentVersion, ID: "task-" + strings.ToLower(rand.Text()), PendingScope: true, Request: o.request, Created: time.Now().UTC(), Start: c, Scope: harness.Scope{Until: o.until, RequireReview: o.requireReview}, CommitTarget: target}
	if err := pinHarnessPolicy(ctx, s, &task); err != nil {
		return "", err
	}
	if err := o.setLimits(&task); err != nil {
		return "", err
	}
	return task.ID, l.write(harnessFile(task.ID), task)
}

func resumeLookupError(id string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return failf("no saved task %s in this worktree. Use kitsu task list to check the ID or git worktree list to find the original checkout", id)
	}
	return err
}

func checkHarnessResumeCheckout(ctx context.Context, s *session, task *harness.Task) error {
	target := task.AuthorizedCommitTarget()
	if target == "" {
		return nil
	}
	current, err := s.repo.CommitTarget(ctx)
	if err != nil || current == target {
		return err
	}
	view, err := observeHarness(ctx, s, task, false)
	if err == nil && view.CurrentCommitTarget != "" {
		return failf("%s", view.Delivery)
	}
	return err
}
