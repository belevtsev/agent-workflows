package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func runRolesEdit(ctx context.Context, cfg Config, args []string, reset bool) error {
	change, asJSON, err := parseRoleSelection(args, reset)
	if err != nil {
		return err
	}
	config, path, err := editRoleSelection(ctx, cfg, change)
	if err != nil {
		if ExitCode(err) == ExitUsage {
			return err
		}
		return failf("role settings were not changed: %v", err)
	}
	host := change.host
	if config.Version == roles.WorkflowConfigVersion {
		host = config.ConfiguredHost(change.role)
	}
	return printSkillRoleSelection(cfg, host, change.role, config, path, change.personal, asJSON)
}

func parseRoleSelection(args []string, reset bool) (roleSelectionChange, bool, error) {
	flags := newFlags("roles set")
	host := flags.String("host", "", "")
	asJSON := flags.Bool("json", false, "")
	global := flags.Bool("global", false, "")
	var model, reasoning, selected string
	var roots stringList
	usage := "usage: kitsu roles reset <role> --host claude|codex [--global] [--json]"
	if !reset {
		flags.StringVar(&model, "model", "", "")
		flags.StringVar(&reasoning, "reasoning", "", "")
		flags.StringVar(&selected, "skills", "", "")
		flags.Var(&roots, "root", "")
		usage = "usage: kitsu roles set <role> --host claude|codex [--model <model>] [--reasoning <effort>] [--skills <references>] [--global] [--root <directory>] [--json]"
	}
	p, err := parseArgs(flags, args)
	if err != nil {
		return roleSelectionChange{}, false, err
	}
	if len(p.pos) != 1 || p.dash || uxFlagSet(flags, "host") && !uxValidHost(*host) {
		return roleSelectionChange{}, false, usagef("%s", usage)
	}
	if uxFlagSet(flags, "model") && strings.TrimSpace(model) == "" {
		return roleSelectionChange{}, false, usagef("--model must not be empty; use `kitsu role reset` to restore defaults")
	}
	change := roleSelectionChange{
		host: *host, role: p.pos[0], model: roles.Model{Model: model, Effort: reasoning},
		selection: selected, roots: roots, reset: reset, personal: *global,
		skillsSet: uxFlagSet(flags, "skills"), resetEffort: uxFlagSet(flags, "reasoning") && strings.TrimSpace(reasoning) == "",
	}
	if len(roots) > 0 && !change.skillsSet {
		return roleSelectionChange{}, false, usagef("--root requires --skills")
	}
	return change, *asJSON, nil
}

func (s *session) editRoles(ctx context.Context, host, role string, model roles.Model, reset bool) (*roles.Config, error) {
	path := filepath.Join(s.repo.Root, roles.FileName)
	return writeRoleSelection(ctx, path, filepath.Join(s.repo.CommonDir, "kitsu-roles.lock"), host, role, model, nil, reset, false)
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
		return nil, 0, fmt.Errorf("%s must be a regular file, not a link or directory", path)
	}
	data, err := os.ReadFile(path)
	return data, info.Mode().Perm(), err
}
