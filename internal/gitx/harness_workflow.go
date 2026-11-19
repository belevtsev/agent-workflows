package gitx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StageWorkflowCandidate installs an already checked tree into the index only
// if the original staging and branch are still current. Its caller authorizes
// the full tree (including any pre-existing edits) before making this call.
func (r *Repo) StageWorkflowCandidate(ctx context.Context, tmp, expectedTarget, head, expectedIndex, tree string) error {
	original, err := readIndexBytes(r.indexPath())
	if err != nil {
		return err
	}
	index, cleanup, err := r.copyIndex(tmp)
	if err != nil {
		return err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + index}
	before, err := r.git(ctx, env, "write-tree")
	if err != nil {
		return err
	}
	if before != expectedIndex {
		return fmt.Errorf("index changed before staging the workflow candidate")
	}
	if _, err := r.git(ctx, env, "read-tree", tree); err != nil {
		return err
	}
	info, err := os.Stat(index)
	if err != nil {
		return err
	}
	replacement, err := readIndexBytes(index)
	if err != nil {
		return err
	}
	target, err := r.CommitTarget(ctx)
	if err != nil {
		return err
	}
	if target != expectedTarget {
		return fmt.Errorf("checkout changed before staging the workflow candidate")
	}
	return r.replaceCommittedIndex(ctx, original, replacement, info.ModTime(), target, head)
}

// CheckoutWorkflowTree materializes a saved tree into a new private directory.
// It uses an alternate index and never stages or checks out the real worktree.
func (r *Repo) CheckoutWorkflowTree(ctx context.Context, tmp, tree, dest string) error {
	index, closeIndex, err := r.copyIndex(tmp)
	if err != nil {
		return err
	}
	defer closeIndex()
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := r.git(ctx, env, "read-tree", tree); err != nil {
		return err
	}
	prefix := filepath.ToSlash(dest)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	_, err = r.git(ctx, env, "checkout-index", "--all", "--prefix="+prefix)
	return err
}

func InitWorkflowReplay(ctx context.Context, root string) (*Repo, error) {
	if _, err := run(ctx, call{dir: root}, "init", "--quiet", "--template="); err != nil {
		return nil, err
	}
	return Open(ctx, root)
}
