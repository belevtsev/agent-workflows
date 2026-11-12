package cli_test

// cw task trace (interface contract L3, revision 3): R36, and the fixtures
// the summary tests share. Each fixture is a real Git repository holding a
// small Go module in t.TempDir(), a separate task store, and real go test
// runs; the command runs through cli.Execute with explicit streams.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	l3cCoordinator = "coord-l3"
	l3cModule      = "example.com/fix"
	// l3cWait bounds waiting for a subprocess; it never bounds the work.
	l3cWait   = 3 * time.Minute
	l3cPTests = `package p

import "testing"

func TestPass(t *testing.T) {}

func TestSkip(t *testing.T) { t.Skip("skipped by the fixture") }
`
	l3cQTests = `package q

import "testing"

func TestFail(t *testing.T) { t.Fatal("fails by the fixture") }
`
)

type l3cFix struct {
	t        *testing.T
	ctx      context.Context
	root     string
	repo     string
	home     string
	dir      string
	id       string
	contract *taskflow.ContractRef
}

func l3cCheck(id string, argv ...string) taskflow.Check {
	return taskflow.Check{ID: id, Argv: argv, TimeoutSeconds: 300}
}

func l3cNew(t *testing.T, reqs []string, checks ...taskflow.Check) *l3cFix {
	t.Helper()
	l3cNeedNoGOFLAGS(t)
	root := t.TempDir()
	f := &l3cFix{t: t, ctx: context.Background(), root: root, repo: filepath.Join(root, "repo dir"), home: filepath.Join(root, "home")}
	f.write("go.mod", "module "+l3cModule+"\n\ngo 1.22\n")
	f.write("p/p.go", "package p\n\n// F is the contract symbol.\nfunc F() int { return 1 }\n")
	if err := os.MkdirAll(f.home, 0o700); err != nil {
		t.Fatal(err)
	}
	f.git("init", "-q")
	f.git("add", ".")
	f.git("-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "-qm", "fixture")
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := taskflow.Plan{Title: "l3 cli fixture", Kind: "substantial", CoordinatorID: l3cCoordinator, Checks: checks,
		Checkpoints: &taskflow.CheckpointPolicy{Required: false, Reason: "fixture"}}
	for _, id := range reqs {
		plan.Requirements = append(plan.Requirements, taskflow.Requirement{ID: id, Description: "criterion " + id, CheckIDs: []string{checks[0].ID}})
	}
	rec, err := taskflow.StartWithPacks(f.ctx, f.repo, store, plan, nil, taskflow.SelectionExplicit, nil)
	if err != nil {
		t.Fatalf("StartWithPacks: %v", err)
	}
	f.dir, f.id = rec.Directory, rec.ID
	extract, err := taskflow.ExtractContract(filepath.Join(f.repo, "p"), []string{"F"})
	if err != nil {
		t.Fatalf("ExtractContract: %v", err)
	}
	data, err := taskflow.EncodeContract(extract)
	if err != nil {
		t.Fatal(err)
	}
	cpath := filepath.Join(root, "contract.json")
	if err := os.WriteFile(cpath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f.contract = &taskflow.ContractRef{Path: cpath, SHA256: l3cSum(data)}
	return f
}

// l3cNeedNoGOFLAGS fails fast when go would run the fixture checks with
// GOFLAGS (environment or go env -w); credit and counts depend on it, and the
// project check and CI set none.
func l3cNeedNoGOFLAGS(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), l3cWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "env", "GOFLAGS").Output()
	if err != nil {
		t.Fatalf("go env GOFLAGS: %v", err)
	}
	if v := strings.TrimSpace(string(out)); v != "" {
		t.Fatalf("these tests need an empty GOFLAGS (the project check and CI set none); go env GOFLAGS = %q", v)
	}
}

