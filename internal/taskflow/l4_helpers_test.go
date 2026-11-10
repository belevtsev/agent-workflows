package taskflow_test

// Shared fixtures for the L4 contract tests (interface contract L4, third
// version). They extend the L3 fixtures (l3_helpers_test.go): every fixture is
// a real Git repository holding a small Go module in t.TempDir(), a separate
// task store, and real go test runs through taskflow.Run. Records with test
// rules are started with taskflow.StartWithPacks, legacy records with
// taskflow.Start. Expectations live in the tests, never here.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Exact texts of the L4 contract.
const (
	l4Rules           = "go.test-evidence/v1"
	l4InvalidRules    = "invalid stored test rules"
	l4Immutable       = "immutable task baseline changed"
	l4ShapeMsg        = "changed files need distinct clean paths and SHA256, or explicit deletion without SHA256"
	l4OnlyMsg         = "changed file %s: cw records gofmt_sha256 only for a Go test file that parses, in a contract-bearing result of a task with test rules; omit it"
	l4DiffersMsg      = "changed file %s: gofmt_sha256 differs from the SHA-256 of its gofmt-formatted bytes"
	l4HashDiffersMsg  = "changed file %s hash differs"
	l4R15             = "test files changed, but the plan has no go test check whose argv earns per-test credit (it needs -json in its own argv and only flags the Go evidence rule allows: no -args, -run, -skip or other refused flag), so no test engineer can carry a contract; the plan cannot change: stop and ask the user how to proceed"
	l4R17             = "a brief with a contract needs a go test check in the plan whose argv earns per-test credit (it needs -json in its own argv and only flags the Go evidence rule allows: no -args, -run, -skip or other refused flag); this task's plan has none"
	l4R23             = "test authorship not judged: the selected path has no go.mod, so no file counts as a Go test file; a go.work workspace root is not supported"
	l4NoManifest      = "task predates the baseline manifest; changed test files cannot be listed"
	l4CannotJudge     = "test authorship cannot be judged: "
	l4SourceChanged   = "source changed during authorship; run it again"
	l4CitationNoHold  = "finding %s of worker assignment %s cites it as a reproduction test, but the citation does not hold: a reproduction test is a new file whose current bytes the citation names, in an acceptance that lists a check"
	l4ReasonBytes     = "worker assignment %s recorded its current bytes"
	l4ReasonDeletion  = "worker assignment %s recorded its deletion"
	l4ReasonGofmt     = "its current bytes are gofmt-equivalent to what worker assignment %s recorded"
	l4ReasonLost      = "its current state is neither what worker assignment %s, its independent test engineer, recorded nor gofmt-equivalent to it"
	l4ReasonRepro     = "reproduction test for finding %s of worker assignment %s"
	l4ReasonNoBytes   = "no worker result recorded its current bytes"
	l4ReasonNoDelete  = "no worker result recorded its deletion"
	l4FaultBytes      = "worker assignment %s recorded its current bytes, but %s"
	l4FaultDeletion   = "worker assignment %s recorded its deletion, but %s"
	l4StalePrefix     = "worker assignment %s contract is stale: "
	l4UnprovenPhrase  = " has no proven PASS at the current source: "
	l4NotIndependent  = " is not independently authored: "
	l4DetailMarked    = "run %s of check %s recorded an output line of it beginning with byte 0x16"
	l4DetailTooMany   = "run %s of check %s recorded %d run events for it, more than its -count, -cpu and declarations allow (%d)"
	l4DetailBound     = "run %s of check %s recorded a PASS, but not a run event for each of its results, and stopped recording at its bound"
	l4DetailNoRun     = "run %s of check %s recorded a PASS, but not a run event for each of its results"
	l4UncoveredReason = "fixture: authorship only"
)

type l4Opts struct {
	files  map[string]string // files relative to the selected path, committed
	reqs   []string          // plan requirement IDs, description "criterion <id>"
	checks []taskflow.Check
	sub    bool // the selected path is "mod/" below the Git root
	legacy bool // taskflow.Start: a legacy record without test rules
	small  bool // plan kind small instead of substantial
	// bare omits the default go.mod and p/p.go; contract names the
	// module-relative package directory of the contract extract ("p").
	bare       bool
	contract   string
	noContract bool // no contract extract (a selected path without Go files)
	ownGOFLAGS bool
}

