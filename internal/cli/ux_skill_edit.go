package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

type uxSkillChange struct {
	action, host, role, ref string
	roots                   []string
	global                  bool
}

func runUXSkillEdit(ctx context.Context, cfg Config, action string, args []string) error {
	flags := newFlags("skill " + action)
	host, role := flags.String("host", "", ""), flags.String("role", "", "")
	global, asJSON := flags.Bool("global", false, ""), flags.Bool("json", false, "")
	var roots stringList
	flags.Var(&roots, "root", "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	needsSkill := action == "attach" || action == "detach"
	if p.dash || needsSkill && len(p.pos) != 1 || !needsSkill && len(p.pos) != 0 || !slices.Contains(roles.Hosts, *host) || !roles.Known(*role) && *role != roles.Coordinator {
		return usagef("%s requires a valid --host, --role and %s", action, map[bool]string{true: "one skill reference", false: "no skill argument"}[needsSkill])
	}
	change := uxSkillChange{action: action, host: *host, role: *role, roots: roots, global: *global}
	if needsSkill {
		change.ref = p.pos[0]
	}
	root, path, lock, err := uxRoleDestination(ctx, cfg, *global)
	if err != nil {
		return err
	}
	config, err := uxWriteSkillChange(ctx, root, path, lock, change)
	if err != nil {
		return failf("skill settings were not changed: %v", err)
	}
	return printSkillRoleSelection(cfg, *host, *role, config, path, *global, *asJSON)
}

func uxWriteSkillChange(ctx context.Context, root, path, lockPath string, change uxSkillChange) (*roles.Config, error) {
	if change.global {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	lock, err := fslock.Acquire(ctx, lockPath, fslock.Options{Wait: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()
	config, err := uxSkillScope(root, change.global)
	if err != nil {
		return nil, err
	}
	refs := slices.Clone(selectedSkills(config, change.host, change.role))
	if refs == nil {
		refs = []string{}
	}
	switch change.action {
	case "attach":
		var changed bool
		refs, changed, err = uxAttachSkill(root, change, refs)
		if err == nil && !changed {
			return config, nil
		}
	case "detach":
		refs, err = uxDetachSkill(refs, change.ref)
	case "clear":
		refs = []string{}
	}
	if err != nil {
		return nil, err
	}
	data, mode, err := readRolesForEdit(path)
	if err != nil {
		return nil, err
	}
	var updated []byte
	if change.action == "reset" {
		updated, err = roles.ResetSkillSelection(data, change.host, change.role, change.global)
	} else {
		updated, err = roles.EditSelection(data, change.host, change.role, roles.Model{}, refs, false, change.global)
	}
	if err == nil && !bytes.Equal(data, updated) {
		err = store.WriteFile(path, updated, mode)
	}
	if err != nil {
		return nil, err
	}
	return uxSkillScope(root, change.global)
}

func uxSkillScope(root string, global bool) (*roles.Config, error) {
	if global {
		return roles.LoadPersonal()
	}
	return roles.Load(root)
}

func uxAttachSkill(root string, change uxSkillChange, refs []string) ([]string, bool, error) {
	catalog, err := skills.DiscoverWithRoots(root, change.host, change.roots)
	if err != nil {
		return nil, false, err
	}
	added, err := catalog.Resolve([]string{change.ref})
	if err != nil {
		return nil, false, err
	}
	for _, ref := range refs {
		if ref == uxSkillSelector(added[0]) {
			return refs, false, nil
		}
		if current, err := catalog.Resolve([]string{ref}); err == nil && len(current) == 1 && (current[0].ID == added[0].ID || current[0].Path == added[0].Path) {
			return refs, false, nil
		}
	}
	return append(refs, uxSkillSelector(added[0])), true, nil
}

func uxDetachSkill(refs []string, requested string) ([]string, error) {
	index := slices.Index(refs, requested)
	if index < 0 {
		for i, ref := range refs {
			if ref[strings.LastIndexAny(ref, ":/\\")+1:] != requested {
				continue
			}
			if index >= 0 {
				return nil, fmt.Errorf("selected skill name %q is ambiguous; use an exact configured reference", requested)
			}
			index = i
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("skill %q is not selected; use a reference from role show", requested)
	}
	return append(refs[:index:index], refs[index+1:]...), nil
}
