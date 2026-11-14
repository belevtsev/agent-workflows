package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/gate"
)

// runUninstall removes what cw wrote into the git directory: its hooks and
// its records. The declaration and every hook of another tool stay.
func runUninstall(ctx context.Context, cfg Config, args []string) error {
	if err := noArguments("uninstall", args); err != nil {
		return err
	}
	s, err := open(ctx, cfg, false)
	if err != nil {
		return err
	}
	_, statErr := os.Stat(s.store.Dir)
	removed, foreign, err := gate.Uninstall(ctx, s.repo)
	if err != nil {
		return failf("the hooks cannot be removed: %v", err)
	}
	if err := s.store.Remove(); err != nil {
		return failf("the records in %s cannot be removed: %v", s.store.Dir, err)
	}
	if removed == 0 && statErr != nil {
		fmt.Fprintf(cfg.Out, "nothing to remove: cw wrote no hook in this clone and keeps no records in %s\n", s.store.Dir)
	} else {
		fmt.Fprintf(cfg.Out, "removed the hooks cw wrote and its records in %s\n", s.store.Dir)
	}
	if _, err := os.Stat(filepath.Join(s.repo.Root, decl.FileName)); err == nil {
		fmt.Fprintf(cfg.Out, "%s is left as it is\n", decl.FileName)
	}
	if len(foreign) == 0 {
		return nil
	}
	fmt.Fprintln(cfg.Out, "these hooks were not written by cw and still call it; remove the line from each:")
	for _, hook := range foreign {
		fmt.Fprintf(cfg.Out, "  %s\n      %s\n", hook.Path, gate.Line(hook.Name))
	}
	return exit(ExitForeignHook)
}
