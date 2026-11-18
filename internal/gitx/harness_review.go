package gitx

import (
	"context"
	"fmt"
)

// ReviewCommit resolves a local commit and its original tree. Replacement
// objects would let the content change while the recorded commit ID stays fixed.
func (r *Repo) ReviewCommit(ctx context.Context, rev string) (string, string, error) {
	const noReplace = "GIT_NO_REPLACE_OBJECTS=1"
	commit, found, err := r.resolve(ctx, rev+"^{commit}", noReplace)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", fmt.Errorf("%q does not resolve to a local commit", rev)
	}
	tree, found, err := r.resolve(ctx, commit+"^{tree}", noReplace)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", fmt.Errorf("tree is unavailable for commit %s", commit)
	}
	return commit, tree, nil
}

// EmptyTreeID computes the empty tree ID in the repository's object format
// without writing objects. Git can compare against it on an unborn branch.
func (r *Repo) EmptyTreeID(ctx context.Context) (string, error) {
	return r.git(ctx, nil, "hash-object", "-t", "tree", "--stdin")
}
