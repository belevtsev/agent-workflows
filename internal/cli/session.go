package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/workflowrunner"
)

// session is what a command needs to work in a repository.
type session struct {
	cfg            Config
	repo           *gitx.Repo
	store          *store.Store
	compact        *compactCheckCapture
	workflowRunner workflowrunner.Runner
}

// open finds the repository of the working directory. A command other than
// a hook needs a working tree.
func open(ctx context.Context, cfg Config, needTree bool) (*session, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, failf("%v", err)
	}
	repo, err := gitx.Open(ctx, dir)
	if errors.Is(err, gitx.ErrNotRepo) {
		return nil, failf("%s is not inside a git repository", dir)
	}
	if err != nil {
		return nil, failf("%v", err)
	}
	if needTree && repo.Root == "" {
		return nil, failf("this repository has no working tree here")
	}
	return &session{cfg: cfg, repo: repo, store: store.Open(repo.CommonDir)}, nil
}

// judgeWorktree takes the snapshot of the working tree and judges it.
func (s *session) judgeWorktree(ctx context.Context, base *evidence.Base) (*evidence.Verdict, error) {
	tree, err := s.repo.Snapshot(ctx, s.store.Tmp())
	if err != nil {
		return nil, err
	}
	return evidence.Judge(ctx, s.repo, s.store, tree, time.Now(), base)
}

// warn prints the warnings of a verdict once each.
func (s *session) warn(warnings []string) {
	if s.compact != nil {
		s.compact.Warnings = append(s.compact.Warnings, warnings...)
	}
	seen := map[string]bool{}
	for _, w := range warnings {
		if !seen[w] {
			seen[w] = true
			fmt.Fprintln(s.cfg.Err, "kitsu: warning:", w)
		}
	}
}

// hooksMissing is the warning of every command that reports the state of
// the gate.
const hooksMissing = "the hooks of kitsu are not installed in this clone, so git gates nothing here; run `kitsu init`"

// hooksAdvice is that warning for this clone. Where core.hooksPath is set,
// kitsu writes no hook, so `kitsu init` can only print the line to add.
func hooksAdvice(ctx context.Context, repo *gitx.Repo) string {
	if dir, configured, err := repo.HooksDir(ctx); err == nil && configured {
		return fmt.Sprintf("git runs the hooks in %s here (core.hooksPath), and they do not all call kitsu, so git gates nothing here; `kitsu init` prints the line to add to each", dir)
	}
	return hooksMissing
}

// more says how many items a list left out.
func more(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("  ... and %d more\n", n)
}
