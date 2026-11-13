package taskflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// reuseFixture is a record whose one check passed at an earlier snapshot, a
// current source with the same bytes at another commit, and a check identity
// equal to the run's: credited reuses the run. Each case below changes one
// condition.
type reuseFixture struct {
	record   Record
	source   *creditSource
	identity runIdentity
	err      error
}

func newReuseFixture(t *testing.T) *reuseFixture {
	t.Helper()
	files := []projectctx.FileState{{Path: "a/a.go", Mode: "100644", SHA256: strings.Repeat("a", 64)}}
	_, sum, err := sourceManifest(files)
	if err != nil || sum == "" {
		t.Fatalf("sourceManifest: %q, %v", sum, err)
	}
	earlier := projectctx.Snapshot{Root: "/r", SelectedPath: "/r", Head: "1111111111111111", Branch: "main", Fingerprint: "f1"}
	current := earlier
	current.Head, current.Fingerprint = "2222222222222222", "f2"
	log := []byte("ok\n")
	tool := &langpack.GoTool{Path: "/go/bin/go", Version: "go1.27.4", SHA256: strings.Repeat("b", 64), GOROOT: "/go"}
	exit := 0
	run := CheckRun{ID: "r1", CheckID: "unit", Status: "passed", Argv: []string{"go", "test", "./..."}, Before: earlier, After: &earlier, ExitCode: &exit,
		Log: "check-r1.log", LogSHA256: digest(log), Tool: tool, GOFLAGS: "", SourceSHA256: sum, EnvKey: strings.Repeat("c", 64)}
	f := &reuseFixture{
		record:   Record{Plan: Plan{Checks: []Check{{ID: "unit", Argv: run.Argv}}}, Checks: []CheckRun{run}},
		identity: runIdentity{tool: new(*tool), goflags: "", envKey: run.EnvKey},
	}
	f.source = (Env{}).credit(t.Context(), fakeArtifacts{"check-r1.log": log}, f.record, current, files)
	f.source.identity = func(Check) (runIdentity, error) { return f.identity, f.err }
	return f
}

func (f *reuseFixture) run() *CheckRun { return &f.record.Checks[0] }

// TestCreditedReusesAPassUnderTheSameDigestAndConfiguration is the baseline
// every invalidator case changes. Oracle: credited is true with the reuse
// reason, and the reuse is listed.
func TestCreditedReusesAPassUnderTheSameDigestAndConfiguration(t *testing.T) {
	f := newReuseFixture(t)
	if ok, reason := credited(f.record, *f.run(), f.source); !ok || reason != reasonReused {
		t.Fatalf("credited = %v, %q; want true, %q", ok, reason, reasonReused)
	}
	if got := reusedLimitations(f.record, f.source); len(got) != 1 || !strings.Contains(got[0], "check unit reuses run r1") {
		t.Errorf("reused limitations %q, want one naming check unit and run r1", got)
	}
}

