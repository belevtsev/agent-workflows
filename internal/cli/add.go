package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
)

const addUsage = "usage: cw add <id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]"

func runAdd(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("add")
	var paths stringList
	flags.Var(&paths, "paths", "")
	timeout := flags.String("timeout", "", "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || !p.dash || len(p.rest) == 0 {
		return usagef("add needs one id and, after `--`, the program and its arguments; %s", addUsage)
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
		return failf("the check was not added, because the file would not be a valid declaration:\n%v", err)
	}
	if err := replaceFile(path, updated); err != nil {
		return failf("%v", err)
	}
	fmt.Fprintf(cfg.Out, "added the check `%s` to %s\n", p.pos[0], decl.FileName)
	return nil
}

// replaceFile swaps the content of a file in one step and keeps its mode.
func replaceFile(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cw-toml-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}
