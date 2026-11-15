package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func runRolesEdit(ctx context.Context, cfg Config, args []string, reset bool) error {
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
		return err
	}
	if len(p.pos) != 1 || p.dash || (*host != "claude" && *host != "codex") {
		return usagef("%s", usage)
	}
	role := p.pos[0]
	var empty string
	var skillsSet bool
	flags.Visit(func(f *flag.Flag) {
		skillsSet = skillsSet || f.Name == "skills"
		if (f.Name == "model" || f.Name == "reasoning") && strings.TrimSpace(f.Value.String()) == "" {
			empty = f.Name
		}
	})
	if err := validateRoleEdit(role, roles.Model{Model: model, Effort: reasoning}, empty, reset, *global, skillsSet); err != nil {
		return err
	}
	if len(roots) > 0 && !skillsSet {
		return usagef("--root requires --skills")
	}
	config, path, err := editRoleSelection(ctx, cfg, *host, role, roles.Model{Model: model, Effort: reasoning}, selected, skillsSet, roots, reset, *global)
	if err != nil {
		return failf("role settings were not changed: %v", err)
	}
	return printSkillRoleSelection(cfg, *host, role, config, path, *global, *asJSON)
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
