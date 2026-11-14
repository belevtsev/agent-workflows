package publish

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// oracle: a zero Deps.Now selects the wall clock, as the zero taskflow.Env
// does. The run publishes a verified small task while a substantial record,
// started just now and never verified, is open on the same repository. R7
// ages open records against the clock, so Run must deny with open-record.
// It must neither panic on the nil clock nor skip the other record as dated
// after now, as a zero time would.
func TestRunWithoutAClockGatesOnARecordOpenedNow(t *testing.T) {
	ctx := t.Context()
	// Start creates the missing store as a private directory.
	repo, store := canonicalTempDir(t), filepath.Join(canonicalTempDir(t), "store")
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	writeSource := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	writeSource("one\ntwo\n")
	git("add", "a.txt")
	git("commit", "-qm", "baseline")
	head := git("rev-parse", "HEAD")

	env := taskflow.Env{}
	check := []taskflow.Check{{ID: "ok", Argv: []string{"true"}, TimeoutSeconds: 30}}
	requirement := []taskflow.Requirement{{ID: "r1", Description: "d", CheckIDs: []string{"ok"}}}
	small := taskflow.Plan{Title: "publish fixture", Kind: "small", Requirements: requirement, Checks: check}
	record, err := env.StartWithPacks(ctx, repo, store, small, nil, taskflow.SelectionExplicit, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	writeSource("one\ntwo\nthree\n")
	if _, err := env.RunCheck(ctx, record.Directory, "ok"); err != nil {
		t.Fatalf("run: %v", err)
	}
	evidence := func(end int) []taskflow.SourceRef {
		return []taskflow.SourceRef{{Path: "a.txt", StartLine: 1, EndLine: end}}
	}
	input := taskflow.AssessmentInput{
		Requirements: []taskflow.Assessment{{ID: "r1", Status: "satisfied", Evidence: evidence(2), CheckIDs: []string{"ok"}}},
		Decision:     &taskflow.Decision{Status: "resolved", Reason: "checked", Evidence: evidence(1), CheckIDs: []string{"ok"}},
	}
	if _, err := env.Assess(ctx, record.Directory, input); err != nil {
		t.Fatalf("assess: %v", err)
	}
	if verified, err := env.Verify(ctx, record.Directory); err != nil || !verified.Complete {
		t.Fatalf("verify: complete %v problems %q: %v", verified.Complete, verified.Problems, err)
	}
	other := taskflow.Plan{Title: "Other open work", Kind: "substantial", Checkpoints: &taskflow.CheckpointPolicy{Reason: "publish fixture"},
		Requirements: requirement, Checks: check}
	if _, err := env.StartWithPacks(ctx, repo, store, other, nil, taskflow.SelectionExplicit, nil); err != nil {
		t.Fatalf("start the other record: %v", err)
	}

	req, err := ParseRequest(Input{Task: record.Directory, Message: testMessage, ExpectHead: head, Remote: "origin", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	report := Run(ctx, req, Deps{LookPath: exec.LookPath, Environ: os.Environ(), HookStore: func() (string, error) { return store, nil }})
	if report.Status != StatusDenied || report.Reason != "open-record" {
		t.Fatalf("Run with a zero Now: status %q reason %q (%s), want denied open-record", report.Status, report.Reason, report.Detail)
	}
}

// canonicalTempDir is a new temporary directory with its symbolic links
// resolved, as task records store their roots.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
