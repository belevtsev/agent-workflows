package evals

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Authored tests for applyPatch, findWorkspaces, loadMutantCases,
// runMutants and mutantReport.err (M1-M4).

func authoredWriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func authoredWantFile(t *testing.T, dir, rel, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s after patch =\n%q\nwant\n%q", rel, got, want)
	}
}

// authoredTenLines is "line1\n" .. "line10\n".
func authoredTenLines() string {
	return "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
}

const authoredGoFile = "package pkg\n\nfunc F() int {\n\treturn 1\n}\n"

// M1: a single hunk with a description before the first "--- " line.
func TestAuthoredApplyPatchSingleHunk(t *testing.T) {
	dir := t.TempDir()
	authoredWriteFiles(t, dir, map[string]string{"a.txt": authoredTenLines(), "untouched.txt": "same\n"})
	patch := "R3: replace line five.\nSecond description line.\n\n" +
		"--- a/a.txt\n+++ b/a.txt\n@@ -4,3 +4,3 @@\n line4\n-line5\n+LINE5\n line6\n"
	if err := applyPatch(dir, []byte(patch)); err != nil {
		t.Fatalf("applyPatch: %v", err)
	}
	authoredWantFile(t, dir, "a.txt", "line1\nline2\nline3\nline4\nLINE5\nline6\nline7\nline8\nline9\nline10\n")
	authoredWantFile(t, dir, "untouched.txt", "same\n")
}

// M1: two hunks in one file, the first growing and the second shrinking it.
func TestAuthoredApplyPatchMultiHunk(t *testing.T) {
	dir := t.TempDir()
	authoredWriteFiles(t, dir, map[string]string{"a.txt": authoredTenLines()})
	patch := "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,3 +1,4 @@\n line1\n-line2\n+two-a\n+two-b\n line3\n" +
		"@@ -8,3 +9,2 @@\n line8\n-line9\n line10\n"
	if err := applyPatch(dir, []byte(patch)); err != nil {
		t.Fatalf("applyPatch: %v", err)
	}
	authoredWantFile(t, dir, "a.txt", "line1\ntwo-a\ntwo-b\nline3\nline4\nline5\nline6\nline7\nline8\nline10\n")
}

// M1: one patch touching two files, one of them in a subdirectory.
func TestAuthoredApplyPatchTwoFiles(t *testing.T) {
	dir := t.TempDir()
	authoredWriteFiles(t, dir, map[string]string{"a.txt": authoredTenLines(), "pkg/b.go": authoredGoFile})
	patch := "Mutant: flip the return value and append a line.\n\n" +
		"--- a/pkg/b.go\n+++ b/pkg/b.go\n@@ -3,3 +3,3 @@\n func F() int {\n-\treturn 1\n+\treturn 2\n }\n" +
		"--- a/a.txt\n+++ b/a.txt\n@@ -10,1 +10,2 @@\n line10\n+line11\n"
	if err := applyPatch(dir, []byte(patch)); err != nil {
		t.Fatalf("applyPatch: %v", err)
	}
	authoredWantFile(t, dir, "pkg/b.go", "package pkg\n\nfunc F() int {\n\treturn 2\n}\n")
	authoredWantFile(t, dir, "a.txt", authoredTenLines()+"line11\n")
}

// M1: mismatched context or removed lines, a wrong position and a missing
// target file are errors.
func TestAuthoredApplyPatchRejects(t *testing.T) {
	cases := map[string]string{
		"context mismatch":   "--- a/a.txt\n+++ b/a.txt\n@@ -4,3 +4,3 @@\n line4\n-line5\n+LINE5\n lineSIX\n",
		"removed mismatch":   "--- a/a.txt\n+++ b/a.txt\n@@ -4,3 +4,3 @@\n line4\n-lineFIVE\n+LINE5\n line6\n",
		"wrong position":     "--- a/a.txt\n+++ b/a.txt\n@@ -6,3 +6,3 @@\n line4\n-line5\n+LINE5\n line6\n",
		"beyond end of file": "--- a/a.txt\n+++ b/a.txt\n@@ -11,1 +11,1 @@\n-line11\n+LINE11\n",
		"missing file":       "--- a/nope.txt\n+++ b/nope.txt\n@@ -1,1 +1,1 @@\n-line1\n+LINE1\n",
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			authoredWriteFiles(t, dir, map[string]string{"a.txt": authoredTenLines()})
			if err := applyPatch(dir, []byte(patch)); err == nil {
				t.Fatal("applyPatch: want an error, got nil")
			}
			if _, err := os.Stat(filepath.Join(dir, "nope.txt")); err == nil {
				t.Fatal("applyPatch created the missing target file")
			}
		})
	}
}

