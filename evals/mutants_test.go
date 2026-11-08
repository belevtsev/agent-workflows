package evals

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestMutantGate runs each test-engineer agent's authored tests from a
// finished eval run against its case's implementation fixture and every
// mutant in testdata/impl-mutants. It fails when the authored tests do not
// pass on the fixture, or still all pass with some mutant applied.
//
// It is skipped unless EVAL_ARTEFACTS_DIR names a directory holding the run
// workspaces (kept with `claude plugin eval --keep-temp`); EVAL_CASES, a
// comma-separated list of case names, limits it to the cases that ran. Every
// selected case must have at least one workspace.
func TestMutantGate(t *testing.T) {
	root := os.Getenv("EVAL_ARTEFACTS_DIR")
	if root == "" {
		t.Skip("EVAL_ARTEFACTS_DIR is not set; it names the kept workspaces of a finished test-engineer run")
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cases, err := loadMutantCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := planGate(cases, root, os.Getenv("EVAL_CASES"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		t.Run(target.Case.Name, func(t *testing.T) {
			t.Parallel()
			for i, workspace := range target.Workspaces {
				t.Run(strconv.Itoa(i+1), func(t *testing.T) {
					report, err := gateWorkspace(t.Context(), target.Case, workspace, t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					t.Log(report.describe())
					if err := report.err(); err != nil {
						t.Errorf("workspace %s: %v", workspace, err)
					}
				})
			}
		})
	}
}

type gateTarget struct {
	Case       mutantCase
	Workspaces []string // sorted, non-empty
}

// planGate selects the cases to gate and finds their workspaces under root.
// only is EVAL_CASES: comma-separated case names, trimmed, with empty entries
// and duplicates ignored; empty selects every case. It fails when only names
// no case or an unknown one, or when a selected case has no workspace, so a
// gate that would check nothing never passes.
func planGate(cases []mutantCase, root, only string) ([]gateTarget, error) {
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
			if !slices.ContainsFunc(cases, func(c mutantCase) bool { return c.Name == name }) {
				return nil, fmt.Errorf("EVAL_CASES names %q, which is not a case with testdata/impl-mutants", name)
			}
		}
		cases = slices.DeleteFunc(slices.Clone(cases), func(c mutantCase) bool { return !slices.Contains(names, c.Name) })
	}
	workspaces, err := findWorkspaces(root, cases)
	if err != nil {
		return nil, err
	}
	var targets []gateTarget
	var missing []string
	for _, c := range cases {
		if len(workspaces[c.Name]) == 0 {
			missing = append(missing, c.Name)
			continue
		}
		targets = append(targets, gateTarget{Case: c, Workspaces: workspaces[c.Name]})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no workspace for case %s under %s: the run kept none, or the case did not run", strings.Join(missing, ", "), root)
	}
	return targets, nil
}

// gateWorkspace runs the test the agent wrote at its owned path in workspace
// against c's fixture and mutants, in workDir.
func gateWorkspace(ctx context.Context, c mutantCase, workspace, workDir string) (mutantReport, error) {
	testFile, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(c.TestPath)))
	if err != nil {
		return mutantReport{Case: c.Name}, fmt.Errorf("workspace %s: the agent wrote no test at its owned path %s: %w", workspace, c.TestPath, err)
	}
	report, err := runMutants(ctx, c, testFile, workDir)
	if err != nil {
		return report, fmt.Errorf("workspace %s: %w", workspace, err)
	}
	return report, nil
}

