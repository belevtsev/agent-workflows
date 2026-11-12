package taskflow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// ReadTestResults reads the per-test results run recorded in the task at
// taskPath, checking the stored file against its hash.
func ReadTestResults(taskPath string, run CheckRun) (langpack.GoTestResults, error) {
	root, _, err := openTask(taskPath)
	if err != nil {
		return langpack.GoTestResults{}, err
	}
	defer root.Close()
	return readTestResults(root, run)
}

// readTestResults reads run's stored results file from the task directory.
func readTestResults(root *os.Root, run CheckRun) (langpack.GoTestResults, error) {
	if run.TestResults == "" {
		return langpack.GoTestResults{}, errors.New("check run recorded no test results")
	}
	data, err := privateRead(root, run.TestResults, MaxInputBytes)
	if err != nil {
		return langpack.GoTestResults{}, err
	}
	if digest(data) != run.TestResultsSHA256 {
		return langpack.GoTestResults{}, errors.New("stored test results hash differs")
	}
	var results langpack.GoTestResults
	if err := decodeBytes(data, &results); err != nil {
		return langpack.GoTestResults{}, err
	}
	return results, nil
}

// testResultsName is the results file of the check run with this ID.
func testResultsName(runID string) string { return "check-" + runID + ".tests.json" }

// writeTestResults stores results as a new private file in the task at path
// and returns its name and SHA-256.
func writeTestResults(path, runID string, results langpack.GoTestResults) (string, string, error) {
	data, err := encode(results)
	if err != nil {
		return "", "", err
	}
	root, _, err := openTask(path)
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	name := testResultsName(runID)
	if err := writeNew(root, name, data); err != nil {
		return "", "", err
	}
	return name, digest(data), nil
}

// Trace reports each plan requirement with the contract requirements linked to
// it, their mapped tests and the check runs that recorded them, judged at the
// current source. It changes nothing.
func Trace(ctx context.Context, taskPath string) (TraceReport, error) {
	root, directory, err := openTask(taskPath)
	if err != nil {
		return TraceReport{}, err
	}
	defer root.Close()
	record, _, err := loadRoot(root, directory)
	if err != nil {
		return TraceReport{}, err
	}
	snapshot, files, err := projectctx.CaptureManifest(ctx, record.Baseline.SelectedPath)
	if err != nil {
		return TraceReport{}, err
	}
	view := newSourceView(snapshot, files)
	report := judgeTrace(root, record, view).report
	view.close()
	after, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
	if err != nil {
		return TraceReport{}, err
	}
	if !equalSnapshot(snapshot, after) {
		report.Problems = append(report.Problems, "source changed during trace; run it again")
	}
	return report, nil
}

type TraceReport struct {
	TaskID      string           `json:"task_id"`
	Fingerprint string           `json:"fingerprint"`
	Criteria    []TraceCriterion `json:"criteria"`
	Problems    []string         `json:"problems"`
}

type TraceCriterion struct {
	ID           string             `json:"id"`
	Description  string             `json:"description"`
	Status       string             `json:"status"` // proven, open, lost or unmapped
	Requirements []TraceRequirement `json:"requirements"`
}

type TraceRequirement struct {
	AssignmentID string      `json:"assignment_id"`
	Requirement  string      `json:"requirement"`
	Status       string      `json:"status"` // proven, unproven, uncovered, pending or dropped
	Uncovered    string      `json:"uncovered,omitempty"`
	Dropped      string      `json:"dropped,omitempty"`
	Tests        []TraceTest `json:"tests"`
}

type TraceTest struct {
	Path    string     `json:"path"`
	Name    string     `json:"name"`
	Package string     `json:"package"`
	Status  string     `json:"status"` // passed, failed, skipped, missing, not_run, pass_unproven or not_independent
	Detail  string     `json:"detail,omitempty"`
	Runs    []TraceRun `json:"runs"`
}

type TraceRun struct {
	RunID       string `json:"run_id"`
	CheckID     string `json:"check_id"`
	Status      string `json:"status"`
	Fingerprint string `json:"fingerprint"`
	Current     bool   `json:"current"`
	Credited    bool   `json:"credited"`
	Pass        int    `json:"pass"`
	Fail        int    `json:"fail"`
	Skip        int    `json:"skip"`
	Run         int    `json:"run,omitzero"`
	Marked      int    `json:"marked,omitzero"`
}

