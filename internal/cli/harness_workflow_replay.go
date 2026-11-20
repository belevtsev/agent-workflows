package cli

import (
	"slices"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/workflowrunner"
)

func workflowBaselineFile(id string) string {
	return "runner-pre-code-" + id + ".json"
}

// Rework replays the revised tests against the exact accepted pre-code
// bytes. The runner receives that complete projection as a new check job.
func workflowBaselineFiles(l *stageLedger, task *harness.Task, files []workflowrunner.File) ([]workflowrunner.File, error) {
	w := task.Workflow
	if w.PreCode == nil {
		return files, nil
	}
	var base []workflowrunner.File
	if err := l.read(workflowBaselineFile(task.ID), &base); err != nil {
		return nil, err
	}
	if workflowFilesKey(base) != w.PreCodeSources["bundle"] {
		return nil, failf("retained pre-code sources no longer match the accepted baseline")
	}
	var tests []workflowrunner.File
	isTest := func(name string) bool {
		return decl.MatchAny(w.TestPolicy, name) && decl.MatchAny(w.Plan.TestPaths, name)
	}
	for _, file := range base {
		if isTest(file.Path) && !slices.ContainsFunc(files, func(current workflowrunner.File) bool { return current.Path == file.Path }) {
			tests = append(tests, workflowrunner.File{Path: file.Path})
		}
	}
	for _, file := range files {
		if !isTest(file.Path) {
			continue
		}
		for _, shared := range w.Plan.Shared {
			if shared.Path != file.Path {
				continue
			}
			original := slices.IndexFunc(base, func(f workflowrunner.File) bool { return f.Path == file.Path })
			if original < 0 {
				return nil, failf("shared pre-code file %q is missing", file.Path)
			}
			old, err := workflowRegion(base[original].Data, shared)
			if err != nil {
				return nil, err
			}
			now, err := workflowRegion(file.Data, shared)
			if err != nil {
				return nil, err
			}
			file.Data = append(slices.Clone(old.prefix), now.body...)
			file.Data = append(file.Data, old.suffix...)
		}
		tests = append(tests, file)
	}
	return workflowChanges(base, tests), nil
}
