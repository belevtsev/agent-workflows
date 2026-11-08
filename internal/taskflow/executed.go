package taskflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// passedAt reports whether run passed without changing the source and ran at
// snapshot.
func passedAt(run CheckRun, snapshot projectctx.Snapshot) bool {
	return run.Status == "passed" && run.ExitCode != nil && *run.ExitCode == 0 && run.After != nil && equalSnapshot(run.Before, *run.After) && equalSnapshot(snapshot, *run.After)
}

// executionProblems asks the language-pack registry which evidence rules
// apply to a changed substantial task and returns what they report, given the
// files changed since the baseline and, for each planned check whose latest
// run passed at the current snapshot, that run. files is the current manifest. Records started before the
// baseline manifest existed cannot list changed files; that gap is returned as
// a limitation.
func executionProblems(root *os.Root, record Record, snapshot projectctx.Snapshot, files []projectctx.FileState) (problems, limitations []string) {
	module, err := filepath.Rel(snapshot.Root, snapshot.SelectedPath)
	if err != nil {
		return []string{fmt.Sprintf("locate the selected path in its project: %v", err)}, nil
	}
	relative := func(name string) (string, bool) {
		if module == "." {
			return name, true
		}
		return strings.CutPrefix(name, filepath.ToSlash(module)+"/")
	}
	source := langpack.Source{Current: []string{}}
	for _, file := range files {
		if name, ok := relative(file.Path); ok {
			source.Current = append(source.Current, name)
		}
	}
	if record.BaselineManifest == nil {
		limitations = append(limitations, "task predates the baseline manifest; language evidence rules saw no changed files")
	} else {
		baseline, err := readBaselineManifest(root, *record.BaselineManifest)
		if err != nil {
			return []string{err.Error()}, nil
		}
		source.Changed = []string{}
		for _, file := range changedSince(baseline, files) {
			if name, ok := relative(file.Path); ok {
				source.Changed = append(source.Changed, name)
			}
		}
	}
	// Only the latest run of each check counts, as in bindChecks: a pass
	// followed by a failure at the same source proves nothing.
	passed := []langpack.Run{}
	for _, check := range record.Plan.Checks {
		if run := latestRun(record, check.ID); run != nil && passedAt(*run, snapshot) {
			passed = append(passed, langpack.Run{Argv: run.Argv, GOFLAGS: run.GOFLAGS})
		}
	}
	for _, rule := range langpack.Rules(source) {
		problems = append(problems, rule.Problems(source, passed)...)
	}
	return problems, limitations
}
