package taskflow_test

// Shared fixtures for the L3 contract tests (interface contract L3, revision 3).
// Every fixture is a real Git repository holding a small Go module in
// t.TempDir(), a separate task store, and real go test runs through
// taskflow.RunCheck. Expectations live in the tests, never here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	l3Coordinator = "coord-l3"
	l3Module      = "example.com/fix"
	// l3Wait bounds how long a test waits for a subprocess or a signal; it
	// never bounds the work under test.
	l3Wait = 3 * time.Minute
)

// Test files the fixture "test engineer" writes. Package p passes; q fails.
const (
	l3GoMod  = "module " + l3Module + "\n\ngo 1.22\n"
	l3PGo    = "package p\n\n// F is the contract symbol.\nfunc F() int { return 1 }\n"
	l3PTests = `package p

import "testing"

func TestPass(t *testing.T) {}

func TestSkip(t *testing.T) { t.Skip("skipped by the fixture") }

func TestSub(t *testing.T) { t.Run("a", func(t *testing.T) {}) }
`
	l3NeverTests = `//go:build never

package p

import "testing"

func TestNever(t *testing.T) {}
`
	l3QTests = `package q

import "testing"

func TestFail(t *testing.T) { t.Fatal("fails by the fixture") }

func TestQPass(t *testing.T) {}
`
	// l3Rule4 is R29's not_run detail 4.
	l3Rule4 = "no current passed go test run with -json in its argv recorded per-test results; run a go test check that has -json in its argv"
)

var (
	l3JSONAll = []string{"go", "test", "-json", "./..."}
	l3JSONP   = []string{"go", "test", "-json", "./p/..."}
	l3JSONQ   = []string{"go", "test", "-json", "./q/..."}
	l3PlainP  = []string{"go", "test", "./p/..."}
)

type l3Results = langpack.GoTestResults

type l3Fix struct {
	t        *testing.T
	ctx      context.Context
	root     string // temporary root outside the project
	repo     string // project root (Git root)
	sel      string // selected path
	prefix   string // selected path relative to repo, "" when equal
	dir      string // task directory
	contract *taskflow.ContractRef
}

type l3Opts struct {
	files  map[string]string // extra files relative to the module root, committed
	reqs   []string          // plan requirement IDs, description "criterion <id>"
	checks []taskflow.Check
	sub    bool // module lives in "mod/" below the Git root
	// ownGOFLAGS skips the empty-GOFLAGS guard for a test that sets GOFLAGS
	// itself.
	ownGOFLAGS bool
}

func l3Check(id string, argv ...string) taskflow.Check {
	return taskflow.Check{ID: id, Argv: argv, TimeoutSeconds: 300}
}

