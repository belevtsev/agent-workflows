package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func runRoleInit(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("role init")
	host := flags.String("host", "", "")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash || uxFlagSet(flags, "host") && !uxValidHost(*host) {
		return usagef("usage: kitsu role init [--host codex|claude] [--json]")
	}
	_, path, lock, err := uxRoleDestination(ctx, cfg, false)
	if err != nil {
		return err
	}
	before, err := uxReadFile(path)
	if err != nil {
		return failf("%v", err)
	}
	if before.exists {
		return failf("%s already exists. Inspect it with `kitsu role list`.", path)
	}
	if *host == "" {
		value, err := uxInstalledHost()
		if err != nil {
			return failf("%v", err)
		}
		if value == "" {
			return usagef("choose a default with `kitsu role init --host codex|claude`")
		}
		*host = value
	}
	data := roles.WorkflowTemplate(*host)
	if _, err := roles.Parse(data); err != nil {
		return failf("%v", err)
	}
	if err := uxSaveFile(ctx, before, lock, nil, data); err != nil {
		return failf("initialize role settings: %v", err)
	}
	if *asJSON {
		return printJSON(cfg, map[string]string{"path": path, "host": *host})
	}
	fmt.Fprintf(cfg.Out, "Created %s with %s role assignments. Inspect them with kitsu role list.\n", path, *host)
	return nil
}

func dailyRoleConfig(root string) (*roles.Config, error) {
	config, err := roles.Load(root)
	if err != nil || config.Version == roles.WorkflowConfigVersion {
		return config, err
	}
	if _, err := os.Lstat(filepath.Join(root, roles.FileName)); !errors.Is(err, fs.ErrNotExist) {
		return config, err
	}
	host, err := uxInstalledHost()
	if err != nil {
		return config, err
	}
	if host == "" {
		return nil, usagef("create role assignments with `kitsu role init --host codex|claude`")
	}
	return roles.LoadWithWorkflowDefault(root, host)
}
