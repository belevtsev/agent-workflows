package gitx

import (
	"context"
	"strings"
)

// ContextIgnored lists tracked paths matching ignore rules, for context discovery.
// Untracked ignored paths are already excluded from a Snapshot.
func (r *Repo) ContextIgnored(ctx context.Context) (map[string]bool, error) {
	out, err := r.git(ctx, nil, "ls-files", "--cached", "--ignored", "--exclude-standard", "-z")
	ignored := map[string]bool{}
	for _, name := range strings.Split(out, "\x00") {
		ignored[name] = true
	}
	return ignored, err
}
