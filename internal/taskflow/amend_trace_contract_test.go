package taskflow_test

// Contract tests for finding M1: when cw task trace and cw task verify judge a
// mapped test, the current passing run of a check added by AddChecks (an
// amendment) counts exactly like the current run of a plan check, while a
// stale run (of an added or a plan check) never does.
//
// Sources: interface-contract.md sections 1.4 and 2 and R11/R13 (added checks
// run and earn credit like plan checks); docs/operations.md "Merging the base
// branch during a task"; global-workflow.md (verify judges every mapped test at
// the current source and fails one with no PASS in a latest passed run of a
// per-test-credited -json check); worker-handoffs.md (requirements and
// requirement_map of a test-engineer round).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	m1Coordinator = "/root/coord"
	m1Agent       = "/root/test-engineer"
	m1Assignment  = "te-1"
	m1Requirement = "M1"
	m1Criterion   = "r1"
	m1TestFile    = "b/b_test.go"
	m1TestName    = "TestBMapped"
)

type m1Fixture struct {
	t     *testing.T
	ctx   context.Context
	repo  string
	store string
	task  string
	id    string
}

// m1Isolate keeps the user's Git and Go settings out of the fixture: no global
// Git config, no GOFLAGS (so every -json run is credited), no toolchain
// download, no workspace.
func m1Isolate(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "fixture@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "fixture@example.com")
	t.Setenv("GIT_EDITOR", "true")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
}

func (f *m1Fixture) git(args ...string) string {
	f.t.Helper()
	full := append([]string{"-C", f.repo, "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *m1Fixture) write(rel, content string) {
	f.t.Helper()
	path := filepath.Join(f.repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *m1Fixture) fileSHA(rel string) (string, int) {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.repo, filepath.FromSlash(rel)))
	if err != nil {
		f.t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), strings.Count(string(data), "\n")
}

const (
	m1GoMod   = "module example.com/m\n\ngo 1.21\n"
	m1AGo     = "package a\n\n// A returns one.\nfunc A() int { return 1 }\n"
	m1ATestGo = "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"A() != 1\")\n\t}\n}\n"
	m1BGo     = "package b\n\n// B returns two.\nfunc B() int { return 2 }\n"
	m1BGoBase = "package b\n\n// B returns two.\nfunc B() int { return 2 }\n\n// Extra arrives with the base branch.\nfunc Extra() int { return 3 }\n"
	m1BTestGo = "package b\n\nimport \"testing\"\n\nfunc TestBMapped(t *testing.T) {\n\tif B() != 2 {\n\t\tt.Fatal(\"B() != 2\")\n\t}\n}\n"
)

// m1NewRepo builds baseline commit B0 on main (go.mod, packages a and b), a
// base-side commit O on top of B0 that baseSide writes, origin/main = O, and
// checks out branch feature at B0.
func m1NewRepo(t *testing.T, baseSide func(f *m1Fixture)) *m1Fixture {
	t.Helper()
	m1Isolate(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &m1Fixture{t: t, ctx: context.Background(), repo: repo, store: store}
	f.git("init", "-q", "-b", "main")
	f.write("go.mod", m1GoMod)
	f.write("a/a.go", m1AGo)
	f.write("a/a_test.go", m1ATestGo)
	f.write("b/b.go", m1BGo)
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "B0")
	f.git("checkout", "-q", "-b", "upstream")
	baseSide(f)
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "O")
	f.git("update-ref", "refs/remotes/origin/main", "HEAD")
	f.git("checkout", "-q", "-b", "feature", "main")
	f.git("branch", "-q", "-D", "upstream")
	return f
}

// m1Start starts a substantial task (checkpoints not required, the permitted
// fixture variation of section 2) whose requirement r1 is bound to checks.
func (f *m1Fixture) start(checks []taskflow.Check) {
	f.t.Helper()
	ids := make([]string, 0, len(checks))
	for _, c := range checks {
		ids = append(ids, c.ID)
	}
	plan := taskflow.Plan{
		Title:         "M1 fixture",
		Kind:          "substantial",
		CoordinatorID: m1Coordinator,
		Requirements: []taskflow.Requirement{{
			ID:          m1Criterion,
			Description: "package b behaves",
			CheckIDs:    ids,
		}},
		Checks:      checks,
		Checkpoints: &taskflow.CheckpointPolicy{Required: false, Reason: "approach follows directly from the contract"},
	}
	rec, err := taskflow.Start(f.ctx, f.repo, f.store, plan, map[string]string{})
	if err != nil {
		f.t.Fatalf("Start: %v", err)
	}
	f.task = rec.Directory
	f.id = rec.ID
}

