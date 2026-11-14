package evals

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Authored tests for planAdversarialGate and checkAdversarialWorkspace
// (adversarial-gate revision 2, P1 and P2). A workspace is a directory under
// root, found without descending into testdata, .git or node_modules, that
// holds a byte-identical copy of the case's
// testdata/contract/interface-contract.md and no prompt.md.

// planContract is a distinct interface contract for synthetic case name.
func planContract(name string) string {
	return "# Interface contract: " + name + "\n\nModule `example.com/" + name + "`.\n\n- R1: synthetic.\n"
}

// planCases writes one synthetic case directory per name under a temporary
// directory and returns the cases, holding only what planning reads.
func planCases(t *testing.T, names ...string) []adversarialCase {
	t.Helper()
	dir := t.TempDir()
	var cases []adversarialCase
	for _, name := range names {
		caseDir := filepath.Join(dir, name)
		authoredWriteFiles(t, caseDir, map[string]string{
			"testdata/contract/interface-contract.md": planContract(name),
			"prompt.md": "---\ndescription: Synthetic.\n---\nBrief.\n",
		})
		cases = append(cases, adversarialCase{Name: name, Dir: caseDir, KeptPath: advAuthoredKept})
	}
	return cases
}

// planWorkspace makes rel under root a workspace copy of case name: its
// contract copy plus extra files.
func planWorkspace(t *testing.T, root, rel, name string, extra map[string]string) {
	t.Helper()
	files := map[string]string{"testdata/contract/interface-contract.md": planContract(name)}
	for k, v := range extra {
		files[k] = v
	}
	authoredWriteFiles(t, filepath.Join(root, filepath.FromSlash(rel)), files)
}

// planRel maps a returned workspace to a slash path relative to root,
// accepting either a root-joined or a root-relative result.
func planRel(t *testing.T, root, ws string) string {
	t.Helper()
	if !filepath.IsAbs(ws) {
		return filepath.ToSlash(ws)
	}
	rel, err := filepath.Rel(root, ws)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

// planGot returns, by case name, the workspaces of each target relative to
// root; it fails on a target listed twice, an empty or unsorted workspace list.
func planGot(t *testing.T, root string, targets []adversarialTarget) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	for _, tg := range targets {
		if _, dup := got[tg.Case.Name]; dup {
			t.Fatalf("case %s is planned twice: %+v", tg.Case.Name, targets)
		}
		if len(tg.Workspaces) == 0 {
			t.Fatalf("case %s is planned with no workspace", tg.Case.Name)
		}
		if !slices.IsSorted(tg.Workspaces) {
			t.Fatalf("case %s workspaces are not sorted: %q", tg.Case.Name, tg.Workspaces)
		}
		var rels []string
		for _, ws := range tg.Workspaces {
			rels = append(rels, planRel(t, root, ws))
		}
		got[tg.Case.Name] = rels
	}
	return got
}

// planWant fails unless got holds exactly want (sorted relative paths).
func planWant(t *testing.T, got, want map[string][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("planned cases %v, want %v", got, want)
	}
	for name, ws := range want {
		if !slices.Equal(got[name], ws) {
			t.Fatalf("case %s workspaces = %q, want %q (all: %v)", name, got[name], ws, got)
		}
	}
}

// P1: every case with workspaces is planned, each with its workspaces sorted,
// and nothing that is not a workspace is counted. Fails when a workspace is
// missed or listed out of order, when the search descends into testdata, .git
// or node_modules, or when a directory with a prompt.md (a case copy) or a
// contract that differs by a byte counts as a workspace.
func TestAuthoredPlanAdversarialGateFindsWorkspaces(t *testing.T) {
	cases := planCases(t, "alpha", "beta")
	root := t.TempDir()
	// Created out of order and at different depths.
	planWorkspace(t, root, "run-2/alpha", "alpha", nil)
	planWorkspace(t, root, "run-1/deep/nested/alpha", "alpha", map[string]string{"artefacts/adversarial_test.go": "package x\n"})
	planWorkspace(t, root, "run-1/beta", "beta", nil)
	// Not workspaces.
	planWorkspace(t, root, "run-1/case-copy", "alpha", map[string]string{"prompt.md": "brief\n"})
	planWorkspace(t, root, "run-1/testdata/alpha", "alpha", nil)
	planWorkspace(t, root, "run-1/.git/alpha", "alpha", nil)
	planWorkspace(t, root, "node_modules/alpha", "alpha", nil)
	authoredWriteFiles(t, filepath.Join(root, "run-3", "near-miss"), map[string]string{
		"testdata/contract/interface-contract.md": planContract("alpha") + " ",
	})

	targets, err := planAdversarialGate(cases, root, "")
	if err != nil {
		t.Fatalf("planAdversarialGate: %v", err)
	}
	planWant(t, planGot(t, root, targets), map[string][]string{
		"alpha": {"run-1/deep/nested/alpha", "run-2/alpha"},
		"beta":  {"run-1/beta"},
	})
}

