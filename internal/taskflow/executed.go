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

// executionProblems applies the evidence rules of the record's pack set to a
// changed substantial task and returns what they report, given the files
// changed since the baseline and, for each planned or added check whose
// latest run passed at the current snapshot, that run. files is the current
// manifest.
func executionProblems(ctx context.Context, root *os.Root, record Record, snapshot projectctx.Snapshot, files []projectctx.FileState) (problems, limitations []string) {
	source, err := evidenceSource(root, record, snapshot, files)
	if err != nil {
		return []string{err.Error()}, nil
	}
	// Only the latest run of each check counts, as in bindChecks: a pass
	// followed by a failure at the same source proves nothing.
	passed := []langpack.Run{}
	for _, check := range allChecks(record) {
		if run := latestRun(record, check.ID); run != nil && passedAt(*run, snapshot) {
			entry := langpack.Run{Argv: run.Argv, GOFLAGS: run.GOFLAGS, CheckID: run.CheckID, Truncated: run.Truncated, Rerun: rerun(record, *run)}
			if data, err := privateRead(root, run.Log, MaxLogBytes); err == nil && digest(data) == run.LogSHA256 {
				entry.Log = data
			}
			passed = append(passed, entry)
		}
	}
	ruleIDs := []string{}
	for _, pack := range record.Packs.Applied {
		if pack.EvidenceRule != "" {
			ruleIDs = append(ruleIDs, pack.EvidenceRule)
		}
	}
	// The rule's go list runs are not a check, so no env_passthrough applies.
	evidence := langpack.Evaluate(ctx, snapshot.SelectedPath, checkEnvironment(Check{}), ruleIDs, source, passed)
	return evidence.Problems, evidence.Limitations
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
// current files and the files changed since the baseline manifest, both
// relative to the selected path. files is the current manifest.
func evidenceSource(root *os.Root, record Record, snapshot projectctx.Snapshot, files []projectctx.FileState) (langpack.Source, error) {
	relative, err := selectedRelative(snapshot)
	if err != nil {
		return langpack.Source{}, err
	}
	source := langpack.Source{Current: []string{}}
	for _, file := range files {
		if name, ok := relative(file.Path); ok {
			source.Current = append(source.Current, name)
		}
	}
	baseline, err := readBaselineManifest(taskDirectory{root}, *record.BaselineManifest)
	if err != nil {
		return langpack.Source{}, err
	}
	source.Changed = []string{}
	for _, file := range changedSince(baseline, files) {
		if name, ok := relative(file.Path); ok {
			source.Changed = append(source.Changed, name)
		}
	}
	return source, nil
}