// l4New builds the repository, starts the task (with test rules unless
// legacy) and saves a contract extract of the contract package outside the
// project.
func l4New(t *testing.T, o l4Opts) *l3Fix {
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
	files := map[string]string{}
	if !o.bare {
		files["go.mod"] = l3GoMod
		files["p/p.go"] = l3PGo
	}
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
	kind := "substantial"
	if o.small {
		kind = "small"
	}
	plan := taskflow.Plan{Title: "l4 fixture", Kind: kind, CoordinatorID: l3Coordinator, Checks: checks}
	if !o.small {
		// Checkpoints apply only to substantial tasks.
		plan.Checkpoints = &taskflow.CheckpointPolicy{Required: false, Reason: "fixture"}
	}
	for _, id := range reqs {
		plan.Requirements = append(plan.Requirements, taskflow.Requirement{ID: id, Description: "criterion " + id, CheckIDs: []string{checks[0].ID}})
	}
	var rec taskflow.Record
	var err error
	if o.legacy {
		rec, err = taskflow.Start(f.ctx, f.sel, store, plan, nil)
	} else {
		rec, err = taskflow.StartWithPacks(f.ctx, f.sel, store, plan, nil, taskflow.SelectionExplicit, nil)
	}
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	f.dir = rec.Directory
	if o.noContract {
		return f
	}
	pkg := o.contract
	if pkg == "" {
		pkg = "p"
	}
	extract, err := taskflow.ExtractContract(filepath.Join(f.sel, filepath.FromSlash(pkg)), []string{"F"})
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

// l4Test is a test file of package pkg declaring the named empty tests.
func l4Test(pkg string, names ...string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\nimport \"testing\"\n")
	for _, n := range names {
		b.WriteString("\nfunc " + n + "(t *testing.T) {}\n")
	}
	return b.String()
}

// te records a contract-bearing (test-engineer) round: assign with
// requirement R1 for criterion C1, write the files, and hand off with the
// given status (an uncovered map when completed). Each content "" deletes.
func (f *l3Fix) te(id, agent, status string, files map[string]string, order ...string) {
	f.t.Helper()
	owned := order
	if owned == nil {
		for p := range files {
			owned = append(owned, p)
		}
		sort.Strings(owned)
	}
	b := f.brief(id, owned, l3Req("R1", "C1"))
	if agent != "" {
		b.AgentID = agent
	}
	f.mustAssign(b)
	f.apply(files)
	var rmap []taskflow.RequirementMapping
	if status == "completed" {
		rmap = []taskflow.RequirementMapping{l3Uncovered("R1", l4UncoveredReason)}
	}
	f.mustSubmit(f.fastResult(id, status, owned, rmap...))
}

