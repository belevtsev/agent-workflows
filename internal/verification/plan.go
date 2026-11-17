// Package verification plans and records behavioral verification without
// changing the declared check gate or its evidence dependencies.
package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

// Plan is a read-only verification policy for a complete source snapshot.
type Plan struct {
	Version    int         `json:"version"`
	BaseTree   string      `json:"base_tree"`
	SourceTree string      `json:"source_tree"`
	PolicyKey  string      `json:"policy_key"`
	Checks     []PlanCheck `json:"checks"`
	Uncovered  []string    `json:"uncovered"`
}

// PlanCheck retains every declared gate check, with its verification priority.
type PlanCheck struct {
	ID           string   `json:"id"`
	Expect       string   `json:"expect"`
	Reason       string   `json:"reason"`
	Priority     bool     `json:"priority"`
	Required     bool     `json:"required"`
	MatchedPaths []string `json:"matched_paths"`
}

// Build reads the declaration from tree and compares the complete snapshot to
// from (HEAD by default). No check is executed and no evidence is modified.
func Build(ctx context.Context, repo *gitx.Repo, tree, from string) (Plan, error) {
	plan := Plan{Version: 1, Checks: []PlanCheck{}, Uncovered: []string{}}
	source, found, err := repo.TreeOf(ctx, tree)
	if err != nil {
		return plan, err
	}
	if !found {
		return plan, fmt.Errorf("source %q does not name a tree", decl.Plain(tree))
	}
	plan.SourceTree = source
	if from == "" {
		from = "HEAD"
	}
	base, found, err := repo.TreeOf(ctx, from)
	if err != nil {
		return plan, err
	}
	if !found {
		if from != "HEAD" {
			return plan, fmt.Errorf("base %q does not name a tree", decl.Plain(from))
		}
		base, err = repo.EmptyTree(ctx)
		if err != nil {
			return plan, err
		}
	}
	plan.BaseTree = base
	data, found, err := repo.Declaration(ctx, source, decl.FileName)
	if err != nil {
		return plan, err
	}
	if !found {
		return plan, fmt.Errorf("source tree has no %s", decl.FileName)
	}
	file, err := decl.Parse(data)
	if err != nil {
		return plan, err
	}
	plan.PolicyKey = PolicyKey(file)
	changes, err := repo.Diff(ctx, base, source)
	if err != nil {
		return plan, err
	}
	raw, err := rawDifferences(ctx, repo, source)
	if err != nil {
		return plan, err
	}
	plan.applyChanges(file, changedPaths(changes, raw), raw)
	return plan, nil
}

func (plan *Plan) applyChanges(file *decl.File, paths []string, raw map[string]bool) {
	declarationChanged := slices.Contains(paths, decl.FileName)
	covered := map[string]bool{}
	for _, check := range file.Checks {
		item := PlanCheck{ID: check.ID, Expect: check.Expect, Reason: "retained in the full check gate; no change trigger matched", MatchedPaths: []string{}}
		for _, path := range paths {
			if decl.MatchAny(check.When, path) || check.Verify && len(check.When) == 0 {
				item.MatchedPaths = append(item.MatchedPaths, path)
				covered[path] = true
			}
		}
		switch {
		case declarationChanged:
			item.Priority, item.Reason = true, "check declaration changed"
		case check.Verify && len(check.When) == 0:
			item.Priority, item.Reason = true, "behavioral verification applies to every change"
		case len(item.MatchedPaths) > 0:
			item.Priority, item.Reason = true, "changed paths match when"
		}
		if slices.ContainsFunc(item.MatchedPaths, func(path string) bool { return raw[path] }) {
			item.Reason += "; original source-byte differences from the Git snapshot conservatively trigger verification"
		}
		item.Required = check.Verify && item.Priority
		plan.Checks = append(plan.Checks, item)
	}
	for _, path := range paths {
		if !covered[path] {
			plan.Uncovered = append(plan.Uncovered, path)
		}
	}
}

func changedPaths(changes []gitx.Change, raw map[string]bool) []string {
	all := map[string]bool{}
	for _, change := range changes {
		if !generatedHistory(change.Path) {
			all[change.Path] = true
		}
	}
	for path := range raw {
		all[path] = true
	}
	paths := make([]string, 0, len(all))
	for path := range all {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// PriorityIDs returns applicable checks in declaration order.
func (p Plan) PriorityIDs() []string { return p.ids(func(c PlanCheck) bool { return c.Priority }) }

// RequiredIDs returns applicable behavioral verification checks.
func (p Plan) RequiredIDs() []string { return p.ids(func(c PlanCheck) bool { return c.Required }) }

// AllIDs returns the complete declared gate, including unmatched checks.
func (p Plan) AllIDs() []string { return p.ids(func(PlanCheck) bool { return true }) }

func (p Plan) ids(include func(PlanCheck) bool) []string {
	ids := []string{}
	for _, check := range p.Checks {
		if include(check) {
			ids = append(ids, check.ID)
		}
	}
	return ids
}

// PolicyKey fingerprints effective check declarations independently of layout.
func PolicyKey(file *decl.File) string {
	checks := make([]decl.Check, len(file.Checks))
	for i, check := range file.Checks {
		check.Line = 0
		checks[i] = check
	}
	data, _ := json.Marshal(checks)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// generatedHistory only recognizes the two owned history locations. Paths
// resembling verification elsewhere remain source paths and evidence inputs.
func generatedHistory(path string) bool {
	parts := strings.Split(path, "/")
	return len(parts) >= 5 && parts[0] == "openspec" && parts[1] == "changes" && parts[2] == ".verification" && parts[3] != "" ||
		len(parts) >= 5 && parts[0] == "openspec" && parts[1] == "changes" && parts[2] != "" && parts[3] == "verification"
}