// l3New builds the repository, starts the task and saves a contract extract
// of package p outside the project.
func l3New(t *testing.T, o l3Opts) *l3Fix {
	t.Helper()
	if !o.ownGOFLAGS {
		l3NeedNoGOFLAGS(t)
	}
	root := t.TempDir()
	f := &l3Fix{t: t, ctx: context.Background(), root: root, repo: filepath.Join(root, "repo dir")}
	f.sel = f.repo
	if o.sub {
		f.prefix = "mod"
		f.sel = filepath.Join(f.repo, "mod")
		l3WriteFile(t, filepath.Join(f.repo, "README.md"), "outside the module\n")
	}
	files := map[string]string{"go.mod": l3GoMod, "p/p.go": l3PGo}
	for k, v := range o.files {
		files[k] = v
	}
	for rel, content := range files {
		l3WriteFile(t, filepath.Join(f.sel, filepath.FromSlash(rel)), content)
	}
	l3Git(t, f.repo, "init", "-q")
	l3Git(t, f.repo, "add", ".")
	l3Git(t, f.repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "-qm", "fixture")
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	checks := o.checks
	if checks == nil {
		checks = []taskflow.Check{l3Check("ok", l3JSONP...)}
	}
	reqs := o.reqs
	if reqs == nil {
		reqs = []string{"C1"}
	}
	plan := taskflow.Plan{
		Title:         "l3 fixture",
		Kind:          "substantial",
		CoordinatorID: l3Coordinator,
		Checks:        checks,
		Checkpoints:   &taskflow.CheckpointPolicy{Required: false, Reason: "fixture"},
	}
	for _, id := range reqs {
		plan.Requirements = append(plan.Requirements, taskflow.Requirement{ID: id, Description: "criterion " + id, CheckIDs: []string{checks[0].ID}})
	}
	rec, err := taskflow.StartWithPacks(f.ctx, f.sel, store, plan, nil, taskflow.SelectionExplicit, nil)
	if err != nil {
		t.Fatalf("taskflow.Start: %v", err)
	}
	f.dir = rec.Directory
	extract, err := taskflow.ExtractContract(filepath.Join(f.sel, "p"), []string{"F"})
	if err != nil {
		t.Fatalf("ExtractContract: %v", err)
	}
	data, err := taskflow.EncodeContract(extract)
	if err != nil {
		t.Fatalf("EncodeContract: %v", err)
	}
	cpath := filepath.Join(root, "contract.json")
	if err := os.WriteFile(cpath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f.contract = &taskflow.ContractRef{Path: cpath, SHA256: l3Sum(data)}
	return f
}

func l3Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), l3Wait)
	defer cancel()
	// Isolate the fixture from user and system Git configuration (signing,
	// hooks, templates).
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// l3NeedNoGOFLAGS fails fast when go would run the fixture checks with
// GOFLAGS (from the environment or go env -w): per-test credit and counts
// depend on it, and the project check and CI set none.
func l3NeedNoGOFLAGS(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), l3Wait)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "env", "GOFLAGS").Output()
	if err != nil {
		t.Fatalf("go env GOFLAGS: %v", err)
	}
	if v := strings.TrimSpace(string(out)); v != "" {
		t.Fatalf("these tests need an empty GOFLAGS (the project check and CI set none); go env GOFLAGS = %q", v)
	}
}

func l3WriteFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// l3EmitterSource is a tiny program for checks that must write exact bytes
// to stdout and stderr (checks cannot run a shell). Each argument is
// "out:<line>", "err:<line>" or "sleep" (an hour).
const l3EmitterSource = `package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	for _, a := range os.Args[1:] {
		switch {
		case strings.HasPrefix(a, "out:"):
			fmt.Fprintln(os.Stdout, a[4:])
		case strings.HasPrefix(a, "err:"):
			fmt.Fprintln(os.Stderr, a[4:])
		case a == "sleep":
			time.Sleep(time.Hour)
		}
	}
}
`

// l3Emitter builds the emitter outside any project and returns its path, so
// no compile step runs inside a check's timed window.
func l3Emitter(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	l3WriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/emitter\n\ngo 1.22\n")
	l3WriteFile(t, filepath.Join(dir, "main.go"), l3EmitterSource)
	bin := filepath.Join(dir, "emit")
	ctx, cancel := context.WithTimeout(context.Background(), l3Wait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build emitter: %v\n%s", err, out)
	}
	return bin
}

func l3Sum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// p joins the module prefix to a module-relative path, giving a worker path
// relative to the project root.
func (f *l3Fix) p(rel string) string {
	if f.prefix == "" {
		return rel
	}
	return path.Join(f.prefix, rel)
}

// abs returns the file of a project-root-relative worker path.
func (f *l3Fix) abs(worker string) string {
	return filepath.Join(f.repo, filepath.FromSlash(worker))
}

func (f *l3Fix) write(worker, content string) { f.t.Helper(); l3WriteFile(f.t, f.abs(worker), content) }

