package evals

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const authored2ContractRel = "testdata/contract/interface-contract.md"

// authored2Write creates path (and its parents) with the given content.
func authored2Write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// authored2Contract is the distinct contract text of a fake case.
func authored2Contract(name string) string {
	return "# Interface contract for fake case " + name + "\n\nR1 something only " + name + " requires.\n"
}

// authored2FakeCases makes one case directory per name under a fresh temp dir,
// each holding its own contract, and returns the cases in the given order.
func authored2FakeCases(t *testing.T, names ...string) []mutantCase {
	t.Helper()
	base := t.TempDir()
	var cases []mutantCase
	for _, n := range names {
		dir := filepath.Join(base, "cases", n)
		authored2Write(t, filepath.Join(dir, filepath.FromSlash(authored2ContractRel)), authored2Contract(n))
		cases = append(cases, mutantCase{
			Name:     n,
			Dir:      dir,
			Module:   "example.com/" + n,
			TestPath: n + "/" + n + "_test.go",
		})
	}
	return cases
}

// authored2Workspace makes a workspace for case name at root/rel (rel uses
// slashes) and returns its path.
func authored2Workspace(t *testing.T, root, rel, name string) string {
	t.Helper()
	ws := filepath.Join(root, filepath.FromSlash(rel))
	authored2Write(t, filepath.Join(ws, filepath.FromSlash(authored2ContractRel)), authored2Contract(name))
	return ws
}

// authored2Canon resolves symlinks (macOS temp dirs live under a symlink) so
// paths can be compared whichever form the implementation reports.
func authored2Canon(t *testing.T, root, p string) string {
	t.Helper()
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func authored2CanonAll(t *testing.T, root string, ps []string) []string {
	t.Helper()
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, authored2Canon(t, root, p))
	}
	return out
}

func authored2Names(targets []gateTarget) []string {
	var names []string
	for _, g := range targets {
		names = append(names, g.Case.Name)
	}
	return names
}

// authored2PlanFixture holds three cases: zeta-case and alpha-case each have
// workspaces at different depths under root, plus decoys that are not
// workspaces; lonely-case has only a decoy holding a prompt.md.
type authored2PlanFixture struct {
	cases []mutantCase // zeta-case, alpha-case, lonely-case (not sorted on purpose)
	root  string
	want  map[string][]string // sorted workspaces per case
}

func authored2NewPlanFixture(t *testing.T) authored2PlanFixture {
	t.Helper()
	cases := authored2FakeCases(t, "zeta-case", "alpha-case", "lonely-case")
	root := t.TempDir()
	want := map[string][]string{
		"zeta-case": {
			authored2Workspace(t, root, "run1/ws-z1", "zeta-case"),
			authored2Workspace(t, root, "run2/deep/nest/ws-z2", "zeta-case"),
		},
		"alpha-case": {
			authored2Workspace(t, root, "a-first", "alpha-case"),
			authored2Workspace(t, root, "run1/ws-a1", "alpha-case"),
			authored2Workspace(t, root, "run3/x/ws-a2", "alpha-case"),
		},
	}
	// Decoy (P6): holds an exact contract copy and a prompt.md.
	for _, n := range []string{"alpha-case", "lonely-case"} {
		d := authored2Workspace(t, root, "cases-copy/"+n, n)
		authored2Write(t, filepath.Join(d, "prompt.md"), "---\ntags: [test-engineer]\n---\nprompt\n")
	}
	// Decoy: a contract that differs by one byte from zeta-case's.
	authored2Write(t, filepath.Join(root, "near-miss", filepath.FromSlash(authored2ContractRel)), authored2Contract("zeta-case")+" ")
	// Decoy: the contract file at the wrong relative path.
	authored2Write(t, filepath.Join(root, "wrong-place", "interface-contract.md"), authored2Contract("alpha-case"))
	return authored2PlanFixture{cases: cases, root: root, want: want}
}

func (f authored2PlanFixture) pick(t *testing.T, names ...string) []mutantCase {
	t.Helper()
	var out []mutantCase
	for _, n := range names {
		found := false
		for _, c := range f.cases {
			if c.Name == n {
				out = append(out, c)
				found = true
			}
		}
		if !found {
			t.Fatalf("fixture has no case %q", n)
		}
	}
	return out
}

