package taskflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Selection kinds recorded in PackSet.Selection: the default selection
// (no packs.json) or an explicit packs.json.
const (
	SelectionDefault  = "default"
	SelectionExplicit = "explicit"
)

// PackSet is the pack set a task records at start: which selection applied
// and the selected packs detected at the selected path, in order.
type PackSet struct {
	Selection string        `json:"selection"`
	Applied   []PackBinding `json:"applied"`
}

// PackBinding is one applied pack: its id, the detect file that matched and
// the evidence rule it names (omitted when empty).
type PackBinding struct {
	ID           string `json:"id"`
	Detected     string `json:"detected"`
	EvidenceRule string `json:"evidence_rule,omitempty"`
}

// StartWithPacks starts a task and records its pack set and test rules:
// selection is SelectionDefault or SelectionExplicit, and Applied holds the
// selected manifests detected among the baseline files at the top of the
// selected path. Verify then applies the rules those packs name.
func (env Env) StartWithPacks(ctx context.Context, repo, store string, plan Plan, advisers map[string]string, selection string, selected []langpack.Manifest) (Record, error) {
	if selection != SelectionDefault && selection != SelectionExplicit {
		return Record{}, errors.New("pack selection must be default or explicit")
	}
	return env.start(ctx, repo, store, plan, advisers, packChoice{selection: selection, selected: selected})
}

// packChoice is the selection StartWithPacks records.
type packChoice struct {
	selection string
	selected  []langpack.Manifest
}

// bind returns the pack set of a task whose baseline is snapshot and files:
// the chosen packs detected among the files at the top of the selected path.
func (choice packChoice) bind(snapshot projectctx.Snapshot, files []projectctx.FileState) (*PackSet, error) {
	relative, err := selectedRelative(snapshot)
	if err != nil {
		return nil, err
	}
	current := []string{}
	for _, file := range files {
		if name, ok := relative(file.Path); ok {
			current = append(current, name)
		}
	}
	set := &PackSet{Selection: choice.selection, Applied: []PackBinding{}}
	for _, applied := range langpack.Applicable(choice.selected, current) {
		set.Applied = append(set.Applied, PackBinding{ID: applied.ID, Detected: applied.Detected, EvidenceRule: applied.EvidenceRule})
	}
	return set, nil
}

// selectedRelative returns a function that spells a project-relative manifest
// path relative to the snapshot's selected path, reporting false for a path
// outside it.
func selectedRelative(snapshot projectctx.Snapshot) (func(string) (string, bool), error) {
	module, err := filepath.Rel(snapshot.Root, snapshot.SelectedPath)
	if err != nil {
		return nil, fmt.Errorf("locate the selected path in its project: %v", err)
	}
	return func(name string) (string, bool) {
		if module == "." {
			return name, true
		}
		return strings.CutPrefix(name, filepath.ToSlash(module)+"/")
	}, nil
}
