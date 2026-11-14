package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
)

const addUsage = "usage: cw add <id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]"

func runAdd(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("add")
	var paths stringList
	flags.Var(&paths, "paths", "")
	timeout := flags.String("timeout", "", "")
	p, err := parseArgs(flags, args)
	if err != nil && !slices.Contains(args, "--") {
		return usagef("%v; the program and its arguments go after `--`; %s", err, addUsage)
	}
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || !p.dash || len(p.rest) == 0 {
		return usagef("add needs one id and, after `--`, the program and its arguments; %s", addUsage)
	}
	if _, err := exec.LookPath(p.rest[0]); err != nil && strings.ContainsAny(p.rest[0], " \t") {
		return usagef("the program `%s` is one word with a space in it, and no such program exists; write the program and each argument as words of their own, with no quotes around the whole command", p.rest[0])
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	path := filepath.Join(s.repo.Root, decl.FileName)
	old, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return failf("this repository has no %s; `cw init` creates one", decl.FileName)
	}
	if err != nil {
		return failf("%v", err)
	}
	updated, err := decl.Append(old, p.pos[0], p.rest, paths, *timeout)
	if err != nil {
		return failf("the check was not added, because the file would not be a valid declaration:\n%v\n`cw add` only appends a check; to change or remove one, edit %s", err, decl.FileName)
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := store.WriteFile(path, updated, mode); err != nil {
		return failf("%v", err)
	}
	fmt.Fprintf(cfg.Out, "added the check `%s` to %s\n", p.pos[0], decl.FileName)
	return nil
}
