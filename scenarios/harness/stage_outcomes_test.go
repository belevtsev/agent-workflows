package harness_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

type stageCandidate struct {
	Head, Index, Tree string
}

type stageEvent struct {
	Action, Actor, Note string
	Findings            []string
	Candidate           stageCandidate
	ReviewedIndex       bool `json:"reviewed_index"`
}

type stageProof struct {
	Run       string `json:"run_id"`
	Expect    string `json:"expected_behavior"`
	Outcome   string
	Argv      []string
	Exit      int `json:"exit_status"`
	Candidate stageCandidate
}

type stageReceipt struct {
	Next, Delivery, Ticket string
	Candidate              stageCandidate
	Verified               bool
	ReviewCurrent          bool        `json:"review_current"`
	ReviewStarts           int         `json:"review_starts"`
	Gate                   string      `json:"repository_checks"`
	Proof                  *stageProof `json:"behavioral_assertion"`
	Last                   stageEvent  `json:"last_result"`
	Publication            *struct {
		Outcome, Target, Commit string
	} `json:"last_publication"`
}

func stageJSON[T any](t *testing.T, r kitsutest.Result) T {
	t.Helper()
	var decoded T
	if r.Code != 0 {
		t.Fatalf("CLI exit %d\n%s", r.Code, r.Output())
	}
	if err := json.Unmarshal([]byte(r.Stdout), &decoded); err != nil {
		t.Fatalf("decode CLI result: %v\n%s", err, r.Output())
	}
	return decoded
}

func stageNext(f *flow) stageReceipt {
	f.t.Helper()
	return stageJSON[stageReceipt](f.t, f.repo.Kitsu("task", "next", f.id, "--json"))
}

func stageHistory(f *flow, actions ...string) []stageEvent {
	f.t.Helper()
	history := stageJSON[struct{ Events []stageEvent }](f.t, f.repo.Kitsu("task", "show", f.id, "--json"))
	actual := make([]string, 0, len(history.Events))
	for _, event := range history.Events {
		actual = append(actual, event.Action)
	}
	if !slices.Equal(actual, actions) {
		f.t.Fatalf("action history = %v, want %v", actual, actions)
	}
	return history.Events
}

func newStageRepo(t *testing.T) *kitsutest.Repo {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Setenv("GIT_AUTHOR_DATE", "2020-01-01T00:00:00Z")
	r.Setenv("GIT_COMMITTER_DATE", "2020-01-01T00:00:00Z")
	r.Setenv("GIT_OPTIONAL_LOCKS", "0")
	return r
}

type stageWorkspace struct {
	head, index, status string
	files               map[string]string
}

func freezeStageWorkspace(r *kitsutest.Repo, paths ...string) stageWorkspace {
	before := stageWorkspace{
		head:   r.Git("rev-parse", "HEAD"),
		status: r.Git("status", "--porcelain", "--untracked-files=all"),
		index:  r.Read(".git/index"),
		files:  make(map[string]string, len(paths)),
	}
	for _, path := range paths {
		before.files[path] = r.Read(path)
	}
	return before
}

func (before stageWorkspace) check(t *testing.T, r *kitsutest.Repo) {
	t.Helper()
	if head := r.Git("rev-parse", "HEAD"); head != before.head {
		t.Errorf("HEAD moved from %s to %s", before.head, head)
	}
	if status := r.Git("status", "--porcelain", "--untracked-files=all"); status != before.status {
		t.Errorf("Git status changed from %q to %q", before.status, status)
	}
	if r.Read(".git/index") != before.index {
		t.Error("Git index bytes changed")
	}
	for path, contents := range before.files {
		if r.Read(path) != contents {
			t.Errorf("working bytes changed in %s", path)
		}
	}
}