// TestMutantFixtures keeps the mutants in step with their fixtures: each
// test-engineer case has two or three, each names the requirement it breaks
// and applies cleanly, and the fixture builds and vets with and without it.
func TestMutantFixtures(t *testing.T) {
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cases, err := loadMutantCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	all, err := findCases(filepath.Join(repo, "evals"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range all {
		if filepath.Base(filepath.Dir(dir)) == "test-engineer" && !slices.ContainsFunc(cases, func(c mutantCase) bool { return c.Dir == dir }) {
			t.Errorf("%s: test-engineer case has no testdata/impl-mutants", dir)
		}
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			if n := len(c.Mutants); n < 2 || n > 3 {
				t.Errorf("%d mutants; keep two or three, each breaking one requirement", n)
			}
			dir := t.TempDir()
			if err := buildModule(c, filepath.Join(dir, "fixture"), nil); err != nil {
				t.Fatal(err)
			}
			if out, err := goCommand(t.Context(), filepath.Join(dir, "fixture"), "vet", "./..."); err != nil {
				t.Fatalf("fixture does not vet: %v\n%s", err, out)
			}
			for i, patch := range c.Mutants {
				name := strings.TrimSuffix(filepath.Base(patch), ".patch")
				if desc := firstLine(patch); !regexp.MustCompile(`^Breaks R[0-9]+: \S`).MatchString(desc) {
					t.Errorf("%s: first line %q must say `Breaks R<n>: <how>`", name, desc)
				}
				work := filepath.Join(dir, strconv.Itoa(i))
				if err := buildModule(c, work, nil); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(patch)
				if err != nil {
					t.Fatal(err)
				}
				if err := applyPatch(work, data); err != nil {
					t.Errorf("%s does not apply to testdata/impl: %v", name, err)
					continue
				}
				if out, err := goCommand(t.Context(), work, "vet", "./..."); err != nil {
					t.Errorf("%s: mutated fixture does not vet: %v\n%s", name, err, out)
				}
			}
		})
	}
}

// TestScaffoldsDropMutants runs each mutant case's scaffold the way the eval
// runner does and checks that the mutants, which hold implementation lines,
// stay out of the agent's workspace.
func TestScaffoldsDropMutants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scaffolds are POSIX shell scripts")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cases, err := loadMutantCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			workspace := t.TempDir()
			cmd := exec.CommandContext(t.Context(), sh, filepath.Join(c.Dir, "scaffold.sh"))
			cmd.Dir = workspace
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("scaffold.sh: %v\n%s", err, out)
			}
			if !fileExists(filepath.Join(workspace, "testdata", "contract", "interface-contract.md")) {
				t.Error("scaffold did not copy testdata/contract/interface-contract.md")
			}
			if _, err := os.Stat(filepath.Join(workspace, "testdata", "impl-mutants")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("testdata/impl-mutants reached the workspace (stat: %v)", err)
			}
		})
	}
}

type mutantCase struct {
	Name     string   // case directory base name
	Dir      string   // absolute case directory
	Module   string   // Go module path the contract names
	TestPath string   // slash path of the brief's owned *_test.go
	Mutants  []string // absolute testdata/impl-mutants/*.patch paths, sorted
}

// toolchainLanguage takes the go.mod version from runtime.Version(), keeping a
// patch or prerelease suffix (go1.27.3, go1.27rc1) so the local toolchain
// always satisfies it.
var toolchainLanguage = regexp.MustCompile(`go(\d+\.\d+(?:\.\d+|(?:rc|beta)\d+)?)`)

var contractModule = regexp.MustCompile("Module\\s+`([^`]+)`")

// loadMutantCases returns, sorted by name, every case under evals/ that has a
// testdata/impl-mutants directory.
func loadMutantCases(repoRoot string) ([]mutantCase, error) {
	dirs, err := findCases(filepath.Join(repoRoot, "evals"))
	if err != nil {
		return nil, err
	}
	var cases []mutantCase
	for _, dir := range dirs {
		if !dirExists(filepath.Join(dir, "testdata", "impl-mutants")) {
			continue
		}
		c := mutantCase{Name: filepath.Base(dir), Dir: dir}
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
		var tests []string
		for _, p := range ownedPaths(string(prompt)) {
			if strings.HasSuffix(p, "_test.go") {
				tests = append(tests, p)
			}
		}
		if len(tests) != 1 {
			return nil, fmt.Errorf("%s: the brief must own exactly one *_test.go, found %v", c.Name, tests)
		}
		c.TestPath = tests[0]
		c.Mutants, err = filepath.Glob(filepath.Join(dir, "testdata", "impl-mutants", "*.patch"))
		if err != nil {
			return nil, err
		}
		if len(c.Mutants) == 0 {
			return nil, fmt.Errorf("%s: testdata/impl-mutants has no *.patch", c.Name)
		}
		sort.Strings(c.Mutants)
		cases = append(cases, c)
	}
	return cases, nil
}

type runOutcome struct {
	Passed bool   // `go test` exited 0
	Output string // combined output, truncated to its end
}

type mutantOutcome struct {
	Name        string // patch base name without ".patch"
	Requirement string // first line of the patch
	runOutcome
}

type mutantReport struct {
	Case     string
	Baseline runOutcome
	Mutants  []mutantOutcome
}