func (f *l3Fix) remove(worker string) {
	f.t.Helper()
	if err := os.Remove(f.abs(worker)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *l3Fix) load() taskflow.Record {
	f.t.Helper()
	rec, err := taskflow.Load(f.dir)
	if err != nil {
		f.t.Fatalf("Load: %v", err)
	}
	return rec
}

func (f *l3Fix) revision() int { f.t.Helper(); return f.load().Revision }

// verify returns the verification whatever its completeness.
func (f *l3Fix) verify() taskflow.Verification {
	f.t.Helper()
	v, err := taskflow.Verify(f.ctx, f.dir)
	if v.TaskID == "" {
		f.t.Fatalf("Verify returned no verification: %v", err)
	}
	return v
}

func l3Req(id string, criteria ...string) taskflow.ContractRequirement {
	return taskflow.ContractRequirement{Requirement: id, Criteria: criteria}
}

// brief is a contract-bearing (test-engineer) implementation brief.
func (f *l3Fix) brief(id string, owned []string, reqs ...taskflow.ContractRequirement) taskflow.WorkerBrief {
	b := f.plainBrief(id, owned)
	c := *f.contract
	b.Contract = &c
	b.Requirements = reqs
	return b
}

// plainBrief is an implementation brief without a contract.
func (f *l3Fix) plainBrief(id string, owned []string) taskflow.WorkerBrief {
	return taskflow.WorkerBrief{
		ID: id, AgentID: "agent-" + id, Role: "implementation", Goal: "fixture work",
		OwnedPaths: owned, Constraints: []string{}, CheckIDs: []string{}, StopCondition: "done",
	}
}

func (f *l3Fix) assign(b taskflow.WorkerBrief) error {
	f.t.Helper()
	_, err := taskflow.AssignWorker(f.ctx, f.dir, b)
	return err
}

func (f *l3Fix) mustAssign(b taskflow.WorkerBrief) {
	f.t.Helper()
	if err := f.assign(b); err != nil {
		f.t.Fatalf("AssignWorker(%s): %v", b.ID, err)
	}
}

// assignFails asserts that assign fails with an error containing want and
// that the record gains no revision.
func (f *l3Fix) assignFails(b taskflow.WorkerBrief, want string) error {
	f.t.Helper()
	before := f.revision()
	err := f.assign(b)
	if err == nil {
		f.t.Errorf("AssignWorker(%s: %s) succeeded, want error containing %q", b.ID, b.Goal, want)
		return nil
	}
	if !strings.Contains(err.Error(), want) {
		f.t.Errorf("AssignWorker(%s: %s) error = %q, want it to contain %q", b.ID, b.Goal, err, want)
	}
	if after := f.revision(); after != before {
		f.t.Errorf("failed AssignWorker(%s) moved the record from revision %d to %d", b.ID, before, after)
	}
	return err
}

func (f *l3Fix) assignment(id string) taskflow.WorkerAssignment {
	f.t.Helper()
	for _, a := range f.load().Assignments {
		if a.Brief.ID == id {
			return a
		}
	}
	f.t.Fatalf("no assignment %s", id)
	return taskflow.WorkerAssignment{}
}

// result builds a handoff for assignment id at the current source; changed
// names worker paths, each bound to its current bytes or marked deleted.
func (f *l3Fix) result(id, status string, changed []string, rmap ...taskflow.RequirementMapping) taskflow.WorkerResult {
	f.t.Helper()
	a := f.assignment(id)
	snap := f.verify().CurrentSnapshot
	res := taskflow.WorkerResult{
		TaskID: a.TaskID, AssignmentID: id, AssignmentSHA256: a.SHA256, AgentID: a.Brief.AgentID,
		Snapshot: snap, Status: status, Summary: "fixture handoff",
		ChangedFiles: []taskflow.WorkerChangedFile{}, CheckIDs: []string{},
		Findings: []taskflow.WorkerFinding{}, Uncertainties: []string{},
		RequirementMap: rmap,
	}
	for _, c := range changed {
		data, err := os.ReadFile(f.abs(c))
		if errors.Is(err, os.ErrNotExist) {
			res.ChangedFiles = append(res.ChangedFiles, taskflow.WorkerChangedFile{Path: c, Deleted: true})
			continue
		}
		if err != nil {
			f.t.Fatal(err)
		}
		res.ChangedFiles = append(res.ChangedFiles, taskflow.WorkerChangedFile{Path: c, SHA256: l3Sum(data)})
		if res.Evidence == nil {
			res.Evidence = []taskflow.SourceRef{{Path: c, StartLine: 1, EndLine: 1, SHA256: l3Sum(data)}}
		}
	}
	if res.Evidence == nil {
		res.Evidence = []taskflow.SourceRef{f.goModRef()}
	}
	return res
}

func (f *l3Fix) goModRef() taskflow.SourceRef {
	f.t.Helper()
	data, err := os.ReadFile(f.abs(f.p("go.mod")))
	if err != nil {
		f.t.Fatal(err)
	}
	return taskflow.SourceRef{Path: f.p("go.mod"), StartLine: 1, EndLine: 1, SHA256: l3Sum(data)}
}

func (f *l3Fix) submit(res taskflow.WorkerResult) error {
	f.t.Helper()
	_, err := taskflow.SubmitWorkerResult(f.ctx, f.dir, res)
	return err
}

func (f *l3Fix) mustSubmit(res taskflow.WorkerResult) {
	f.t.Helper()
	if err := f.submit(res); err != nil {
		f.t.Fatalf("SubmitWorkerResult(%s): %v", res.AssignmentID, err)
	}
}

// submitFails asserts that the handoff fails with an error containing want
// and that the record gains no revision.
func (f *l3Fix) submitFails(res taskflow.WorkerResult, want string) {
	f.t.Helper()
	before := f.revision()
	err := f.submit(res)
	if err == nil {
		// The assignment now has its terminal result, so later cases of the
		// same test could not be judged: stop here.
		f.t.Fatalf("SubmitWorkerResult(%s) succeeded, want error containing %q", res.AssignmentID, want)
	}
	if !strings.Contains(err.Error(), want) {
		f.t.Errorf("SubmitWorkerResult(%s) error = %q, want it to contain %q", res.AssignmentID, err, want)
	}
	if after := f.revision(); after != before {
		f.t.Errorf("failed SubmitWorkerResult(%s) moved the record from revision %d to %d", res.AssignmentID, before, after)
	}
}

// accept records the coordinator's judgment (accepted, rejected or unused)
// of the assignment's result.
func (f *l3Fix) accept(id, status string) {
	f.t.Helper()
	rec := f.load()
	var sha string
	for _, r := range rec.WorkerResults {
		if r.Result.AssignmentID == id {
			sha = r.SHA256
		}
	}
	if sha == "" {
		f.t.Fatalf("no result for %s", id)
	}
	_, err := taskflow.AcceptWorkerResult(f.ctx, f.dir, taskflow.WorkerAcceptance{
		TaskID: rec.ID, AssignmentID: id, ResultSHA256: sha, CoordinatorID: l3Coordinator,
		Status: status, Reason: "fixture judgment", Evidence: []taskflow.SourceRef{f.goModRef()},
		CheckIDs: []string{}, Findings: []taskflow.FindingDisposition{},
	})
	if err != nil {
		f.t.Fatalf("AcceptWorkerResult(%s, %s): %v", id, status, err)
	}
}

// run executes a planned check and returns the terminal run as recorded.
func (f *l3Fix) run(check string) taskflow.CheckRun {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, l3Wait)
	defer cancel()
	run, err := taskflow.RunCheck(ctx, f.dir, check)
	if run.ID == "" {
		f.t.Fatalf("RunCheck(%s): %v", check, err)
	}
	return f.recordedRun(run.ID)
}

