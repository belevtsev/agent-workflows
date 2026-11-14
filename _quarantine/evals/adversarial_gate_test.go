package evals

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// TestAdversarialGate runs the tests an adversarial-tester agent kept in a
// finished eval run. Each kept test must fail when run alone on the case's
// seeded source, and the whole package (kept and black-box tests) must pass
// once the case's fix.patch repairs the seeded defect. A kept test that
// passes on the seeded source breaks the agent's must-fail rule; a package
// that still fails after the fix means a kept test fails for some other
// reason.
//
// It is skipped unless EVAL_ARTEFACTS_DIR names a directory holding the run
// workspaces (kept with `claude plugin eval --keep-temp`); EVAL_CASES, a
// comma-separated list of case names, limits it to the cases that ran. Every
// selected case must have at least one workspace.
func TestAdversarialGate(t *testing.T) {
	root := os.Getenv("EVAL_ARTEFACTS_DIR")
	if root == "" {
		t.Skip("EVAL_ARTEFACTS_DIR is not set; it names the kept workspaces of a finished adversarial-tester run")
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cases, err := loadAdversarialCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := planAdversarialGate(cases, root, os.Getenv("EVAL_CASES"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		t.Run(target.Case.Name, func(t *testing.T) {
			t.Parallel()
			for i, workspace := range target.Workspaces {
				t.Run(strconv.Itoa(i+1), func(t *testing.T) {
					report, err := checkAdversarialWorkspace(t.Context(), target.Case, workspace, t.TempDir())
					if len(report.Seeded) > 0 {
						t.Log(report.describe())
					}
					if err != nil {
						t.Error(err)
					}
				})
			}
		})
	}
}

type adversarialTarget struct {
	Case       adversarialCase
	Workspaces []string // sorted, non-empty
}

// planAdversarialGate selects the cases to gate and finds their workspaces
// under root. only is EVAL_CASES: comma-separated case names, trimmed, with
// empty entries and duplicates ignored; empty selects every case. It fails
// when only names no case or an unknown one, when no case is selected, or
// when a selected case has no workspace, so a gate that would check nothing
// never passes.
func planAdversarialGate(cases []adversarialCase, root, only string) ([]adversarialTarget, error) {
	if only != "" {
		var names []string
		for name := range strings.SplitSeq(only, ",") {
			if name = strings.TrimSpace(name); name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("EVAL_CASES %q names no case; unset it to gate every case", only)
		}
		for _, name := range names {
			if !slices.ContainsFunc(cases, func(c adversarialCase) bool { return c.Name == name }) {
				return nil, fmt.Errorf("EVAL_CASES names %q, which is not a case with a fix.patch", name)
			}
		}
		cases = slices.DeleteFunc(slices.Clone(cases), func(c adversarialCase) bool { return !slices.Contains(names, c.Name) })
	}
	if len(cases) == 0 {
		return nil, errors.New("no case with a fix.patch to gate")
	}
	byContract := make([]mutantCase, 0, len(cases))
	for _, c := range cases {
		byContract = append(byContract, mutantCase{Name: c.Name, Dir: c.Dir})
	}
	workspaces, err := findWorkspaces(root, byContract)
	if err != nil {
		return nil, err
	}
	var targets []adversarialTarget
	var missing []string
	for _, c := range cases {
		if len(workspaces[c.Name]) == 0 {
			missing = append(missing, c.Name)
			continue
		}
		targets = append(targets, adversarialTarget{Case: c, Workspaces: workspaces[c.Name]})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no workspace for case %s under %s: the run kept none, or the case did not run", strings.Join(missing, ", "), root)
	}
	return targets, nil
}

// checkAdversarialWorkspace gates one workspace: it reads the kept tests at
// the case's owned path, runs them with runAdversarial in workDir and returns
// the report with the verdict. The error is nil only when the kept file
// exists, runAdversarial succeeds and the report's err is nil; it names the
// workspace.
func checkAdversarialWorkspace(ctx context.Context, c adversarialCase, workspace, workDir string) (adversarialReport, error) {
	kept, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(c.KeptPath)))
	if err != nil {
		return adversarialReport{Case: c.Name}, fmt.Errorf("workspace %s: the agent kept no tests at its owned path %s: %w", workspace, c.KeptPath, err)
	}
	report, err := runAdversarial(ctx, c, kept, workDir)
	if err != nil {
		return report, fmt.Errorf("workspace %s: %w", workspace, err)
	}
	if err := report.err(); err != nil {
		return report, fmt.Errorf("workspace %s: %w", workspace, err)
	}
	return report, nil
}