// fastResult is l3Fix.result without a snapshot, which the handoff fills
// with the current source's, and with the brief's check IDs; it spares a
// verify per handoff.
func (f *l3Fix) fastResult(id, status string, changed []string, rmap ...taskflow.RequirementMapping) taskflow.WorkerResult {
	f.t.Helper()
	a := f.assignment(id)
	res := taskflow.WorkerResult{
		TaskID: a.TaskID, AssignmentID: id, AssignmentSHA256: a.SHA256, AgentID: a.Brief.AgentID,
		Status: status, Summary: "fixture handoff",
		ChangedFiles: []taskflow.WorkerChangedFile{}, CheckIDs: append([]string{}, a.Brief.CheckIDs...),
		Findings: []taskflow.WorkerFinding{}, Uncertainties: []string{},
		RequirementMap: rmap,
	}
	for _, c := range changed {
		data, err := os.ReadFile(f.abs(c))
		if os.IsNotExist(err) {
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

// plain records an implementation round without a contract that writes the
// files and hands off completed.
func (f *l3Fix) plain(id, agent string, files map[string]string) {
	f.t.Helper()
	var owned []string
	for p := range files {
		owned = append(owned, p)
	}
	sort.Strings(owned)
	b := f.plainBrief(id, owned)
	if agent != "" {
		b.AgentID = agent
	}
	f.mustAssign(b)
	f.apply(files)
	f.mustSubmit(f.fastResult(id, "completed", owned))
}

// apply writes each file, or deletes it when its content is "".
func (f *l3Fix) apply(files map[string]string) {
	f.t.Helper()
	for p, c := range files {
		if c == "" {
			if err := os.Remove(f.abs(p)); err != nil && !os.IsNotExist(err) {
				f.t.Fatal(err)
			}
			continue
		}
		f.write(p, c)
	}
}

// roleBrief is a brief of role evidence, review or verification.
func (f *l3Fix) roleBrief(id, agent, role string, checks []string) taskflow.WorkerBrief {
	if checks == nil {
		checks = []string{}
	}
	return taskflow.WorkerBrief{
		ID: id, AgentID: agent, Role: role, Goal: "fixture " + role,
		OwnedPaths: []string{"."}, Constraints: []string{}, CheckIDs: checks, StopCondition: "done",
	}
}

// review assigns a review round with a filled fixture bundle.
func (f *l3Fix) review(id, agent string) {
	f.t.Helper()
	bundle, err := taskflow.BuildBundle(f.ctx, f.dir, id)
	if err != nil {
		f.t.Fatalf("BuildBundle(%s): %v", id, err)
	}
	for i := range bundle.Reach {
		r := &bundle.Reach[i]
		r.Callers, r.InvocationModes, r.Platforms, r.Consumers = "n/a: fixture", "n/a: fixture", "n/a: fixture", "n/a: fixture"
	}
	data, err := taskflow.EncodeBundle(bundle)
	if err != nil {
		f.t.Fatal(err)
	}
	p := filepath.Join(f.root, "bundle-"+id+".json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
	b := f.roleBrief(id, agent, "review", []string{f.load().Plan.Checks[0].ID})
	b.Bundle = &taskflow.BundleRef{Path: p, SHA256: l3Sum(data)}
	f.mustAssign(b)
}

// ref is a source reference to the whole current bytes of a worker path.
func (f *l3Fix) ref(worker string) taskflow.SourceRef {
	f.t.Helper()
	data, err := os.ReadFile(f.abs(worker))
	if err != nil {
		f.t.Fatal(err)
	}
	return taskflow.SourceRef{Path: worker, StartLine: 1, EndLine: 1, SHA256: l3Sum(data)}
}

// l4Cite is one finding of a citing round and the path it cites.
type l4Cite struct{ finding, path string }

// citer assigns a round of role evidence or verification and hands off a
// completed result with one finding per cite.
func (f *l3Fix) citer(id, role string, cites ...l4Cite) {
	f.t.Helper()
	f.mustAssign(f.roleBrief(id, "agent-"+id, role, []string{f.load().Plan.Checks[0].ID}))
	res := f.fastResult(id, "completed", nil)
	for _, c := range cites {
		r := f.ref(c.path)
		res.Findings = append(res.Findings, taskflow.WorkerFinding{ID: c.finding, Summary: "Reproduction test " + c.path + ".", Severity: "minor", Evidence: []taskflow.SourceRef{r}})
	}
	f.mustSubmit(res)
}

// judge records an acceptance of the assignment's result with the given
// check IDs and one disposition of status disp per finding, citing the
// finding's path at its current bytes.
func (f *l3Fix) judge(id, status string, checks []string, disp string) {
	f.t.Helper()
	rec := f.load()
	var res taskflow.BoundWorkerResult
	found := false
	for _, r := range rec.WorkerResults {
		if r.Result.AssignmentID == id {
			res, found = r, true
		}
	}
	if !found {
		f.t.Fatalf("no result for %s", id)
	}
	if checks == nil {
		checks = []string{}
	}
	ds := []taskflow.FindingDisposition{}
	for _, fd := range res.Result.Findings {
		ds = append(ds, taskflow.FindingDisposition{FindingID: fd.ID, Status: disp, Reason: "fixture disposition of " + fd.ID,
			Evidence: []taskflow.SourceRef{f.ref(fd.Evidence[0].Path)}})
	}
	_, err := taskflow.AcceptWorkerResult(f.ctx, f.dir, taskflow.WorkerAcceptance{
		TaskID: rec.ID, AssignmentID: id, ResultSHA256: res.SHA256, CoordinatorID: l3Coordinator,
		Status: status, Reason: "fixture judgment", Evidence: []taskflow.SourceRef{f.goModRef()},
		CheckIDs: checks, Findings: ds,
	})
	if err != nil {
		f.t.Fatalf("AcceptWorkerResult(%s, %s): %v", id, status, err)
	}
}

// authorship calls taskflow.Authorship, turning a panic of an unimplemented
// stub into an error.
func l4AuthorshipErr(ctx context.Context, dir string) (rep taskflow.AuthorshipReport, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Authorship panicked: %v", r)
		}
	}()
	return taskflow.Authorship(ctx, dir)
}

func (f *l3Fix) authorship() taskflow.AuthorshipReport {
	f.t.Helper()
	rep, err := l4AuthorshipErr(f.ctx, f.dir)
	if err != nil {
		f.t.Fatalf("Authorship: %v", err)
	}
	return rep
}

// l4File is an expected AuthorshipFile; asg names the assignment R12 names
// ("" for none), whose agent and brief contract it carries.
func (f *l3Fix) l4File(path string, deleted bool, status, reason, asg, finding string) taskflow.AuthorshipFile {
	f.t.Helper()
	x := taskflow.AuthorshipFile{Path: path, Deleted: deleted, Status: status, Reason: reason, FindingID: finding}
	if asg != "" {
		a := f.assignment(asg)
		x.AssignmentID = asg
		x.AgentID = a.Brief.AgentID
		if a.Brief.Contract != nil {
			c := *a.Brief.Contract
			x.Contract = &c
		}
	}
	return x
}

// l4Report checks a whole authorship report: identity members, then files and
// problems as JSON (every list an array, never null).
func (f *l3Fix) l4Report(label string, rep taskflow.AuthorshipReport, testRules string, gated bool, problems []string, files ...taskflow.AuthorshipFile) {
	f.t.Helper()
	rec := f.load()
	if rep.TaskID != rec.ID {
		f.t.Errorf("%s: task_id %q, want %q (R19)", label, rep.TaskID, rec.ID)
	}
	if fp := f.verify().CurrentSnapshot.Fingerprint; rep.Fingerprint != fp {
		f.t.Errorf("%s: fingerprint %q, want the current source's %q (R19)", label, rep.Fingerprint, fp)
	}
	if rep.TestRules != testRules || rep.Gated != gated {
		f.t.Errorf("%s: test_rules %q gated %v, want %q and %v (R19)", label, rep.TestRules, rep.Gated, testRules, gated)
	}
	if problems == nil {
		problems = []string{}
	}
	if files == nil {
		files = []taskflow.AuthorshipFile{}
	}
	l3SameJSON(f.t, label+" problems", rep.Problems, problems)
	l3SameJSON(f.t, label+" files", rep.Files, files)
	if data := l3JSON(rep); strings.Contains(data, "null") {
		f.t.Errorf("%s: report JSON holds null; files and problems are arrays (R19): %s", label, data)
	}
}

// l4Gofmt is the gofmt hash of data: the SHA-256 of go/format.Source, and
// false when data has none.
func l4Gofmt(data []byte) (string, bool) {
	out, err := format.Source(data)
	if err != nil {
		return "", false
	}
	return l3Sum(out), true
}

// rewrite rewrites every stored revision through edit, keeping the hash
// chain (previous_sha256 is the SHA-256 of the previous revision file's
// bytes), so a test can store a record this cw would not write.
func (f *l3Fix) rewrite(edit func(revision int, doc map[string]any)) {
	f.t.Helper()
	names, err := filepath.Glob(filepath.Join(f.dir, "record-*.json"))
	if err != nil || len(names) == 0 {
		f.t.Fatalf("no record files: %v", err)
	}
	sort.Strings(names)
	prev, origPrev := "", ""
	for i, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			f.t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			f.t.Fatalf("decode %s: %v", name, err)
		}
		if i > 0 && doc["previous_sha256"] != origPrev {
			f.t.Fatalf("fixture: %s does not chain to the SHA-256 of the previous revision file", name)
		}
		origPrev = l3Sum(data)
		edit(i+1, doc)
		if i > 0 {
			doc["previous_sha256"] = prev
		}
		out, err := json.Marshal(doc)
		if err != nil {
			f.t.Fatal(err)
		}
		info, err := os.Stat(name)
		if err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(name, out, info.Mode().Perm()); err != nil {
			f.t.Fatal(err)
		}
		prev = l3Sum(out)
	}
}

