package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func uxRoleDestination(ctx context.Context, cfg Config, global bool) (root, path, lock string, err error) {
	root, err = skillWorkingRoot(ctx)
	if err != nil {
		return "", "", "", err
	}
	if global {
		path, err = roles.PersonalPath()
		return root, path, path + ".lock", err
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return "", "", "", err
	}
	return s.repo.Root, filepath.Join(s.repo.Root, roles.FileName), filepath.Join(s.repo.CommonDir, "kitsu-roles.lock"), nil
}

func uxEditRole(ctx context.Context, cfg Config, role, host string, global, accessible bool, roots []string) error {
	root, path, lock, err := uxRoleDestination(ctx, cfg, global)
	if err != nil {
		return err
	}
	before, err := uxReadFile(path)
	if err != nil {
		return failf("%v", err)
	}
	config, related, err := uxEditorRoleConfig(root, global)
	if err != nil {
		return failf("%v", err)
	}
	configured := config.Version == roles.WorkflowConfigVersion
	if configured {
		if role == roles.Coordinator {
			return usagef("configure coordinator skills with --global")
		}
		if _, err := config.SpecialistHost(host, role); err != nil {
			return usagef("%v", err)
		}
	}
	catalog, err := skills.DiscoverWithRoots(root, host, roots)
	if err != nil {
		return failf("%v", err)
	}
	draft := &UXEditor{
		Kind: "role", Host: host, Role: role, Path: path, Global: global, Accessible: accessible,
		AvailableSkills: catalog.Skills, NativeEffort: configured,
	}
	draft.Skills = uxEditorSkillRefs(catalog, selectedSkills(config, host, role))
	for _, r := range roles.All(config) {
		if r.ID == role {
			draft.Model, draft.Reasoning = r.Models[host].Model, r.Models[host].Effort
		}
	}
	draft.Models, draft.ReasoningChoices = uxModelSuggestions(config, host, false), uxModelSuggestions(config, host, true)
	draft.Before = draft.summary()
	initial := *draft
	initial.Skills = slices.Clone(draft.Skills)
	if save, err := uxLaunchEditor(ctx, cfg, draft); err != nil || !save {
		return err
	}
	updated, err := uxRoleEditorData(root, roots, before, &initial, draft)
	if err != nil {
		return failf("role settings were not changed: %v", err)
	}
	if err := uxSaveFile(ctx, before, lock, related, updated); err != nil {
		return failf("role settings were not changed: %v", err)
	}
	var saved *roles.Config
	if global {
		saved, err = roles.ParsePersonal(updated)
	} else {
		saved, err = roles.Load(root)
	}
	if err != nil {
		return failf("%v", err)
	}
	return printSkillRoleSelection(cfg, host, role, saved, path, global, false)
}

func uxEditorRoleConfig(root string, global bool) (*roles.Config, []uxSnapshot, error) {
	if global {
		cfg, err := roles.LoadPersonal()
		return cfg, nil, err
	}
	personal, err := roles.PersonalPath()
	if err != nil {
		return nil, nil, err
	}
	file, err := uxReadFile(personal)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := roles.Load(root)
	return cfg, []uxSnapshot{file}, err
}

func uxEditorSkillRefs(catalog skills.Catalog, refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if entries, err := catalog.Resolve([]string{ref}); err == nil && len(entries) == 1 {
			ref = uxSkillSelector(entries[0])
		}
		out = append(out, ref)
	}
	return out
}

func uxRoleEditorData(root string, roots []string, file uxSnapshot, initial, draft *UXEditor) ([]byte, error) {
	if draft.Role != initial.Role || draft.Host != initial.Host || draft.Global != initial.Global || draft.NativeEffort != initial.NativeEffort {
		return nil, fmt.Errorf("editor scope changed; reopen for the intended role and host")
	}
	emptyEffort := strings.TrimSpace(draft.Reasoning) == ""
	if !draft.Global && draft.Role != roles.Coordinator && (strings.TrimSpace(draft.Model) == "" || emptyEffort && !draft.NativeEffort) {
		return nil, fmt.Errorf("model and reasoning must not be empty")
	}
	catalog, err := skills.DiscoverWithRoots(root, initial.Host, roots)
	if err != nil {
		return nil, err
	}
	entries, err := catalog.Resolve(draft.Skills)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(draft.Skills) {
		return nil, fmt.Errorf("duplicate skill selections")
	}
	var refs []string
	if !slices.Equal(initial.Skills, draft.Skills) {
		refs = []string{}
		for _, entry := range entries {
			refs = append(refs, uxSkillSelector(entry))
		}
	}
	data := file.data
	if !file.exists {
		data = []byte("version = 1\n")
	}
	updated, err := roles.EditSelection(data, initial.Host, initial.Role, uxRoleEditModel(initial, draft), refs, false, initial.Global)
	if err == nil && draft.NativeEffort && emptyEffort && initial.Reasoning != "" {
		return roles.ResetConfiguredEffort(updated, initial.Host, initial.Role)
	}
	return updated, err
}