func (f *l3Fix) recordedRun(id string) taskflow.CheckRun {
	f.t.Helper()
	runs := f.load().Checks
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].ID == id {
			return runs[i]
		}
	}
	f.t.Fatalf("run %s is not recorded", id)
	return taskflow.CheckRun{}
}

// trace calls taskflow.Trace, turning a panic of an unimplemented stub into
// a test failure.
func (f *l3Fix) trace() taskflow.TraceReport {
	f.t.Helper()
	rep, err := l3TraceErr(f.ctx, f.dir)
	if err != nil {
		f.t.Fatalf("Trace: %v", err)
	}
	return rep
}

func l3TraceErr(ctx context.Context, dir string) (rep taskflow.TraceReport, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Trace panicked: %v", r)
		}
	}()
	return taskflow.Trace(ctx, dir)
}

func l3ReadResults(dir string, run taskflow.CheckRun) (res l3Results, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("ReadTestResults panicked: %v", r)
		}
	}()
	return taskflow.ReadTestResults(dir, run)
}

// l3Tree captures every entry of a directory tree (name, mode and bytes)
// outside .git, so a test can require that an operation wrote nothing.
func l3Tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			// Git's own bookkeeping (index refreshes) is not project content.
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		val := info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			val += " " + l3Sum(data)
		}
		out[p] = val
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func l3SameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	var diffs []string
	for k, v := range before {
		if after[k] != v {
			diffs = append(diffs, "changed or removed "+k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			diffs = append(diffs, "added "+k)
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Errorf("%s wrote to the directory: %v", what, diffs)
	}
}

// latestRecordJSON decodes the newest stored revision file generically, so a
// test can see which members were stored.
func (f *l3Fix) latestRecordJSON() map[string]any {
	f.t.Helper()
	names, err := filepath.Glob(filepath.Join(f.dir, "record-*.json"))
	if err != nil || len(names) == 0 {
		f.t.Fatalf("no record files: %v", err)
	}
	sort.Strings(names)
	data, err := os.ReadFile(names[len(names)-1])
	if err != nil {
		f.t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		f.t.Fatalf("decode %s: %v", names[len(names)-1], err)
	}
	return out
}

// l3Members lists the member names of a decoded JSON object.
func l3Members(v any) []string {
	m, _ := v.(map[string]any)
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// storedRun returns the last stored check run with id, decoded generically
// from the newest revision file.
func (f *l3Fix) storedRun(id string) map[string]any {
	f.t.Helper()
	list, _ := f.latestRecordJSON()["checks"].([]any)
	for i := len(list) - 1; i >= 0; i-- {
		if m, _ := list[i].(map[string]any); m["id"] == id {
			return m
		}
	}
	f.t.Fatalf("stored record has no check run %s", id)
	return nil
}

// storedAssignment returns the stored assignment of brief id, decoded
// generically from the newest revision file.
func (f *l3Fix) storedAssignment(id string) map[string]any {
	f.t.Helper()
	list, _ := f.latestRecordJSON()["assignments"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		if b, _ := m["brief"].(map[string]any); b["id"] == id {
			return m
		}
	}
	f.t.Fatalf("stored record has no assignment %s", id)
	return nil
}

// storedResult returns the stored result of assignment id, decoded
// generically from the newest revision file.
func (f *l3Fix) storedResult(id string) map[string]any {
	f.t.Helper()
	list, _ := f.latestRecordJSON()["worker_results"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		if r, _ := m["result"].(map[string]any); r["assignment_id"] == id {
			return r
		}
	}
	f.t.Fatalf("stored record has no result for %s", id)
	return nil
}

func l3Has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// l3InOrder reports whether want appears in list as a subsequence, in order.
func l3InOrder(list, want []string) bool {
	i := 0
	for _, x := range list {
		if i < len(want) && x == want[i] {
			i++
		}
	}
	return i == len(want)
}

func l3WithPrefix(list []string, prefix string) []string {
	var out []string
	for _, x := range list {
		if strings.HasPrefix(x, prefix) {
			out = append(out, x)
		}
	}
	return out
}

func l3Criterion(t *testing.T, rep taskflow.TraceReport, id string) taskflow.TraceCriterion {
	t.Helper()
	for _, c := range rep.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("trace has no criterion %s in %s", id, l3JSON(rep))
	return taskflow.TraceCriterion{}
}

func l3Mapped(path, name string) taskflow.MappedTest {
	return taskflow.MappedTest{Path: path, Name: name}
}

func l3Map(req string, tests ...taskflow.MappedTest) taskflow.RequirementMapping {
	return taskflow.RequirementMapping{Requirement: req, Tests: tests}
}

func l3Uncovered(req, reason string) taskflow.RequirementMapping {
	return taskflow.RequirementMapping{Requirement: req, Uncovered: reason}
}

func l3JSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(data)
}

// l3TraceRun is the expected TraceRun of run for one test's entry. The
// fixtures run go test -json, where a test that ran once has one run event.
func l3TraceRun(run taskflow.CheckRun, current, credited bool, pass, fail, skip int) taskflow.TraceRun {
	fp := ""
	if run.After != nil {
		fp = run.After.Fingerprint
	}
	return taskflow.TraceRun{RunID: run.ID, CheckID: run.CheckID, Status: run.Status, Fingerprint: fp,
		Current: current, Credited: credited, Pass: pass, Fail: fail, Skip: skip, Run: 1}
}
