package gitx

import "context"

// EmptyTree writes and returns the empty tree in this repository's object format.
// It changes neither the working tree nor the index or refs.
func (r *Repo) EmptyTree(ctx context.Context) (string, error) {
	return r.git(ctx, nil, "hash-object", "-w", "-t", "tree", "--stdin")
}