func authored2CheckTargets(t *testing.T, f authored2PlanFixture, got []gateTarget, wantCases []mutantCase) {
	t.Helper()
	var wantNames []string
	for _, c := range wantCases {
		wantNames = append(wantNames, c.Name)
	}
	if names := authored2Names(got); !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("planGate selected %v, want %v", names, wantNames)
	}
	for i, g := range got {
		if !reflect.DeepEqual(g.Case, wantCases[i]) {
			t.Errorf("target %d Case = %+v, want %+v", i, g.Case, wantCases[i])
		}
		if !authored2Sorted(g.Workspaces) {
			t.Errorf("target %s workspaces not sorted: %v", g.Case.Name, g.Workspaces)
		}
		gotWS := authored2CanonAll(t, f.root, g.Workspaces)
		wantWS := authored2CanonAll(t, f.root, f.want[g.Case.Name])
		if !reflect.DeepEqual(gotWS, wantWS) {
			t.Errorf("target %s workspaces = %v, want %v", g.Case.Name, gotWS, wantWS)
		}
	}
}

func authored2Sorted(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

// TestAuthored2FindWorkspaces covers P6 and the workspace rule planGate relies on.
func TestAuthored2FindWorkspaces(t *testing.T) {
	f := authored2NewPlanFixture(t)
	got, err := findWorkspaces(f.root, f.cases)
	if err != nil {
		t.Fatalf("findWorkspaces: %v", err)
	}
	for _, name := range []string{"zeta-case", "alpha-case"} {
		if !authored2Sorted(got[name]) {
			t.Errorf("findWorkspaces[%s] not sorted: %v", name, got[name])
		}
		gotWS := authored2CanonAll(t, f.root, got[name])
		wantWS := authored2CanonAll(t, f.root, f.want[name])
		if !reflect.DeepEqual(gotWS, wantWS) {
			t.Errorf("findWorkspaces[%s] = %v, want %v", name, gotWS, wantWS)
		}
	}
	if ws := got["lonely-case"]; len(ws) != 0 {
		t.Errorf("findWorkspaces[lonely-case] = %v, want none (its only copy sits beside a prompt.md)", ws)
	}
	for name, ws := range got {
		for _, w := range ws {
			if _, err := os.Stat(filepath.Join(authored2Canon(t, f.root, w), "prompt.md")); err == nil {
				t.Errorf("findWorkspaces[%s] includes %s, which holds a prompt.md", name, w)
			}
		}
	}
}

// TestAuthored2PlanGateAll covers P1.
func TestAuthored2PlanGateAll(t *testing.T) {
	f := authored2NewPlanFixture(t)
	cases := f.pick(t, "zeta-case", "alpha-case")
	got, err := planGate(cases, f.root, "")
	if err != nil {
		t.Fatalf("planGate(only=\"\"): %v", err)
	}
	authored2CheckTargets(t, f, got, cases)
}

// TestAuthored2PlanGateOnly covers P2.
func TestAuthored2PlanGateOnly(t *testing.T) {
	f := authored2NewPlanFixture(t)
	for _, tc := range []struct {
		only string
		want []string
	}{
		{" alpha-case , alpha-case,", []string{"alpha-case"}},
		{"alpha-case", []string{"alpha-case"}},
		{",zeta-case,,", []string{"zeta-case"}},
		{"zeta-case ,\talpha-case\t", []string{"zeta-case", "alpha-case"}},
		{"zeta-case,alpha-case,zeta-case", []string{"zeta-case", "alpha-case"}},
	} {
		t.Run(tc.only, func(t *testing.T) {
			// lonely-case has no workspace; it must not matter when not selected.
			got, err := planGate(f.cases, f.root, tc.only)
			if err != nil {
				t.Fatalf("planGate(only=%q): %v", tc.only, err)
			}
			authored2CheckTargets(t, f, got, f.pick(t, tc.want...))
		})
	}
}

// TestAuthored2PlanGateErrors covers P3, P4, P5 and P6.
func TestAuthored2PlanGateErrors(t *testing.T) {
	f := authored2NewPlanFixture(t)
	for _, tc := range []struct {
		name  string
		req   string
		cases []mutantCase
		only  string
		want  []string
	}{
		{"blank list", "P3", f.cases, " , ", []string{"names no case"}},
		{"commas only", "P3", f.cases, ",,", []string{"names no case"}},
		{"unknown name", "P4", f.cases, "alpha-case, nope-case", []string{"nope-case", "not a case with testdata/impl-mutants"}},
		{"selected without workspace", "P5/P6", f.cases, "lonely-case", []string{"no workspace for case", "lonely-case"}},
		{"all with one missing", "P5/P6", f.cases, "", []string{"no workspace for case", "lonely-case"}},
	} {
		t.Run(tc.req+"/"+tc.name, func(t *testing.T) {
			got, err := planGate(tc.cases, f.root, tc.only)
			if err == nil {
				t.Fatalf("planGate(only=%q) = %v, nil error; want an error containing %q", tc.only, authored2Names(got), tc.want)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("planGate(only=%q) error = %q, want it to contain %q", tc.only, err, w)
				}
			}
		})
	}
}