// P1: a directory that holds the contract copy and a prompt.md is not a
// workspace, so a case whose only candidate is such a directory fails. Fails
// when the gate would check the case's own directory or a copy of it.
func TestAuthoredPlanAdversarialGatePromptIsNotWorkspace(t *testing.T) {
	cases := planCases(t, "alpha")
	root := t.TempDir()
	planWorkspace(t, root, "run-1/alpha", "alpha", map[string]string{"prompt.md": "brief\n"})
	if targets, err := planAdversarialGate(cases, root, ""); err == nil {
		t.Fatalf("planAdversarialGate = %+v, nil; want an error: the only candidate holds prompt.md", targets)
	} else if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("error %q does not name case alpha", err)
	}
}

// P1: failures that would let a gate that checks nothing pass. Fails when any
// of them returns a nil error.
func TestAuthoredPlanAdversarialGateRejects(t *testing.T) {
	t.Run("empty root", func(t *testing.T) {
		if targets, err := planAdversarialGate(planCases(t, "alpha", "beta"), t.TempDir(), ""); err == nil {
			t.Fatalf("planAdversarialGate on an empty root = %+v, nil; want an error", targets)
		}
	})
	t.Run("case without a workspace is named", func(t *testing.T) {
		root := t.TempDir()
		planWorkspace(t, root, "run-1/alpha", "alpha", nil)
		_, err := planAdversarialGate(planCases(t, "alpha", "beta"), root, "")
		if err == nil {
			t.Fatal("planAdversarialGate = nil error; want one naming beta, which has no workspace")
		}
		if !strings.Contains(err.Error(), "beta") {
			t.Fatalf("error %q does not name case beta", err)
		}
	})
	t.Run("unknown name in only", func(t *testing.T) {
		root := t.TempDir()
		planWorkspace(t, root, "run-1/alpha", "alpha", nil)
		for _, only := range []string{"gamma", "alpha,gamma"} {
			if targets, err := planAdversarialGate(planCases(t, "alpha"), root, only); err == nil {
				t.Errorf("only %q: planAdversarialGate = %+v, nil; want an error for the unknown name", only, targets)
			}
		}
	})
	t.Run("only selects nothing", func(t *testing.T) {
		root := t.TempDir()
		planWorkspace(t, root, "run-1/alpha", "alpha", nil)
		for _, only := range []string{" , ", ",", " ,, , "} {
			if targets, err := planAdversarialGate(planCases(t, "alpha"), root, only); err == nil {
				t.Errorf("only %q: planAdversarialGate = %+v, nil; want an error", only, targets)
			}
		}
	})
	t.Run("no cases", func(t *testing.T) {
		root := t.TempDir()
		planWorkspace(t, root, "run-1/alpha", "alpha", nil)
		for _, cases := range [][]adversarialCase{nil, {}} {
			if targets, err := planAdversarialGate(cases, root, ""); err == nil {
				t.Errorf("planAdversarialGate(%v) = %+v, nil; want an error", cases, targets)
			}
		}
	})
}

// P1: only is trimmed, empty entries and duplicates are ignored, and an
// unselected case needs no workspace. Fails when a case is planned twice, a
// spaced name is not recognised, or an unselected case without a workspace
// fails the plan.
func TestAuthoredPlanAdversarialGateOnly(t *testing.T) {
	cases := planCases(t, "alpha", "beta", "gamma")
	root := t.TempDir()
	planWorkspace(t, root, "run-1/alpha", "alpha", nil)
	planWorkspace(t, root, "run-1/beta", "beta", nil)
	// gamma has no workspace and is never selected below.
	for _, test := range []struct {
		only string
		want map[string][]string
	}{
		{" alpha , alpha,, beta ,alpha", map[string][]string{"alpha": {"run-1/alpha"}, "beta": {"run-1/beta"}}},
		{"beta", map[string][]string{"beta": {"run-1/beta"}}},
		{" alpha ,", map[string][]string{"alpha": {"run-1/alpha"}}},
	} {
		t.Run(test.only, func(t *testing.T) {
			targets, err := planAdversarialGate(cases, root, test.only)
			if err != nil {
				t.Fatalf("planAdversarialGate(only %q): %v", test.only, err)
			}
			planWant(t, planGot(t, root, targets), test.want)
		})
	}
	t.Run("empty only selects every case", func(t *testing.T) {
		if targets, err := planAdversarialGate(cases, root, ""); err == nil {
			t.Fatalf("planAdversarialGate = %+v, nil; want an error naming gamma, which has no workspace", targets)
		} else if !strings.Contains(err.Error(), "gamma") {
			t.Fatalf("error %q does not name gamma", err)
		}
	})
}