// err returns nil when the authored tests pass on the fixture and fail with
// every mutant applied.
func (r mutantReport) err() error {
	if !r.Baseline.Passed {
		return fmt.Errorf("case %s: the authored tests fail on the unmutated implementation, so they cannot gate mutants:\n%s", r.Case, r.Baseline.Output)
	}
	var survivors []string
	for _, m := range r.Mutants {
		if m.Passed {
			survivors = append(survivors, fmt.Sprintf("%s (%s)", m.Name, m.Requirement))
		}
	}
	if len(survivors) > 0 {
		return fmt.Errorf("case %s: every authored test still passes with %d of %d mutant(s) applied: %s", r.Case, len(survivors), len(r.Mutants), strings.Join(survivors, "; "))
	}
	return nil
}

func (r mutantReport) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s baseline passed=%v\n", r.Case, r.Baseline.Passed)
	for _, m := range r.Mutants {
		verdict := "killed"
		if m.Passed {
			verdict = "SURVIVED"
		}
		fmt.Fprintf(&b, "  %-32s %s  %s\n", m.Name, verdict, m.Requirement)
	}
	return b.String()
}

// runMutants runs testFile against the fixture and each mutant, each in a
// fresh module under workDir. Its error is for harness failures only.
func runMutants(ctx context.Context, c mutantCase, testFile []byte, workDir string) (mutantReport, error) {
	report := mutantReport{Case: c.Name}
	baseline := filepath.Join(workDir, "baseline")
	if err := buildModule(c, baseline, testFile); err != nil {
		return report, err
	}
	report.Baseline = goTest(ctx, baseline)
	for i, patch := range c.Mutants {
		data, err := os.ReadFile(patch)
		if err != nil {
			return report, err
		}
		dir := filepath.Join(workDir, "mutant-"+strconv.Itoa(i))
		if err := buildModule(c, dir, testFile); err != nil {
			return report, err
		}
		name := strings.TrimSuffix(filepath.Base(patch), ".patch")
		if err := applyPatch(dir, data); err != nil {
			return report, fmt.Errorf("mutant %s: %w", name, err)
		}
		report.Mutants = append(report.Mutants, mutantOutcome{Name: name, Requirement: firstLine(patch), runOutcome: goTest(ctx, dir)})
	}
	return report, nil
}

// buildModule lays out a throwaway module: go.mod for the contract's module,
// the case's testdata/impl tree at the module root, and testFile (when not
// nil) at the brief's owned test path.
func buildModule(c mutantCase, dir string, testFile []byte) error {
	impl := filepath.Join(c.Dir, "testdata", "impl")
	err := filepath.WalkDir(impl, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(impl, p)
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
		return fmt.Errorf("copy %s implementation: %w", c.Name, err)
	}
	// The language version is the running toolchain's, so an authored test may
	// use any feature of the toolchain that runs the gate.
	version := toolchainLanguage.FindStringSubmatch(runtime.Version())
	if version == nil {
		return fmt.Errorf("cannot derive a go.mod language version from toolchain %q", runtime.Version())
	}
	if err := writeFile(filepath.Join(dir, "go.mod"), []byte("module "+c.Module+"\n\ngo "+version[1]+"\n")); err != nil {
		return err
	}
	if testFile != nil {
		return writeFile(filepath.Join(dir, filepath.FromSlash(c.TestPath)), testFile)
	}
	return nil
}

func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func goTest(ctx context.Context, dir string) runOutcome {
	out, err := goCommand(ctx, dir, "test", "-count=1", "-timeout=120s", "./...")
	const keep = 6000
	if len(out) > keep {
		out = append([]byte("...\n"), out[len(out)-keep:]...)
	}
	return runOutcome{Passed: err == nil, Output: string(out)}
}