func TestStageOutcomePlanOnly(t *testing.T) {
	r := newStageRepo(t)
	r.Write("DESIGN.md", "Explore a JavaScript double export.\n")
	r.Git("add", "DESIGN.md")
	r.Git("commit", "-qm", "Initial idea")
	r.Write("DESIGN.md", "Double a JavaScript number.\n")
	r.Git("add", "DESIGN.md")
	r.Write("DESIGN.md", "Double a JavaScript number. Decide what to do with numeric strings.\n")
	r.Write("draft.txt", "Developer examples on a separate draft.\n")
	before := freezeStageWorkspace(r, "DESIGN.md", "draft.txt")

	f := flow{t: t, repo: r, id: "double-plan"}
	f.call(0, "open",
		"--entry", "plan", "--until", "plan",
		"--request", "Plan the double export from DESIGN.md and flag unsettled inputs. Do not edit files.",
	)
	f.record("intake", "coordinator", "Read the idea and keep the numeric string decision open.")
	const plan = `## Acceptance example

A caller imports double and observes double(3) === 6.

## Open decision

Confirm whether to accept numeric strings. With coercion, double("3") === 6.
With number-only inputs, double("3") throws TypeError.
After approval, add a public assertion for the chosen behavior.`
	f.record("plan", "coordinator", plan)
	f.want("done")
	receipt := stageNext(&f)
	if receipt.Next != "done" || receipt.Ticket != "" || receipt.Verified {
		t.Fatalf("plan finish = %+v", receipt)
	}
	saved := stageHistory(&f, "intake", "plan")[1].Note
	if saved != plan || receipt.Last.Note != saved {
		t.Fatal("the submitted plan could not be recovered")
	}
	// Rubric for the saved artifact: a public numeric example, a named open
	// decision, and observable consequences for both choices.
	for _, item := range []string{
		"## Open decision",
		"double(3) === 6",
		"double(\"3\") === 6",
		"double(\"3\") throws TypeError",
	} {
		if !strings.Contains(saved, item) {
			t.Errorf("plan rubric missing %q", item)
		}
	}
	before.check(t, r)
	t.Logf("start %s, plan only, recovered artifact:\n%s", before.head, saved)
}

const stageReferenceDouble = `module.exports = value => {
  if (typeof value !== "number") throw new TypeError("expected a number");
  return value * 2;
};
`

const stageFaultyDouble = `module.exports = value => {
  if (typeof value !== "number" && typeof value !== "string") throw new TypeError("expected a number");
  return value * 2;
};
`

const stageDoubleAssertion = `const double = require("./double.js");
try {
  double("3");
} catch (error) {
  if (!(error instanceof TypeError)) throw error;
  console.log("numeric string rejected");
  process.exit(0);
}
console.error("numeric string was accepted");
process.exit(18);
`

func stageJavaScript(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"node", "bun"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("requires Node.js or Bun to exercise the public double export")
	return ""
}

func newStageDoubleRepo(t *testing.T, js string) *kitsutest.Repo {
	t.Helper()
	r := newStageRepo(t)
	r.Write("double.js", "module.exports = value => value * 2;\n")
	r.Write("DESIGN.md", "Approved: double JavaScript numbers and reject numeric strings with TypeError.\n")
	r.Write("README.md", "# Double\n")
	r.Write(".gitignore", "build/\n")
	r.Write("unit.cjs", `const assert = require("node:assert/strict");
assert.equal(require("./double.js")(3), 6);
console.log("positive-number suite passed");
`)
	r.Write("kitsu.toml", fmt.Sprintf("version = 1\n\n[[check]]\nid = \"unit\"\nrun = [%q, \"unit.cjs\"]\n", filepath.Base(js)))
	r.Git("add", "-A")
	r.Git("commit", "-qm", "Pin the original number behavior and approved design")
	return r
}

func gradeStageDouble(t *testing.T, r *kitsutest.Repo, js string, wantPass bool) {
	t.Helper()
	// This grader lives outside the fixture repository and exercises additional
	// public examples. The scripted implementer receives only DESIGN.md.
	const grader = `const assert = require("node:assert/strict");
const double = require("./double.js");
assert.equal(double(0), 0);
assert.equal(double(2.5), 5);
assert.equal(double(-4), -8);
try {
  double("4");
} catch (error) {
  if (!(error instanceof TypeError)) throw error;
  process.exit(0);
}
console.error("hidden example accepted a numeric string");
process.exit(18);
`
	wantExit := 0
	if !wantPass {
		wantExit = 18
	}
	if got := r.Run("", js, "-e", grader); got.Code != wantExit {
		t.Fatalf("public double grader = %d, want %d\n%s", got.Code, wantExit, got.Output())
	}
}

func testStageDouble(f *flow, js string, exit int) {
	f.t.Helper()
	f.call(exit, "test",
		"--ticket", f.state.Ticket, "--expect", "double rejects numeric strings",
		"--", js, "-e", stageDoubleAssertion,
	)
}