// P1 (idempotency): planning twice gives the same targets and leaves root
// unchanged. Fails when planning depends on or changes state.
func TestAuthoredPlanAdversarialGateRepeat(t *testing.T) {
	cases := planCases(t, "alpha", "beta")
	root := t.TempDir()
	planWorkspace(t, root, "b/alpha", "alpha", nil)
	planWorkspace(t, root, "a/alpha", "alpha", nil)
	planWorkspace(t, root, "c/beta", "beta", nil)
	before := advAuthoredSnapshot(t, root)
	first, err := planAdversarialGate(cases, root, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := planAdversarialGate(cases, root, "")
	if err != nil {
		t.Fatal(err)
	}
	planWant(t, planGot(t, root, second), planGot(t, root, first))
	after := advAuthoredSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("planning changed root: %d files, before %d", len(after), len(before))
	}
	for p, body := range before {
		if after[p] != body {
			t.Fatalf("planning changed %s", p)
		}
	}
}

// planKeptWorkspace makes a workspace for real case c whose kept file at
// c.KeptPath holds kept; a nil kept leaves the file out.
func planKeptWorkspace(t *testing.T, c adversarialCase, kept []byte) string {
	t.Helper()
	ws := t.TempDir()
	contract, err := os.ReadFile(filepath.Join(c.Dir, "testdata", "contract", "interface-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"testdata/contract/interface-contract.md": string(contract)}
	if kept != nil {
		files[c.KeptPath] = string(kept)
	}
	authoredWriteFiles(t, ws, files)
	return ws
}

// P2: a workspace without the kept file fails, naming the workspace. Fails
// when a run that kept no tests passes the gate.
func TestAuthoredCheckAdversarialWorkspaceMissingKept(t *testing.T) {
	root := authoredRealRoot(t)
	for _, c := range advAuthoredRealCases(t, root) {
		t.Run(c.Name, func(t *testing.T) {
			ws := planKeptWorkspace(t, c, nil)
			_, err := checkAdversarialWorkspace(t.Context(), c, ws, t.TempDir())
			if err == nil {
				t.Fatal("checkAdversarialWorkspace = nil error; want one for the missing kept file")
			}
			if !strings.Contains(err.Error(), ws) {
				t.Fatalf("error %q does not name the workspace %s", err, ws)
			}
		})
	}
}

// P2: the known-good kept file passes; a kept file with a test that already
// passes on the seeded source and one that fails for a reason fix.patch does
// not repair both fail, naming the workspace. Fails when the gate's verdict
// is lost between runAdversarial's report and the returned error.
func TestAuthoredCheckAdversarialWorkspaceVerdict(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test against every case")
	}
	root := authoredRealRoot(t)
	for _, c := range advAuthoredRealCases(t, root) {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			t.Run("good", func(t *testing.T) {
				t.Parallel()
				ws := planKeptWorkspace(t, c, advAuthoredFixture(t, root, c.Name, "good_test.go"))
				r, err := checkAdversarialWorkspace(t.Context(), c, ws, t.TempDir())
				if err != nil {
					t.Fatalf("checkAdversarialWorkspace(good kept file) = %v, want nil", err)
				}
				if r.Case != c.Name || len(r.Seeded) == 0 || !r.Fixed.Passed {
					t.Fatalf("report = %+v, want case %s with seeded outcomes and a passing fixed run", r, c.Name)
				}
			})
			for _, file := range []string{"extra-passing_test.go", "unrelated-fail_test.go"} {
				t.Run(strings.TrimSuffix(file, "_test.go"), func(t *testing.T) {
					t.Parallel()
					ws := planKeptWorkspace(t, c, advAuthoredFixture(t, root, c.Name, file))
					_, err := checkAdversarialWorkspace(t.Context(), c, ws, t.TempDir())
					if err == nil {
						t.Fatalf("checkAdversarialWorkspace(%s) = nil error; want the gate to fail", file)
					}
					if !strings.Contains(err.Error(), ws) {
						t.Fatalf("error %q does not name the workspace %s", err, ws)
					}
				})
			}
		})
	}
}