// l3cEmitter builds, outside any project, a tiny program for checks that must
// write exact lines (checks cannot run a shell). Each argument is
// "out:<line>", "err:<line>" or "sleep" (an hour).
func l3cEmitter(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n\t\"time\"\n)\n\n" +
		"func main() {\n\tfor _, a := range os.Args[1:] {\n\t\tswitch {\n" +
		"\t\tcase strings.HasPrefix(a, \"out:\"):\n\t\t\tfmt.Fprintln(os.Stdout, a[4:])\n" +
		"\t\tcase strings.HasPrefix(a, \"err:\"):\n\t\t\tfmt.Fprintln(os.Stderr, a[4:])\n" +
		"\t\tcase a == \"sleep\":\n\t\t\ttime.Sleep(time.Hour)\n\t\t}\n\t}\n}\n"
	for name, content := range map[string]string{"go.mod": "module example.com/emitter\n\ngo 1.22\n", "main.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "emit")
	ctx, cancel := context.WithTimeout(context.Background(), l3cWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build emitter: %v\n%s", err, out)
	}
	return bin
}

func l3cSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f *l3cFix) git(args ...string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), l3cWait)
	defer cancel()
	// Isolate the fixture from user and system Git configuration.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = f.repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (f *l3cFix) write(rel, content string) {
	f.t.Helper()
	name := filepath.Join(f.repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *l3cFix) load() taskflow.Record {
	f.t.Helper()
	rec, err := taskflow.Load(f.dir)
	if err != nil {
		f.t.Fatalf("Load: %v", err)
	}
	return rec
}

func l3cReq(id string, criteria ...string) taskflow.ContractRequirement {
	return taskflow.ContractRequirement{Requirement: id, Criteria: criteria}
}

func (f *l3cFix) assign(id string, owned []string, reqs ...taskflow.ContractRequirement) {
	f.t.Helper()
	c := *f.contract
	b := taskflow.WorkerBrief{ID: id, AgentID: "agent-" + id, Role: "implementation", Goal: "fixture work",
		OwnedPaths: owned, Constraints: []string{}, CheckIDs: []string{}, StopCondition: "done",
		Contract: &c, Requirements: reqs}
	if _, err := taskflow.AssignWorker(f.ctx, f.dir, b); err != nil {
		f.t.Fatalf("AssignWorker(%s): %v", id, err)
	}
}

func (f *l3cFix) goModRef() taskflow.SourceRef {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.repo, "go.mod"))
	if err != nil {
		f.t.Fatal(err)
	}
	return taskflow.SourceRef{Path: "go.mod", StartLine: 1, EndLine: 1, SHA256: l3cSum(data)}
}

// submit records a completed handoff of the changed files with the map.
func (f *l3cFix) submit(id string, changed []string, rmap ...taskflow.RequirementMapping) {
	f.t.Helper()
	rec := f.load()
	var a taskflow.WorkerAssignment
	for _, x := range rec.Assignments {
		if x.Brief.ID == id {
			a = x
		}
	}
	v, err := taskflow.Verify(f.ctx, f.dir)
	if v.TaskID == "" {
		f.t.Fatalf("Verify: %v", err)
	}
	res := taskflow.WorkerResult{TaskID: f.id, AssignmentID: id, AssignmentSHA256: a.SHA256, AgentID: a.Brief.AgentID,
		Snapshot: v.CurrentSnapshot, Status: "completed", Summary: "fixture handoff", Evidence: []taskflow.SourceRef{f.goModRef()},
		ChangedFiles: []taskflow.WorkerChangedFile{}, CheckIDs: []string{}, Findings: []taskflow.WorkerFinding{},
		Uncertainties: []string{}, RequirementMap: rmap}
	for _, c := range changed {
		data, err := os.ReadFile(filepath.Join(f.repo, filepath.FromSlash(c)))
		if err != nil {
			f.t.Fatal(err)
		}
		res.ChangedFiles = append(res.ChangedFiles, taskflow.WorkerChangedFile{Path: c, SHA256: l3cSum(data)})
	}
	if _, err := taskflow.SubmitWorkerResult(f.ctx, f.dir, res); err != nil {
		f.t.Fatalf("SubmitWorkerResult(%s): %v", id, err)
	}
}