func TestStageOutcomeApprovedImplementation(t *testing.T) {
	js := stageJavaScript(t)
	r := newStageDoubleRepo(t, js)
	gradeStageDouble(t, r, js, false)
	before := freezeStageWorkspace(r, "DESIGN.md", "README.md", ".gitignore", "unit.cjs", "kitsu.toml")

	f := flow{t: t, repo: r, id: "approved-double"}
	f.call(0, "open",
		"--entry", "implement", "--until", "implement",
		"--request", "Implement the approved DESIGN.md contract and verify the result.",
	)
	f.record("intake", "coordinator", "Use the approved number-only contract in DESIGN.md.")
	f.want("implement")
	r.Write("double.js", stageReferenceDouble)
	f.record("implement", "coordinator", "Added the approved type check to double.")
	f.want("verify")
	if f.state.Verified {
		t.Fatal("implementation was marked verified before the required check")
	}
	testStageDouble(&f, js, 0)
	f.want("done")
	receipt := stageNext(&f)
	if receipt.Next != "done" || !receipt.Verified || receipt.Gate != "passed" || receipt.ReviewStarts != 0 {
		t.Fatalf("implementation finish = %+v", receipt)
	}
	if len(r.RecordFiles("unit")) != 1 {
		t.Fatal("declared suite did not run exactly once")
	}
	stageHistory(&f, "intake", "implement", "test")
	gradeStageDouble(t, r, js, true)
	before.status = " M double.js"
	before.files["double.js"] = stageReferenceDouble
	before.check(t, r)
	t.Logf("start %s, implement only, public assertion and declared suite passed", before.head)
}

func TestStageOutcomeVerifyOnly(t *testing.T) {
	js := stageJavaScript(t)
	for _, tt := range []struct {
		name, source, next, outcome, assertionLog string
		cliExit, assertionExit, unitRuns          int
	}{
		{
			name: "faulty", source: stageFaultyDouble,
			next: "verify", outcome: "failed", assertionLog: "numeric string was accepted",
			cliExit: 5, assertionExit: 18,
		},
		{
			name: "reference", source: stageReferenceDouble,
			next: "done", outcome: "passed", assertionLog: "numeric string rejected",
			unitRuns: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newStageDoubleRepo(t, js)
			r.Write("double.js", tt.source)
			r.Git("add", "double.js")
			before := freezeStageWorkspace(r, "double.js", "DESIGN.md", "README.md", ".gitignore", "unit.cjs", "kitsu.toml")
			if suite := r.Run("", js, "unit.cjs"); suite.Code != 0 {
				t.Fatalf("normal suite failed on the %s control:\n%s", tt.name, suite.Output())
			}
			gradeStageDouble(t, r, js, tt.cliExit == 0)

			f := flow{t: t, repo: r, id: "verify-double"}
			f.call(0, "open",
				"--entry", "verify", "--until", "verify",
				"--request", "Verify the existing double implementation against DESIGN.md.",
			)
			f.record("intake", "coordinator", "Exercise the public number-only export.")
			f.want("verify")
			testStageDouble(&f, js, tt.cliExit)
			f.want(tt.next)
			receipt := stageNext(&f)
			p := receipt.Proof
			verdictMatches := p != nil && p.Outcome == tt.outcome && p.Exit == tt.assertionExit
			if !verdictMatches || receipt.Next != tt.next || receipt.Verified != (tt.cliExit == 0) {
				t.Fatalf("verification finish = %+v, proof = %+v", receipt, p)
			}
			if !strings.Contains(r.Read(".git/kitsu/logs/"+p.Run+".log"), tt.assertionLog) {
				t.Fatal("retained assertion log lacks public command output")
			}
			if got := len(r.RecordFiles("unit")); got != tt.unitRuns {
				t.Fatalf("declared suite runs = %d, want %d", got, tt.unitRuns)
			}
			if tt.cliExit != 0 {
				f.call(1, "record", "repair",
					"--actor", "coordinator", "--note", "Unrequested repair", "--ticket", f.state.Ticket,
				)
			}
			stageHistory(&f, "intake", "test")
			before.check(t, r)
			t.Logf("start %s, verify only, normal suite exit 0, task test exit %d, next %s", before.head, tt.cliExit, tt.next)
		})
	}
}

type stageReviewInput struct {
	Candidate     stageCandidate `json:"reserved_candidate"`
	Round         int            `json:"review_round"`
	PriorProof    *stageProof    `json:"prior_assertion"`
	PriorFindings []string       `json:"prior_findings"`
	Scope         struct{ Entry, Until string }
}

func claimStageReview(f *flow) stageReviewInput {
	f.t.Helper()
	f.want("review")
	r := f.repo.Kitsu("task", "claim", f.id,
		"--ticket", f.state.Ticket, "--actor", "coordinator", "--host", "codex", "--brief",
	)
	claim := stageJSON[struct {
		Created bool     `json:"claim_created"`
		Command []string `json:"review_input"`
	}](f.t, r)
	if !claim.Created || len(claim.Command) < 2 || claim.Command[0] != "kitsu" {
		f.t.Fatalf("no reserved reviewer input: %+v", claim)
	}
	return stageJSON[stageReviewInput](f.t, f.repo.Kitsu(claim.Command[1:]...))
}

