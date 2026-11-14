package taskflow

import (
	"context"
	"fmt"
	"go/format"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Authorship lists each changed test file with its authorship status (R19).
// It changes nothing.
func (env Env) Authorship(ctx context.Context, taskPath string) (AuthorshipReport, error) {
	root, directory, err := openTask(taskPath)
	if err != nil {
		return AuthorshipReport{}, err
	}
	defer root.Close()
	record, _, err := loadRoot(root, directory)
	if err != nil {
		return AuthorshipReport{}, err
	}
	snapshot, files, err := env.source().Manifest(ctx, record.Baseline.SelectedPath)
	if err != nil {
		return AuthorshipReport{}, err
	}
	view := newSourceView(snapshot, files)
	report := authorshipReport(view.authorship(taskDirectory{root}, record), record, snapshot)
	view.close()
	after, err := env.source().Snapshot(ctx, record.Baseline.SelectedPath)
	if err != nil {
		return AuthorshipReport{}, err
	}
	if !equalSnapshot(snapshot, after) {
		report.Problems = append(report.Problems, "source changed during authorship; run it again")
	}
	return report, nil
}

type AuthorshipReport struct {
	TaskID      string           `json:"task_id"`
	Fingerprint string           `json:"fingerprint"`
	TestRules   string           `json:"test_rules"`
	Gated       bool             `json:"gated"`
	Files       []AuthorshipFile `json:"files"`
	Problems    []string         `json:"problems"`
}

type AuthorshipFile struct {
	Path         string       `json:"path"`
	Deleted      bool         `json:"deleted"` // always present
	Status       string       `json:"status"`  // independent, source_aware, lost or unowned
	AssignmentID string       `json:"assignment_id,omitempty"`
	AgentID      string       `json:"agent_id,omitempty"`
	Contract     *ContractRef `json:"contract,omitempty"`
	FindingID    string       `json:"finding_id,omitempty"`
	Reason       string       `json:"reason"`
}

const (
	// noGoModLimitation is R23's limitation and report problem.
	noGoModLimitation = "test authorship not judged: the selected path has no go.mod, so no file counts as a Go test file; a go.work workspace root is not supported"
	// noPlanCreditProblem is R15's problem.
	noPlanCreditProblem = "test files changed, but the plan has no go test check whose argv earns per-test credit (it needs -json in its own argv and only flags the Go evidence rule allows: no -args, -run, -skip or other refused flag), so no test engineer can carry a contract; the plan cannot change: stop and ask the user how to proceed"
)

// gated reports whether the record has a substantial plan, so verify and
// assign apply the authorship rules to it.
func gated(record Record) bool {
	return record.Plan.Kind == "substantial"
}

// hasAcceptance reports whether the assignment has some acceptance with status.
func hasAcceptance(record Record, id, status string) bool {
	return slices.ContainsFunc(record.WorkerAcceptances, func(bound BoundWorkerAcceptance) bool {
		return bound.Acceptance.AssignmentID == id && bound.Acceptance.Status == status
	})
}

// testSidePath reports whether a project-relative path has a testdata element
// or a last element ending in _test.go, wherever it lies.
func testSidePath(name string) bool {
	return strings.HasSuffix(path.Base(name), "_test.go") || slices.Contains(strings.Split(name, "/"), "testdata")
}

// goTestSourcePath reports whether a path's last element ends in _test.go and
// none of its elements is testdata.
func goTestSourcePath(name string) bool {
	return strings.HasSuffix(path.Base(name), "_test.go") && !slices.Contains(strings.Split(name, "/"), "testdata")
}

// gofmtHash returns the SHA-256 of go/format.Source(data), or false when the
// bytes do not format.
func gofmtHash(data []byte) (string, bool) {
	formatted, err := format.Source(data)
	if err != nil {
		return "", false
	}
	return digest(formatted), true
}

// sourceView is one call's view of the current source: its manifest, and the
// project root opened on first use with a cache, so the trace judgement and
// the authorship classifier read each file at most once. The caller closes it.
type sourceView struct {
	snapshot projectctx.Snapshot
	files    []projectctx.FileState
	source   *testSource
	authors  *authorship
}

func newSourceView(snapshot projectctx.Snapshot, files []projectctx.FileState) *sourceView {
	return &sourceView{snapshot: snapshot, files: files}
}

// testSource returns the view's test source, opening the project root once.
// An unopenable project root leaves every file unreadable.
func (view *sourceView) testSource() *testSource {
	if view.source == nil {
		projectRoot, _ := os.OpenRoot(view.snapshot.Root)
		value := newTestSource(projectRoot, view.snapshot, view.files)
		view.source = &value
	}
	return view.source
}

// authorship returns the view's classifier for record, built once.
func (view *sourceView) authorship(artifacts Artifacts, record Record) *authorship {
	if view.authors == nil {
		state := baselineState{}
		state.files, state.err = readBaselineManifest(artifacts, *record.BaselineManifest)
		source := view.testSource()
		view.authors = newAuthorship(record, state, view.files, func(name string) ([]byte, error) {
			return source.readFile(projectctx.ManifestRawPath(name))
		})
	}
	return view.authors
}

func (view *sourceView) close() {
	if view.source != nil && view.source.root != nil {
		view.source.root.Close()
	}
}

// baselineState is the record's baseline manifest as the classifier sees it:
// unreadable, or its files.
type baselineState struct {
	err   error
	files []projectctx.FileState
}

// authorship classifies paths at one source (R11, R12). It is a pure function
// of the record, the baseline manifest or its read error, the current manifest
// and a file reader.
type authorship struct {
	record   Record
	baseline baselineState
	before   map[string]string // baseline path -> SHA-256
	after    map[string]string // current path -> SHA-256
	paths    []string          // every baseline or current path, in path order
	prefix   testSource        // only its prefix is used, for module-relative forms
	moduled  []string          // F: module-relative forms inside the prefix
	read     func(string) ([]byte, error)

	everRejected []bool
	independent  []bool
	index        map[string]int // assignment ID -> position
}

// authorshipStatus is one path's status, reason, the assignment it names (-1
// for none) and, for a citation, its finding.
type authorshipStatus struct {
	status, reason string
	assignment     int
	finding        string
}

func newAuthorship(record Record, baseline baselineState, current []projectctx.FileState, read func(string) ([]byte, error)) *authorship {
	a := &authorship{record: record, baseline: baseline, before: map[string]string{}, after: map[string]string{}, read: read, index: map[string]int{}}
	if prefix, ok := selectedPrefix(record.Baseline); ok {
		a.prefix.prefix = prefix
	}
	for _, file := range baseline.files {
		a.before[file.Path] = file.SHA256
	}
	for _, file := range current {
		a.after[file.Path] = file.SHA256
	}
	seen := map[string]bool{}
	for _, files := range [][]projectctx.FileState{baseline.files, current} {
		for _, file := range files {
			if seen[file.Path] {
				continue
			}
			seen[file.Path] = true
			a.paths = append(a.paths, file.Path)
			if name, ok := a.prefix.moduleRelative(file.Path); ok {
				a.moduled = append(a.moduled, name)
			}
		}
	}
	slices.Sort(a.paths)
	slices.Sort(a.moduled)
	a.decideAssignments()
	return a
}

// decideAssignments fills, per assignment, whether it was ever rejected and
// whether it is independent: contract-bearing, never rejected and held by an
// agent that is not excluded.
func (a *authorship) decideAssignments() {
	record := a.record
	a.everRejected = make([]bool, len(record.Assignments))
	a.independent = make([]bool, len(record.Assignments))
	excluded := map[string]bool{}
	if record.Plan.CoordinatorID != "" {
		excluded[record.Plan.CoordinatorID] = true
	}
	for i, assigned := range record.Assignments {
		a.index[assigned.Brief.ID] = i
		a.everRejected[i] = hasAcceptance(record, assigned.Brief.ID, "rejected")
		if _, ok := a.exclusion(assigned); ok {
			excluded[assigned.Brief.AgentID] = true
		}
	}
	for i, assigned := range record.Assignments {
		a.independent[i] = assigned.Brief.Contract != nil && !a.everRejected[i] && !excluded[assigned.Brief.AgentID]
	}
}

// exclusion reports whether assigned excludes its agent, with the fault text
// that names it: an implementation assignment without a contract, a review or
// verification assignment, or a contract-bearing assignment whose result wrote
// a path that is not a test-side path.
func (a *authorship) exclusion(assigned WorkerAssignment) (string, bool) {
	switch {
	case assigned.Brief.Role == "implementation" && assigned.Brief.Contract == nil:
		return "an implementation assignment without a contract", true
	case assigned.Brief.Role == "review":
		return "a review assignment", true
	case assigned.Brief.Role == "verification":
		return "a verification assignment", true
	case assigned.Brief.Contract != nil:
		if result := findWorkerResult(a.record, assigned.Brief.ID); result != nil {
			for _, changed := range result.Result.ChangedFiles {
				if !testSidePath(changed.Path) {
					return fmt.Sprintf("whose result changed %s, which is neither a _test.go file nor under a testdata directory", changed.Path), true
				}
			}
		}
	}
	return "", false
}

// unreadable returns the R16 problem text when the baseline manifest cannot
// be read.
func (a *authorship) unreadable() (string, bool) {
	if a.baseline.err == nil {
		return "", false
	}
	return "test authorship cannot be judged: " + a.baseline.err.Error(), true
}

// noGoMod reports R23's condition: F has no go.mod but has go.work or a .go
// entry.
func (a *authorship) noGoMod() bool {
	return !slices.Contains(a.moduled, "go.mod") && slices.ContainsFunc(a.moduled, func(name string) bool {
		return name == "go.work" || strings.HasSuffix(name, ".go")
	})
}

// testFile reports whether a project-relative path is a test file (R10).
func (a *authorship) testFile(name string) bool {
	relative, ok := a.prefix.moduleRelative(name)
	return ok && langpack.GoTestFile(a.moduled, relative)
}

// changedTestFiles returns the test files whose state differs between the
// baseline and the current files, in path order (R11).
func (a *authorship) changedTestFiles() []string {
	changed := []string{}
	for _, name := range a.paths {
		before, inBefore := a.before[name]
		after, inAfter := a.after[name]
		if (inBefore != inAfter || before != after) && a.testFile(name) {
			changed = append(changed, name)
		}
	}
	return changed
}

// write is one changed-file entry of a worker result, with the position of
// the result's assignment.
type write struct {
	assignment int
	result     *BoundWorkerResult
	entry      WorkerChangedFile
}

// writes returns every write of name, in record order.
func (a *authorship) writes(name string) []write {
	found := []write{}
	for i := range a.record.WorkerResults {
		bound := &a.record.WorkerResults[i]
		index, ok := a.index[bound.Result.AssignmentID]
		if !ok {
			continue
		}
		for _, changed := range bound.Result.ChangedFiles {
			if changed.Path == name {
				found = append(found, write{assignment: index, result: bound, entry: changed})
			}
		}
	}
	return found
}

// independentWrite reports whether w is in a completed result of an
// independent assignment whose latest acceptance is accepted or unused.
func (a *authorship) independentWrite(w write) bool {
	if w.result.Result.Status != "completed" || !a.independent[w.assignment] {
		return false
	}
	latest := latestAcceptance(a.record, a.record.Assignments[w.assignment].Brief.ID)
	return latest != nil && (latest.Acceptance.Status == "accepted" || latest.Acceptance.Status == "unused")
}

// citation is one resolved finding disposition that cites a path.
type citation struct {
	assignment int
	finding    string
	sha256     string // the SHA-256 of the disposition's first reference of the path
	checked    bool   // the acceptance bound at least one check run
}

// citations returns the citations of name, in assignment order, then finding
// order.
func (a *authorship) citations(name string) []citation {
	found := []citation{}
	for i, assigned := range a.record.Assignments {
		if !slices.Contains([]string{"review", "evidence", "verification"}, assigned.Brief.Role) || a.everRejected[i] {
			continue
		}
		latest := latestAcceptance(a.record, assigned.Brief.ID)
		if latest == nil || latest.Acceptance.Status != "accepted" && latest.Acceptance.Status != "unused" {
			continue
		}
		for _, disposition := range latest.Acceptance.Findings {
			if disposition.Status != "resolved" {
				continue
			}
			if at := slices.IndexFunc(disposition.Evidence, func(ref SourceRef) bool { return ref.Path == name }); at >= 0 {
				found = append(found, citation{assignment: i, finding: disposition.FindingID, sha256: disposition.Evidence[at].SHA256, checked: len(latest.CheckRuns) > 0})
			}
		}
	}
	return found
}

// classify returns the authorship status of a path at the current source (R12).
func (a *authorship) classify(name string) authorshipStatus {
	if reason, ok := a.unreadable(); ok {
		return authorshipStatus{status: "unowned", reason: reason, assignment: -1}
	}
	sum, present := a.after[name]
	recorded := func(id string) string {
		if present {
			return "worker assignment " + id + " recorded its current bytes"
		}
		return "worker assignment " + id + " recorded its deletion"
	}
	current := func(w write) bool {
		if !present {
			return w.entry.Deleted
		}
		return !w.entry.Deleted && w.entry.SHA256 == sum
	}
	writes := a.writes(name)
	id := func(index int) string { return a.record.Assignments[index].Brief.ID }
	last := func(match func(write) bool) int {
		found := -1
		for _, w := range writes {
			if match(w) {
				found = w.assignment
			}
		}
		return found
	}
	if index := last(func(w write) bool { return a.independentWrite(w) && current(w) }); index >= 0 {
		return authorshipStatus{status: "independent", reason: recorded(id(index)), assignment: index}
	}
	if present && goTestSourcePath(name) {
		if data, err := a.read(name); err == nil && digest(data) == sum {
			if hash, ok := gofmtHash(data); ok {
				if index := last(func(w write) bool {
					return a.independentWrite(w) && w.entry.GofmtSHA256 != "" && w.entry.GofmtSHA256 == hash
				}); index >= 0 {
					return authorshipStatus{status: "independent", reason: "its current bytes are gofmt-equivalent to what worker assignment " + id(index) + " recorded", assignment: index}
				}
			}
		}
	}
	if index := last(a.independentWrite); index >= 0 {
		return authorshipStatus{status: "lost", reason: "its current state is neither what worker assignment " + id(index) + ", its independent test engineer, recorded nor gofmt-equivalent to it", assignment: index}
	}
	citations := a.citations(name)
	_, inBaseline := a.before[name]
	if present {
		for _, cited := range citations {
			if cited.sha256 == sum && !inBaseline && cited.checked {
				return authorshipStatus{status: "source_aware", reason: "reproduction test for finding " + cited.finding + " of worker assignment " + id(cited.assignment), assignment: cited.assignment, finding: cited.finding}
			}
		}
	}
	if len(citations) > 0 {
		cited := citations[0]
		return authorshipStatus{status: "unowned", reason: "finding " + cited.finding + " of worker assignment " + id(cited.assignment) + " cites it as a reproduction test, but the citation does not hold: a reproduction test is a new file whose current bytes the citation names, in an acceptance that lists a check", assignment: cited.assignment, finding: cited.finding}
	}
	if index := last(current); index >= 0 {
		return authorshipStatus{status: "unowned", reason: recorded(id(index)) + ", but " + a.fault(index), assignment: index}
	}
	if present {
		return authorshipStatus{status: "unowned", reason: "no worker result recorded its current bytes", assignment: -1}
	}
	return authorshipStatus{status: "unowned", reason: "no worker result recorded its deletion", assignment: -1}
}

// fault returns the first reason the assignment at index makes no write
// independent (R12.5). One always applies to a write that is not independent.
func (a *authorship) fault(index int) string {
	assigned := a.record.Assignments[index]
	agent := assigned.Brief.AgentID
	result := findWorkerResult(a.record, assigned.Brief.ID)
	switch {
	case assigned.Brief.Contract == nil:
		return "it carries no contract"
	case result == nil || result.Result.Status != "completed":
		return "its handoff is not completed"
	case latestAcceptance(a.record, assigned.Brief.ID) == nil:
		return "it has no acceptance"
	case a.everRejected[index]:
		return "it has a rejected acceptance"
	case a.record.Plan.CoordinatorID != "" && agent == a.record.Plan.CoordinatorID:
		return "its agent " + agent + " is the coordinator"
	}
	kinds := []func(WorkerAssignment) bool{
		func(other WorkerAssignment) bool {
			return other.Brief.Role == "implementation" && other.Brief.Contract == nil
		},
		func(other WorkerAssignment) bool { return other.Brief.Role == "review" },
		func(other WorkerAssignment) bool { return other.Brief.Role == "verification" },
		func(other WorkerAssignment) bool {
			_, ok := a.exclusion(other)
			return ok && other.Brief.Contract != nil
		},
	}
	for _, kind := range kinds {
		for _, other := range a.record.Assignments {
			if other.Brief.AgentID == agent && kind(other) {
				text, _ := a.exclusion(other)
				return "its agent " + agent + " holds worker assignment " + other.Brief.ID + ", " + text
			}
		}
	}
	// Unreachable for a write that is not independent; kept total.
	return "it is not independent"
}

// authorshipReport builds the R19 report from one classifier.
func authorshipReport(a *authorship, record Record, snapshot projectctx.Snapshot) AuthorshipReport {
	report := AuthorshipReport{TaskID: record.ID, Fingerprint: snapshot.Fingerprint, TestRules: record.TestRules, Gated: gated(record), Files: []AuthorshipFile{}, Problems: []string{}}
	if problem, ok := a.unreadable(); ok {
		report.Problems = append(report.Problems, problem)
		return report
	}
	if a.noGoMod() {
		report.Problems = append(report.Problems, noGoModLimitation)
	}
	for _, name := range a.changedTestFiles() {
		judged := a.classify(name)
		_, present := a.after[name]
		file := AuthorshipFile{Path: name, Deleted: !present, Status: judged.status, FindingID: judged.finding, Reason: judged.reason}
		if judged.assignment >= 0 {
			brief := record.Assignments[judged.assignment].Brief
			file.AssignmentID, file.AgentID, file.Contract = brief.ID, brief.AgentID, brief.Contract
		}
		report.Files = append(report.Files, file)
	}
	return report
}

// authorshipVerifyEntries returns verify's R16, R15 and R14 problems, in that
// order, and R23's limitation, for a gated record.
func authorshipVerifyEntries(a *authorship, record Record) (problems, limitations []string) {
	if !gated(record) {
		return nil, nil
	}
	if problem, ok := a.unreadable(); ok {
		return []string{problem}, nil
	}
	if a.noGoMod() {
		limitations = append(limitations, noGoModLimitation)
	}
	changed := a.changedTestFiles()
	judged := make([]authorshipStatus, len(changed))
	sourceAwareOnly := true
	for i, name := range changed {
		judged[i] = a.classify(name)
		sourceAwareOnly = sourceAwareOnly && judged[i].status == "source_aware"
	}
	if !sourceAwareOnly && !planProvesTests(record.Plan) {
		problems = append(problems, noPlanCreditProblem)
	}
	for i, name := range changed {
		if judged[i].status == "lost" || judged[i].status == "unowned" {
			problems = append(problems, fmt.Sprintf("test file %s is %s: %s", name, judged[i].status, judged[i].reason))
		}
	}
	return problems, limitations
}
