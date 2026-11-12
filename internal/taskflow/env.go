package taskflow

import (
	"context"
	"os"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Source captures the selected project source. A use case reads the source
// identity only through it: production captures with projectctx, which runs
// git; a rule test passes a fake and needs no repository.
type Source interface {
	// Snapshot identifies the source selected at path.
	Snapshot(ctx context.Context, path string) (projectctx.Snapshot, error)
	// Manifest returns the snapshot of the source selected at path and one
	// state per eligible file, sorted by path, from the same read.
	Manifest(ctx context.Context, path string) (projectctx.Snapshot, []projectctx.FileState, error)
}

// Clock returns the current time. Every time a task record stores comes from
// it: created_at and updated_at, a check run's start and finish, an
// assignment, handoff, acceptance, amendment, verification or abandonment.
type Clock func() time.Time

// Artifacts reads the files one task directory holds beside its record
// revisions: check logs and test results, the baseline manifest,
// consultations, worker input copies, contracts and review bundles. Read
// returns the named private regular file, and refuses one over limit bytes.
// The rules that judge a record read its artefacts only through it. Write
// stores data as a new private file named name, whole or not at all, and
// fails with an error matching fs.ErrExist when name exists.
type Artifacts interface {
	Read(name string, limit int64) ([]byte, error)
	Write(name string, data []byte) error
}

// Env carries the ports a task use case reads instead of reaching the machine
// directly, so a rule can run in a unit test with fakes. The ports are
// declared here, at their consumer. A nil port selects its production
// implementation, so the zero Env is production: projectctx captures the
// source and time.Now is the clock.
//
// Each use case is a method on Env; one that writes a task revision passes
// the Env to update, which stamps the revision. Artifacts belong to one task
// directory, so they are not in Env: a use case hands its rules the directory
// it opened (taskDirectory).
type Env struct {
	Source Source
	Clock  Clock
}

// source returns the Env's Source; a nil one captures with projectctx.
func (env Env) source() Source {
	if env.Source == nil {
		return projectSource{}
	}
	return env.Source
}

// now returns the clock's time in UTC, as a record stores it.
func (env Env) now() time.Time {
	if env.Clock == nil {
		return time.Now().UTC()
	}
	return env.Clock().UTC()
}

// projectSource is the production Source.
type projectSource struct{}

func (projectSource) Snapshot(ctx context.Context, path string) (projectctx.Snapshot, error) {
	return projectctx.Capture(ctx, path)
}

func (projectSource) Manifest(ctx context.Context, path string) (projectctx.Snapshot, []projectctx.FileState, error) {
	return projectctx.CaptureManifest(ctx, path)
}

// taskDirectory is the production Artifacts: the task directory root, whose
// artefacts privateRead reads.
type taskDirectory struct{ root *os.Root }

func (directory taskDirectory) Read(name string, limit int64) ([]byte, error) {
	return privateRead(directory.root, name, limit)
}

func (directory taskDirectory) Write(name string, data []byte) error {
	return writeNew(directory.root, name, data)
}