func stageCompileSize(r *kitsutest.Repo, cxx, source, assertion string) kitsutest.Result {
	return r.Run(source+"\n"+assertion+"\n", cxx, "-std=c++11", "-x", "c++", "-fsyntax-only", "-")
}

func TestStageOutcomeStagedReview(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("requires a C++ compiler to grade the staged source")
	}
	const clean = "constexpr int bytes(int n) {\n    return n * 4;\n}\n"
	const faulty = "constexpr int bytes(int n) {\n    return n == 0 ? 4 : n * 4;\n}\n"
	for _, tt := range []struct {
		name, staged, working string
		seeded                bool
	}{
		{name: "faulty index with clean working copy", staged: faulty, working: clean, seeded: true},
		{name: "clean index with faulty working copy", staged: clean, working: faulty},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newStageRepo(t)
			r.Write("size.cpp", "constexpr int bytes(int n) { return n; }\n")
			r.Write("notes.txt", "Original notes.\n")
			r.Git("add", "-A")
			r.Git("commit", "-qm", "Original count behavior")
			r.Write("size.cpp", tt.staged)
			r.Git("add", "size.cpp")
			r.Write("size.cpp", tt.working)
			r.Write("notes.txt", "Unstaged developer notes.\n")
			r.Write("draft.txt", "Untracked developer notes.\n")
			indexTree := r.Git("write-tree")
			before := freezeStageWorkspace(r, "size.cpp", "notes.txt", "draft.txt")

			f := flow{t: t, repo: r, id: "staged-size"}
			f.call(0, "open",
				"--entry", "review", "--until", "review",
				"--request", "Review only the staged C++ word-to-byte conversion. An empty range uses zero bytes.",
			)
			f.record("intake", "coordinator", "Review the reserved index only. Each word takes four bytes.")
			input := claimStageReview(&f)
			if input.Scope.Until != "review" || input.Candidate.Head != before.head || input.Candidate.Index != indexTree {
				t.Fatalf("review input did not pin the requested index: %+v", input)
			}
			staged := r.TryGit("show", input.Candidate.Index+":size.cpp")
			if staged.Code != 0 || staged.Stdout != tt.staged || input.Candidate.Tree == input.Candidate.Index {
				t.Fatalf("reserved index and live worktree should differ: %+v\n%s", input, staged.Output())
			}

			// The scripted reviewer formats a judgment from a real compiler result.
			const reproduction = `static_assert(bytes(0) == 0, "empty range uses zero bytes");`
			review := stageCompileSize(r, cxx, staged.Stdout, reproduction)
			if review.Code != 0 {
				if !strings.Contains(review.Output(), "empty range uses zero bytes") {
					t.Fatal("review reproduction was inconclusive", review.Output())
				}
				f.record("review", "index-reviewer", "The staged conversion rejects the empty-range example.",
					"--finding", "Major, size.cpp:2. Compile git show :size.cpp with "+reproduction+" The result is 4.",
				)
			} else {
				f.record("review", "index-reviewer", "The staged conversion passes the empty-range example.")
			}
			f.want("done")
			event := stageHistory(&f, "intake", "review")[1]
			gradeStageIndexReview(t, r, cxx, staged.Stdout, tt.seeded, event)
			if !event.ReviewedIndex || event.Candidate.Index != indexTree || event.Actor != "index-reviewer" {
				t.Fatalf("review event did not retain the index or reviewer: %+v", event)
			}
			receipt := stageNext(&f)
			if receipt.Next != "done" || !receipt.ReviewCurrent || receipt.ReviewStarts != 1 || receipt.Verified {
				t.Fatalf("review-only finish = %+v", receipt)
			}
			before.check(t, r)
			t.Logf("start %s, review only, index %s, seeded defect %t, findings %d", before.head, indexTree, tt.seeded, len(event.Findings))
		})
	}
}

func gradeStageIndexReview(t *testing.T, r *kitsutest.Repo, cxx, staged string, seeded bool, event stageEvent) {
	t.Helper()
	// Independent control checks compile both versions and include nonempty inputs.
	const grade = `static_assert(bytes(0) + bytes(1) + bytes(3) == 16, "wrong aggregate byte count");`
	index := stageCompileSize(r, cxx, staged, grade)
	worktree := stageCompileSize(r, cxx, r.Read("size.cpp"), grade)
	if (index.Code != 0) != seeded || (worktree.Code != 0) == seeded {
		t.Fatalf("unexpected C++ controls, staged exit %d and worktree exit %d:\n%s\n%s",
			index.Code, worktree.Code, index.Output(), worktree.Output())
	}
	if !seeded {
		if len(event.Findings) != 0 {
			t.Errorf("false findings on clean staged control: %v", event.Findings)
		}
		return
	}
	var matching int
	for _, finding := range event.Findings {
		located := strings.Contains(finding, "size.cpp:2")
		reproducible := strings.Contains(finding, "git show :size.cpp") && strings.Contains(finding, "bytes(0) == 0")
		if located && reproducible {
			matching++
			continue
		}
		t.Errorf("unexpected finding on seeded control: %s", finding)
	}
	if matching != 1 {
		t.Errorf("wanted one located and reproducible seeded finding, got %d: %v", matching, event.Findings)
	}
}