// extractContract runs the read-only cw task contract-extract for package a in
// the fixture and saves the output outside the project.
func (f *m1Fixture) extractContract() (string, string) {
	f.t.Helper()
	home := f.t.TempDir()
	var out, errOut bytes.Buffer
	config := cli.Config{
		In:         strings.NewReader(""),
		Out:        &out,
		Err:        &errOut,
		HomeDir:    func() (string, error) { return home, nil },
		WorkingDir: func() (string, error) { return f.repo, nil },
		Getenv: func(key string) string {
			switch key {
			case "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME":
				return filepath.Join(home, key)
			}
			return os.Getenv(key)
		},
	}
	if err := cli.Execute(f.ctx, []string{"task", "contract-extract", "--path", "a", "--symbols", "A"}, config); err != nil {
		f.t.Fatalf("contract-extract: %v\n%s", err, errOut.String())
	}
	path := filepath.Join(f.t.TempDir(), "contract-r1.json")
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		f.t.Fatal(err)
	}
	sum := sha256.Sum256(out.Bytes())
	return path, hex.EncodeToString(sum[:])
}

// assignTestEngineer records a contract-bearing test-engineer brief that
// declares requirement M1 implementing plan requirement r1.
func (f *m1Fixture) assignTestEngineer() {
	f.t.Helper()
	contractPath, contractSHA := f.extractContract()
	raw := fmt.Sprintf(`{
		"id": %q, "agent_id": %q, "role": "implementation",
		"goal": "Write tests for M1 from the contract",
		"owned_paths": [%q], "constraints": ["contract only"], "check_ids": [],
		"stop_condition": "Return tests and the requirement map",
		"contract": {"path": %q, "sha256": %q},
		"requirements": [{"requirement": %q, "criteria": [%q]}]
	}`, m1Assignment, m1Agent, m1TestFile, contractPath, contractSHA, m1Requirement, m1Criterion)
	var brief taskflow.WorkerBrief
	if err := json.Unmarshal([]byte(raw), &brief); err != nil {
		f.t.Fatal(err)
	}
	if _, err := taskflow.AssignWorker(f.ctx, f.task, brief); err != nil {
		f.t.Fatalf("AssignWorker: %v", err)
	}
}

// handOffTests writes and commits b/b_test.go (commit F on feature), then
// records the completed test-engineer result mapping M1 to TestBMapped and the
// coordinator's acceptance.
func (f *m1Fixture) handOffTests() {
	f.t.Helper()
	f.write(m1TestFile, m1BTestGo)
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "F: tests for b")
	sum, lines := f.fileSHA(m1TestFile)
	snap, err := projectctx.Capture(f.ctx, f.repo)
	if err != nil {
		f.t.Fatalf("Capture: %v", err)
	}
	ref := []taskflow.SourceRef{{Path: m1TestFile, StartLine: 1, EndLine: lines, SHA256: sum}}
	result := taskflow.WorkerResult{
		TaskID:         f.id,
		AssignmentID:   m1Assignment,
		AgentID:        m1Agent,
		Snapshot:       snap,
		Status:         "completed",
		Summary:        "TestBMapped guards M1",
		Evidence:       ref,
		ChangedFiles:   []taskflow.WorkerChangedFile{{Path: m1TestFile, SHA256: sum}},
		CheckIDs:       []string{},
		Findings:       []taskflow.WorkerFinding{},
		Uncertainties:  []string{},
		RequirementMap: []taskflow.RequirementMapping{{Requirement: m1Requirement, Tests: []taskflow.MappedTest{{Path: m1TestFile, Name: m1TestName}}}},
	}
	if _, err := taskflow.SubmitWorkerResult(f.ctx, f.task, result); err != nil {
		f.t.Fatalf("SubmitWorkerResult: %v", err)
	}
	acceptance := taskflow.WorkerAcceptance{
		TaskID:        f.id,
		AssignmentID:  m1Assignment,
		CoordinatorID: m1Coordinator,
		Status:        "accepted",
		Reason:        "the mapped test exercises package b",
		Evidence:      ref,
		CheckIDs:      []string{},
		Findings:      []taskflow.FindingDisposition{},
	}
	if _, err := taskflow.AcceptWorkerResult(f.ctx, f.task, acceptance); err != nil {
		f.t.Fatalf("AcceptWorkerResult: %v", err)
	}
}