// TestAuthored2PlanGateNamesEveryMissingCase covers P5: two missing cases are
// both named in one error.
func TestAuthored2PlanGateNamesEveryMissingCase(t *testing.T) {
	cases := authored2FakeCases(t, "present-case", "missing-one", "missing-two")
	root := t.TempDir()
	authored2Workspace(t, root, "ws", "present-case")
	for _, only := range []string{"", "missing-one,present-case,missing-two"} {
		_, err := planGate(cases, root, only)
		if err == nil {
			t.Fatalf("planGate(only=%q): nil error, want one naming missing-one and missing-two", only)
		}
		for _, w := range []string{"no workspace for case", "missing-one", "missing-two"} {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("planGate(only=%q) error = %q, want it to contain %q", only, err, w)
			}
		}
		if strings.Contains(err.Error(), "present-case") {
			t.Errorf("planGate(only=%q) error = %q names present-case, which has a workspace", only, err)
		}
	}
}

func authored2RepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func authored2TTLCase(t *testing.T) mutantCase {
	t.Helper()
	cases, err := loadMutantCases(authored2RepoRoot(t))
	if err != nil {
		t.Fatalf("loadMutantCases: %v", err)
	}
	var names []string
	for _, c := range cases {
		names = append(names, c.Name)
	}
	if want := []string{"clamp-claude-md-probe", "notes-init-e2e", "retry-budget-matrix", "ttl-cache-matrix"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("loadMutantCases names = %v, want %v", names, want)
	}
	// The names above are sorted, so ttl-cache-matrix is the last case.
	c := cases[len(cases)-1]
	if c.Name != "ttl-cache-matrix" {
		t.Fatalf("last mutant case = %q, want ttl-cache-matrix", c.Name)
	}
	if c.TestPath != "cache/cache_test.go" {
		t.Fatalf("ttl-cache-matrix TestPath = %q, want %q", c.TestPath, "cache/cache_test.go")
	}
	if len(c.Mutants) == 0 {
		t.Fatalf("ttl-cache-matrix has no mutants")
	}
	return c
}

// TestAuthored2GateWorkspaceMissingTest covers W1.
func TestAuthored2GateWorkspaceMissingTest(t *testing.T) {
	c := authored2TTLCase(t)
	ws := filepath.Join(t.TempDir(), "workspace-without-test")
	// A real-looking workspace: the contract copy is there, the test file is not.
	contract, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(authored2ContractRel)))
	if err != nil {
		t.Fatal(err)
	}
	authored2Write(t, filepath.Join(ws, filepath.FromSlash(authored2ContractRel)), string(contract))
	_, err = gateWorkspace(context.Background(), c, ws, t.TempDir())
	if err == nil {
		t.Fatalf("gateWorkspace on a workspace without %s: nil error", c.TestPath)
	}
	for _, w := range []string{ws, c.TestPath} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("gateWorkspace error = %q, want it to contain %q", err, w)
		}
	}
}