func TestStageOutcomePartialRepair(t *testing.T) {
	r := newStageLabelRepo(t)
	probe := stageLabelProbe(t)
	r.Write("label.go", "package format\nfunc Label(n int) int { if n == -1 { return 1 }; return n }\n")
	r.Write("notes.txt", "Developer notes stay intact during the fix.\n")
	before := freezeStageWorkspace(r, "go.mod", "label_test.go", "kitsu.toml", "notes.txt")

	f := flow{t: t, repo: r, id: "partial-label-fix"}
	f.call(0, "open",
		"--require-review", "--entry", "verify", "--until", "verify", "--repair",
		"--request", "Verify the partial magnitude fix, review any repair, and stop after verification.",
	)
	f.record("intake", "coordinator", "The public Label function returns the magnitude for the agreed input range.")
	argv := []string{"go", "run", probe, "-3"}
	f.call(5, append([]string{"test", "--ticket", f.state.Ticket, "--expect", "Label(-3) equals 3", "--"}, argv...)...)
	f.want("repair")
	failed := stageNext(&f).Proof
	if failed == nil || failed.Outcome != "failed" || !slices.Equal(failed.Argv, argv) {
		t.Fatalf("missing saved public failure: %+v", failed)
	}
	log := ".git/kitsu/logs/" + failed.Run + ".log"
	failedOutput := r.Read(log)
	if !strings.Contains(failedOutput, "Label(-3) = -3, want 3") {
		t.Fatal("saved assertion lacks real failure", failedOutput)
	}

	r.Write("label.go", "package format\nfunc Label(n int) int { if n == -1 || n == -2 { return -n }; return n }\n")
	f.record("repair", "coordinator", "First repair handles -2 as well as -1.")
	first := claimStageReview(&f)
	checkStageRepairInput(t, r, first, failed, 1)
	bad := r.Run("", argv[0], argv[1:]...)
	if bad.Code != 1 || !strings.Contains(bad.Output(), "Label(-3) = -3, want 3") {
		t.Fatal("inadequate repair should retain the original failure", bad.Output())
	}
	const finding = "Major, label.go:2. The repaired Label(-3) is still -3, reproduced with the saved Go assertion."
	f.observedReview("codex", "coordinator", "repair-reviewer-1", "The original failure still reproduces on the changed candidate.", "--finding", finding)
	f.want("repair")

	const fixed = "package format\nfunc Label(n int) int { if n < 0 { return -n }; return n }\n"
	r.Write("label.go", fixed)
	f.record("repair", "coordinator", "Handle negative values across the agreed range.")
	second := claimStageReview(&f)
	checkStageRepairInput(t, r, second, failed, 2)
	if second.Candidate.Tree == first.Candidate.Tree || !slices.Equal(second.PriorFindings, []string{finding}) {
		t.Fatalf("new reviewer lacked the inadequate repair's finding: %+v", second)
	}
	if got := r.Run("", argv[0], argv[1:]...); got.Code != 0 {
		t.Fatal("fixed candidate still fails the independent review", got.Output())
	}
	f.observedReview("codex", "coordinator", "repair-reviewer-2", "Reproduced the regression on the first repair and confirmed Label(-3) on the new candidate.")
	f.want("verify")
	f.call(0, "test", "--ticket", f.state.Ticket, "--retry")
	f.want("done")
	finished := stageNext(&f)
	passed := finished.Proof
	if passed == nil || passed.Run == failed.Run || !slices.Equal(passed.Argv, argv) || passed.Expect != failed.Expect {
		t.Fatalf("the final assertion did not replay the saved behavior: %+v", passed)
	}
	if !finished.Verified || finished.Gate != "passed" || finished.ReviewStarts != 2 || !finished.ReviewCurrent {
		t.Fatalf("verified repair finish = %+v", finished)
	}
	history := stageHistory(&f, "intake", "test", "repair", "review", "repair", "review", "test")
	if !strings.Contains(history[1].Note, failed.Run) || r.Read(log) != failedOutput {
		t.Fatal("the observed failure was lost after the repair")
	}
	if len(r.RecordFiles("unit")) != 1 || r.Run("", "go", "run", probe, "-4").Code != 0 {
		t.Fatal("final declared suite or additional public input did not pass")
	}
	before.files["label.go"] = fixed
	before.check(t, r)
	t.Logf("start %s, verified fix, original failing run %s survived, two independent scripted review rounds", before.head, failed.Run)
}