// testSource resolves mapped tests at one source: the project root, the
// selected path's prefix in it, its go.mod and the current files inside it.
type testSource struct {
	root    *os.Root // nil when the project root cannot be opened
	prefix  string   // "" when the selected path is not inside the root
	gomod   []byte
	module  bool // the selected path holds a regular go.mod
	current []string
	// read caches each project-relative file read through root, so a file is
	// read at most once per source.
	read map[string]readFile
}

// readFile is one cached read of a regular file.
type readFile struct {
	data []byte
	err  error
}

// selectedPrefix returns the selected path relative to the project root,
// slash-separated, when it is inside the root.
func selectedPrefix(snapshot projectctx.Snapshot) (string, bool) {
	prefix, err := filepath.Rel(snapshot.Root, snapshot.SelectedPath)
	if err != nil || prefix == ".." || strings.HasPrefix(prefix, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(prefix), true
}

func newTestSource(root *os.Root, snapshot projectctx.Snapshot, files []projectctx.FileState) testSource {
	source := testSource{root: root, current: []string{}, read: map[string]readFile{}}
	if prefix, ok := selectedPrefix(snapshot); ok {
		source.prefix = prefix
	}
	if source.prefix == "" {
		return source
	}
	for _, file := range files {
		if name, ok := source.moduleRelative(projectctx.ManifestRawPath(file.Path)); ok {
			source.current = append(source.current, name)
		}
	}
	if root != nil {
		name := "go.mod"
		if source.prefix != "." {
			name = source.prefix + "/go.mod"
		}
		if data, err := source.readFile(name); err == nil {
			source.gomod, source.module = data, true
		}
	}
	return source
}

// readFile reads a project-relative regular file through the source's root,
// refusing symlinks, at most once per source.
func (source testSource) readFile(name string) ([]byte, error) {
	if source.root == nil {
		return nil, errors.New("project root cannot be opened")
	}
	if cached, ok := source.read[name]; ok {
		return cached.data, cached.err
	}
	data, err := readWorkerFile(source.root, name)
	if source.read != nil {
		source.read[name] = readFile{data: data, err: err}
	}
	return data, err
}

// moduleRelative returns a project-relative path relative to the selected
// path, when it is inside it.
func (source testSource) moduleRelative(name string) (string, bool) {
	if source.prefix == "." {
		return name, true
	}
	if source.prefix == "" {
		return "", false
	}
	return strings.CutPrefix(name, source.prefix+"/")
}

// resolve returns a mapped test's package ("" when unresolved) and, when the
// test is missing at the source, the first applicable reason.
func (source testSource) resolve(test MappedTest) (string, string) {
	pkg, ok := "", false
	if name, inside := source.moduleRelative(test.Path); inside && source.module {
		pkg, ok = langpack.GoTestPackage(source.gomod, source.current, name)
	}
	if !ok {
		return "", fmt.Sprintf("%s is not in a package that go test ./... runs at the selected path", test.Path)
	}
	if source.root == nil {
		return pkg, fmt.Sprintf("%s is absent or not a regular file", test.Path)
	}
	data, err := source.readFile(test.Path)
	if err != nil {
		return pkg, fmt.Sprintf("%s is absent or not a regular file", test.Path)
	}
	if !langpack.GoTestDeclared(data, test.Name) {
		top, _, _ := strings.Cut(test.Name, "/")
		return pkg, fmt.Sprintf("%s declares no func %s", test.Path, top)
	}
	return pkg, ""
}

func mapBearing(assigned WorkerAssignment) bool { return len(assigned.Brief.Requirements) > 0 }

// covers reports whether every owned path of a is equal to or inside an owned
// path of b.
func covers(b, a WorkerAssignment) bool {
	return !slices.ContainsFunc(a.Brief.OwnedPaths, func(path string) bool {
		return !slices.ContainsFunc(b.Brief.OwnedPaths, func(scope string) bool { return scopeContains(scope, path) })
	})
}

// overlaps reports whether an owned path of one is equal to or inside an
// owned path of the other.
func overlaps(a, b WorkerAssignment) bool {
	return slices.ContainsFunc(a.Brief.OwnedPaths, func(x string) bool {
		return slices.ContainsFunc(b.Brief.OwnedPaths, func(y string) bool { return scopeContains(x, y) || scopeContains(y, x) })
	})
}

// mapCurrency decides, from the last assignment back to the first, which
// map-bearing assignments are current, and the state of each closed one: its
// latest acceptance when that is rejected or unused, or else the first later
// current map-bearing assignment that covers it. The result is indexed like
// record.Assignments; an assignment that bears no map is neither current nor
// closed.
func mapCurrency(record Record) (current []bool, closed []string) {
	current, closed = make([]bool, len(record.Assignments)), make([]string, len(record.Assignments))
	for i := len(record.Assignments) - 1; i >= 0; i-- {
		assigned := record.Assignments[i]
		if !mapBearing(assigned) {
			continue
		}
		if accepted := latestAcceptance(record, assigned.Brief.ID); accepted != nil && accepted.Acceptance.Status == "rejected" {
			closed[i] = "rejected"
			continue
		} else if accepted != nil && accepted.Acceptance.Status == "unused" {
			closed[i] = "closed unused"
			continue
		}
		for j := i + 1; j < len(record.Assignments); j++ {
			if current[j] && covers(record.Assignments[j], assigned) {
				closed[i] = "replaced by worker assignment " + record.Assignments[j].Brief.ID
				break
			}
		}
		current[i] = closed[i] == ""
	}
	return current, closed
}

// storedResults is one terminal run's readable results, by package and test.
type storedResults struct {
	truncated bool
	tests     map[[2]string]langpack.GoTestResult
}

// judgedRun is the latest run of a planned or added check, taken in plan then
// amendment order, that
// ran at the current source.
type judgedRun struct {
	run      *CheckRun
	passed   bool
	credited bool
	results  *storedResults
}

// traceJudgement is one judgement of mapped tests at the current source: the
// trace report and the entries verify adds.
type traceJudgement struct {
	report         TraceReport
	judged         int      // tests judged (R27)
	unreadable     []string // R32
	testProblems   []string // R30
	lostProblems   []string // R42
	uncoveredNotes []string // R31
	unmappedNotes  []string // R41
}

// judgedEntry is one map entry of a current assignment, with its tests judged.
type judgedEntry struct {
	uncovered string
	tests     []TraceTest
}

// judgeTrace judges the mapped tests of every current map-bearing assignment
// with a completed result, and the coverage closed ones lost, from the record,
// the task directory and the current source. A PASS counts only when the run
// proves it (R7, R8).
func judgeTrace(taskRoot *os.Root, record Record, view *sourceView) traceJudgement {
	snapshot := view.snapshot
	judgement := traceJudgement{report: TraceReport{TaskID: record.ID, Fingerprint: snapshot.Fingerprint, Criteria: []TraceCriterion{}, Problems: []string{}}}
	loaded := map[string]*storedResults{}
	for i := range record.Checks {
		run := &record.Checks[i]
		if run.Status == "running" || run.TestResults == "" {
			continue
		}
		results, err := readTestResults(taskRoot, *run)
		if err != nil {
			problem := fmt.Sprintf("check run %s test results: stored file cannot be read or differs from its sha256", run.ID)
			judgement.unreadable = append(judgement.unreadable, problem)
			judgement.report.Problems = append(judgement.report.Problems, problem)
			continue
		}
		stored := &storedResults{truncated: results.Truncated, tests: map[[2]string]langpack.GoTestResult{}}
		for _, test := range results.Tests {
			stored.tests[[2]string{test.Package, test.Test}] = test
		}
		loaded[run.ID] = stored
	}
	currentRuns := []judgedRun{}
	isCurrent := map[string]bool{}
	for _, check := range allChecks(record) {
		run := latestRun(record, check.ID)
		if run == nil || run.Status == "running" || run.After == nil || !equalSnapshot(run.Before, snapshot) || !equalSnapshot(*run.After, snapshot) {
			continue
		}
		isCurrent[run.ID] = true
		currentRuns = append(currentRuns, judgedRun{run: run, passed: passedAt(*run, snapshot), credited: langpack.GoTestsCredited(evidenceRun(*run)), results: loaded[run.ID]})
	}
	entry := func(results *storedResults, pkg, name string) langpack.GoTestResult {
		found, _ := results.lookup(pkg, name)
		return found
	}
	described := func(run *CheckRun, text string) string {
		return fmt.Sprintf("run %s of check %s %s", run.ID, run.CheckID, text)
	}
	// declarations counts, per mapped test, the packages of the test's
	// directory that declare it (R7.a); each directory's sources are read once.
	sources, counted := map[string][][]byte{}, map[MappedTest]int{}
	declarations := func(test MappedTest) int {
		if count, ok := counted[test]; ok {
			return count
		}
		dir := path.Dir(test.Path)
		read, ok := sources[dir]
		if !ok {
			read = [][]byte{}
			for _, file := range view.files {
				name := projectctx.ManifestRawPath(file.Path)
				base := path.Base(name)
				if path.Dir(name) != dir || !strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
					continue
				}
				if data, err := view.testSource().readFile(name); err == nil {
					read = append(read, data)
				}
			}
			sources[dir] = read
		}
		counted[test] = max(langpack.GoTestDeclaringPackages(read, test.Name), 1)
		return counted[test]
	}
	limit := func(run *CheckRun, test MappedTest) int {
		return saturatingProduct(langpack.GoTestRunLimit(evidenceRun(*run)), declarations(test))
	}
	proves := func(run *CheckRun, found langpack.GoTestResult, test MappedTest) bool {
		return found.Pass >= 1 && found.Run >= found.Pass+found.Fail+found.Skip && found.Run <= limit(run, test) && found.Marked == 0
	}
	judge := func(test MappedTest) TraceTest {
		source := view.testSource()
		pkg, missing := source.resolve(test)
		judged := TraceTest{Path: test.Path, Name: test.Name, Package: pkg, Runs: []TraceRun{}}
		if pkg != "" {
			for i := range record.Checks {
				run := &record.Checks[i]
				found, ok := loaded[run.ID].lookup(pkg, test.Name)
				if !ok {
					continue
				}
				fingerprint := ""
				if run.After != nil {
					fingerprint = run.After.Fingerprint
				}
				judged.Runs = append(judged.Runs, TraceRun{RunID: run.ID, CheckID: run.CheckID, Status: run.Status, Fingerprint: fingerprint, Current: isCurrent[run.ID], Credited: langpack.GoTestsCredited(evidenceRun(*run)), Pass: found.Pass, Fail: found.Fail, Skip: found.Skip, Run: found.Run, Marked: found.Marked})
			}
		}
		if missing != "" {
			judged.Status, judged.Detail = "missing", missing
			return judged
		}
		if gated(record) {
			if authored := view.authorship(taskRoot, record).classify(test.Path); authored.status != "independent" {
				judged.Status, judged.Detail = "not_independent", fmt.Sprintf("%s is %s: %s", test.Path, authored.status, authored.reason)
				return judged
			}
		}
		for _, current := range currentRuns {
			if entry(current.results, pkg, test.Name).Fail > 0 {
				judged.Status, judged.Detail = "failed", described(current.run, "recorded a FAIL")
				return judged
			}
		}
		for _, current := range currentRuns {
			if found := entry(current.results, pkg, test.Name); current.passed && current.credited && found.Pass > 0 && proves(current.run, found, test) {
				judged.Status = "passed"
				return judged
			}
		}
		for _, current := range currentRuns {
			found := entry(current.results, pkg, test.Name)
			if !current.passed || !current.credited || found.Pass < 1 {
				continue
			}
			judged.Status = "pass_unproven"
			switch {
			case found.Marked >= 1:
				judged.Detail = described(current.run, "recorded an output line of it beginning with byte 0x16")
			case found.Run > limit(current.run, test):
				judged.Detail = described(current.run, fmt.Sprintf("recorded %d run events for it, more than its -count, -cpu and declarations allow (%d)", found.Run, limit(current.run, test)))
			case current.results.truncated:
				judged.Detail = described(current.run, "recorded a PASS, but not a run event for each of its results, and stopped recording at its bound")
			default:
				judged.Detail = described(current.run, "recorded a PASS, but not a run event for each of its results")
			}
			return judged
		}
		for _, current := range currentRuns {
			if current.passed && current.credited && entry(current.results, pkg, test.Name).Skip > 0 {
				judged.Status, judged.Detail = "skipped", described(current.run, "recorded only SKIP")
				return judged
			}
		}
		judged.Status = "not_run"
		for _, current := range currentRuns {
			if current.passed && !current.credited && entry(current.results, pkg, test.Name).Pass > 0 {
				switch withheld := evidenceRun(*current.run).Withheld; {
				case !langpack.GoTestsCredited(langpack.Run{Argv: current.run.Argv}):
					judged.Detail = described(current.run, "recorded a PASS, but its argv earns no per-test credit")
				case withheld != "":
					judged.Detail = described(current.run, "recorded a PASS, but earns no per-test credit: "+withheld)
				default:
					judged.Detail = described(current.run, "recorded a PASS, but the GOFLAGS it ran with earn no per-test credit")
				}
				return judged
			}
		}
		for i := len(record.Checks) - 1; i >= 0; i-- {
			if found, ok := loaded[record.Checks[i].ID].lookup(pkg, test.Name); ok && found.Pass > 0 {
				judged.Detail = described(&record.Checks[i], "recorded a PASS, but that run is not a passed latest run at the current source")
				return judged
			}
		}
		for _, current := range currentRuns {
			if current.passed && current.credited && current.results != nil && current.results.truncated {
				judged.Detail = described(current.run, "stopped recording tests at its bound")
				return judged
			}
		}
		if !slices.ContainsFunc(currentRuns, func(current judgedRun) bool { return current.passed && current.credited && current.results != nil }) {
			judged.Detail = "no current passed go test run with -json in its argv recorded per-test results; run a go test check that has -json in its argv"
		}
		return judged
	}
	phrases := map[string]string{"missing": "is missing at the current source", "failed": "failed at the current source", "skipped": "was skipped at the current source", "not_run": "has no PASS at the current source", "pass_unproven": "has no proven PASS at the current source", "not_independent": "is not independently authored"} //nolint:gosec // G101: report phrases, not credentials.

	current, closed := mapCurrency(record)
	// Judge each current map-bearing assignment's completed map once, in
	// assignment record order, then map entry order, then test order.
	judged := make([]map[string]judgedEntry, len(record.Assignments))
	for i, assigned := range record.Assignments {
		if !current[i] {
			continue
		}
		result := findWorkerResult(record, assigned.Brief.ID)
		if result == nil || result.Result.Status != "completed" {
			continue
		}
		judged[i] = map[string]judgedEntry{}
		for _, mapping := range result.Result.RequirementMap {
			value := judgedEntry{uncovered: mapping.Uncovered, tests: []TraceTest{}}
			if mapping.Uncovered != "" {
				judgement.uncoveredNotes = append(judgement.uncoveredNotes, fmt.Sprintf("worker assignment %s requirement %s uncovered: %s", assigned.Brief.ID, mapping.Requirement, mapping.Uncovered))
			}
			for _, test := range mapping.Tests {
				traced := judge(test)
				judgement.judged++
				if traced.Status != "passed" {
					problem := fmt.Sprintf("worker assignment %s requirement %s test %s %s %s", assigned.Brief.ID, mapping.Requirement, test.Path, test.Name, phrases[traced.Status])
					if traced.Detail != "" {
						problem += ": " + traced.Detail
					}
					judgement.testProblems = append(judgement.testProblems, problem)
				}
				value.tests = append(value.tests, traced)
			}
			judged[i][mapping.Requirement] = value
		}
	}
	// declaredByCurrent reports whether a current map-bearing assignment that
	// overlaps the one at index declares requirement, with criterion among its
	// criteria when criterion is not empty.
	declaredByCurrent := func(index int, requirement, criterion string) bool {
		for j, other := range record.Assignments {
			if !current[j] || !overlaps(record.Assignments[index], other) {
				continue
			}
			if slices.ContainsFunc(other.Brief.Requirements, func(declared ContractRequirement) bool {
				return declared.Requirement == requirement && (criterion == "" || slices.Contains(declared.Criteria, criterion))
			}) {
				return true
			}
		}
		return false
	}
	for i, assigned := range record.Assignments {
		if closed[i] == "" {
			continue
		}
		for _, requirement := range assigned.Brief.Requirements {
			if !declaredByCurrent(i, requirement.Requirement, "") {
				judgement.unmappedNotes = append(judgement.unmappedNotes, fmt.Sprintf("requirement %s is no longer mapped: worker assignment %s declared it and is %s", requirement.Requirement, assigned.Brief.ID, closed[i]))
			}
		}
	}
	links := func(i int, criterion string) bool {
		return slices.ContainsFunc(record.Assignments[i].Brief.Requirements, func(declared ContractRequirement) bool { return slices.Contains(declared.Criteria, criterion) })
	}
	for _, criterion := range record.Plan.Requirements {
		traced := TraceCriterion{ID: criterion.ID, Description: criterion.Description, Requirements: []TraceRequirement{}}
		live, proven, dropped := 0, 0, false
		for i, assigned := range record.Assignments {
			if !current[i] && closed[i] == "" {
				continue
			}
			for _, requirement := range assigned.Brief.Requirements {
				if !slices.Contains(requirement.Criteria, criterion.ID) {
					continue
				}
				value := TraceRequirement{AssignmentID: assigned.Brief.ID, Requirement: requirement.Requirement, Tests: []TraceTest{}}
				switch {
				case closed[i] != "":
					if declaredByCurrent(i, requirement.Requirement, criterion.ID) {
						continue
					}
					value.Status, value.Dropped, dropped = "dropped", closed[i], true
				case judged[i] == nil:
					value.Status = "pending"
				default:
					mapped := judged[i][requirement.Requirement]
					switch {
					case mapped.uncovered != "":
						value.Status, value.Uncovered = "uncovered", mapped.uncovered
					case !slices.ContainsFunc(mapped.tests, func(test TraceTest) bool { return test.Status != "passed" }):
						value.Status, value.Tests = "proven", mapped.tests
					default:
						value.Status, value.Tests = "unproven", mapped.tests
					}
				}
				if value.Status != "dropped" {
					live++
					if value.Status == "proven" {
						proven++
					}
				}
				traced.Requirements = append(traced.Requirements, value)
			}
		}
		switch {
		case live > 0 && proven == live:
			traced.Status = "proven"
		case live > 0:
			traced.Status = "open"
		case dropped:
			traced.Status = "lost"
		default:
			traced.Status = "unmapped"
		}
		judgement.report.Criteria = append(judgement.report.Criteria, traced)
		linkedByClosed, linkedByCurrent := false, false
		for i := range record.Assignments {
			linkedByClosed = linkedByClosed || closed[i] != "" && links(i, criterion.ID)
			linkedByCurrent = linkedByCurrent || current[i] && links(i, criterion.ID)
		}
		if linkedByClosed && !linkedByCurrent {
			judgement.lostProblems = append(judgement.lostProblems, fmt.Sprintf("plan requirement %s lost its mapped tests: every test-engineer assignment that linked it is closed unused, rejected or replaced", criterion.ID))
		}
	}
	return judgement
}