// TestCreditedWithholdsReuse changes one condition of the baseline per case.
// Oracle: credited is false with that condition's reason; each case passes
// the baseline's other conditions, so it fails when its condition is removed.
func TestCreditedWithholdsReuse(t *testing.T) {
	cases := []struct {
		name   string
		change func(f *reuseFixture)
		reason string
	}{
		{"a source byte, path or mode differs", func(f *reuseFixture) { f.run().SourceSHA256 = strings.Repeat("d", 64) }, reasonSourceChanged},
		{"the go command's path differs", func(f *reuseFixture) { f.identity.tool.Path = "/other/bin/go" }, reasonTool},
		{"the go version differs", func(f *reuseFixture) { f.identity.tool.Version = "go1.27.5" }, reasonTool},
		{"the go command's bytes differ", func(f *reuseFixture) { f.identity.tool.SHA256 = strings.Repeat("e", 64) }, reasonTool},
		{"no go command now", func(f *reuseFixture) { f.identity.tool = nil }, reasonTool},
		{"GOFLAGS differs", func(f *reuseFixture) { f.identity.goflags = "-race" }, reasonGOFLAGS},
		{"the environment key differs", func(f *reuseFixture) { f.identity.envKey = strings.Repeat("f", 64) }, reasonEnvKey},
		{"a newer run did not pass", func(f *reuseFixture) {
			f.record.Checks = append(f.record.Checks, CheckRun{ID: "r2", CheckID: "unit", Status: "failed"})
		}, reasonSuperseded},
		{"the log is truncated", func(f *reuseFixture) { f.run().Truncated = true }, reasonLog},
		{"the log is missing", func(f *reuseFixture) { f.run().Log = "check-gone.log" }, reasonLog},
		{"the log differs from its hash", func(f *reuseFixture) { f.run().LogSHA256 = strings.Repeat("0", 64) }, reasonLog},
		{"recorded without a source digest", func(f *reuseFixture) { f.run().SourceSHA256 = "" }, reasonUnkeyed},
		{"recorded without an environment key", func(f *reuseFixture) { f.run().EnvKey = "" }, reasonUnkeyed},
		{"the run failed", func(f *reuseFixture) { f.run().Status = "failed" }, reasonNotPassed},
		{"the run exited non-zero", func(f *reuseFixture) { code := 1; f.run().ExitCode = &code }, reasonNotPassed},
		{"the source changed during the run", func(f *reuseFixture) {
			after := f.run().Before
			after.Fingerprint = "moved"
			f.run().After = &after
		}, reasonNotPassed},
		{"the current configuration cannot be read", func(f *reuseFixture) { f.err = errors.New("no go") }, reasonUnreadable},
		{"reuse is off", func(f *reuseFixture) { f.source.reuse = false }, reasonOtherSnapshot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReuseFixture(t)
			tc.change(f)
			if ok, reason := credited(f.record, *f.run(), f.source); ok || !strings.HasPrefix(reason, tc.reason) {
				t.Fatalf("credited = %v, %q; want false, %q", ok, reason, tc.reason)
			}
		})
	}
}

// TestNoReuseVariableTurnsReuseOff sets CW_NO_REUSE=1. Oracle: the baseline
// run earns no credit at the later snapshot.
func TestNoReuseVariableTurnsReuseOff(t *testing.T) {
	t.Setenv("CW_NO_REUSE", "1")
	f := newReuseFixture(t)
	if ok, reason := credited(f.record, *f.run(), f.source); ok || reason != reasonOtherSnapshot {
		t.Fatalf("credited under CW_NO_REUSE=1 = %v, %q; want false, %q", ok, reason, reasonOtherSnapshot)
	}
}

// TestCreditedAtTheSameSnapshot keeps today's rule for a run at the current
// snapshot. Oracle: verify and binding credit it without reading the
// configuration; cw task run (exact off) still compares it, so a run under
// other GOFLAGS executes.
func TestCreditedAtTheSameSnapshot(t *testing.T) {
	f := newReuseFixture(t)
	f.source.snapshot = *f.run().After
	f.err = errors.New("must not be read")
	if ok, reason := credited(f.record, *f.run(), f.source); !ok || reason != reasonSnapshot {
		t.Fatalf("credited at the run's snapshot = %v, %q; want true, %q", ok, reason, reasonSnapshot)
	}
	f.err = nil
	f.identity.goflags = "-race"
	f.source.exact = false
	if ok, reason := credited(f.record, *f.run(), f.source); ok || reason != reasonGOFLAGS {
		t.Fatalf("credited for cw task run under other GOFLAGS = %v, %q; want false, %q", ok, reason, reasonGOFLAGS)
	}
}

// TestRerunMatchesAFailureAtTheSameDigest records a failure at one commit and
// a pass of the same bytes at the next. Oracle: the pass is a rerun.
func TestRerunMatchesAFailureAtTheSameDigest(t *testing.T) {
	f := newReuseFixture(t)
	passed := *f.run()
	passed.ID, passed.Before = "r2", f.source.snapshot
	failed := *f.run()
	failed.Status = "failed"
	if !rerun(Record{Checks: []CheckRun{failed, passed}}, passed) {
		t.Fatal("a pass after a failure at the same source digest is not a rerun")
	}
}