func (f *l3cFix) accept(id, status string) {
	f.t.Helper()
	var sha string
	for _, r := range f.load().WorkerResults {
		if r.Result.AssignmentID == id {
			sha = r.SHA256
		}
	}
	_, err := taskflow.AcceptWorkerResult(f.ctx, f.dir, taskflow.WorkerAcceptance{TaskID: f.id, AssignmentID: id, ResultSHA256: sha,
		CoordinatorID: l3cCoordinator, Status: status, Reason: "fixture judgment", Evidence: []taskflow.SourceRef{f.goModRef()},
		CheckIDs: []string{}, Findings: []taskflow.FindingDisposition{}})
	if err != nil {
		f.t.Fatalf("AcceptWorkerResult(%s): %v", id, err)
	}
}

func (f *l3cFix) run(check string) taskflow.CheckRun {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, l3cWait)
	defer cancel()
	run, err := taskflow.RunCheck(ctx, f.dir, check)
	if run.ID == "" {
		f.t.Fatalf("RunCheck(%s): %v", check, err)
	}
	return run
}

func l3cMap(req string, tests ...taskflow.MappedTest) taskflow.RequirementMapping {
	return taskflow.RequirementMapping{Requirement: req, Tests: tests}
}

func l3cUncovered(req, reason string) taskflow.RequirementMapping {
	return taskflow.RequirementMapping{Requirement: req, Uncovered: reason}
}

func l3cTest(path, name string) taskflow.MappedTest {
	return taskflow.MappedTest{Path: path, Name: name}
}

// execute runs cw with args through cli.Execute and an isolated home and
// environment, returning stdout, stderr and the error. A panic of an
// unimplemented stub becomes an error.
func (f *l3cFix) execute(args ...string) (stdout, stderr string, err error) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, l3cWait)
	defer cancel()
	var out, errOut bytes.Buffer
	env := map[string]string{"HOME": f.home, "PATH": os.Getenv("PATH"), "TERM": "dumb", "LC_ALL": "C"}
	cfg := cli.Config{
		In: strings.NewReader(""), Out: &out, Err: &errOut,
		HomeDir:    func() (string, error) { return f.home, nil },
		WorkingDir: func() (string, error) { return f.repo, nil },
		Getenv:     func(k string) string { return env[k] },
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("cw panicked")
				f.t.Errorf("cw %v panicked: %v", args, r)
			}
		}()
		err = cli.Execute(ctx, args, cfg)
	}()
	return out.String(), errOut.String(), err
}

// l3cTree captures every entry of the task directory (name, mode, bytes).
func l3cTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
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
			val += " " + l3cSum(data)
		}
		out[p] = val
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func l3cSameTree(t *testing.T, what string, before, after map[string]string) {
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
		t.Errorf("%s wrote to the task directory: %v", what, diffs)
	}
}

// l3cOneLine requires out to be exactly one line ending in a newline.
func l3cOneLine(t *testing.T, label, out string) string {
	t.Helper()
	if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 {
		t.Errorf("%s: stdout is not exactly one line: %q", label, out)
	}
	return strings.TrimSuffix(out, "\n")
}

// l3cTraceFixture is a task with a proven criterion C1 (TestPass), an open
// criterion C2 (a failed TestFail) and an unmapped C3.
func l3cTraceFixture(t *testing.T) *l3cFix {
	f := l3cNew(t, []string{"C1", "C2", "C3"}, l3cCheck("ok", "go", "test", "-json", "./p/..."), l3cCheck("bad", "go", "test", "-json", "./q/..."))
	f.assign("te1", []string{"p/te_test.go", "q/te_test.go"}, l3cReq("R1", "C1"), l3cReq("R2", "C2"))
	f.write("p/te_test.go", l3cPTests)
	f.write("q/te_test.go", l3cQTests)
	f.submit("te1", []string{"p/te_test.go", "q/te_test.go"},
		l3cMap("R1", l3cTest("p/te_test.go", "TestPass")), l3cMap("R2", l3cTest("q/te_test.go", "TestFail")))
	f.accept("te1", "accepted")
	f.run("ok")
	f.run("bad")
	return f
}

