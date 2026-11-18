package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gate"
)

// Detach owned hooks first. Purging common check evidence is a separate opt-in.
func runUninstall(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("uninstall")
	purge := flags.Bool("purge", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash {
		return usagef("uninstall accepts only --purge")
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
	if removed == 0 && os.IsNotExist(statErr) {
		fmt.Fprintln(cfg.Out, "nothing to remove: no owned hooks or check records")
	} else {
		fmt.Fprintf(cfg.Out, "removed the hooks kitsu wrote, %d in all\n", removed)
	}
	if *purge && len(foreign) == 0 {
		if err := s.store.Remove(); err != nil {
			return failf("remove check records: %v", err)
		}
		fmt.Fprintf(cfg.Out, "purged check and token records in %s\n", s.store.Dir)
	} else if statErr == nil {
		fmt.Fprintf(cfg.Out, "retained check and token records in %s\n", s.store.Dir)
	}
	if _, err := os.Stat(filepath.Join(s.repo.Root, decl.FileName)); err == nil {
		fmt.Fprintf(cfg.Out, "%s is left as it is\n", decl.FileName)
	}
	if len(foreign) == 0 {
		return nil
	}
	fmt.Fprintln(cfg.Out, "these hooks are not as kitsu wrote them and still call it; remove the call from each:")
	for _, hook := range foreign {
		fmt.Fprintf(cfg.Out, "  %s\n      %s\n", hook.Path, decl.Plain(hook.Line))
	}
	return exit(ExitForeignHook)
}