func newStageLabelRepo(t *testing.T) *kitsutest.Repo {
	t.Helper()
	r := newStageRepo(t)
	r.Setenv("GOWORK", "off")
	r.Setenv("GOPROXY", "off")
	r.Write("go.mod", "module example.test/format\n\ngo 1.22\n")
	r.Write("label.go", "package format\nfunc Label(n int) int { return n }\n")
	r.Write("label_test.go", `package format

import "testing"

func TestPositive(t *testing.T) {
	if got := Label(2); got != 2 {
		t.Fatalf("Label(2) = %d, want 2", got)
	}
}
`)
	r.Write("kitsu.toml", "version = 1\n\n[[check]]\nid = \"unit\"\nrun = [\"go\", \"test\", \"-count=1\", \"./...\"]\n")
	r.Git("add", "-A")
	r.Git("commit", "-qm", "Original public Label function and positive suite")
	return r
}

func stageLabelProbe(t *testing.T) string {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe.go")
	const source = `package main

import (
	"example.test/format"
	"fmt"
	"os"
	"strconv"
)

func main() {
	n, err := strconv.Atoi(os.Args[1])
	if err != nil || n >= 0 {
		panic("expected a negative fixture input")
	}
	if got := format.Label(n); got != -n {
		fmt.Fprintf(os.Stderr, "Label(%d) = %d, want %d\n", n, got, -n)
		os.Exit(1)
	}
	fmt.Printf("Label(%d) = %d\n", n, -n)
}
`
	if err := os.WriteFile(probe, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return probe
}

func checkStageRepairInput(t *testing.T, r *kitsutest.Repo, input stageReviewInput, failed *stageProof, round int) {
	t.Helper()
	if input.PriorProof == nil || input.PriorProof.Run != failed.Run || input.Round != round {
		t.Fatalf("review round %d lost the original failure: %+v", round, input)
	}
	if input.Candidate.Tree == failed.Candidate.Tree {
		t.Fatal("repair re-review used the original candidate")
	}
	blob := r.TryGit("show", input.Candidate.Tree+":label.go")
	if blob.Code != 0 || blob.Stdout != r.Read("label.go") {
		t.Fatalf("reviewer did not receive the changed source: %+v", blob)
	}
}

func verifyStageDeliveryCandidate(t *testing.T, r *kitsutest.Repo, js string) *flow {
	t.Helper()
	f := &flow{t: t, repo: r, id: "verified-double"}
	f.call(0, "open",
		"--entry", "verify", "--until", "verify",
		"--request", "Verify the existing approved implementation of double.",
	)
	f.record("intake", "coordinator", "Use the approved public export contract.")
	testStageDouble(f, js, 0)
	f.want("done")
	if !f.state.Verified || f.state.Proof == nil {
		t.Fatal("cannot start delivery from incomplete verification")
	}
	return f
}

func TestStageOutcomeVerifiedDelivery(t *testing.T) {
	js := stageJavaScript(t)
	t.Run("commit preserves developer scratch file", func(t *testing.T) {
		r := newStageDoubleRepo(t, js)
		r.Write("double.js", stageReferenceDouble)
		const scratch = "build/notes.txt"
		const developerWork = "Developer notes excluded by the existing ignore rule.\n"
		r.Write(scratch, developerWork)
		start := freezeStageWorkspace(r, "double.js", scratch)
		verified := verifyStageDeliveryCandidate(t, r, js)
		start.check(t, r)
		run := verified.state.Proof.Run

		resumed := flow{t: t, repo: r, id: verified.id}
		resumed.call(0, "extend", "--until", "deliver", "--commit")
		resumed.want("commit")
		reused := resumed.state.Proof != nil && resumed.state.Proof.Run == run
		if !resumed.state.Verified || !reused || resumed.state.CommitTarget != "refs/heads/main" {
			t.Fatalf("delivery lost the saved verification or target: %+v", resumed.state)
		}
		r.Git("add", "double.js")
		tree := r.Git("write-tree")
		staged := freezeStageWorkspace(r, "double.js", scratch)
		ticket := resumed.state.Ticket
		rejected := r.Kitsu("task", "commit", resumed.id, "--ticket", ticket, "--message", "Deliver strict double")
		if rejected.Code != 1 || !strings.Contains(rejected.Output(), `pre-existing edit "double.js" requires explicit --include`) {
			t.Fatal("ownership guard did not reject an unacknowledged pre-existing edit", rejected.Output())
		}
		staged.check(t, r)
		resumed.call(0, "commit",
			"--ticket", ticket, "--message", "Deliver strict double", "--include", "double.js",
		)
		resumed.want("done")
		finished := stageNext(&resumed)
		stagedProof := finished.Proof != nil && finished.Proof.Run != run && finished.Proof.Candidate.Tree == tree && finished.Proof.Candidate.Index == tree
		if finished.Delivery != "committed" || !finished.Verified || !stagedProof {
			t.Fatalf("wrong verified delivery: %+v", finished)
		}
		head := r.Git("rev-parse", "HEAD")
		if got := r.Git("rev-list", "--parents", "-n", "1", "HEAD"); got != head+" "+start.head {
			t.Fatalf("commit and ordered parents = %s, want %s %s", got, head, start.head)
		}
		if got := r.Git("rev-parse", "HEAD^{tree}"); got != tree {
			t.Fatalf("delivered tree = %s, want %s", got, tree)
		}
		if changes := r.Git("diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"); changes != "double.js" {
			t.Fatalf("unexpected delivered paths: %q", changes)
		}
		if got := r.TryGit("show", "HEAD:double.js"); got.Code != 0 || got.Stdout != stageReferenceDouble {
			t.Fatal("delivered blob differs from the reference implementation", got.Output())
		}
		if got := r.Git("status", "--porcelain"); got != "" || r.Read(scratch) != developerWork {
			t.Fatalf("delivery affected unrelated developer work, status %q", got)
		}
		if len(r.RecordFiles("unit")) != 2 {
			t.Fatal("staging the change must run the repository check against the new index")
		}
		stageHistory(&resumed, "intake", "test", "test", "commit")
		gradeStageDouble(t, r, js, true)
		t.Logf("start %s, original proof %s, delivered %s with tree %s and excluded scratch file", start.head, run, head, tree)
	})

	t.Run("conflicting staged notes are preserved", func(t *testing.T) {
		r := newStageDoubleRepo(t, js)
		original := r.Read("README.md")
		r.Write("README.md", "Unrelated staged developer notes.\n")
		r.Git("add", "README.md")
		r.Write("README.md", original)
		r.Write("double.js", stageReferenceDouble)
		f := verifyStageDeliveryCandidate(t, r, js)
		f.call(0, "extend", "--until", "deliver", "--commit")
		f.want("commit")
		r.Git("add", "double.js")
		before := freezeStageWorkspace(r, "double.js", "README.md")
		rejected := r.Kitsu("task", "commit", f.id,
			"--ticket", f.state.Ticket, "--message", "Deliver strict double", "--include", "double.js",
		)
		if rejected.Code != 1 || !strings.Contains(rejected.Output(), "staged content differs from the verified candidate") {
			t.Fatal("delivery accepted an index containing unrelated work", rejected.Output())
		}
		before.check(t, r)
		f.call(0, "next")
		f.want("verify")
		stageHistory(f, "intake", "test")
		t.Logf("start %s, mixed index blocked with exit 1, original staged and working bytes intact", before.head)
	})
}

func TestStageOutcomeExplicitPublication(t *testing.T) {
	js := stageJavaScript(t)
	r := newStageDoubleRepo(t, js)
	r.Setenv("GIT_ALLOW_PROTOCOL", "file")
	parent := r.Git("rev-parse", "HEAD")
	r.Write("double.js", stageReferenceDouble)
	r.Git("add", "double.js")
	r.Git("commit", "-qm", "Existing approved delivery")
	before := freezeStageWorkspace(r, "double.js")
	f := verifyStageDeliveryCandidate(t, r, js)
	run := f.state.Proof.Run

	const target = "sandbox/topic"
	const ref = "refs/heads/topic"
	remote := filepath.Join(t.TempDir(), "destination.git")
	r.Git("init", "--bare", remote)
	r.Git("remote", "add", "sandbox", remote)
	f.call(0, "extend", "--until", "deliver", "--publish-to", target)
	f.want("publish")
	r.Git("push", "sandbox", parent+":"+ref)
	f.call(1, "confirm", "--ticket", f.state.Ticket)
	f.call(0, "next")
	f.want("publish")
	r.Git("push", "sandbox", "HEAD:"+ref)
	f.call(0, "confirm", "--ticket", f.state.Ticket)
	f.want("done")
	receipt := stageNext(f)
	published := receipt.Publication
	if published == nil || published.Outcome != "confirmed" || published.Target != target || published.Commit != before.head {
		t.Fatalf("wrong publication receipt: %+v", published)
	}
	sameProof := receipt.Proof != nil && receipt.Proof.Run == run
	if !receipt.Verified || !sameProof || r.In(remote).Git("rev-parse", ref) != before.head {
		t.Fatal("publication did not confirm the verified commit at the requested ref")
	}
	stageHistory(f, "intake", "test", "publish")
	before.check(t, r)
	t.Logf("start %s, prior commit rejected with exit 1, exact commit confirmed at %s with exit 0", before.head, target)
}

func TestReadOnlyPlanKeepsOpeningCandidateAcrossResume(t *testing.T) {
	for _, phase := range []string{"intake", "plan"} {
		for _, change := range []string{"worktree", "filtered", "untracked", "index", "parent"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				r := newStageRepo(t)
				r.Write(".gitattributes", "idea.txt text eol=lf\n")
				r.Write(".gitignore", "scratch/\n")
				r.Write("idea.txt", "original idea\n")
				r.Git("add", ".gitattributes", ".gitignore", "idea.txt")
				r.Git("commit", "-qm", "original")
				head := r.Git("rev-parse", "HEAD")
				r.Write("idea.txt", "user staged idea\n")
				r.Git("add", "idea.txt")
				r.Write("idea.txt", "user working idea\n")
				r.Write("notes.txt", "user draft\n")
				f := flow{t: t, repo: r, id: "plan-scope"}
				f.call(0, "open", "--entry", "plan", "--until", "plan", "--request", "Diagnose only, leave my edits intact")
				if phase == "plan" {
					f.record("intake", "host", "Inspect the current idea")
				}
				ticket := f.state.Ticket
				r.Write("scratch/probe.log", "ignored diagnostic output\n")
				switch change {
				case "worktree":
					r.Write("idea.txt", "different working idea\n")
				case "filtered":
					before := r.Git("hash-object", "--path=idea.txt", "idea.txt")
					r.Write("idea.txt", "user working idea\r\n")
					if after := r.Git("hash-object", "--path=idea.txt", "idea.txt"); before != after {
						t.Fatal("fixture should have an unchanged filtered blob")
					}
				case "untracked":
					r.Write("extra.txt", "concurrent draft\n")
				case "index":
					r.Git("add", "idea.txt")
				case "parent":
					// Advance HEAD while preserving the user's distinct working and staged bytes.
					newHead := r.Git("commit-tree", head+"^{tree}", "-p", head, "-m", "concurrent commit")
					r.Git("update-ref", "HEAD", newHead, head)
				}
				changed := freezeStageWorkspace(r, "idea.txt", "notes.txt")
				f.call(1, "record", phase, "--actor", "host", "--ticket", ticket, "--note", "Unconfirmed result")
				if phase == "intake" {
					// Intake may describe changed facts. It cannot certify a completed plan.
					f.call(0, "next")
					f.record("intake", "host", "The inputs changed since opening")
					f.want("blocked")
				}
				reported := "working files"
				switch change {
				case "index":
					reported = "staged files"
				case "parent":
					reported = "Git parents"
				}
				for _, flag := range []string{"--brief", "--json"} {
					receipt := stageJSON[stageReceipt](t, r.Kitsu("task", "next", f.id, flag))
					if receipt.Next != "blocked" || receipt.Ticket != "" || !strings.Contains(receipt.Delivery, "("+reported+")") {
						t.Fatalf("issued a new ticket for changed read-only inputs: %+v", receipt)
					}
				}
				if out := r.Kitsu("task", "next", f.id); out.Code != 0 || !strings.Contains(out.Stdout, "planning") {
					t.Fatal(out.Output())
				}
				changed.check(t, r)
				// The developer restores this fixture deliberately. Kitsu only observes it.
				switch change {
				case "worktree", "filtered":
					r.Write("idea.txt", "user working idea\n")
				case "untracked":
					if err := os.Remove(r.Path("extra.txt")); err != nil {
						t.Fatal(err)
					}
				case "index":
					r.Write("idea.txt", "user staged idea\n")
					r.Git("add", "idea.txt")
					r.Write("idea.txt", "user working idea\n")
				case "parent":
					r.Git("update-ref", "HEAD", head)
				}
				f.call(0, "next")
				if phase == "plan" && f.state.Ticket != ticket {
					t.Fatal("lost original planning ticket on recovery")
				}
				f.record("plan", "host", "Keep the numeric-string question open")
				f.want("done")
				r.Write("notes.txt", "later developer work\n")
				f.call(0, "next")
				f.want("done")
				stageHistory(&f, "intake", "plan")
			})
		}
	}
}
