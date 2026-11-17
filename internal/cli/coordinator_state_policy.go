package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

type coordinatorPolicyKeys struct {
	Checks       string              `json:"checks"`
	Verification string              `json:"verification"`
	Roles        string              `json:"roles"`
	Skills       string              `json:"skills"`
	Advisers     string              `json:"advisers"`
	Sources      string              `json:"source_visibility"`
	Problems     []map[string]any    `json:"problems"`
	References   map[string][]string `json:"references"`
}

func coordinatorPolicy(ctx context.Context, s *session, task string, state contextpack.State) (coordinatorPolicyKeys, error) {
	p := coordinatorPolicyKeys{Sources: state.SourcePolicy, Problems: []map[string]any{}, References: map[string][]string{
		"roles":         {"kitsu", "role", "list", "--json"},
		"advisers":      {"kitsu", "adviser", "list", "--task", task, "--json"},
		"checks":        {"kitsu", "check", "list", "--json"},
		"codex_skills":  {"kitsu", "skill", "list", "--host", "codex", "--json"},
		"claude_skills": {"kitsu", "skill", "list", "--host", "claude", "--json"},
	}}
	var err error
	p.Checks, err = stageCheckPolicy(ctx, s, state.Tree)
	if err != nil {
		return p, err
	}
	p.Verification, err = taskVerificationPolicyKey(ctx, s, state.Tree)
	if err != nil {
		return p, err
	}
	config, err := roles.Load(s.repo.Root)
	if err != nil {
		return p, err
	}
	instructions := map[string]string{}
	for _, role := range append(roles.IDs(), roles.Coordinator) {
		text, err := roles.Text(role)
		if err != nil {
			return p, err
		}
		instructions[role] = contextpack.Key(text)
	}
	p.Roles = contextpack.Key([]any{roles.All(config), config.Tests, instructions})
	selection := map[string]any{}
	for _, host := range roles.Hosts {
		catalog, err := skills.Discover(s.repo.Root, host)
		if err != nil {
			return p, err
		}
		warnings := slices.Clone(catalog.Warnings)
		slices.Sort(warnings)
		selection[host+":warnings"] = warnings
		for _, role := range append(roles.IDs(), roles.Coordinator) {
			selected := selectedSkills(config, host, role)
			entries, err := catalog.Resolve(selected)
			if err != nil {
				// Keep unavailable selections visible, without copying skill bodies
				// or a provider's private configuration into the view.
				problem := map[string]any{"host": host, "role": role, "reason_key": contextpack.Key(err.Error()), "reference": []string{"kitsu", "skill", "resolve", role, "--host", host}}
				p.Problems = append(p.Problems, problem)
			}
			selection[host+":"+role] = []any{selected, entries}
		}
	}
	p.Skills = contextpack.Key(selection)
	configured, err := advisers.Load(s.repo.Root)
	if err != nil {
		return p, err
	}
	binding, err := advisers.LoadBinding(s.repo.Root, task)
	if err != nil && !errors.Is(err, advisers.ErrNoBinding) {
		return p, err
	}
	p.Advisers = contextpack.Key([]any{configured, binding})
	return p, nil
}

// Bind ignored task history and raw bytes too: Git trees can hide both ignored
// records and edits concealed by clean filters. Reject links before parsing.
func coordinatorRecordKey(ctx context.Context, directory, task string) (string, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	files := map[string]string{}
	for _, dir := range []string{"openspec/changes/" + task, "openspec/changes/.skills/" + task, "openspec/changes/.advice/" + task, "openspec/changes/.verification/" + task, "openspec/changes/.usage/" + task} {
		if exists, err := coordinatorRealDir(root, dir); err != nil {
			return "", err
		} else if !exists {
			continue
		}
		err := fs.WalkDir(root.FS(), dir, func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() && !entry.Type().IsRegular() {
				return fmt.Errorf("task records must be real regular files/directories: %s", name)
			}
			if entry.IsDir() {
				return nil
			}
			if len(files) >= 4096 {
				return errors.New("too many task records for a consistent snapshot")
			}
			digest, err := stageRawSource(root, name)
			if err == nil {
				files[name] = digest
			}
			return err
		})
		if err != nil {
			return "", err
		}
	}
	return contextpack.Key(files), nil
}

func coordinatorRealDir(root *os.Root, name string) (bool, error) {
	part := ""
	for _, word := range strings.Split(name, "/") {
		part = path.Join(part, word)
		info, err := root.Lstat(part)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("task record directory is unsafe: %s", part)
		}
	}
	return true, nil
}