// authored2SelftestWorkspace copies the selftest fixture of the given kind
// ("good" or "weak") to c.TestPath in a fresh workspace.
func authored2SelftestWorkspace(t *testing.T, c mutantCase, kind string) string {
	t.Helper()
	src := filepath.Join(authored2RepoRoot(t), "evals", "testdata", "mutant-selftest", c.Name, kind, filepath.FromSlash(c.TestPath))
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(t.TempDir(), kind)
	authored2Write(t, filepath.Join(ws, filepath.FromSlash(c.TestPath)), string(data))
	return ws
}

// TestAuthored2GateWorkspaceSelftest covers W2.
func TestAuthored2GateWorkspaceSelftest(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on the fixture and every mutant")
	}
	c := authored2TTLCase(t)

	t.Run("good", func(t *testing.T) {
		ws := authored2SelftestWorkspace(t, c, "good")
		rep, err := gateWorkspace(t.Context(), c, ws, t.TempDir())
		if err != nil {
			t.Fatalf("gateWorkspace(good): %v", err)
		}
		if gerr := rep.err(); gerr != nil {
			t.Fatalf("known-good report err() = %v, want nil\nbaseline output:\n%s", gerr, rep.Baseline.Output)
		}
		if !rep.Baseline.Passed {
			t.Errorf("known-good baseline did not pass:\n%s", rep.Baseline.Output)
		}
		if len(rep.Mutants) != len(c.Mutants) {
			t.Errorf("known-good report has %d mutant outcomes, want %d", len(rep.Mutants), len(c.Mutants))
		}
		for _, m := range rep.Mutants {
			if m.Passed {
				t.Errorf("mutant %s survived the known-good test", m.Name)
			}
		}
	})

	t.Run("weak", func(t *testing.T) {
		ws := authored2SelftestWorkspace(t, c, "weak")
		rep, err := gateWorkspace(t.Context(), c, ws, t.TempDir())
		if err != nil {
			t.Fatalf("gateWorkspace(weak): %v", err)
		}
		if rep.err() == nil {
			t.Fatalf("known-weak report err() = nil, want an error (some mutant survives)")
		}
		if !rep.Baseline.Passed {
			t.Errorf("known-weak baseline did not pass, so err() is non-nil for the wrong reason:\n%s", rep.Baseline.Output)
		}
		survived := false
		for _, m := range rep.Mutants {
			if m.Passed {
				survived = true
			}
		}
		if !survived {
			t.Errorf("no mutant survived the known-weak test")
		}
	})
}

// TestAuthored2MutantReportErr checks the err() rule gateWorkspace's report
// relies on (W2): nil iff the baseline passed and every mutant failed.
func TestAuthored2MutantReportErr(t *testing.T) {
	pass := runOutcome{Passed: true, Output: "ok"}
	fail := runOutcome{Passed: false, Output: "FAIL"}
	killed := func(n string) mutantOutcome { return mutantOutcome{Name: n, Requirement: "R1", runOutcome: fail} }
	survived := func(n string) mutantOutcome { return mutantOutcome{Name: n, Requirement: "R2", runOutcome: pass} }
	for _, tc := range []struct {
		name    string
		rep     mutantReport
		wantNil bool
	}{
		{"all killed", mutantReport{Case: "c", Baseline: pass, Mutants: []mutantOutcome{killed("m1"), killed("m2")}}, true},
		{"one survives", mutantReport{Case: "c", Baseline: pass, Mutants: []mutantOutcome{killed("m1"), survived("m2")}}, false},
		{"baseline fails", mutantReport{Case: "c", Baseline: fail, Mutants: []mutantOutcome{killed("m1"), killed("m2")}}, false},
		{"baseline fails and survivor", mutantReport{Case: "c", Baseline: fail, Mutants: []mutantOutcome{survived("m1")}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rep.err(); (got == nil) != tc.wantNil {
				t.Fatalf("err() = %v, want nil: %v", got, tc.wantNil)
			}
		})
	}
}
