package main_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Independent end-to-end contract tests for executed evidence and finding
// severity, driven through the real cw binary:
//
//   - start refuses a substantial plan with no requirement bound to a check;
//     small plans are unaffected;
//   - verify on a changed substantial task needs a passed run at the current
//     source and, in a Go module, a passed covering `go test` run for each
//     changed non-test package; failed runs, `go vet` and runs at an older
//     source do not count;
//   - review handoffs refuse a finding without a valid severity; a rejected
//     blocker is a limitation and a problem until a later accepted
//     independent review that does not re-raise it; `verify --summary` shows
//     rejected findings first under rejected_findings.

type evidenceE2E struct {
	t      *testing.T
	binary string
	env    []string
	repo   string
	store  string
	policy string
	task   string
	files  string
}

func evidenceGoEnv(t *testing.T, name string) string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), goTool(t), "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(output))
}

func newEvidenceE2E(t *testing.T) *evidenceE2E {
	t.Helper()
	if testing.Short() {
		t.Skip("builds cmd/cw and runs go test in a temporary module")
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	home := hooksCanonicalTempDir(t)
	e := &evidenceE2E{
		t:      t,
		binary: buildPlainCW(t),
		repo:   hooksCanonicalTempDir(t),
		store:  filepath.Join(hooksCanonicalTempDir(t), "store"),
		policy: filepath.Join(root, "skills", "task-orchestration", "model-policy.yaml"),
		files:  hooksCanonicalTempDir(t),
	}
	e.env = []string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(goTool(t)) + ":/usr/bin:/bin",
		"GOCACHE=" + evidenceGoEnv(t, "GOCACHE"),
		"GOPATH=" + filepath.Join(home, "go"),
		"GOTOOLCHAIN=local",
		"GOWORK=off",
		"GOFLAGS=",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"HTTPS_PROXY=http://127.0.0.1:9",
		"HTTP_PROXY=http://127.0.0.1:9",
	}
	return e
}

// cw runs the binary and returns exit code, stdout and stderr.
func (e *evidenceE2E) cw(arguments ...string) (int, string, string) {
	e.t.Helper()
	return e.cwEnv(nil, arguments...)
}