// goCommand runs the go tool that built this test in dir, as a standalone
// module that may fetch the authored tests' dependencies.
func goCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = dir
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return name == "GOWORK" || name == "GOFLAGS" || name == "GOTOOLCHAIN" || name == "PATH"
	})
	cmd.Env = append(env,
		"GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local",
		// End-to-end tests build their binary with the go on PATH.
		"PATH="+filepath.Dir(goBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cmd.CombinedOutput()
}

func firstLine(file string) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// applyPatch applies a unified diff to the files under dir. Hunks must match
// exactly at their stated lines; it neither creates nor deletes files.
func applyPatch(dir string, patch []byte) error {
	lines := strings.SplitAfter(strings.ReplaceAll(string(patch), "\r\n", "\n"), "\n")
	files := 0
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "--- ") {
			i++
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
			return fmt.Errorf("line %d: --- without a following +++ line", i+1)
		}
		target, err := patchTarget(lines[i+1])
		if err != nil {
			return fmt.Errorf("line %d: %w", i+2, err)
		}
		i += 2
		file := filepath.Join(dir, filepath.FromSlash(target))
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("patch target %s: %w", target, err)
		}
		content := strings.SplitAfter(string(data), "\n")
		if content[len(content)-1] == "" {
			content = content[:len(content)-1]
		}
		offset, hunks := 0, 0
		for i < len(lines) && strings.HasPrefix(lines[i], "@@") {
			m := hunkHeader.FindStringSubmatch(lines[i])
			if m == nil {
				return fmt.Errorf("line %d: malformed hunk header %q", i+1, strings.TrimSpace(lines[i]))
			}
			start, _ := strconv.Atoi(m[1])
			oldCount, newCount := hunkCount(m[2]), hunkCount(m[4])
			i++
			var old, replacement []string
			for (len(old) < oldCount || len(replacement) < newCount) && i < len(lines) {
				line := lines[i]
				i++
				if strings.HasPrefix(line, `\`) {
					continue // "\ No newline at end of file"
				}
				if line == "" {
					break // end of patch
				}
				if line == "\n" {
					line = " \n" // some tools drop the space of an empty context line
				}
				switch line[0] {
				case ' ':
					old, replacement = append(old, line[1:]), append(replacement, line[1:])
				case '-':
					old = append(old, line[1:])
				case '+':
					replacement = append(replacement, line[1:])
				default:
					return fmt.Errorf("line %d: unexpected hunk line %q", i, strings.TrimSpace(line))
				}
			}
			if len(old) != oldCount || len(replacement) != newCount {
				return fmt.Errorf("%s: hunk at line %d is truncated", target, start)
			}
			at := start - 1 + offset
			if oldCount == 0 {
				at = start + offset
			}
			if at < 0 || at+len(old) > len(content) || !slices.Equal(content[at:at+len(old)], old) {
				return fmt.Errorf("%s: hunk at line %d does not match the file", target, start)
			}
			content = slices.Concat(content[:at], replacement, content[at+len(old):])
			offset += len(replacement) - len(old)
			hunks++
		}
		if hunks == 0 {
			return fmt.Errorf("%s: no hunks", target)
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(strings.Join(content, "")), info.Mode().Perm()); err != nil {
			return err
		}
		files++
	}
	if files == 0 {
		return errors.New("no file sections (--- / +++) in the patch")
	}
	return nil
}

func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, _ := strconv.Atoi(s)
	return n
}

func patchTarget(header string) (string, error) {
	name := strings.TrimSpace(strings.TrimPrefix(header, "+++ "))
	name, _, _ = strings.Cut(name, "\t")
	if name == "/dev/null" {
		return "", errors.New("the patch deletes a file, which mutants may not do")
	}
	name = strings.TrimPrefix(name, "b/")
	if !cleanRelative(name) {
		return "", fmt.Errorf("unsafe patch path %q", name)
	}
	return path.Clean(name), nil
}

// findWorkspaces finds run workspaces under root: directories holding a
// testdata/contract/interface-contract.md identical to a case's, and no
// prompt.md (which would make them a copy of the case itself).
func findWorkspaces(root string, cases []mutantCase) (map[string][]string, error) {
	contracts := map[string]string{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(c.Dir, "testdata", "contract", "interface-contract.md"))
		if err != nil {
			return nil, err
		}
		contracts[string(data)] = c.Name
	}
	found := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != root {
			switch d.Name() {
			case "testdata", ".git", "node_modules":
				return filepath.SkipDir
			}
		}
		data, err := os.ReadFile(filepath.Join(p, "testdata", "contract", "interface-contract.md"))
		if err != nil {
			return nil
		}
		name, ok := contracts[string(data)]
		if !ok || fileExists(filepath.Join(p, "prompt.md")) {
			return nil
		}
		found[name] = append(found[name], p)
		return filepath.SkipDir
	})
	for _, dirs := range found {
		sort.Strings(dirs)
	}
	return found, err
}