// TestAdversarialFixtures keeps each case's fix.patch in step with its seeded
// source: the black-box tests pass on the seeded source and again after the
// patch, which applies exactly.
func TestAdversarialFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("builds each case twice")
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cases, err := loadAdversarialCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no eval case has a fix.patch")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			seeded := filepath.Join(t.TempDir(), "seeded")
			if err := buildAdversarialModule(c, seeded, nil); err != nil {
				t.Fatal(err)
			}
			if out := goTest(t.Context(), seeded); !out.Passed {
				t.Errorf("black-box tests fail on the seeded source:\n%s", out.Output)
			}
			fixed := filepath.Join(t.TempDir(), "fixed")
			if err := buildAdversarialModule(c, fixed, nil); err != nil {
				t.Fatal(err)
			}
			patch, err := os.ReadFile(c.Fix)
			if err != nil {
				t.Fatal(err)
			}
			if err := applyPatch(fixed, patch); err != nil {
				t.Fatalf("fix.patch: %v", err)
			}
			if out := goTest(t.Context(), fixed); !out.Passed {
				t.Errorf("black-box tests fail after fix.patch:\n%s", out.Output)
			}
		})
	}
}

// adversarialCase is one eval case with a fix.patch at its root.
type adversarialCase struct {
	Name     string // case directory base name
	Dir      string // absolute case directory
	Module   string // module path the interface contract names
	KeptPath string // slash path of the brief's owned *_test.go
	Fix      string // absolute path of the case's fix.patch
}

