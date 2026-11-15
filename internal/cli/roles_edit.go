package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func runRolesEdit(ctx context.Context, cfg Config, args []string, reset bool) error {
	flags := newFlags("roles set")
	host := flags.String("host", "", "")
	asJSON := flags.Bool("json", false, "")
	var model, reasoning string
	usage := "usage: kitsu roles reset <role> --host claude|codex [--json]"
	if !reset {
		flags.StringVar(&model, "model", "", "")
		flags.StringVar(&reasoning, "reasoning", "", "")
		usage = "usage: kitsu roles set <role> --host claude|codex [--model <model>] [--reasoning <effort>] [--json]"
	}
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || p.dash || (*host != "claude" && *host != "codex") {
		return usagef("%s", usage)
	}
	role := p.pos[0]
	if role == roles.Coordinator {
		return usagef("the coordinator uses your host session's model and reasoning; select them in the host")
	}
	if !roles.Known(role) {
		return usagef("unknown role %q; the roles are %s", role, strings.Join(roles.IDs(), ", "))
	}
	var empty string
	flags.Visit(func(f *flag.Flag) {
		if (f.Name == "model" || f.Name == "reasoning") && strings.TrimSpace(f.Value.String()) == "" {
			empty = f.Name
		}
	})
	if empty != "" {
		return usagef("--%s must not be empty; use `kitsu roles reset %s --host %s` to restore defaults", empty, role, *host)
	}
	if !reset && model == "" && reasoning == "" {
		return usagef("set requires --model, --reasoning, or both; %s", usage)
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	config, err := s.editRoles(ctx, *host, role, roles.Model{Model: model, Effort: reasoning}, reset)
	if err != nil {
		return failf("role settings were not changed: %v", err)
	}
	for _, r := range roles.All(config) {
		if r.ID == role {
			return printRoleSelection(cfg, *host, role, r.Models[*host], *asJSON)
		}
	}
	return nil
}

func (s *session) editRoles(ctx context.Context, host, role string, model roles.Model, reset bool) (*roles.Config, error) {
	lock, err := fslock.Acquire(ctx, filepath.Join(s.repo.CommonDir, "kitsu-roles.lock"), fslock.Options{Wait: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()
	path := filepath.Join(s.repo.Root, roles.FileName)
	data, mode, err := readRolesForEdit(path)
	if err != nil {
		return nil, err
	}
	updated, err := roles.Edit(data, host, role, model, reset)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, updated) {
		if err := store.WriteFile(path, updated, mode); err != nil {
			return nil, err
		}
	}
	return roles.Parse(updated)
}

func readRolesForEdit(path string) ([]byte, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []byte("version = 1\n"), 0o644, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("%s must be a regular file, not a link or directory", roles.FileName)
	}
	data, err := os.ReadFile(path)
	return data, info.Mode().Perm(), err
}

func printRoleSelection(cfg Config, host, role string, model roles.Model, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(cfg.Out).Encode(struct {
			Role string `json:"role"`
			Host string `json:"host"`
			roles.Model
		}{role, host, model})
	}
	fmt.Fprintf(cfg.Out, "Repository role settings (%s):\n", roles.FileName)
	w := tabwriter.NewWriter(cfg.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROLE\tHOST\tMODEL\tREASONING")
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", role, host, model.Model, model.Effort)
	return w.Flush()
}