// cwEnv runs the binary with extra environment entries, which override the
// fixture's.
func (e *evidenceE2E) cwEnv(extra []string, arguments ...string) (int, string, string) {
	e.t.Helper()
	command := exec.CommandContext(e.t.Context(), e.binary, arguments...)
	command.Dir = e.files
	command.Env = append(slices.Clone(e.env), extra...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			e.t.Fatalf("cw %v did not run: %v", arguments, err)
		}
		code = exit.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

func (e *evidenceE2E) mustCW(arguments ...string) string {
	e.t.Helper()
	code, stdout, stderr := e.cw(arguments...)
	if code != 0 {
		e.t.Fatalf("cw %v: exit %d\nstdout %s\nstderr %s", arguments, code, stdout, stderr)
	}
	return stdout
}

func (e *evidenceE2E) git(arguments ...string) {
	e.t.Helper()
	base := []string{"-C", e.repo, "-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}
	command := exec.CommandContext(e.t.Context(), "git", append(base, arguments...)...)
	command.Env = e.env
	if output, err := command.CombinedOutput(); err != nil {
		e.t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func (e *evidenceE2E) write(path, data string) {
	e.t.Helper()
	full := filepath.Join(e.repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// file writes a private input file outside the project and returns its path.
func (e *evidenceE2E) file(name string, value any) string {
	e.t.Helper()
	var data []byte
	switch value := value.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		var err error
		if data, err = json.Marshal(value); err != nil {
			e.t.Fatal(err)
		}
	}
	path := filepath.Join(e.files, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		e.t.Fatal(err)
	}
	return path
}

// start starts a task with plan in store and returns the task directory, or
// the failing exit code and stderr when the plan is refused.
func (e *evidenceE2E) start(store, name, plan string) (string, int, string) {
	e.t.Helper()
	code, stdout, stderr := e.cw("task", "start", "--repo", e.repo, "--plan", e.file(name, plan), "--policy", e.policy, "--summary", "--store", store)
	if code != 0 {
		return "", code, stderr + stdout
	}
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		e.t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	return filepath.Join(store, entries[0].Name()), 0, ""
}

func (e *evidenceE2E) verify() taskflow.Verification {
	e.t.Helper()
	_, stdout, stderr := e.cw("task", "verify", "--full", "--task", e.task)
	var verification taskflow.Verification
	if err := json.Unmarshal([]byte(stdout), &verification); err != nil {
		e.t.Fatalf("verify output is not a verification: %v\nstdout %s\nstderr %s", err, stdout, stderr)
	}
	return verification
}

func (e *evidenceE2E) run(check string, extraEnv ...string) {
	e.t.Helper()
	// A failed check exits non-zero; the verification decides what counts.
	// Checks inherit cw's environment, so extraEnv reaches the check.
	e.cwEnv(extraEnv, "task", "run", "--task", e.task, "--check", check, "--summary")
}

// runExpect runs check and requires cw to report it passed (exit 0) or not.
func (e *evidenceE2E) runExpect(passed bool, check string, extraEnv ...string) {
	e.t.Helper()
	code, stdout, stderr := e.cwEnv(extraEnv, "task", "run", "--task", e.task, "--check", check, "--summary")
	if (code == 0) != passed {
		e.t.Fatalf("check %s with %q: exit %d, want passed=%v\nstdout %s\nstderr %s", check, extraEnv, code, passed, stdout, stderr)
	}
}

const evidenceModuleGo = "module example.com/evidence\n\ngo 1.21\n"

const (
	alphaSource = "package alpha\n\nfunc Name() string { return \"alpha\" }\n"
	alphaTest   = "package alpha\n\nimport \"testing\"\n\nfunc TestName(t *testing.T) {\n\tif Name() != \"alpha\" {\n\t\tt.Fatal(Name())\n\t}\n}\n"
	betaSource  = "package beta\n\nfunc Value() int { return 1 }\n"
	// betaTest fails when BETA_FAIL is set, so the same source can give a
	// passed and then a failed run of the same check.
	betaTest = "package beta\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestValue(t *testing.T) {\n\tif Value() < 1 || os.Getenv(\"BETA_FAIL\") != \"\" {\n\t\tt.Fatal(Value())\n\t}\n}\n"
)

const evidencePlan = `{"title":"Executed evidence","kind":"substantial",
 "checkpoints":{"required":false,"reason":"contract test fixture"},
 "requirements":[{"id":"behaviour","description":"Packages keep their behaviour","check_ids":["alpha","beta","vet","all"],"allow_non_execution":false}],
 "checks":[
  {"id":"alpha","argv":["go","test","-count=1","./alpha"],"timeout_seconds":300},
  {"id":"beta","argv":["go","test","-count=1","./beta"],"timeout_seconds":300},
  {"id":"vet","argv":["go","vet","./..."],"timeout_seconds":300},
  {"id":"all","argv":["go","test","-count=1","./..."],"timeout_seconds":300}]}`

func (e *evidenceE2E) setUpModule() {
	e.t.Helper()
	e.write("go.mod", evidenceModuleGo)
	e.write("alpha/alpha.go", alphaSource)
	e.write("alpha/alpha_test.go", alphaTest)
	e.write("beta/beta.go", betaSource)
	e.write("beta/beta_test.go", betaTest)
	e.git("init", "-q")
	e.git("add", ".")
	e.git("commit", "-qm", "baseline")
}

// anyRunProblem and packageProblem are the exact problems the language rules
// report, taken from the exported rules so the test does not guess wording.
func anyRunProblem(t *testing.T) string {
	t.Helper()
	problems := (langpack.AnyPassedRun{}).Problems(langpack.Source{Current: []string{"a"}, Changed: []string{"a"}}, nil)
	if len(problems) != 1 {
		t.Fatalf("AnyPassedRun problems %q, want one", problems)
	}
	return problems[0]
}

func packageProblem(t *testing.T, pkg string) string {
	t.Helper()
	source := langpack.Source{Current: []string{"go.mod", pkg + "/x.go"}, Changed: []string{pkg + "/x.go"}}
	problems := langpack.GoEvidence(source, nil, langpack.GoFacts{NoRace: "not needed"}).Problems
	if len(problems) != 1 || !strings.Contains(problems[0], pkg) || !strings.Contains(problems[0], "has no passed go test run at the current source") {
		t.Fatalf("Go rule problems %q, want one naming %s", problems, pkg)
	}
	return problems[0]
}

func TestEvidenceE2EStartRequiresBoundCheck(t *testing.T) {
	e := newEvidenceE2E(t)
	e.setUpModule()
	unbound := `{"title":"Unbound","kind":"%s",%s
	 "requirements":[{"id":"r","description":"Inspected by reading","check_ids":[],"allow_non_execution":true}],
	 "checks":[{"id":"unit","argv":["go","test","./..."],"timeout_seconds":60}]}`
	noChecks := `{"title":"No checks","kind":"%s",%s
	 "requirements":[{"id":"r","description":"Inspected by reading","check_ids":[],"allow_non_execution":true}],
	 "checks":[]}`
	checkpoints := `"checkpoints":{"required":false,"reason":"contract test fixture"},`
	for _, tc := range []struct {
		name    string
		plan    string
		refused bool
	}{
		{"substantial, check defined but unbound", strings.Replace(strings.Replace(unbound, "%s", "substantial", 1), "%s", checkpoints, 1), true},
		{"substantial, no checks", strings.Replace(strings.Replace(noChecks, "%s", "substantial", 1), "%s", checkpoints, 1), true},
		{"substantial, required checkpoints, unbound", strings.Replace(strings.Replace(unbound, "%s", "substantial", 1), "%s", "", 1), true},
		{"small, check defined but unbound", strings.Replace(strings.Replace(unbound, "%s", "small", 1), "%s", "", 1), false},
		{"small, no checks", strings.Replace(strings.Replace(noChecks, "%s", "small", 1), "%s", "", 1), false},
		{"substantial, one of two requirements bound", `{"title":"Bound","kind":"substantial",` + checkpoints + `
		 "requirements":[{"id":"r","description":"Inspected by reading","check_ids":[],"allow_non_execution":true},
		  {"id":"s","description":"Tested","check_ids":["unit"],"allow_non_execution":false}],
		 "checks":[{"id":"unit","argv":["go","test","./..."],"timeout_seconds":60}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := filepath.Join(hooksCanonicalTempDir(t), "store")
			task, code, output := e.start(store, "plan.json", tc.plan)
			if tc.refused {
				if code == 0 {
					t.Fatalf("start accepted the plan as task %s", task)
				}
				if !strings.Contains(output, "check") {
					t.Errorf("refusal %q does not mention the missing check", output)
				}
				if entries, _ := os.ReadDir(store); len(entries) != 0 {
					t.Errorf("refused start left %d entries in the store", len(entries))
				}
				return
			}
			if code != 0 {
				t.Fatalf("start refused the plan: exit %d: %s", code, output)
			}
		})
	}
}

func TestEvidenceE2EGoPackagesNeedCoveringPassedRuns(t *testing.T) {
	e := newEvidenceE2E(t)
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", evidencePlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	anyRun := anyRunProblem(t)
	beta := packageProblem(t, "beta")
	alpha := packageProblem(t, "alpha")

	expect := func(step string, want, unwanted []string) taskflow.Verification {
		t.Helper()
		verification := e.verify()
		if verification.Complete {
			t.Fatalf("%s: a task without review or assessment verified complete", step)
		}
		for _, problem := range want {
			if !slices.Contains(verification.Problems, problem) {
				t.Errorf("%s: problems lack %q\nproblems: %q", step, problem, verification.Problems)
			}
		}
		for _, problem := range unwanted {
			if slices.Contains(verification.Problems, problem) {
				t.Errorf("%s: problems include %q\nproblems: %q", step, problem, verification.Problems)
			}
		}
		return verification
	}

	// Unchanged source: no executed-evidence problem yet.
	expect("unchanged source", nil, []string{anyRun, beta, alpha})

	// beta changes in a non-test file; alpha only in its test, which still
	// changes the alpha package.
	e.write("beta/beta.go", betaSource+"\nfunc Double() int { return 2 * Value() }\n")
	e.write("alpha/alpha_test.go", alphaTest+"\nfunc TestMore(t *testing.T) {}\n")
	expect("changed, nothing run", []string{anyRun, beta, alpha}, nil)

	// go vet passes: some check passed, but it is not go test.
	e.run("vet")
	expect("after go vet", []string{beta, alpha}, []string{anyRun})

	// go test ./alpha passes and covers alpha, but not beta.
	e.run("alpha")
	expect("after go test ./alpha", []string{beta}, []string{anyRun, alpha})

	// beta now fails its test: the source changed, so the earlier passes no
	// longer count, and the failed covering run does not count either.
	e.write("beta/beta.go", "package beta\n\nfunc Value() int { return 0 }\n")
	e.run("beta")
	expect("after a failed go test ./beta", []string{anyRun, beta, alpha}, nil)

	// Fixing beta changes the source again; only alpha is rerun.
	e.write("beta/beta.go", betaSource+"\nfunc Triple() int { return 3 * Value() }\n")
	expect("fixed, not rerun", []string{anyRun, beta, alpha}, nil)
	e.run("alpha")
	expect("fixed, alpha rerun", []string{beta}, []string{anyRun, alpha})

	// A covering run filtered through GOFLAGS passes but gives no credit.
	e.runExpect(true, "beta", "GOFLAGS=-run=^$")
	expect("after go test ./beta with GOFLAGS=-run=^$", []string{beta}, []string{anyRun, alpha})

	// A passed covering run at the current source clears the package problem.
	e.run("beta")
	expect("after a passed go test ./beta", nil, []string{anyRun, beta, alpha})

	// Only the latest run of a check at the current source counts: a later
	// failed run of the same check at the same source withdraws the credit.
	// BETA_FAIL is outside the run key, so only CW_NO_REUSE runs it again.
	e.runExpect(false, "beta", "BETA_FAIL=1", "CW_NO_REUSE=1")
	expect("pass then fail at the same source", []string{beta}, []string{anyRun, alpha})
	// P3a-2 B10, B22: a pass after a failed run of the same check at the same
	// source and GOFLAGS is a rerun; it earns no credit and says so.
	rerun := "check beta passed after a failed run at the same source and GOFLAGS, so it earns no Go evidence credit"
	e.run("beta")
	if v := expect("fail then pass at the same source", []string{beta}, []string{anyRun, alpha}); !slices.Contains(v.Limitations, rerun) {
		t.Errorf("fail then pass at the same source: limitations lack %q\nlimitations: %q", rerun, v.Limitations)
	}

	// So does a later filtered run of the same check; the plain pass after it
	// is still a rerun of the failure at this source.
	e.runExpect(true, "beta", "GOFLAGS=-run=^$")
	expect("pass then filtered pass at the same source", []string{beta}, []string{anyRun, alpha})
	e.run("beta")
	if v := expect("filtered then plain pass at the same source", []string{beta}, []string{anyRun, alpha}); !slices.Contains(v.Limitations, rerun) {
		t.Errorf("filtered then plain pass at the same source: limitations lack %q\nlimitations: %q", rerun, v.Limitations)
	}

	// Any further change to beta needs a new run, and so does alpha.
	e.write("beta/beta.go", betaSource+"\nfunc Triple() int { return 3 * Value() }\n\nfunc Four() int { return 4 }\n")
	expect("changed after the pass", []string{anyRun, beta, alpha}, nil)
	e.run("alpha")
	e.run("beta")
	expect("both rerun", nil, []string{anyRun, alpha, beta})

	// A go.mod change needs a passed ./... run; per-package runs do not do.
	module := moduleFileProblems(t)
	e.write("go.mod", evidenceModuleGo+"\n// contract test edit\n")
	e.run("alpha")
	e.run("beta")
	expect("go.mod changed, packages rerun", module, []string{anyRun, alpha, beta})
	e.runExpect(true, "all", "GOFLAGS=-run=^$")
	expect("go.mod changed, filtered ./... run", module, []string{anyRun})
	e.run("all")
	expect("go.mod changed, ./... passed", nil, append([]string{anyRun, alpha, beta}, module...))
}

// moduleFileProblems returns the problems the Go rule reports for a go.mod
// change in a module without a root package once every package has its own
// passed run: exactly the module-file rule's problems.
func moduleFileProblems(t *testing.T) []string {
	t.Helper()
	source := langpack.Source{
		Current: []string{"go.mod", "alpha/alpha.go", "alpha/alpha_test.go", "beta/beta.go", "beta/beta_test.go"},
		Changed: []string{"go.mod"},
	}
	problems := langpack.GoEvidence(source, []langpack.Run{{Argv: []string{"go", "test", "./alpha"}}, {Argv: []string{"go", "test", "./beta"}}}, langpack.GoFacts{NoRace: "not needed"}).Problems
	want := []string{"changed go.mod has no passed go test ./... run at the current source"}
	if !slices.Equal(problems, want) {
		t.Errorf("Go rule problems for a go.mod change with per-package runs %q, want %q", problems, want)
	}
	return want
}

// --- severity through the CLI handoff path ---

func evidenceSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// bundle writes the filled bundle for the review about to be assigned and
// returns it and its reference.
func (e *evidenceE2E) bundle(reviewID string) (taskflow.Bundle, map[string]string) {
	e.t.Helper()
	output := filepath.Join(e.files, reviewID+"-raw.json")
	e.mustCW("task", "bundle", "--task", e.task, "--review-id", reviewID, "--output", output)
	data, err := os.ReadFile(output)
	if err != nil {
		e.t.Fatal(err)
	}
	var bundle taskflow.Bundle
	if err := taskflow.Decode(bytes.NewReader(data), &bundle); err != nil {
		e.t.Fatalf("decode bundle: %v", err)
	}
	for index := range bundle.Reach {
		row := &bundle.Reach[index]
		row.Callers, row.InvocationModes, row.Platforms, row.Consumers = "n/a: contract test fixture", "n/a: contract test fixture", "n/a: contract test fixture", "n/a: contract test fixture"
	}
	filled, err := taskflow.EncodeBundle(bundle)
	if err != nil {
		e.t.Fatal(err)
	}
	path := e.file(reviewID+"-bundle.json", filled)
	return bundle, map[string]string{"path": path, "sha256": evidenceSum(filled)}
}

func (e *evidenceE2E) assignReview(reviewID, agent string) taskflow.Bundle {
	e.t.Helper()
	bundle, ref := e.bundle(reviewID)
	brief := map[string]any{
		"id": reviewID, "agent_id": agent, "role": "review", "stage": "code-review",
		"goal":           "Review the change against the contract.",
		"owned_paths":    []string{"."},
		"constraints":    []string{"Read only."},
		"check_ids":      []string{"beta"},
		"stop_condition": "Return a terminal source-bound handoff.",
		"bundle":         ref,
	}
	e.mustCW("task", "assign", "--task", e.task, "--input", e.file(reviewID+"-brief.json", brief), "--summary")
	return bundle
}

func (e *evidenceE2E) sourceRef(path string) map[string]any {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.repo, filepath.FromSlash(path)))
	if err != nil {
		e.t.Fatal(err)
	}
	return map[string]any{"path": path, "start_line": 1, "end_line": 1, "sha256": evidenceSum(data)}
}

func (e *evidenceE2E) reviewResult(reviewID string, bundle taskflow.Bundle, findings []map[string]any) map[string]any {
	runs := []map[string]any{}
	for _, row := range bundle.Reach {
		if row.Deleted {
			continue
		}
		runs = append(runs, map[string]any{"path": row.Path, "mode": "go test", "status": "ran", "command": "go test ./beta", "observed": "ok"})
	}
	if findings == nil {
		findings = []map[string]any{}
	}
	return map[string]any{
		"assignment_id": reviewID, "status": "completed",
		"summary":       "Read the changed package and ran its tests.",
		"evidence":      []any{e.sourceRef("beta/beta.go")},
		"changed_files": []any{}, "check_ids": []string{"beta"},
		"findings": findings, "uncertainties": []string{}, "reach_runs": runs,
	}
}

func (e *evidenceE2E) handoff(name string, result map[string]any) (int, string) {
	e.t.Helper()
	code, stdout, stderr := e.cw("task", "handoff", "--task", e.task, "--input", e.file(name, result), "--summary")
	return code, stderr + stdout
}

func (e *evidenceE2E) accept(reviewID string, dispositions []map[string]any) {
	e.t.Helper()
	if dispositions == nil {
		dispositions = []map[string]any{}
	}
	acceptance := map[string]any{
		"assignment_id": reviewID, "status": "accepted",
		"reason":    "Checked each finding against the current source.",
		"evidence":  []any{e.sourceRef("beta/beta.go")},
		"check_ids": []string{"beta"}, "findings": dispositions,
	}
	e.mustCW("task", "accept", "--task", e.task, "--input", e.file(reviewID+"-acceptance.json", acceptance), "--summary")
}

func evidenceFinding(id, severity string, ref map[string]any) map[string]any {
	finding := map[string]any{"id": id, "summary": "Value can return a non-positive number.", "evidence": []any{ref}}
	if severity != "-" {
		finding["severity"] = severity
	}
	return finding
}

func evidenceMentions(lines []string, fragment string) []string {
	var found []string
	for _, line := range lines {
		if strings.Contains(line, fragment) {
			found = append(found, line)
		}
	}
	return found
}

func TestEvidenceE2EReviewSeverityAndRejectedBlocker(t *testing.T) {
	e := newEvidenceE2E(t)
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", evidencePlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	e.write("beta/beta.go", betaSource+"\nfunc Double() int { return 2 * Value() }\n")
	e.run("beta")

	const blocker = "F-blocker-7"
	const minor = "F-minor-3"
	bundle := e.assignReview("review-1", "/root/reviewer-1")
	ref := e.sourceRef("beta/beta.go")

	for _, tc := range []struct{ name, severity string }{
		{"missing severity", "-"},
		{"empty severity", ""},
		{"unknown severity", "critical"},
		{"wrong case", "Blocker"},
	} {
		t.Run("review handoff refuses "+tc.name, func(t *testing.T) {
			before := e.verify()
			code, output := e.handoff("bad-"+strings.ReplaceAll(tc.name, " ", "-")+".json",
				e.reviewResult("review-1", bundle, []map[string]any{evidenceFinding(blocker, tc.severity, ref)}))
			if code == 0 {
				t.Fatalf("handoff with severity %q accepted: %s", tc.severity, output)
			}
			if !strings.Contains(output, "severity") {
				t.Errorf("refusal %q does not name severity", output)
			}
			after := e.verify()
			// P3a-2 tolerance: through the binary the facts budget is 5 s, so
			// the B9 problem may appear in one verify only; compare without it.
			graph := func(p string) bool {
				return p == "changed Go packages need a passed go test ./... run at the current source because the package graph is unavailable"
			}
			if !slices.Equal(slices.DeleteFunc(slices.Clone(before.Problems), graph), slices.DeleteFunc(slices.Clone(after.Problems), graph)) {
				t.Errorf("refused handoff changed verification problems:\nbefore %q\nafter %q", before.Problems, after.Problems)
			}
		})
	}

	code, output = e.handoff("review-1.json", e.reviewResult("review-1", bundle, []map[string]any{
		evidenceFinding(blocker, "blocker", ref), evidenceFinding(minor, "minor", ref),
	}))
	if code != 0 {
		t.Fatalf("valid review handoff refused: %s", output)
	}
	e.accept("review-1", []map[string]any{
		{"finding_id": blocker, "status": "rejected", "reason": "Value returns 1 on line 3.", "evidence": []any{ref}},
		{"finding_id": minor, "status": "rejected", "reason": "Value returns 1 on line 3.", "evidence": []any{ref}},
	})

	verification := e.verify()
	rejectedLimitations := func(v taskflow.Verification, id string) []string {
		var found []string
		for _, limitation := range evidenceMentions(v.Limitations, id) {
			if taskflow.IsRejectedFindingLimitation(limitation) {
				found = append(found, limitation)
			}
		}
		return found
	}
	if got := rejectedLimitations(verification, blocker); len(got) != 1 {
		t.Errorf("rejected blocker limitations %q, want one\nlimitations: %q", got, verification.Limitations)
	}
	if got := evidenceMentions(verification.Limitations, minor); len(got) != 0 {
		t.Errorf("a rejected minor finding gave limitations %q", got)
	}
	if got := evidenceMentions(verification.Problems, blocker); len(got) == 0 {
		t.Errorf("a rejected blocker without a later review is not a problem\nproblems: %q", verification.Problems)
	}
	if got := evidenceMentions(verification.Problems, minor); len(got) != 0 {
		t.Errorf("a rejected minor finding gave problems %q", got)
	}

	// --summary prints the rejected findings first, under rejected_findings.
	_, summary, _ := e.cw("task", "verify", "--task", e.task, "--summary")
	var compact struct {
		RejectedFindings []string `json:"rejected_findings"`
		Problems         []string `json:"problems"`
	}
	if err := json.Unmarshal([]byte(summary), &compact); err != nil {
		t.Fatalf("verify --summary is not JSON: %v: %q", err, summary)
	}
	if got := evidenceMentions(compact.RejectedFindings, blocker); len(got) != 1 {
		t.Errorf("rejected_findings %q, want one entry for %s", compact.RejectedFindings, blocker)
	}
	if strings.Contains(summary, minor) {
		t.Errorf("verify --summary lists the rejected minor finding: %q", summary)
	}
	if len(evidenceMentions(compact.Problems, blocker)) == 0 {
		t.Errorf("verify --summary problems omit the rejected blocker: %q", compact.Problems)
	}
	// "First": the rejected_findings member precedes every other list.
	marker := strings.Index(summary, `"rejected_findings"`)
	for _, member := range []string{`"problems"`, `"limitations"`} {
		if at := strings.Index(summary, member); at >= 0 && at < marker {
			t.Errorf("verify --summary prints %s before rejected_findings: %q", member, summary)
		}
	}

	// A later accepted independent review that re-raises the finding does not
	// clear it.
	bundle2 := e.assignReview("review-2", "/root/reviewer-2")
	listed := false
	for _, prior := range bundle2.PriorFindings {
		if prior.FindingID == blocker && prior.Disposition == "rejected" {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("review-2 bundle does not list %s as rejected: %+v", blocker, bundle2.PriorFindings)
	}
	code, output = e.handoff("review-2.json", e.reviewResult("review-2", bundle2, []map[string]any{evidenceFinding(blocker, "blocker", ref)}))
	if code != 0 {
		t.Fatalf("review-2 handoff refused: %s", output)
	}
	e.accept("review-2", []map[string]any{{"finding_id": blocker, "status": "rejected", "reason": "Still returns 1 on line 3.", "evidence": []any{ref}}})
	verification = e.verify()
	if got := evidenceMentions(verification.Problems, blocker); len(got) == 0 {
		t.Errorf("a review that re-raised the blocker cleared it\nproblems: %q", verification.Problems)
	}

	// A further accepted independent review that does not re-raise it clears
	// the problem; the rejected blocker stays a limitation.
	bundle3 := e.assignReview("review-3", "/root/reviewer-3")
	code, output = e.handoff("review-3.json", e.reviewResult("review-3", bundle3, nil))
	if code != 0 {
		t.Fatalf("review-3 handoff refused: %s", output)
	}
	e.accept("review-3", nil)
	verification = e.verify()
	if got := evidenceMentions(verification.Problems, blocker); len(got) != 0 {
		t.Errorf("a later accepted review without the finding left problems %q", got)
	}
	if got := rejectedLimitations(verification, blocker); len(got) == 0 {
		t.Errorf("the rejected blocker is no longer a limitation\nlimitations: %q", verification.Limitations)
	}
}

// --- strict go test flag allowlist through the real cw binary ---

const allowlistPlan = `{"title":"Strict flags","kind":"substantial",
 "checkpoints":{"required":false,"reason":"contract test fixture"},
 "requirements":[{"id":"behaviour","description":"Packages keep their behaviour","check_ids":["alpha","beta","beta-args","beta-zero","split","both"],"allow_non_execution":false}],
 "checks":[
  {"id":"alpha","argv":["go","test","-count=1","./alpha"],"timeout_seconds":300},
  {"id":"beta","argv":["go","test","-count=1","./beta"],"timeout_seconds":300},
  {"id":"beta-args","argv":["go","test","-count=1","./beta","-args","-test.run=^$"],"timeout_seconds":300},
  {"id":"beta-zero","argv":["go","test","-count=0","./beta"],"timeout_seconds":300},
  {"id":"split","argv":["go","test","-count=1","./alpha","-v","./beta"],"timeout_seconds":300},
  {"id":"both","argv":["go","test","./alpha","./beta","-count=1","-v"],"timeout_seconds":300}]}`

func (e *evidenceE2E) expectProblems(step string, want, unwanted []string) {
	e.t.Helper()
	verification := e.verify()
	for _, problem := range want {
		if !slices.Contains(verification.Problems, problem) {
			e.t.Errorf("%s: problems lack %q\nproblems: %q", step, problem, verification.Problems)
		}
	}
	for _, problem := range unwanted {
		if slices.Contains(verification.Problems, problem) {
			e.t.Errorf("%s: problems include %q\nproblems: %q", step, problem, verification.Problems)
		}
	}
}

// latestCheckRun returns the latest stored run of check.
func (e *evidenceE2E) latestCheckRun(check string) taskflow.CheckRun {
	e.t.Helper()
	record, err := taskflow.Load(e.task)
	if err != nil {
		e.t.Fatalf("load task: %v", err)
	}
	for index := len(record.Checks) - 1; index >= 0; index-- {
		if record.Checks[index].CheckID == check {
			return record.Checks[index]
		}
	}
	e.t.Fatalf("no stored run of check %s", check)
	return taskflow.CheckRun{}
}

// storedRunMembers returns, for each stored run of check in the newest record
// file, the JSON members it was encoded with.
func (e *evidenceE2E) storedRunMembers(check string) []map[string]any {
	e.t.Helper()
	files, err := filepath.Glob(filepath.Join(e.task, "record-*.json"))
	if err != nil || len(files) == 0 {
		e.t.Fatalf("no record files in %s: %v", e.task, err)
	}
	slices.Sort(files)
	data, err := os.ReadFile(files[len(files)-1])
	if err != nil {
		e.t.Fatal(err)
	}
	var stored struct {
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		e.t.Fatalf("decode %s: %v", files[len(files)-1], err)
	}
	var found []map[string]any
	for _, run := range stored.Checks {
		if run["check_id"] == check {
			found = append(found, run)
		}
	}
	return found
}

// recordFilesContain reports whether any stored record file holds fragment.
func (e *evidenceE2E) recordFilesContain(fragment string) []string {
	e.t.Helper()
	files, err := filepath.Glob(filepath.Join(e.task, "record-*.json"))
	if err != nil {
		e.t.Fatal(err)
	}
	var holding []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			e.t.Fatal(err)
		}
		if bytes.Contains(data, []byte(fragment)) {
			holding = append(holding, filepath.Base(file))
		}
	}
	return holding
}

// A covering run that passes but runs no tests, or runs under a GOFLAGS the
// allowlist refuses, earns no credit; GOFLAGS is split like cmd/go splits it
// and recorded with build-flag values redacted.
func TestEvidenceE2EStrictFlagAllowlist(t *testing.T) {
	e := newEvidenceE2E(t)
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", allowlistPlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	anyRun := anyRunProblem(t)
	beta := packageProblem(t, "beta")
	alpha := packageProblem(t, "alpha")

	e.write("beta/beta.go", betaSource+"\nfunc Double() int { return 2 * Value() }\n")
	e.expectProblems("changed, nothing run", []string{anyRun, beta}, []string{alpha})

	// go test ./beta -args -test.run=^$ passes without running a test.
	e.runExpect(true, "beta-args")
	e.expectProblems("after -args -test.run=^$", []string{beta}, []string{anyRun, alpha})

	// go test -count=0 ./beta passes without running a test.
	e.runExpect(true, "beta-zero")
	e.expectProblems("after -count=0", []string{beta}, []string{anyRun, alpha})

	// Quoted GOFLAGS fields are split like cmd/go: still a filter.
	for _, goflags := range []string{"GOFLAGS='-run=^$'", `GOFLAGS="-test.skip=."`, "GOFLAGS=-count=1 '-skip=.'", "GOFLAGS=-count=0"} {
		e.runExpect(true, "beta", goflags)
		e.expectProblems("after go test ./beta with "+goflags, []string{beta}, []string{anyRun, alpha})
	}

	// An allowlisted GOFLAGS keeps the credit.
	e.runExpect(true, "beta", "GOFLAGS=-count=1 -v")
	e.expectProblems("after go test ./beta with GOFLAGS=-count=1 -v", nil, []string{anyRun, beta, alpha})
	if got := e.latestCheckRun("beta").GOFLAGS; got != "-count=1 -v" {
		t.Errorf("stored GOFLAGS %q, want %q", got, "-count=1 -v")
	}

	// A filtered run then withdraws it again.
	e.runExpect(true, "beta", "GOFLAGS='-run=^$'")
	e.expectProblems("after a later quoted -run GOFLAGS", []string{beta}, []string{anyRun, alpha})

	// Build-flag values are redacted in the stored record; a run using
	// ldflags or gcflags earns no credit.
	const secret = "hunter2-evidence-secret"
	e.runExpect(true, "beta", "GOFLAGS='-ldflags=-X main.secret="+secret+"' -gcflags=-N -count=1")
	e.expectProblems("after a run with ldflags and gcflags in GOFLAGS", []string{beta}, []string{anyRun, alpha})
	stored := e.latestCheckRun("beta").GOFLAGS
	if !strings.Contains(stored, "<redacted>") || strings.Contains(stored, secret) || strings.Contains(stored, "-N") || !strings.Contains(stored, "-count=1") {
		t.Errorf("stored GOFLAGS %q, want ldflags and gcflags values redacted and -count=1 kept", stored)
	}
	if holding := e.recordFilesContain(secret); len(holding) != 0 {
		t.Errorf("record files %q hold the ldflags value", holding)
	}

	// The bare form keeps the value in the following field, which is not
	// stored verbatim either (cmd/go refuses this GOFLAGS, so the run fails).
	const bareSecret = "bare-evidence-secret"
	e.run("beta", "GOFLAGS=-ldflags -X=main.k="+bareSecret+" -count=1")
	if got := e.latestCheckRun("beta").GOFLAGS; strings.Contains(got, bareSecret) {
		t.Errorf("stored GOFLAGS %q holds the bare ldflags value", got)
	}
	if holding := e.recordFilesContain(bareSecret); len(holding) != 0 {
		t.Errorf("record files %q hold the bare ldflags value", holding)
	}
	e.expectProblems("after a bare -ldflags GOFLAGS run", []string{beta}, []string{alpha})

	// A run whose environment sets an empty GOFLAGS encodes no goflags member.
	runs := e.storedRunMembers("beta-args")
	if len(runs) == 0 {
		t.Fatal("no stored beta-args run")
	}
	for _, run := range runs {
		if value, ok := run["goflags"]; ok {
			t.Errorf("a run with empty GOFLAGS stored goflags %q", value)
		}
	}
	redacted := false
	for _, run := range e.storedRunMembers("beta") {
		if value, ok := run["goflags"].(string); ok && strings.Contains(value, "<redacted>") {
			redacted = true
		}
	}
	if !redacted {
		t.Error("no stored beta run has a redacted goflags member")
	}
}

// A changed asset in a directory without Go files belongs to the nearest
// ancestor package.
func TestEvidenceE2EAssetMapsToAncestorPackage(t *testing.T) {
	e := newEvidenceE2E(t)
	e.write("beta/static/index.html", "<p>beta</p>\n")
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", allowlistPlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	anyRun := anyRunProblem(t)
	beta := packageProblem(t, "beta")
	alpha := packageProblem(t, "alpha")

	e.write("beta/static/index.html", "<p>beta, changed</p>\n")
	e.expectProblems("asset changed", []string{anyRun, beta}, []string{alpha})
	e.runExpect(true, "alpha")
	e.expectProblems("asset changed, alpha run", []string{beta}, []string{anyRun, alpha})
	e.runExpect(true, "beta")
	e.expectProblems("asset changed, beta run", nil, []string{anyRun, beta, alpha})

	// A new file in a new asset directory below the package does too.
	e.write("beta/static/css/site.css", "p { color: red }\n")
	e.expectProblems("new nested asset", []string{anyRun, beta}, []string{alpha})
	e.runExpect(true, "beta")
	e.expectProblems("new nested asset, beta run", nil, []string{anyRun, beta, alpha})
}

// Like cmd/go, a non-flag argument after a flag that follows a package
// pattern ends the package list: go test ./alpha -v ./beta passes running only
// alpha and earns no credit for either package.
func TestEvidenceE2EPackageListEnds(t *testing.T) {
	e := newEvidenceE2E(t)
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", allowlistPlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	anyRun := anyRunProblem(t)
	beta := packageProblem(t, "beta")
	alpha := packageProblem(t, "alpha")

	e.write("alpha/alpha.go", alphaSource+"\nfunc Upper() string { return \"ALPHA\" }\n")
	e.write("beta/beta.go", betaSource+"\nfunc Double() int { return 2 * Value() }\n")
	e.expectProblems("changed, nothing run", []string{anyRun, alpha, beta}, nil)

	e.runExpect(true, "split")
	e.expectProblems("after go test ./alpha -v ./beta", []string{alpha, beta}, []string{anyRun})

	e.runExpect(true, "both")
	e.expectProblems("after go test ./alpha ./beta -count=1 -v", nil, []string{anyRun, alpha, beta})
}

const testPrefixPlan = `{"title":"test. prefix","kind":"substantial",
 "checkpoints":{"required":false,"reason":"contract test fixture"},
 "requirements":[{"id":"behaviour","description":"Packages keep their behaviour","check_ids":["beta","race-first","race-last"],"allow_non_execution":false}],
 "checks":[
  {"id":"beta","argv":["go","test","-count=1","./beta"],"timeout_seconds":300},
  {"id":"race-first","argv":["go","test","-count=1","-test.race","./beta"],"timeout_seconds":300},
  {"id":"race-last","argv":["go","test","-count=1","./beta","-test.race"],"timeout_seconds":300}]}`

// -test.race is not a flag cmd/go passes to the test binary under that name:
// go test -test.race ./beta hands ./beta to the binary as the flag's value and
// tests the module root instead. Both runs below pass, because the fixture's
// test binaries define a test.race flag, and neither covers beta.
func TestEvidenceE2ETestPrefixedBuildFlag(t *testing.T) {
	e := newEvidenceE2E(t)
	e.write("root_test.go", "package evidence\n\nimport (\n\t\"flag\"\n\t\"testing\"\n)\n\nvar _ = flag.String(\"test.race\", \"\", \"\")\n\nfunc TestRoot(t *testing.T) {}\n")
	e.write("beta/flag_test.go", "package beta\n\nimport \"flag\"\n\nvar _ = flag.Bool(\"test.race\", false, \"\")\n")
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", testPrefixPlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	anyRun := anyRunProblem(t)
	beta := packageProblem(t, "beta")

	e.write("beta/beta.go", betaSource+"\nfunc Double() int { return 2 * Value() }\n")
	e.expectProblems("changed, nothing run", []string{anyRun, beta}, nil)

	e.runExpect(true, "race-first")
	e.expectProblems("after go test -test.race ./beta", []string{beta}, []string{anyRun})
	e.runExpect(true, "race-last")
	e.expectProblems("after go test ./beta -test.race", []string{beta}, []string{anyRun})

	e.runExpect(true, "beta")
	e.expectProblems("after go test ./beta", nil, []string{anyRun, beta})
}