// M2: workspaces are found by byte-identical contracts and keyed by case.
func TestAuthoredFindWorkspaces(t *testing.T) {
	casesDir := t.TempDir()
	contracts := map[string]string{
		"case-a": "# Contract A\n\n- R1: a.\n",
		"case-b": "# Contract B\n\n- R1: b.\n",
		"case-c": "# Contract C\n\n- R1: c.\n",
	}
	var cases []mutantCase
	for _, name := range []string{"case-a", "case-b", "case-c"} {
		dir := filepath.Join(casesDir, "suite", name)
		authoredWriteFiles(t, dir, map[string]string{
			"testdata/contract/interface-contract.md": contracts[name],
			"prompt.md": "---\ntags: [suite]\n---\nprompt\n",
		})
		cases = append(cases, mutantCase{Name: name, Dir: dir})
	}

	root := t.TempDir()
	const contractPath = "testdata/contract/interface-contract.md"
	authoredWriteFiles(t, root, map[string]string{
		"ws1/" + contractPath:                        contracts["case-a"],
		"deep/x/ws2/" + contractPath:                 contracts["case-a"],
		"ws3/" + contractPath:                        contracts["case-b"],
		"changed/" + contractPath:                    contracts["case-a"] + "- R2: extra.\n",
		"with-prompt/" + contractPath:                contracts["case-b"],
		"with-prompt/prompt.md":                      "---\ntags: [suite]\n---\nprompt\n",
		"unrelated/notes.md":                         contracts["case-a"],
		"wrong-place/contract/interface-contract.md": contracts["case-a"],
	})

	got, err := findWorkspaces(root, cases)
	if err != nil {
		t.Fatalf("findWorkspaces: %v", err)
	}
	wantA := []string{filepath.Join(root, "deep", "x", "ws2"), filepath.Join(root, "ws1")}
	sort.Strings(wantA)
	want := map[string][]string{
		"case-a": wantA,
		"case-b": {filepath.Join(root, "ws3")},
	}
	if len(got) != len(want) {
		t.Fatalf("findWorkspaces = %v, want %v", got, want)
	}
	for name, dirs := range want {
		if strings.Join(got[name], "\n") != strings.Join(dirs, "\n") {
			t.Fatalf("findWorkspaces[%s] = %q, want %q (full result %v)", name, got[name], dirs, got)
		}
	}
	if _, ok := got["case-c"]; ok {
		t.Fatalf("findWorkspaces has an entry for case-c, which has no workspace: %v", got)
	}
}

// authoredMutantCases is the expected loadMutantCases result (M3).
var authoredMutantCases = []struct {
	name, module, testPath string
}{
	{"notes-init-e2e", "example.com/notes", "e2e/init_test.go"},
	{"retry-budget-matrix", "example.com/retry", "retry/budget_test.go"},
	{"ttl-cache-matrix", "example.com/cache", "cache/cache_test.go"},
}

// M3: the three test-engineer cases with their modules, test paths and mutants.
func TestAuthoredLoadMutantCases(t *testing.T) {
	root := authoredRealRoot(t)
	got, err := loadMutantCases(root)
	if err != nil {
		t.Fatalf("loadMutantCases: %v", err)
	}
	if len(got) != len(authoredMutantCases) {
		t.Fatalf("loadMutantCases returned %d cases, want %d: %+v", len(got), len(authoredMutantCases), got)
	}
	for i, want := range authoredMutantCases {
		c := got[i]
		if c.Name != want.name || c.Module != want.module || c.TestPath != want.testPath {
			t.Fatalf("case %d = {Name %q Module %q TestPath %q}, want {%q %q %q}", i, c.Name, c.Module, c.TestPath, want.name, want.module, want.testPath)
		}
		if wantDir := filepath.Join(root, "evals", "test-engineer", want.name); c.Dir != wantDir {
			t.Fatalf("case %s Dir = %q, want %q", c.Name, c.Dir, wantDir)
		}
		if n := len(c.Mutants); n < 2 || n > 3 {
			t.Fatalf("case %s has %d mutants, want 2 or 3: %q", c.Name, n, c.Mutants)
		}
		if !sort.StringsAreSorted(c.Mutants) {
			t.Fatalf("case %s mutants are not sorted: %q", c.Name, c.Mutants)
		}
		for _, m := range c.Mutants {
			if !filepath.IsAbs(m) || filepath.Dir(m) != filepath.Join(c.Dir, "testdata", "impl-mutants") || filepath.Ext(m) != ".patch" {
				t.Fatalf("case %s mutant %q is not an absolute testdata/impl-mutants/*.patch path", c.Name, m)
			}
			if _, err := os.Stat(m); err != nil {
				t.Fatalf("case %s mutant %q: %v", c.Name, m, err)
			}
		}
	}
}

