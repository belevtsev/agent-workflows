package cli

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
)

const undeclaredPolicy = "undeclared"

func checkDeclaration(ctx context.Context, repo *gitx.Repo, tree string) ([]byte, string, error) {
	data, found, err := repo.Declaration(ctx, tree, decl.FileName)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, undeclaredPolicy, nil
	}
	return data, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func pinHarnessPolicy(ctx context.Context, s *session, task *harness.Task) error {
	if task.RepositoryPolicy != "" {
		return nil
	}
	base := task.Start.Head
	if base == "" {
		base = task.Start.Tree // Use the opening tree before the first commit.
	}
	_, fingerprint, err := checkDeclaration(ctx, s.repo, base)
	if err != nil {
		return fmt.Errorf("read opening repository check policy: %w", err)
	}
	task.RepositoryPolicy = fingerprint
	// Older proofs can establish the same policy from their recorded Git tree.
	if proof := task.Proof; proof != nil && proof.RepositoryPolicy == "" && checkHarnessPolicy(ctx, s, task, proof.Candidate.Tree) == nil {
		proof.RepositoryPolicy = fingerprint
	}
	return nil
}

func checkHarnessPolicy(ctx context.Context, s *session, task *harness.Task, candidate string) error {
	_, fingerprint, err := checkDeclaration(ctx, s.repo, candidate)
	if err != nil {
		return fmt.Errorf("read candidate repository check policy: %w", err)
	}
	if fingerprint != task.RepositoryPolicy {
		return fmt.Errorf("kitsu.toml differs from the opening policy (%s). Restore the opening declaration or open a new task from a commit with the reviewed policy", task.RepositoryPolicy)
	}
	return nil
}

func prepareHarnessVerification(ctx context.Context, s *session, task *harness.Task, candidate harness.Snapshot) error {
	if err := checkHarnessPolicy(ctx, s, task, candidate.Tree); err != nil {
		return failf("%v", err)
	}
	if task.Workflow != nil {
		return failf("configured checks require a trusted workflow runner")
	}
	if task.NeedsRepairAuthority(candidate) {
		return failf("candidate changed after failed verification. Authorize repair with kitsu task extend %s --repair", task.ID)
	}
	task.PinVerifyOnlyFailure()
	if err := pinHarnessCommitTarget(ctx, s, task); err != nil {
		return err
	}
	return s.checkHarnessCommitTarget(ctx, task.CommitTarget)
}