// mergeBase merges origin/main with --no-ff (merge commit M, parents F and O)
// and admits the added check through AddChecks.
func (f *m1Fixture) mergeBaseAndAdd(check taskflow.Check) {
	f.t.Helper()
	f.git("merge", "-q", "--no-ff", "-m", "merge origin/main", "origin/main")
	rec, err := taskflow.AddChecks(f.ctx, f.task, taskflow.AmendmentInput{
		MergeCommit: "HEAD",
		BaseRef:     "origin/main",
		Reason:      "merged origin/main; the merge changed files no plan check covers",
		Checks:      []taskflow.Check{check},
	})
	if err != nil {
		f.t.Fatalf("AddChecks: %v", err)
	}
	if n := len(rec.Amendments); n != 1 {
		f.t.Fatalf("AddChecks recorded %d amendments, want 1", n)
	}
}

func (f *m1Fixture) run(checkID string) taskflow.CheckRun {
	f.t.Helper()
	run, err := taskflow.Run(f.ctx, f.task, checkID)
	if err != nil {
		f.t.Fatalf("Run %s: %v", checkID, err)
	}
	if run.Status != "passed" {
		f.t.Fatalf("Run %s: status %q, want passed (log %s)", checkID, run.Status, run.Log)
	}
	return run
}

// mapped returns the trace entries for M1 under r1 and its test TestBMapped.
func (f *m1Fixture) mapped() (taskflow.TraceReport, taskflow.TraceRequirement, taskflow.TraceTest) {
	f.t.Helper()
	report, err := taskflow.Trace(f.ctx, f.task)
	if err != nil {
		f.t.Fatalf("Trace: %v", err)
	}
	for _, crit := range report.Criteria {
		if crit.ID != m1Criterion {
			continue
		}
		for _, req := range crit.Requirements {
			if req.AssignmentID != m1Assignment || req.Requirement != m1Requirement {
				continue
			}
			for _, test := range req.Tests {
				if test.Path == m1TestFile && test.Name == m1TestName {
					return report, req, test
				}
			}
			f.t.Fatalf("trace of %s/%s has no test %s %s: %+v", m1Assignment, m1Requirement, m1TestFile, m1TestName, req.Tests)
		}
		f.t.Fatalf("trace criterion %s does not link %s/%s: %+v", m1Criterion, m1Assignment, m1Requirement, crit.Requirements)
	}
	f.t.Fatalf("trace has no criterion %s: %+v", m1Criterion, report.Criteria)
	return report, taskflow.TraceRequirement{}, taskflow.TraceTest{}
}

func m1FindRun(test taskflow.TraceTest, runID string) (taskflow.TraceRun, bool) {
	for _, r := range test.Runs {
		if r.RunID == runID {
			return r, true
		}
	}
	return taskflow.TraceRun{}, false
}

// requireProvenBy asserts that TestBMapped is judged passed at the current
// source, with run as a current, credited, passing run that recorded it.
func (f *m1Fixture) requireProvenBy(run taskflow.CheckRun) {
	f.t.Helper()
	report, req, test := f.mapped()
	if test.Status != "passed" {
		f.t.Errorf("TestBMapped status %q (detail %q), want passed: the current passing run %s of check %s was not credited; runs %+v",
			test.Status, test.Detail, run.ID, run.CheckID, test.Runs)
	}
	if req.Status != "proven" {
		f.t.Errorf("requirement %s status %q, want proven", m1Requirement, req.Status)
	}
	tr, ok := m1FindRun(test, run.ID)
	if !ok {
		f.t.Fatalf("TestBMapped runs do not include run %s of check %s: %+v", run.ID, run.CheckID, test.Runs)
	}
	if tr.CheckID != run.CheckID || tr.Status != "passed" || !tr.Current || !tr.Credited || tr.Pass < 1 || tr.Fail != 0 {
		f.t.Errorf("trace run %+v, want check %s passed, current, credited, pass>=1, fail 0", tr, run.CheckID)
	}
	for _, p := range report.Problems {
		if strings.Contains(p, m1TestName) {
			f.t.Errorf("trace problem names the proven test: %q", p)
		}
	}
	v := f.verify()
	for _, p := range v.Problems {
		if strings.Contains(p, m1TestName) {
			f.t.Errorf("verify reports a problem for the mapped test proven by a current run: %q", p)
		}
	}
}