// M4 (gate logic): err is nil only when the baseline passed and every mutant failed.
func TestAuthoredMutantReportErr(t *testing.T) {
	killed := func(name string) mutantOutcome {
		return mutantOutcome{Name: name, Requirement: "R1: " + name, runOutcome: runOutcome{Passed: false}}
	}
	survived := func(name string) mutantOutcome {
		return mutantOutcome{Name: name, Requirement: "R2: " + name, runOutcome: runOutcome{Passed: true}}
	}

	all := mutantReport{Case: "demo-case", Baseline: runOutcome{Passed: true}, Mutants: []mutantOutcome{killed("m-one"), killed("m-two")}}
	if err := all.err(); err != nil {
		t.Fatalf("all mutants killed: err = %v, want nil", err)
	}

	baseline := mutantReport{Case: "demo-case", Baseline: runOutcome{Passed: false, Output: "FAIL"}, Mutants: []mutantOutcome{killed("m-one")}}
	if err := baseline.err(); err == nil || !strings.Contains(err.Error(), "demo-case") {
		t.Fatalf("baseline failed: err = %v, want an error naming demo-case", err)
	}

	some := mutantReport{Case: "demo-case", Baseline: runOutcome{Passed: true}, Mutants: []mutantOutcome{survived("m-alive-one"), killed("m-dead"), survived("m-alive-two")}}
	err := some.err()
	if err == nil {
		t.Fatal("surviving mutants: err = nil, want an error")
	}
	for _, name := range []string{"m-alive-one", "m-alive-two"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("surviving mutants: err %q does not name %s", err, name)
		}
	}
}

// authoredSelfTestFile reads a fixture test file written for the self-test.
func authoredSelfTestFile(t *testing.T, root string, c mutantCase, kind string) []byte {
	t.Helper()
	p := filepath.Join(root, "evals", "testdata", "mutant-selftest", c.Name, kind, filepath.FromSlash(c.TestPath))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// M4: the known-good test file kills every mutant, and the known-weak test
// file passes the baseline but lets a mutant survive.
func TestAuthoredMutantGateSelfTest(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test against every mutant")
	}
	root := authoredRealRoot(t)
	cases, err := loadMutantCases(root)
	if err != nil {
		t.Fatalf("loadMutantCases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("loadMutantCases returned no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			t.Run("good", func(t *testing.T) {
				t.Parallel()
				report, err := runMutants(t.Context(), c, authoredSelfTestFile(t, root, c, "good"), t.TempDir())
				if err != nil {
					t.Fatalf("runMutants: harness error %v", err)
				}
				if report.Case != c.Name {
					t.Fatalf("report.Case = %q, want %q", report.Case, c.Name)
				}
				if !report.Baseline.Passed {
					t.Fatalf("good tests fail the unmutated implementation:\n%s", report.Baseline.Output)
				}
				if len(report.Mutants) != len(c.Mutants) {
					t.Fatalf("report has %d mutant outcomes, want %d", len(report.Mutants), len(c.Mutants))
				}
				for i, m := range report.Mutants {
					wantName := strings.TrimSuffix(filepath.Base(c.Mutants[i]), ".patch")
					if m.Name != wantName {
						t.Errorf("mutant %d Name = %q, want %q", i, m.Name, wantName)
					}
					if strings.TrimSpace(m.Requirement) == "" {
						t.Errorf("mutant %s has an empty Requirement", m.Name)
					}
					if m.Passed {
						t.Errorf("mutant %s (%s) survived the good tests:\n%s", m.Name, m.Requirement, m.Output)
					}
				}
				if err := report.err(); err != nil {
					t.Fatalf("report.err() = %v, want nil", err)
				}
			})
			t.Run("weak", func(t *testing.T) {
				t.Parallel()
				report, err := runMutants(t.Context(), c, authoredSelfTestFile(t, root, c, "weak"), t.TempDir())
				if err != nil {
					t.Fatalf("runMutants: harness error %v", err)
				}
				if !report.Baseline.Passed {
					t.Fatalf("weak tests fail the unmutated implementation:\n%s", report.Baseline.Output)
				}
				gateErr := report.err()
				if gateErr == nil {
					t.Fatal("report.err() = nil for the weak tests, want surviving mutants")
				}
				for _, m := range report.Mutants {
					if m.Passed && !strings.Contains(gateErr.Error(), m.Name) {
						t.Errorf("report.err() %q does not name surviving mutant %s", gateErr, m.Name)
					}
				}
			})
		})
	}
}

// M4 (harness): a mutant patch that does not apply is a harness error.
func TestAuthoredRunMutantsPatchFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test")
	}
	root := authoredRealRoot(t)
	cases, err := loadMutantCases(root)
	if err != nil {
		t.Fatalf("loadMutantCases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("loadMutantCases returned no cases")
	}
	c := cases[0]
	bogus := filepath.Join(t.TempDir(), "bogus.patch")
	patch := "R1: targets a file that does not exist.\n--- a/no/such/file.go\n+++ b/no/such/file.go\n@@ -1,1 +1,1 @@\n-package x\n+package y\n"
	if err := os.WriteFile(bogus, []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Mutants = []string{bogus}
	if _, err := runMutants(t.Context(), c, authoredSelfTestFile(t, root, c, "good"), t.TempDir()); err == nil {
		t.Fatal("runMutants with an unappliable patch: want a harness error, got nil")
	}
}