// saturatingProduct returns a times b for non-negative a and b, saturating at
// the largest int.
func saturatingProduct(a, b int) int {
	if a != 0 && b > math.MaxInt/a {
		return math.MaxInt
	}
	return a * b
}

// lookup returns the test's entry in results; a nil results holds none.
func (results *storedResults) lookup(pkg, name string) (langpack.GoTestResult, bool) {
	if results == nil {
		return langpack.GoTestResult{}, false
	}
	found, ok := results.tests[[2]string{pkg, name}]
	return found, ok
}

// traceVerifyEntries turns the judgement of mapped tests into verify's
// problems (R6, R32, R30, R42, in that order) and limitations (R31, R41).
func traceVerifyEntries(root *os.Root, record Record, view *sourceView) (problems, limitations []string) {
	problems = inputCopyProblems(root, record)
	if !slices.ContainsFunc(record.Assignments, mapBearing) {
		return problems, nil
	}
	judgement := judgeTrace(root, record, view)
	if judgement.judged > 0 {
		problems = append(problems, judgement.unreadable...)
	}
	problems = append(problems, judgement.testProblems...)
	problems = append(problems, judgement.lostProblems...)
	limitations = append(judgement.uncoveredNotes, judgement.unmappedNotes...)
	return problems, limitations
}