// TestTaskTraceCLIPrintsReport fails when cw task trace does not print the
// TraceReport as one JSON line, exits non-zero because a criterion is open,
// writes to the task, or behaves differently with --dry-run (R36, R33).
func TestTaskTraceCLIPrintsReport(t *testing.T) {
	t.Parallel()
	f := l3cTraceFixture(t)
	before := l3cTree(t, f.dir)
	out, stderr, err := f.execute("task", "trace", "--task", f.dir)
	if err != nil || cli.ExitCode(err) != 0 {
		t.Fatalf("cw task trace: %v (exit %d), stderr %q; want exit 0 whatever the statuses (R36)", err, cli.ExitCode(err), stderr)
	}
	line := l3cOneLine(t, "cw task trace", out)
	var rep taskflow.TraceReport
	if err := taskflow.Decode(strings.NewReader(line), &rep); err != nil {
		t.Fatalf("stdout does not decode strictly as a TraceReport: %v\n%s (R36)", err, line)
	}
	if rep.TaskID != f.id || len(rep.Criteria) != 3 {
		t.Fatalf("printed report task_id %q with %d criteria, want %q with 3 (R36)", rep.TaskID, len(rep.Criteria), f.id)
	}
	statuses := map[string]string{}
	for _, c := range rep.Criteria {
		statuses[c.ID] = c.Status
	}
	if want := map[string]string{"C1": "proven", "C2": "open", "C3": "unmapped"}; !l3cEqualMaps(statuses, want) {
		t.Errorf("printed criterion statuses %v, want %v", statuses, want)
	}
	direct, err := taskflow.Trace(f.ctx, f.dir)
	if err != nil {
		t.Fatalf("taskflow.Trace: %v", err)
	}
	want, _ := json.Marshal(direct)
	if !l3cSameJSON(line, string(want)) {
		t.Errorf("printed report differs from taskflow.Trace's:\n got  %s\n want %s (R36)", line, want)
	}
	dry, _, err := f.execute("task", "trace", "--task", f.dir, "--dry-run")
	if err != nil {
		t.Errorf("cw task trace --dry-run: %v, want it accepted (R36)", err)
	} else if dry != out {
		t.Errorf("--dry-run changed the output:\n got  %s\n want %s (R36)", dry, out)
	}
	l3cSameTree(t, "cw task trace", before, l3cTree(t, f.dir))
}

// l3cSameJSON reports whether two JSON texts hold equal values.
func l3cSameJSON(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func l3cEqualMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestTaskTraceCLIErrors fails when a missing --task or a positional argument
// is not a usage error (exit 2), or when a task that cannot be traced does not
// exit 1 with nothing on stdout, with or without --summary (R36, R37).
func TestTaskTraceCLIErrors(t *testing.T) {
	t.Parallel()
	f := l3cNew(t, []string{"C1"}, l3cCheck("ok", "go", "test", "-json", "./p/..."))
	for _, summary := range []bool{false, true} {
		extra := []string{}
		if summary {
			extra = []string{"--summary"}
		}
		out, stderr, err := f.execute(append([]string{"task", "trace"}, extra...)...)
		if cli.ExitCode(err) != 2 || err == nil || !strings.Contains(err.Error()+stderr, "--task DIR is required") {
			t.Errorf("cw task trace %v without --task: %v (exit %d), stderr %q; want usage error %q, exit 2 (R36)", extra, err, cli.ExitCode(err), stderr, "--task DIR is required")
		}
		if out != "" {
			t.Errorf("usage error printed %q on stdout", out)
		}
		_, _, err = f.execute(append([]string{"task", "trace", "--task", f.dir, "extra"}, extra...)...)
		if cli.ExitCode(err) != 2 {
			t.Errorf("cw task trace %v with a positional argument: %v (exit %d), want exit 2 (R36)", extra, err, cli.ExitCode(err))
		}
		missing := filepath.Join(f.root, "no-such-task")
		out, _, err = f.execute(append([]string{"task", "trace", "--task", missing}, extra...)...)
		if err == nil || cli.ExitCode(err) != 1 || out != "" {
			t.Errorf("cw task trace %v of a missing task: %v (exit %d), stdout %q; want exit 1 and no stdout (R36)", extra, err, cli.ExitCode(err), out)
		}
	}
}