// manifestFile is the task's stored baseline manifest file.
func (f *l3Fix) manifestFile() string {
	f.t.Helper()
	m, _ := f.latestRecordJSON()["baseline_manifest"].(map[string]any)
	name, _ := m["file"].(string)
	if name == "" {
		f.t.Fatalf("record names no baseline manifest file: %v", m)
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(f.dir, name)
}

// resultsFile is the path of a run's stored results file.
func (f *l3Fix) resultsFile(run taskflow.CheckRun) string {
	f.t.Helper()
	if run.TestResults == "" {
		f.t.Fatalf("run %s stored no results file", run.ID)
	}
	if filepath.IsAbs(run.TestResults) {
		return run.TestResults
	}
	return filepath.Join(f.dir, run.TestResults)
}

// l4TR is the expected TraceRun of a current, credited run entry.
func l4TR(run taskflow.CheckRun, pass, fail, skip, runs, marked int) taskflow.TraceRun {
	r := l3TraceRun(run, true, true, pass, fail, skip)
	r.Run, r.Marked = runs, marked
	return r
}

// l4HasPrefix reports whether some entry of list has the prefix.
func l4HasPrefix(list []string, prefix string) bool {
	return len(l3WithPrefix(list, prefix)) > 0
}

// l4AuthorshipProblems lists verify's authorship problems: R14, R15 and R16 entries.
func l4AuthorshipProblems(v taskflow.Verification) []string {
	var out []string
	for _, p := range v.Problems {
		if strings.HasPrefix(p, "test file ") || strings.HasPrefix(p, "test files changed") || strings.HasPrefix(p, l4CannotJudge) {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}
