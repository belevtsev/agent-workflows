package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func validateRoleEdit(role string, model roles.Model, empty string, reset, global, skillsSet bool) error {
	if !roles.Known(role) && role != roles.Coordinator {
		return usagef("unknown role %q; the roles are %s, coordinator", role, strings.Join(roles.IDs(), ", "))
	}
	if (role == roles.Coordinator || global) && (model != (roles.Model{}) || empty != "") {
		return usagef("--global and coordinator permit only skill choices; the coordinator's model and reasoning belong to the host session")
	}
	if empty != "" {
		return usagef("--%s must not be empty; use `kitsu roles reset` to restore defaults", empty)
	}
	if !reset && model == (roles.Model{}) && !skillsSet {
		return usagef("set requires --model, --reasoning, or --skills; see `kitsu roles --help`")
	}
	return nil
}

func editRoleSelection(ctx context.Context, cfg Config, host, role string, model roles.Model, selected string, skillsSet bool, roots []string, reset, global bool) (*roles.Config, string, error) {
	root, err := skillWorkingRoot(ctx)
	if err != nil {
		return nil, "", err
	}
	path, lockPath := "", ""
	if global {
		path, err = roles.PersonalPath()
		lockPath = path + ".lock"
	} else {
		var s *session
		s, err = open(ctx, cfg, true)
		if err == nil {
			path = filepath.Join(s.repo.Root, roles.FileName)
			lockPath = filepath.Join(s.repo.CommonDir, "kitsu-roles.lock")
			_, err = roles.Load(root)
		}
	}
	if err != nil {
		return nil, "", err
	}
	var refs []string
	if skillsSet {
		refs, err = resolveSkillFlags(root, host, selected, roots)
		if err != nil {
			return nil, "", err
		}
	}
	if global {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, "", err
		}
	}
	config, err := writeRoleSelection(ctx, path, lockPath, host, role, model, refs, reset, global)
	if err == nil && !global {
		config, err = roles.Load(root)
	}
	return config, path, err
}

func resolveSkillFlags(root, host, selected string, roots []string) ([]string, error) {
	refs := []string{}
	if selected != "" {
		for _, ref := range strings.Split(selected, ",") {
			refs = append(refs, strings.TrimSpace(ref))
		}
	}
	catalog, err := skills.DiscoverWithRoots(root, host, roots)
	if err != nil {
		return nil, err
	}
	entries, err := catalog.Resolve(refs)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(refs) {
		return nil, fmt.Errorf("--skills contains duplicate references to the same skill")
	}
	for i, entry := range entries {
		refs[i] = entry.ID
		if entry.Scope == "explicit" {
			refs[i] = entry.Path
		}
	}
	return refs, nil
}

func writeRoleSelection(ctx context.Context, path, lockPath, host, role string, model roles.Model, refs []string, reset, personal bool) (*roles.Config, error) {
	lock, err := fslock.Acquire(ctx, lockPath, fslock.Options{Wait: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()
	data, mode, err := readRolesForEdit(path)
	if err != nil {
		return nil, err
	}
	updated, err := roles.EditSelection(data, host, role, model, refs, reset, personal)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, updated) {
		if err := store.WriteFile(path, updated, mode); err != nil {
			return nil, err
		}
	}
	if personal {
		return roles.ParsePersonal(updated)
	}
	return roles.Parse(updated)
}

func printSkillRoleSelection(cfg Config, host, role string, config *roles.Config, path string, global, asJSON bool) error {
	model := roles.Model{Model: "host session", Effort: "host setting"}
	for _, r := range roles.All(config) {
		if r.ID == role {
			model = r.Models[host]
		}
	}
	scope := "repository"
	if global {
		scope, model = "personal", roles.Model{}
	}
	refs := selectedSkills(config, host, role)
	if asJSON {
		return printJSON(cfg, struct {
			Role  string `json:"role"`
			Host  string `json:"host"`
			Scope string `json:"scope"`
			roles.Model
			Skills []string `json:"skills"`
		}{role, host, scope, model, refs})
	}
	writeRoleLine(cfg.Out, fmt.Sprintf("%s role settings (%s):", strings.ToUpper(scope[:1])+scope[1:], path))
	if global {
		writeRoleLine(cfg.Out, "ROLE  HOST  SKILLS")
		writeRoleLine(cfg.Out, role+"  "+host)
	} else {
		writeRoleLine(cfg.Out, "ROLE  HOST  MODEL  REASONING")
		writeRoleLine(cfg.Out, fmt.Sprintf("%s  %s  %s  %s", role, host, model.Model, model.Effort))
	}
	writeRoleLine(cfg.Out, "    Skills: "+skillListText(refs))
	return nil
}
