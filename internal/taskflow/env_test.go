package taskflow

import (
	"context"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// The package-level forms below run each use case with the zero Env, the
// production ports, for the tests written before the ports existed. A test
// that needs a fake port calls the method on its own Env instead.

func StartWithPacks(ctx context.Context, repo, store string, plan Plan, advisers map[string]string, selection string, selected []langpack.Manifest) (Record, error) {
	return Env{}.StartWithPacks(ctx, repo, store, plan, advisers, selection, selected)
}

func PrepareContext(ctx context.Context, path, phase string) (Context, error) {
	return Env{}.PrepareContext(ctx, path, phase)
}

func Assess(ctx context.Context, path string, input AssessmentInput) (Record, error) {
	return Env{}.Assess(ctx, path, input)
}

func Verify(ctx context.Context, path string) (Verification, error) {
	return Env{}.Verify(ctx, path)
}

func Abandon(path, reason string) (Record, error) {
	return Env{}.Abandon(path, reason)
}

func RunCheck(ctx context.Context, path, checkID string) (CheckRun, error) {
	return Env{}.RunCheck(ctx, path, checkID)
}

func AddChecks(ctx context.Context, path string, input AmendmentInput) (Record, error) {
	return Env{}.AddChecks(ctx, path, input)
}

func AssignWorker(ctx context.Context, taskPath string, brief WorkerBrief) (Record, error) {
	return Env{}.AssignWorker(ctx, taskPath, brief)
}

func SubmitWorkerResult(ctx context.Context, taskPath string, input WorkerResult) (Record, error) {
	return Env{}.SubmitWorkerResult(ctx, taskPath, input)
}

func AcceptWorkerResult(ctx context.Context, taskPath string, input WorkerAcceptance) (Record, error) {
	return Env{}.AcceptWorkerResult(ctx, taskPath, input)
}

func AttachConsultation(ctx context.Context, path, phase, inputPath string) (Record, error) {
	return Env{}.AttachConsultation(ctx, path, phase, inputPath)
}

// fixedClock is a Clock that always returns at.
func fixedClock(at time.Time) Clock {
	return func() time.Time { return at }
}