// verify runs Verify; an incomplete task is expected here (no review, no
// assessment), so an error is accepted when it comes with recorded problems.
func (f *m1Fixture) verify() taskflow.Verification {
	f.t.Helper()
	v, err := taskflow.Verify(f.ctx, f.task)
	if err != nil && (v.TaskID == "" || len(v.Problems) == 0) {
		f.t.Fatalf("Verify: %v", err)
	}
	return v
}

// requireNotProven asserts that TestBMapped is not judged passed, that a
// listed stale run is not current, and that verify reports a problem for it.
func (f *m1Fixture) requireNotProven(stale *taskflow.CheckRun) {
	f.t.Helper()
	_, req, test := f.mapped()
	if test.Status == "passed" {
		f.t.Errorf("TestBMapped status passed with no passing run at the current source; runs %+v", test.Runs)
	}
	if req.Status == "proven" {
		f.t.Errorf("requirement %s proven with no passing run at the current source", m1Requirement)
	}
	if stale != nil {
		tr, ok := m1FindRun(test, stale.ID)
		if !ok {
			f.t.Errorf("TestBMapped runs do not list the stale run %s of check %s: %+v", stale.ID, stale.CheckID, test.Runs)
		} else if tr.Current {
			f.t.Errorf("stale run %+v judged current", tr)
		}
	}
	v := f.verify()
	for _, p := range v.Problems {
		if strings.Contains(p, m1TestName) {
			return
		}
	}
	f.t.Errorf("verify reports no problem naming %s, which has no PASS at the current source; problems %q", m1TestName, v.Problems)
}

// TestM1TraceCreditsAddedCheckRunForPackageNoPlanCheckCovers: the plan check
// names only ./a; the merge changes package b, which holds the mapped test, and
// AddChecks admits a -json check for ./b. Fails if trace or verify ignore the
// added check's current passing run (the test would be not_run), or if they
// credit that run after the source moved on (stale).
func TestM1TraceCreditsAddedCheckRunForPackageNoPlanCheckCovers(t *testing.T) {
	f := m1NewRepo(t, func(f *m1Fixture) { f.write("b/b.go", m1BGoBase) })
	f.start([]taskflow.Check{{ID: "unit", Argv: []string{"go", "test", "-count=1", "-json", "./a"}, TimeoutSeconds: 300}})
	f.assignTestEngineer()
	f.handOffTests()
	f.run("unit")

	f.mergeBaseAndAdd(taskflow.Check{ID: "bjson", Argv: []string{"go", "test", "-count=1", "-json", "./b"}, TimeoutSeconds: 300})
	f.run("unit")

	// Only plan check runs exist: no run recorded TestBMapped.
	f.requireNotProven(nil)

	// Only the added check runs at the post-merge source.
	added := f.run("bjson")
	f.requireProvenBy(added)

	// The source moves on: the added check's run is stale and proves nothing.
	f.write("b/b.go", m1BGoBase+"\n// edited after the run\n")
	f.requireNotProven(&added)
}

// TestM1TraceCreditsAddedCheckRunAfterPlanRunWentStale: plan checks cover ./a
// and ./b, and a plan run before the merge proved the mapped test. The merge
// changes go.mod, AddChecks admits go test -json ./..., and after the merge
// only that added check runs. Fails if the pre-merge plan run is still judged
// current, or if the added check's current passing run is ignored.
func TestM1TraceCreditsAddedCheckRunAfterPlanRunWentStale(t *testing.T) {
	f := m1NewRepo(t, func(f *m1Fixture) { f.write("go.mod", m1GoMod+"\n// base branch note\n") })
	f.start([]taskflow.Check{
		{ID: "unit-a", Argv: []string{"go", "test", "-count=1", "-json", "./a"}, TimeoutSeconds: 300},
		{ID: "unit-b", Argv: []string{"go", "test", "-count=1", "-json", "./b"}, TimeoutSeconds: 300},
	})
	f.assignTestEngineer()
	f.handOffTests()
	f.run("unit-a")
	planRun := f.run("unit-b")

	// Before the merge the plan run is current and proves the test (sanity:
	// the fixture's mapping and credit work at all).
	f.requireProvenBy(planRun)

	f.mergeBaseAndAdd(taskflow.Check{ID: "wide", Argv: []string{"go", "test", "-count=1", "-json", "./..."}, TimeoutSeconds: 300})

	// The test passed only in a plan check run before the merge.
	f.requireNotProven(&planRun)

	// Only the added check runs at the post-merge source.
	added := f.run("wide")
	f.requireProvenBy(added)
}
