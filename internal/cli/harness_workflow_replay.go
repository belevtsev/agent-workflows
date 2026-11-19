package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
)

// A revised regression is checked against the pre-code source and the new
// tests in a private directory. The real checkout and index are never changed.
func workflowBaselineProof(ctx context.Context, s *session, t *harness.Task, o harnessOptions, c harness.Snapshot, out io.Writer) (*harness.Proof, error) {
	w := t.Workflow
	if w.PreCode == nil || t.Latest("implement", "repair") == nil {
		return executeHarnessAssertion(ctx, s, t, o, c, out)
	}
	dir, err := os.MkdirTemp(s.repo.GitDir, "kitsu-baseline-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := s.repo.CheckoutWorkflowTree(ctx, s.store.Tmp(), w.PreCode.Tree, dir); err != nil {
		return nil, fmt.Errorf("restore pre-code baseline: %w", err)
	}
	repo, err := gitx.InitWorkflowReplay(ctx, dir)
	if err != nil {
		return nil, err
	}
	replay := &session{cfg: s.cfg, repo: repo, store: s.store}
	restoreTree, err := repo.Snapshot(ctx, s.store.Tmp())
	if err != nil {
		return nil, err
	}
	raw, err := stageRawSources(ctx, replay, restoreTree)
	if err != nil {
		return nil, err
	}
	if restoreTree != w.PreCode.Tree || !maps.Equal(raw, w.PreCodeSources) {
		return nil, failf("pre-code bytes could not be restored exactly for the revised assertion")
	}
	if err := overlayWorkflowTests(ctx, s, w, c, dir); err != nil {
		return nil, err
	}
	replayed, err := harnessSnapshot(ctx, replay)
	if err != nil {
		return nil, err
	}
	task := *t
	task.Scope.Commit = false
	task.CommitTarget = ""
	return executeHarnessAssertion(ctx, replay, &task, o, replayed, out)
}

func overlayWorkflowTests(ctx context.Context, s *session, w *harness.Workflow, c harness.Snapshot, dir string) error {
	changes, err := s.repo.Diff(ctx, w.PreCode.Tree, c.Tree)
	if err != nil {
		return err
	}
	current, err := stageRawSources(ctx, s, c.Tree)
	if err != nil {
		return err
	}
	from, err := os.OpenRoot(s.repo.Root)
	if err != nil {
		return err
	}
	defer func() { _ = from.Close() }()
	to, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = to.Close() }()
	shared := map[string]harness.WorkflowTestRegion{}
	for _, region := range w.Plan.Shared {
		shared[region.Path] = region
	}
	for _, name := range workflowChangePaths(changes, w.PreCodeSources, current) {
		if !decl.MatchAny(w.Plan.TestPaths, name) || len(w.TestPolicy) > 0 && !decl.MatchAny(w.TestPolicy, name) {
			continue
		}
		if region, mixed := shared[name]; mixed {
			if err := overlayWorkflowShared(from, to, region); err != nil {
				return err
			}
			continue
		}
		if err := overlayWorkflowTestPath(from, to, name); err != nil {
			return err
		}
	}
	return nil
}

func overlayWorkflowShared(from, to *os.Root, region harness.WorkflowTestRegion) error {
	old, err := readSharedFile(to, region)
	if err != nil {
		return fmt.Errorf("recover pre-code test boundaries: %w", err)
	}
	now, err := readSharedFile(from, region)
	if err != nil {
		return err
	}
	full := append(append(append([]byte(nil), old.prefix...), now.body...), old.suffix...)
	return to.WriteFile(region.Path, full, 0o600)
}

func overlayWorkflowTestPath(from, to *os.Root, name string) error {
	info, err := from.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		err = to.Remove(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err != nil || !info.Mode().IsRegular() {
		return failf("cannot replay non-regular test path %q", name)
	}
	src, err := from.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	if err := to.MkdirAll(path.Dir(name), 0o700); err != nil {
		return err
	}
	dst, err := to.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	return errors.Join(copyErr, dst.Chmod(info.Mode().Perm()), dst.Close())
}
