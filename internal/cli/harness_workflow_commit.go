package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/belevtsev/kitsu/v3/internal/gate"
)

// The configured check runner is the only place task-written code executes.
// Git still runs its normal Kitsu hooks when creating the requested commit.
func checkWorkflowCommitHooks(ctx context.Context, s *session) error {
	dir, custom, err := s.repo.HooksDir(ctx)
	if err != nil {
		return err
	}
	if custom {
		return failf("configured commits require Git's standard hooks directory; use --until verify and commit through your repository's hooks")
	}
	for _, name := range []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "post-index-change", "reference-transaction"} {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if name == "pre-commit" && info.Mode().IsRegular() && info.Size() <= 4096 {
			if data, err := os.ReadFile(path); err == nil && string(data) == gate.Script(name) {
				continue
			}
		}
		return failf("configured commit would execute an unreviewed local Git hook %q; verify and commit through your repository's hooks", name)
	}
	return nil
}
