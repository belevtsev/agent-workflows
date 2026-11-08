package taskflow_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Approved test-author scaffold for the WP9 contract tests (no assertions).

// scaffoldDir returns a new empty directory with symlinks resolved.
func scaffoldDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// scaffoldWrite writes data to the slash-separated path under dir, creating
// parent directories.
func scaffoldWrite(t *testing.T, dir, path, data string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// scaffoldSHA256 returns the lowercase hex SHA-256 of data.
func scaffoldSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// scaffoldTask writes files (slash-separated path -> content) into a new
// project directory and starts a substantial task over it, with adviser
// checkpoints not required, one requirement that allows non-execution and no
// checks, in a new private store outside the project. It returns the project
// root and the task directory.
func scaffoldTask(t *testing.T, files map[string]string) (repo, task string) {
	t.Helper()
	repo = scaffoldDir(t)
	for path, data := range files {
		scaffoldWrite(t, repo, path, data)
	}
	var plan taskflow.Plan
	const planJSON = `{"title":"contract scaffold","kind":"substantial","checkpoints":{"required":false,"reason":"scaffold"},"requirements":[{"id":"r","description":"scaffold requirement","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`
	if err := taskflow.Decode(strings.NewReader(planJSON), &plan); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(scaffoldDir(t), "store")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	record, err := taskflow.Start(t.Context(), repo, store, plan, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	return repo, record.Directory
}

// scaffoldBrief returns a valid implementation (test-author) brief owning the
// given repository-relative paths, with no checks and no contract.
func scaffoldBrief(id string, owned ...string) taskflow.WorkerBrief {
	return taskflow.WorkerBrief{ID: id, AgentID: "author-" + id, Role: "implementation", Goal: "Author tests from the contract", OwnedPaths: owned, Constraints: []string{}, CheckIDs: []string{}, StopCondition: "Tests authored"}
}

// scaffoldBlockedHandoff records a blocked terminal handoff for an assignment,
// which ends its writer ownership so a later assignment may own the same paths.
func scaffoldBlockedHandoff(t *testing.T, task, assignmentID string) {
	t.Helper()
	result := taskflow.WorkerResult{AssignmentID: assignmentID, Status: "blocked", Summary: "blocked for the scaffold", Evidence: []taskflow.SourceRef{}, ChangedFiles: []taskflow.WorkerChangedFile{}, CheckIDs: []string{}, Findings: []taskflow.WorkerFinding{}, Uncertainties: []string{}}
	if _, err := taskflow.SubmitWorkerResult(t.Context(), task, result); err != nil {
		t.Fatal(err)
	}
}
