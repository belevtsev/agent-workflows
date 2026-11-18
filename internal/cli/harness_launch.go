package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// Pin the source before the host can change it. Outside a repository the
// existing interactive handoff is still useful for choosing a project.
func pinHostTask(ctx context.Context, cfg Config, o onboardingArgs) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	repo, err := gitx.Open(ctx, dir)
	if errors.Is(err, gitx.ErrNotRepo) && o.resume == "" && o.reviewLimits == (reviewLimits{}) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	s := &session{cfg: cfg, repo: repo, store: store.Open(repo.CommonDir)}
	l, err := openStageLedger(ctx, s)
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
			return "", err
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
	task := harness.Task{Version: 1, ID: "task-" + strings.ToLower(rand.Text()), PendingScope: true, Request: o.request, Created: time.Now().UTC(), Start: c, Scope: harness.Scope{Until: o.until}, CommitTarget: target}
	if err := o.setLimits(&task); err != nil {
		return "", err
	}
	return task.ID, l.write(harnessFile(task.ID), task)
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
