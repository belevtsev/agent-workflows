package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
)

const newDeclaration = `# The checks this repository requires before a commit or a push.
# Add one with:  cw add <id> -- <program> [<argument>...]
version = 1
`

func runInit(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("init")
	observe := flags.Bool("observe", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash {
		return usagef("init takes no arguments; usage: cw init [--observe]")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	created, err := createDeclaration(filepath.Join(s.repo.Root, decl.FileName))
	if err != nil {
		return failf("%v", err)
	}
	foreign, err := gate.Install(ctx, s.repo)
	if err != nil {
		return failf("the hooks cannot be installed: %v", err)
	}
	mode := store.Enforce
	if *observe {
		mode = store.Observe
	}
	if err := s.store.SetMode(mode); err != nil {
		return failf("the mode cannot be written to %s: %v", s.store.Dir, err)
	}
	if created {
		fmt.Fprintf(cfg.Out, "created %s with no checks\n", decl.FileName)
	} else {
		fmt.Fprintf(cfg.Out, "%s is kept as it is\n", decl.FileName)
	}
	if installed := installedHooks(foreign); len(installed) > 0 {
		fmt.Fprintf(cfg.Out, "installed the hooks %s\n", strings.Join(installed, ", "))
	}
	if mode == store.Observe {
		fmt.Fprintln(cfg.Out, "the gate observes: each hook records what it would have decided and allows the operation")
	} else {
		fmt.Fprintln(cfg.Out, "the gate enforces: a commit or a push needs a passing run of every declared check")
	}
	fmt.Fprintln(cfg.Out, "next: cw add <id> -- <program> [<argument>...], then cw run")
	if len(foreign) == 0 {
		return nil
	}
	printForeign(cfg, s, foreign)
	return exit(ExitForeignHook)
}

// createDeclaration writes a new cw.toml and leaves an existing one alone.
func createDeclaration(path string) (created bool, err error) {
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, os.WriteFile(path, []byte(newDeclaration), 0o644)
}

func installedHooks(foreign []gate.Foreign) []string {
	var installed []string
	for _, name := range gate.Hooks {
		left := false
		for _, f := range foreign {
			left = left || f.Name == name
		}
		if !left {
			installed = append(installed, name)
		}
	}
	return installed
}

// printForeign tells the user which hooks cw left alone and the line to add
// to each.
func printForeign(cfg Config, s *session, foreign []gate.Foreign) {
	if _, configured, _ := s.repo.HooksDir(context.Background()); configured {
		fmt.Fprintln(cfg.Err, "cw: core.hooksPath is set, so cw wrote no hook. That directory belongs to its owner.")
	} else {
		fmt.Fprintln(cfg.Err, "cw: these hooks were written by another tool, and cw did not change them.")
	}
	fmt.Fprintln(cfg.Err, "cw: add the line shown to each hook. It must be the last command of that hook:")
	for _, f := range foreign {
		fmt.Fprintf(cfg.Err, "  %s\n      %s\n", f.Path, gate.Line(f.Name))
	}
}
