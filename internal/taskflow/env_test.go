package taskflow

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// The package-level forms below run each use case with productionEnv, the
// production ports, for the tests written before the ports existed. A test
// that needs a fake port calls the method on its own Env instead.

// productionEnv is the Env cw's task commands use: the zero Env's production
// Source and Clock, and cw consult's record validator, which taskflow cannot
// default to because it must not import internal/consult.
var productionEnv = Env{Consultation: consult.ValidateRecord}

func StartWithPacks(ctx context.Context, repo, store string, plan Plan, advisers map[string]string, selection string, selected []langpack.Manifest) (Record, error) {
	return productionEnv.StartWithPacks(ctx, repo, store, plan, advisers, selection, selected)
}

func PrepareContext(ctx context.Context, path, phase string) (Context, error) {
	return productionEnv.PrepareContext(ctx, path, phase)
}

func Assess(ctx context.Context, path string, input AssessmentInput) (Record, error) {
	return productionEnv.Assess(ctx, path, input)
}

func Verify(ctx context.Context, path string) (Verification, error) {
	return productionEnv.Verify(ctx, path)
}

func Abandon(path, reason string) (Record, error) {
	return productionEnv.Abandon(path, reason)
}

func RunCheck(ctx context.Context, path, checkID string) (CheckRun, error) {
	return productionEnv.RunCheck(ctx, path, checkID)
}

func AddChecks(ctx context.Context, path string, input AmendmentInput) (Record, error) {
	return productionEnv.AddChecks(ctx, path, input)
}

func AssignWorker(ctx context.Context, taskPath string, brief WorkerBrief) (Record, error) {
	return productionEnv.AssignWorker(ctx, taskPath, brief)
}

func SubmitWorkerResult(ctx context.Context, taskPath string, input WorkerResult) (Record, error) {
	return productionEnv.SubmitWorkerResult(ctx, taskPath, input)
}

func AcceptWorkerResult(ctx context.Context, taskPath string, input WorkerAcceptance) (Record, error) {
	return productionEnv.AcceptWorkerResult(ctx, taskPath, input)
}

func AttachConsultation(ctx context.Context, path, phase, inputPath string) (Record, error) {
	return productionEnv.AttachConsultation(ctx, path, phase, inputPath)
}

func Trace(ctx context.Context, taskPath string) (TraceReport, error) {
	return productionEnv.Trace(ctx, taskPath)
}

func Authorship(ctx context.Context, taskPath string) (AuthorshipReport, error) {
	return productionEnv.Authorship(ctx, taskPath)
}

func BuildBundle(ctx context.Context, taskPath, reviewID string) (Bundle, error) {
	return productionEnv.BuildBundle(ctx, taskPath, reviewID)
}

// fixedClock is a Clock that always returns at.
func fixedClock(at time.Time) Clock {
	return func() time.Time { return at }
}

// fakeSource is a Source that runs nothing: Snapshot and Manifest return the
// snapshot and files the test set, whatever the path.
type fakeSource struct {
	snapshot projectctx.Snapshot
	files    []projectctx.FileState
}

func (source *fakeSource) Snapshot(context.Context, string) (projectctx.Snapshot, error) {
	return source.snapshot, nil
}

func (source *fakeSource) Manifest(context.Context, string) (projectctx.Snapshot, []projectctx.FileState, error) {
	return source.snapshot, slices.Clone(source.files), nil
}

// fakeArtifacts is an Artifacts over named bytes; a name it lacks does not
// exist.
type fakeArtifacts map[string][]byte

func (artifacts fakeArtifacts) Read(name string, limit int64) ([]byte, error) {
	data, ok := artifacts[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if int64(len(data)) > limit {
		return nil, errors.New("task artifact exceeds its size limit")
	}
	return data, nil
}

// fakeSourceTask is fixtureTask without git: it starts plan, bound to the
// shipped default advisers, with an Env whose Source is a fakeSource and
// whose clock is fixed at at. The fake's snapshot is a clean checkout of
// branch main in a fresh directory holding source.txt, the one file its
// manifest lists; the directory exists because source references and checks
// read it. It returns the record, a reference to source.txt, the fake and the
// Env.
func fakeSourceTask(t *testing.T, plan Plan, at time.Time) (Record, SourceRef, *fakeSource, Env) {
	t.Helper()
	repo := t.TempDir()
	data := []byte("first line\nsecond line\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{
		snapshot: projectctx.Snapshot{Root: repo, SelectedPath: repo, Head: strings.Repeat("a", 40), Branch: "main", Fingerprint: "sha256:" + digest(data), FileCount: 1},
		files:    []projectctx.FileState{{Path: "source.txt", Mode: "100644", SHA256: digest(data)}},
	}
	env := Env{Source: source, Clock: fixedClock(at), Consultation: consult.ValidateRecord}
	record, err := env.StartWithPacks(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), plan, fixtureBinding(), SelectionExplicit, nil)
	if err != nil {
		t.Fatal(err)
	}
	return record, SourceRef{Path: "source.txt", StartLine: 1, EndLine: 2, SHA256: digest(data)}, source, env
}
