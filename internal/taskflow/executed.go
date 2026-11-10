package taskflow

import (
	"context"
	"os"
	"slices"

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
// files changed since the baseline and, for each planned or added check whose
// latest run passed at the current snapshot, that run. files is the current
// manifest. Records started before the baseline manifest existed cannot list
// changed files; that gap is returned as a limitation.
func executionProblems(ctx context.Context, root *os.Root, record Record, snapshot projectctx.Snapshot, files []projectctx.FileState) (problems, limitations []string) {
	source, limitations, err := evidenceSource(root, record, snapshot, files)
	if err != nil {
		return []string{err.Error()}, nil
	}
	// Only the latest run of each check counts, as in bindChecks: a pass
	// followed by a failure at the same source proves nothing.
	passed := []langpack.Run{}
	for _, check := range allChecks(record) {
		if run := latestRun(record, check.ID); run != nil && passedAt(*run, snapshot) {
			entry := langpack.Run{Argv: run.Argv, GOFLAGS: run.GOFLAGS}
			if record.Packs != nil {
				entry.CheckID, entry.Truncated, entry.Rerun = run.CheckID, run.Truncated, rerun(record, *run)
				if data, err := privateRead(root, run.Log, MaxLogBytes); err == nil && digest(data) == run.LogSHA256 {
					entry.Log = data
				}
			}
			passed = append(passed, entry)
		}
	}
	if record.Packs == nil {
		for _, rule := range langpack.Rules(source) {
			problems = append(problems, rule.Problems(source, passed)...)
		}
		return problems, limitations
	}
	ruleIDs := []string{}
	for _, pack := range record.Packs.Applied {
		if pack.EvidenceRule != "" {
			ruleIDs = append(ruleIDs, pack.EvidenceRule)
		}
	}
	evidence := langpack.Evaluate(ctx, snapshot.SelectedPath, checkEnvironment(), ruleIDs, source, passed)
	return append(problems, evidence.Problems...), append(limitations, evidence.Limitations...)
}

// rerun reports whether another run of run's check failed with an exit code
// at the same before-snapshot and with the same recorded GOFLAGS. A passed
// run is never failed, so run never matches itself.
func rerun(record Record, run CheckRun) bool {
	return slices.ContainsFunc(record.Checks, func(other CheckRun) bool {
		return other.CheckID == run.CheckID && other.Status == "failed" && other.ExitCode != nil && equalSnapshot(other.Before, run.Before) && other.GOFLAGS == run.GOFLAGS
	})
}

// evidenceSource describes the selected project for the evidence rules: the
// current files and, when the record has a baseline manifest, the files
// changed since it, both relative to the selected path. files is the current
// manifest. A record without a baseline manifest gets a limitation instead of
// a changed-file list.
func evidenceSource(root *os.Root, record Record, snapshot projectctx.Snapshot, files []projectctx.FileState) (langpack.Source, []string, error) {
	relative, err := selectedRelative(snapshot)
	if err != nil {
		return langpack.Source{}, nil, err
	}
	source := langpack.Source{Current: []string{}}
	for _, file := range files {
		if name, ok := relative(file.Path); ok {
			source.Current = append(source.Current, name)
		}
	}
	if record.BaselineManifest == nil {
		return source, []string{"task predates the baseline manifest; language evidence rules saw no changed files"}, nil
	}
	baseline, err := readBaselineManifest(root, *record.BaselineManifest)
	if err != nil {
		return langpack.Source{}, nil, err
	}
	source.Changed = []string{}
	for _, file := range changedSince(baseline, files) {
		if name, ok := relative(file.Path); ok {
			source.Changed = append(source.Changed, name)
		}
	}
	return source, nil, nil
}
