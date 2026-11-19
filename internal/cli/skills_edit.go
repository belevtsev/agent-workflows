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

type roleSelectionChange struct {
	host, role, selection  string
	model                  roles.Model
	roots                  []string
	reset, personal        bool
	skillsSet, resetEffort bool
}

func editRoleSelection(ctx context.Context, cfg Config, change roleSelectionChange) (*roles.Config, string, error) {
	root, path, lockPath, err := uxRoleDestination(ctx, cfg, change.personal)
	if err != nil {
		return nil, "", err
	}
	if !change.personal {
		if _, err := roles.Load(root); err != nil {
			return nil, "", err
		}
	}
	if change.personal {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, "", err
		}
	}
	config, err := updateRoleSelection(ctx, path, lockPath, change.personal, func(data []byte) ([]byte, error) {
		return change.apply(data, root)
	})
	if err == nil && !change.personal {
		config, err = roles.Load(root)
	}
	return config, path, err
}

func (o roleSelectionChange) apply(data []byte, root string) ([]byte, error) {
	parse := roles.Parse
	if o.personal {
		parse = roles.ParsePersonal
	}
	config, err := parse(data)
	if err != nil {
		return nil, err
	}
	host, err := o.editHost(config)
	if err != nil {
		return nil, err
	}
	var refs []string
	if o.skillsSet {
		refs, err = resolveSkillFlags(root, host, o.selection, o.roots)
		if err != nil {
			return nil, err
		}
	}
	model := o.model
	if o.resetEffort {
		model.Effort = ""
	}
	updated, err := roles.EditSelection(data, host, o.role, model, refs, o.reset, o.personal)
	if err == nil && o.resetEffort {
		return roles.ResetConfiguredEffort(updated, host, o.role)
	}
	return updated, err
}

func (o roleSelectionChange) editHost(config *roles.Config) (string, error) {
	if !roles.Known(o.role) && o.role != roles.Coordinator {
		return "", usagef("unknown role %q; the roles are %s, coordinator", o.role, strings.Join(roles.IDs(), ", "))
	}
	if (o.role == roles.Coordinator || o.personal) && (o.model != (roles.Model{}) || o.resetEffort) {
		return "", usagef("--global and coordinator permit only skill choices; the coordinator's model and reasoning belong to the host session")
	}
	configured := config.Version == roles.WorkflowConfigVersion
	if o.resetEffort && !configured {
		return "", usagef("--reasoning must not be empty; use `kitsu role reset` to restore defaults")
	}
	if !o.reset && o.model == (roles.Model{}) && !o.skillsSet && !o.resetEffort && !(configured && o.host != "") {
		return "", usagef("set requires --model, --reasoning, --skills, or a host assignment for a version 2 role")
	}
	if configured && (o.host == "" || o.reset) {
		host, err := config.SpecialistHost(o.host, o.role)
		if err != nil {
			return "", usagef("%v", err)
		}
		return host, nil
	}
	if !uxValidHost(o.host) {
		return "", usagef("select --host codex|claude")
	}
	return o.host, nil
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
	return updateRoleSelection(ctx, path, lockPath, personal, func(data []byte) ([]byte, error) {
		return roles.EditSelection(data, host, role, model, refs, reset, personal)
	})
}

func updateRoleSelection(ctx context.Context, path, lockPath string, personal bool, edit func([]byte) ([]byte, error)) (*roles.Config, error) {
	lock, err := fslock.Acquire(ctx, lockPath, fslock.Options{Wait: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()
	data, mode, err := readRolesForEdit(path)
	if err != nil {
		return nil, err
	}
	updated, err := edit(data)
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
		writeRoleLine(cfg.Out, fmt.Sprintf("%s  %s  %s  %s", role, host, model.Model, roleEffortText(model.Effort)))
	}
	writeRoleLine(cfg.Out, "    Skills: "+skillListText(refs))
	return nil
}