// loadAdversarialCases returns, sorted by name, every case under
// <repoRoot>/evals/ that has a fix.patch at its root. The kept-test path is
// the one *_test.go the brief's "Owned paths:" bullet names under artefacts/;
// a path the bullet mentions elsewhere, such as where the kept file belongs
// in the source, is not owned.
func loadAdversarialCases(repoRoot string) ([]adversarialCase, error) {
	dirs, err := findCases(filepath.Join(repoRoot, "evals"))
	if err != nil {
		return nil, err
	}
	var cases []adversarialCase
	for _, dir := range dirs {
		fix := filepath.Join(dir, "fix.patch")
		if info, err := os.Stat(fix); err != nil || !info.Mode().IsRegular() {
			continue
		}
		c := adversarialCase{Name: filepath.Base(dir), Dir: dir, Fix: fix}
		contract, err := os.ReadFile(filepath.Join(dir, "testdata", "contract", "interface-contract.md"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		m := contractModule.FindSubmatch(contract)
		if m == nil {
			return nil, fmt.Errorf("%s: interface-contract.md names no Module `...`", c.Name)
		}
		c.Module = string(m[1])
		prompt, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		var kept []string
		for _, p := range briefOwnedPaths(string(prompt)) {
			if strings.HasPrefix(p, "artefacts/") && strings.HasSuffix(p, "_test.go") && !slices.Contains(kept, p) {
				kept = append(kept, p)
			}
		}
		if len(kept) != 1 {
			return nil, fmt.Errorf("%s: the brief must own exactly one artefacts/*_test.go for the kept tests, found %v", c.Name, kept)
		}
		c.KeptPath = kept[0]
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

// anyOwnedBullet matches a brief bullet that names its owned paths, whether
// the bullet starts with "Owned paths:" or carries it after another item, as
// in "- Artefact directory: `artefacts/`. Owned paths: ...".
var anyOwnedBullet = regexp.MustCompile(`^\s*-\s.*\bOwned paths:`)

// briefOwnedPaths returns the backticked paths after "Owned paths:" in the
// brief's bullet that names them, nil when the brief has none.
func briefOwnedPaths(body string) []string {
	text := bulletText(body, anyOwnedBullet)
	_, after, ok := strings.Cut(text, "Owned paths:")
	if !ok {
		return nil
	}
	var owned []string
	for _, m := range backticked.FindAllStringSubmatch(after, -1) {
		if pathLike.MatchString(m[1]) && !strings.ContainsAny(m[1], " ") {
			owned = append(owned, m[1])
		}
	}
	return owned
}

// keptTestNames returns, in source order, the names of the top-level
// functions in src of the form `func TestXxx(t *testing.T)` (one parameter
// of type *testing.T, no receiver, no type parameters, name "Test" followed
// by nothing or by a character that is not a lower-case letter). Methods,
// TestMain, Benchmark/Fuzz/Example functions and helpers are not listed.
// Source that does not parse as Go is an error.
func keptTestNames(src []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "adversarial_test.go", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Type.TypeParams != nil || !isTestName(fn.Name.Name) {
			continue
		}
		if params := fn.Type.Params.List; len(params) == 1 && len(params[0].Names) <= 1 && isTestingT(params[0].Type) {
			names = append(names, fn.Name.Name)
		}
	}
	return names, nil
}

func isTestName(name string) bool {
	rest, ok := strings.CutPrefix(name, "Test")
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(r)
}

func isTestingT(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

type keptOutcome struct {
	Name string // kept test function name
	runOutcome
}

type adversarialReport struct {
	Case   string
	Seeded []keptOutcome // one per kept test, in source order, run alone on the seeded source
	Fixed  runOutcome    // whole package, kept and black-box tests, after fix.patch
	// FixedRan is true once the post-fix package run happened. It stays false
	// when runAdversarial stops earlier, for example because fix.patch does
	// not apply, and then Fixed is the zero value, not a real result.
	FixedRan bool
}

// err is nil only when Seeded is non-empty, every Seeded entry failed and
// Fixed passed.
func (r adversarialReport) err() error {
	var problems []string
	if len(r.Seeded) == 0 {
		problems = append(problems, "no kept test was run")
	}
	var passed []string
	for _, s := range r.Seeded {
		if s.Passed {
			passed = append(passed, s.Name)
		}
	}
	if len(passed) > 0 {
		problems = append(problems, "kept tests pass on the seeded source, so they show no defect: "+strings.Join(passed, ", "))
	}
	if !r.Fixed.Passed {
		problems = append(problems, "the package fails after fix.patch, so a kept test fails for another reason than the seeded defect")
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", r.Case, strings.Join(problems, "; "))
}

func (r adversarialReport) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:", r.Case)
	for _, s := range r.Seeded {
		state := "fails on the seeded source"
		if s.Passed {
			state = "PASSES on the seeded source"
		}
		fmt.Fprintf(&b, "\n  %s %s", s.Name, state)
	}
	if !r.FixedRan {
		return b.String()
	}
	state := "passes"
	if !r.Fixed.Passed {
		state = "FAILS"
	}
	fmt.Fprintf(&b, "\n  package after fix.patch %s", state)
	if !r.Fixed.Passed {
		fmt.Fprintf(&b, ":\n%s", r.Fixed.Output)
	}
	return b.String()
}

// runAdversarial builds two throwaway modules under workDir from c's
// testdata/work plus a go.mod for c.Module, with kept written at the module
// root as adversarial_test.go. In the seeded module it runs each kept test
// alone; in the second it applies c.Fix and runs the whole package once. It
// returns an error when kept does not parse, names no test, or fix.patch does
// not apply.
func runAdversarial(ctx context.Context, c adversarialCase, kept []byte, workDir string) (adversarialReport, error) {
	report := adversarialReport{Case: c.Name}
	names, err := keptTestNames(kept)
	if err != nil {
		return report, fmt.Errorf("kept tests do not parse: %w", err)
	}
	if len(names) == 0 {
		return report, errors.New("the kept file has no func TestXxx(t *testing.T)")
	}
	seeded := filepath.Join(workDir, "seeded")
	if err := buildAdversarialModule(c, seeded, kept); err != nil {
		return report, err
	}
	for _, name := range names {
		out, err := goCommand(ctx, seeded, "test", "-count=1", "-timeout=120s", "-run", "^"+name+"$", ".")
		report.Seeded = append(report.Seeded, keptOutcome{Name: name, runOutcome: trimmedOutcome(out, err)})
	}
	fixed := filepath.Join(workDir, "fixed")
	if err := buildAdversarialModule(c, fixed, kept); err != nil {
		return report, err
	}
	patch, err := os.ReadFile(c.Fix)
	if err != nil {
		return report, err
	}
	if err := applyPatch(fixed, patch); err != nil {
		return report, fmt.Errorf("fix.patch: %w", err)
	}
	report.Fixed = goTest(ctx, fixed)
	report.FixedRan = true
	return report, nil
}

func trimmedOutcome(out []byte, err error) runOutcome {
	const keep = 6000
	if len(out) > keep {
		out = append([]byte("...\n"), out[len(out)-keep:]...)
	}
	return runOutcome{Passed: err == nil, Output: string(out)}
}

// buildAdversarialModule lays out a module in dir: the case's testdata/work
// tree, a go.mod for its module and, when kept is not nil, kept as
// adversarial_test.go at the module root.
func buildAdversarialModule(c adversarialCase, dir string, kept []byte) error {
	work := filepath.Join(c.Dir, "testdata", "work")
	err := filepath.WalkDir(work, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(work, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dir, rel), data)
	})
	if err != nil {
		return fmt.Errorf("copy %s source: %w", c.Name, err)
	}
	version := toolchainLanguage.FindStringSubmatch(runtime.Version())
	if version == nil {
		return fmt.Errorf("cannot derive a go.mod language version from toolchain %q", runtime.Version())
	}
	if err := writeFile(filepath.Join(dir, "go.mod"), []byte("module "+c.Module+"\n\ngo "+version[1]+"\n")); err != nil {
		return err
	}
	if kept != nil {
		return writeFile(filepath.Join(dir, "adversarial_test.go"), kept)
	}
	return nil
}
