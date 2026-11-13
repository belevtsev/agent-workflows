// Contract tests for the post-merge check amendment (change
// add-post-merge-check-amendment, interface contract revision 2), requirements
// R1-R18 and R21. They are authored from the interface contract and the public
// declarations alone; every expected value comes from the contract. Each test
// names, in its comment or messages, the requirement whose violation fails it.
package taskflow_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// acPacks is the explicit pack selection the amendment fixtures start with: a
// Go pack detected by go.mod that names go.package-test/v2, the Go rule these
// contracts state their verify problems in.
func acPacks() []langpack.Manifest {
	return []langpack.Manifest{spManifest("go", langpack.GoPackageTestV2, "go.mod")}
}

const (
	acReason          = "merged origin/main; go.mod and internal/b changed"
	acGoModProblem    = "changed go.mod has no passed go test ./... run at the current source"
	acPkgBProblem     = "changed package internal/b has no passed go test run at the current source"
	acFallbackProblem = "changed substantial task has no passed check run at the current source"
	acNoFFHint        = "git merge --no-ff"
	acC12Tail         = "covers no package or module file that the merge changed, that is still changed since the baseline, and that no earlier check covers"
	acB3Phrase        = "checks can be added only to a task whose evidence rules include the Go package-test rule"
)

// acWidenedArgv runs under -race, which go.package-test/v2 needs for each
// changed package.
var acWidenedArgv = []string{"go", "test", "-race", "./..."}

// ---------------------------------------------------------------------------
// Environment and fixture (contract section 2).
//
// The taskflow API runs Git and go test with the process environment, so the
// isolation is process-wide. It is scoped to the single top-level test
// TestContractAddChecks: the group sets HOME, XDG_CONFIG_HOME, Git's global
// and system configuration, GOENV (a missing file), GOFLAGS (empty) and the
// other variables below before any case starts, and t.Cleanup restores every
// one of them after all cases, parallel ones included, have finished. Go runs
// no other test while a sequential top-level test runs (parallel top-level
// tests wait for the sequential phase to end), so no other test sees the
// change. The template repository lives in the group's own t.TempDir, which is
// private (mode 0700, created by os.MkdirTemp) and removed at the end. The
// developer's Go build cache is kept so fixture checks do not recompile the
// standard library. Cases that vary GOFLAGS or CWFIX_FAIL_* use t.Setenv and
// never run in parallel.

var acShared struct {
	active      bool
	root        string // the group's private directory
	template    string // repository at baseline B with fixture refs
	B, F, O, M  string // commit IDs in the template
	originalEnv []string
}

// acSetup requires the TestContractAddChecks group, which owns the isolated
// environment and the template repository.
func acSetup(t *testing.T) {
	t.Helper()
	if !acShared.active {
		t.Fatalf("setup: contract cases run only as subtests of TestContractAddChecks")
	}
}

// TestContractAddChecks runs every contract case of this file as a subtest
// under one isolated environment (see above).
func TestContractAddChecks(t *testing.T) {
	acIsolate(t)
	for _, c := range acCases {
		t.Run(c.name, c.fn)
	}
}

// acSetenvScoped sets (or, for nil, unsets) environment variables for the
// rest of the test and restores their previous values in t.Cleanup.
func acSetenvScoped(t *testing.T, vars map[string]*string) {
	t.Helper()
	type prev struct {
		value string
		ok    bool
	}
	saved := map[string]prev{}
	for k := range vars {
		v, ok := os.LookupEnv(k)
		saved[k] = prev{v, ok}
	}
	t.Cleanup(func() {
		for k, p := range saved {
			if p.ok {
				os.Setenv(k, p.value)
			} else {
				os.Unsetenv(k)
			}
		}
	})
	for k, v := range vars {
		var err error
		if v == nil {
			err = os.Unsetenv(k)
		} else {
			err = os.Setenv(k, *v)
		}
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
}

func acStr(s string) *string { return &s }

func acIsolate(t *testing.T) {
	t.Helper()
	acShared.originalEnv = os.Environ()
	cache := os.Getenv("GOCACHE")
	if cache == "" || cache == "off" {
		out, err := exec.Command("go", "env", "GOCACHE").Output()
		if err != nil {
			t.Fatalf("setup: go env GOCACHE: %v", err)
		}
		cache = strings.TrimSpace(string(out))
	}
	// os.MkdirTemp creates the directory with mode 0700 under the test's own
	// temporary directory, which the testing package removes at the end.
	private, err := os.MkdirTemp(t.TempDir(), "group-")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	root, err := filepath.EvalSymlinks(private)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("setup: group directory %s is not a private directory (%v)", root, err)
	}
	tmpRaw := os.TempDir()
	tmp, err := filepath.EvalSymlinks(tmpRaw)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, d := range []string{filepath.Join(root, "home"), filepath.Join(root, "xdg")} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	gitConfig := filepath.Join(root, "gitconfig")
	cfg := "[user]\n\tname = Contract Test\n\temail = contract-test@example.invalid\n" +
		"[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n[core]\n\tautocrlf = false\n"
	if err := os.WriteFile(gitConfig, []byte(cfg), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	vars := map[string]*string{
		"HOME":                    acStr(filepath.Join(root, "home")),
		"XDG_CONFIG_HOME":         acStr(filepath.Join(root, "xdg")),
		"GIT_CONFIG_NOSYSTEM":     acStr("1"),
		"GIT_CONFIG_GLOBAL":       acStr(gitConfig),
		"GIT_CEILING_DIRECTORIES": acStr(tmpRaw + string(os.PathListSeparator) + tmp),
		"GIT_TERMINAL_PROMPT":     acStr("0"),
		"GOFLAGS":                 acStr(""),
		"GOTOOLCHAIN":             acStr("local"),
		"GOPROXY":                 acStr("off"),
		"GOCACHE":                 acStr(cache),
		"GOENV":                   acStr(filepath.Join(root, "absent-goenv")),
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GOWORK", "CWFIX_FAIL_A", "CWFIX_FAIL_B"} {
		vars[k] = nil
	}
	t.Cleanup(func() { acShared.active = false })
	acSetenvScoped(t, vars)
	acShared.root = root
	if err := acBuildTemplate(root); err != nil {
		t.Fatalf("setup: %v", err)
	}
	acShared.active = true
}

func acGitE(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func acWriteFiles(root string, files map[string]string) error {
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// acTestFile writes a test that fails while CWFIX_FAIL_<PKG> is "1", so a
// check can fail at an unchanged source. go test caches results by the
// environment variables a test reads, so toggling it re-runs the test.
func acTestFile(pkg, fn string) string {
	return "package " + pkg + "\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
		"func Test" + fn + "(t *testing.T) {\n" +
		"\tif os.Getenv(\"CWFIX_FAIL_" + strings.ToUpper(pkg) + "\") == \"1\" {\n\t\tt.Fatal(\"forced fixture failure\")\n\t}\n" +
		"\tif " + fn + "() == 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n"
}

const acBaselineC = "package c\n\n// C returns a fixture value.\nfunc C() int { return 3 }\n"

// acModuleFiles is the section 2 module: go.mod at the selected path and
// packages internal/a, internal/b, internal/c and internal/d.
func acModuleFiles() map[string]string {
	return map[string]string{
		"go.mod":               "module example.com/m\n\ngo 1.21\n",
		"internal/a/a.go":      "package a\n\n// A returns a fixture value.\nfunc A() int { return 1 }\n",
		"internal/a/a_test.go": acTestFile("a", "A"),
		"internal/b/b.go":      "package b\n\n// B returns a fixture value.\nfunc B() int { return 2 }\n",
		"internal/b/b_test.go": acTestFile("b", "B"),
		"internal/c/c.go":      acBaselineC,
		"internal/d/d.go":      "package d\n\n// D returns a fixture value.\nfunc D() int { return 4 }\n",
	}
}

var (
	acFContent = map[string]string{"internal/a/a.go": "package a\n\n// A returns a fixture value.\nfunc A() int { return 10 }\n"}
	acOContent = map[string]string{"go.mod": "module example.com/m\n\ngo 1.21\n\n// base change\n", "internal/b/b.go": "package b\n\n// B returns a fixture value.\nfunc B() int { return 20 }\n"}
)

// acBuildTemplate builds the section 2 repository once: main holds a root
// commit and a --no-ff merge of a side branch (so baseline HEAD B is itself a
// merge commit, used by R3.b); refs/remotes/origin/main and feature point at
// B. It also stores F (internal/a), O (go.mod and internal/b, parent B) and
// M = merge(F, O) under refs/fixture/, so a test reaches the standard merged
// state with two commands after starting its task at B.
func acBuildTemplate(root string) error {
	dir, err := os.MkdirTemp(root, "template-")
	if err != nil {
		return err
	}
	g := func(args ...string) string {
		if err != nil {
			return ""
		}
		var out string
		out, err = acGitE(dir, args...)
		return out
	}
	if err := acWriteFiles(dir, acModuleFiles()); err != nil {
		return err
	}
	g("init", "-q")
	g("symbolic-ref", "HEAD", "refs/heads/main")
	g("add", "-A")
	g("commit", "-q", "-m", "root")
	g("checkout", "-q", "-b", "side")
	if err == nil {
		err = acWriteFiles(dir, map[string]string{"NOTES.txt": "side notes\n"})
	}
	g("add", "-A")
	g("commit", "-q", "-m", "side")
	g("checkout", "-q", "main")
	g("merge", "-q", "--no-ff", "--no-edit", "side")
	b := g("rev-parse", "HEAD")
	g("branch", "-q", "-D", "side")
	g("checkout", "-q", "-b", "feature")
	if err == nil {
		err = acWriteFiles(dir, acFContent)
	}
	g("add", "-A")
	g("commit", "-q", "-m", "F")
	f := g("rev-parse", "HEAD")
	g("checkout", "-q", "-b", "mainline", b)
	if err == nil {
		err = acWriteFiles(dir, acOContent)
	}
	g("add", "-A")
	g("commit", "-q", "-m", "O")
	o := g("rev-parse", "HEAD")
	g("checkout", "-q", "feature")
	g("merge", "-q", "--no-ff", "--no-edit", o)
	m := g("rev-parse", "HEAD")
	g("update-ref", "refs/fixture/F", f)
	g("update-ref", "refs/fixture/O", o)
	g("update-ref", "refs/fixture/M", m)
	g("reset", "-q", "--hard", b)
	g("branch", "-q", "-D", "mainline")
	g("update-ref", "refs/remotes/origin/main", b)
	if err != nil {
		return fmt.Errorf("build template: %v", err)
	}
	if p := g("rev-parse", m+"^1", m+"^2"); p != f+"\n"+o {
		return fmt.Errorf("build template: M parents %q, want F and O", p)
	}
	if st := g("status", "--porcelain"); st != "" || err != nil {
		return fmt.Errorf("build template: tree not clean (%q, %v)", st, err)
	}
	acShared.template, acShared.B, acShared.F, acShared.O, acShared.M = dir, b, f, o, m
	return nil
}

func acCopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}
	})
}

// acBase returns a canonical (symlink-resolved) per-test directory.
func acBase(t *testing.T) string {
	t.Helper()
	acSetup(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: resolve temp dir: %v", err)
	}
	return base
}

func acGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := acGitE(dir, args...)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return out
}

func acHead(t *testing.T, dir string) string {
	t.Helper()
	return acGit(t, dir, "rev-parse", "HEAD")
}

func acWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	if err := acWriteFiles(root, map[string]string{rel: content}); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func acCommitAll(t *testing.T, root, msg string) string {
	t.Helper()
	acGit(t, root, "add", "-A")
	acGit(t, root, "commit", "-q", "-m", msg)
	return acHead(t, root)
}

// acMark makes the fixture test of pkg (a or b) fail or pass through
// CWFIX_FAIL_<PKG>. It uses t.Setenv, so its callers never run in parallel.
func (r *acRepo) acMark(t *testing.T, pkg string, fail bool) {
	t.Helper()
	v := ""
	if fail {
		v = "1"
	}
	t.Setenv("CWFIX_FAIL_"+strings.ToUpper(pkg), v)
}

type acRepo struct {
	base, root, store string
	B, F, O, M        string
}

// acNewRepo copies the template repository (baseline B checked out on
// feature, origin/main at B) into a fresh per-test directory.
func acNewRepo(t *testing.T) *acRepo {
	t.Helper()
	base := acBase(t)
	r := &acRepo{base: base, root: filepath.Join(base, "repo"), store: filepath.Join(base, "store"), B: acShared.B}
	if err := acCopyTree(acShared.template, r.root); err != nil {
		t.Fatalf("setup: copy template: %v", err)
	}
	if err := os.MkdirAll(r.store, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmd := exec.Command("git", "update-index", "-q", "--refresh")
	cmd.Dir = r.root
	_ = cmd.Run()
	if st := acGit(t, r.root, "status", "--porcelain"); st != "" {
		t.Fatalf("setup: copied template is not clean: %q", st)
	}
	return r
}

// commitF commits F on feature: it changes internal/a/a.go plus extra files.
func (r *acRepo) commitF(t *testing.T, extra map[string]string) {
	t.Helper()
	for rel, content := range acFContent {
		acWrite(t, r.root, rel, content)
	}
	for rel, content := range extra {
		acWrite(t, r.root, rel, content)
	}
	r.F = acCommitAll(t, r.root, "F")
}

// commitO advances origin/main by one single-parent commit O that changes
// go.mod and internal/b/b.go (plus extra files), and returns to feature.
func (r *acRepo) commitO(t *testing.T, extra map[string]string) {
	t.Helper()
	acGit(t, r.root, "checkout", "-q", "-B", "mainline", "refs/remotes/origin/main")
	for rel, content := range acOContent {
		acWrite(t, r.root, rel, content)
	}
	for rel, content := range extra {
		acWrite(t, r.root, rel, content)
	}
	r.O = acCommitAll(t, r.root, "O")
	acGit(t, r.root, "update-ref", "refs/remotes/origin/main", r.O)
	acGit(t, r.root, "checkout", "-q", "feature")
}

// mergeM creates merge commit M with first parent F and second parent O.
func (r *acRepo) mergeM(t *testing.T) {
	t.Helper()
	acGit(t, r.root, "merge", "-q", "--no-ff", "--no-edit", "origin/main")
	r.M = acHead(t, r.root)
	if p1, p2 := acGit(t, r.root, "rev-parse", r.M+"^1"), acGit(t, r.root, "rev-parse", r.M+"^2"); p1 != r.F || p2 != r.O {
		t.Fatalf("setup: M parents = %s %s, want F %s and O %s", p1, p2, r.F, r.O)
	}
}

// standard reaches the section 2 merged state from the template's F, O and M:
// origin/main moves to O and feature fast-forwards to M.
func (r *acRepo) standard(t *testing.T) {
	t.Helper()
	acGit(t, r.root, "update-ref", "refs/remotes/origin/main", acShared.O)
	acGit(t, r.root, "merge", "-q", "--ff-only", acShared.M)
	r.F, r.O, r.M = acShared.F, acShared.O, acShared.M
	if h := acHead(t, r.root); h != r.M {
		t.Fatalf("setup: HEAD %s after fast-forward, want M %s", h, r.M)
	}
}

type acPlanOpts struct {
	small          bool
	noCoordinator  bool
	checkpointsOff bool
	planChecks     int // total plan checks; 0 means unit only
}

func acPlan(o acPlanOpts) taskflow.Plan {
	p := taskflow.Plan{Title: "contract fixture", Kind: "substantial", CoordinatorID: "/root/coord"}
	if o.small {
		p.Kind = "small"
	}
	if o.noCoordinator {
		p.CoordinatorID = ""
	}
	if o.checkpointsOff {
		p.Checkpoints = &taskflow.CheckpointPolicy{Required: false, Reason: "contract fixture: checkpoints are independent of amendments"}
	}
	checks := []taskflow.Check{{ID: "unit", Argv: []string{"go", "test", "./internal/a"}, TimeoutSeconds: 300}}
	for i := 1; i < o.planChecks; i++ {
		checks = append(checks, taskflow.Check{ID: fmt.Sprintf("p%03d", i), Argv: []string{"go", "test", "./internal/a"}, TimeoutSeconds: 300})
	}
	ids := make([]string, len(checks))
	for i, c := range checks {
		ids[i] = c.ID
	}
	p.Checks = checks
	p.Requirements = []taskflow.Requirement{{ID: "r1", Description: "the fixture requirement", CheckIDs: ids}}
	return p
}

// start begins the task with taskflow.StartWithPacks and acPacks.
func (r *acRepo) start(t *testing.T, o acPlanOpts) string {
	t.Helper()
	rec, err := taskflow.StartWithPacks(context.Background(), r.root, r.store, acPlan(o), map[string]string{}, taskflow.SelectionExplicit, acPacks())
	if err != nil {
		t.Fatalf("setup: taskflow.StartWithPacks: %v", err)
	}
	if rec.Directory == "" {
		t.Fatalf("setup: taskflow.StartWithPacks returned no directory")
	}
	return rec.Directory
}

func acStandardTask(t *testing.T, o acPlanOpts) (*acRepo, string) {
	t.Helper()
	r := acNewRepo(t)
	dir := r.start(t, o)
	r.standard(t)
	return r, dir
}

func acCheck(id string, argv ...string) taskflow.Check {
	return taskflow.Check{ID: id, Argv: argv}
}

func acWidened() taskflow.Check { return acCheck("widened", acWidenedArgv...) }

// acPassthroughCheck is acWidened with names as its env_passthrough, decoded
// from add-checks input JSON as cw task add-checks decodes it.
func acPassthroughCheck(t *testing.T, names ...string) taskflow.Check {
	t.Helper()
	data, err := json.Marshal(map[string]any{"id": "widened", "argv": acWidenedArgv, "env_passthrough": names})
	if err != nil {
		t.Fatal(err)
	}
	var check taskflow.Check
	if err := taskflow.Decode(bytes.NewReader(data), &check); err != nil {
		t.Fatal(err)
	}
	return check
}

func acInput(merge string, checks ...taskflow.Check) taskflow.AmendmentInput {
	return taskflow.AmendmentInput{MergeCommit: merge, BaseRef: "origin/main", Reason: acReason, Checks: checks}
}

// ---------------------------------------------------------------------------
// Task directory observation.

func acLoad(t *testing.T, dir string) taskflow.Record {
	t.Helper()
	rec, err := taskflow.Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	return rec
}

var acRevisionName = regexp.MustCompile(`^record-\d{6}\.json$`)

func acRevisionFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read task dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if acRevisionName.MatchString(e.Name()) {
			n++
		}
	}
	return n
}

func acRevisionPath(dir string, n int) string {
	return filepath.Join(dir, fmt.Sprintf("record-%06d.json", n))
}

func acRevisionBytes(t *testing.T, dir string, n int) []byte {
	t.Helper()
	data, err := os.ReadFile(acRevisionPath(dir, n))
	if err != nil {
		t.Fatalf("read revision %d: %v", n, err)
	}
	return data
}

// acDirState maps every path under the task directory to its mode and content
// hash, so a refusal can be shown to write nothing (section 1.5, invariant 7).
func acDirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			state[rel] = info.Mode().String()
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		state[rel] = info.Mode().String() + " " + hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk task dir: %v", err)
	}
	return state
}

func acStateDiff(before, after map[string]string) string {
	var diffs []string
	for k, v := range before {
		if w, ok := after[k]; !ok {
			diffs = append(diffs, "removed "+k)
		} else if v != w {
			diffs = append(diffs, "changed "+k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			diffs = append(diffs, "added "+k)
		}
	}
	sort.Strings(diffs)
	return strings.Join(diffs, ", ")
}

func acAdd(dir string, in taskflow.AmendmentInput) (taskflow.Record, error) {
	return taskflow.AddChecks(context.Background(), dir, in)
}

// acExpectRefused asserts the section 1.5 meaning of "refused": a non-nil
// error containing every phrase, and a task directory left byte-identical.
func acExpectRefused(t *testing.T, dir string, in taskflow.AmendmentInput, phrases ...string) {
	t.Helper()
	before := acDirState(t, dir)
	rec, err := acAdd(dir, in)
	if err == nil {
		t.Fatalf("AddChecks accepted the amendment (revision %d); want a refusal containing %q", rec.Revision, phrases)
	}
	for _, p := range phrases {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("AddChecks error = %q; want it to contain %q", err.Error(), p)
		}
	}
	if diff := acStateDiff(before, acDirState(t, dir)); diff != "" {
		t.Errorf("refused AddChecks changed the task directory: %s", diff)
	}
}

// acExpectRefusedIs is acExpectRefused for a refusal whose kind is the
// point: the error matches target with errors.Is, whatever its wording.
func acExpectRefusedIs(t *testing.T, dir string, in taskflow.AmendmentInput, target error) {
	t.Helper()
	before := acDirState(t, dir)
	if rec, err := acAdd(dir, in); !errors.Is(err, target) {
		t.Fatalf("AddChecks = revision %d, %v; want a refusal matching %v", rec.Revision, err, target)
	}
	if diff := acStateDiff(before, acDirState(t, dir)); diff != "" {
		t.Errorf("refused AddChecks changed the task directory: %s", diff)
	}
}

// acExpectAccepted asserts acceptance writes exactly one new revision that
// appends exactly one amendment (R3.a, invariant 7) and returns the loaded
// record.
func acExpectAccepted(t *testing.T, dir string, in taskflow.AmendmentInput) taskflow.Record {
	t.Helper()
	prev := acLoad(t, dir)
	rec, err := acAdd(dir, in)
	if err != nil {
		t.Fatalf("AddChecks refused a qualifying amendment: %v", err)
	}
	if rec.Revision != prev.Revision+1 {
		t.Errorf("AddChecks returned revision %d, want %d", rec.Revision, prev.Revision+1)
	}
	loaded := acLoad(t, dir)
	if loaded.Revision != prev.Revision+1 {
		t.Errorf("latest revision after acceptance = %d, want exactly one new revision (%d)", loaded.Revision, prev.Revision+1)
	}
	if n := acRevisionFiles(t, dir); n != prev.Revision+1 {
		t.Errorf("revision files after acceptance = %d, want %d", n, prev.Revision+1)
	}
	if len(loaded.Amendments) != len(prev.Amendments)+1 {
		t.Fatalf("amendments after acceptance = %d, want %d", len(loaded.Amendments), len(prev.Amendments)+1)
	}
	return loaded
}

func acRawMember(t *testing.T, data []byte, name string) (json.RawMessage, bool) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode revision: %v", err)
	}
	raw, ok := m[name]
	if !ok {
		return nil, false
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact %s: %v", name, err)
	}
	return buf.Bytes(), true
}

// ---------------------------------------------------------------------------
// Tampered revisions (section 5 item 1).

type acCodec struct {
	indent     string
	escapeHTML bool
	newline    bool
}

func (c acCodec) encode(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(c.escapeHTML)
	if c.indent != "" {
		enc.SetIndent("", c.indent)
	}
	if err := enc.Encode(v); err != nil {
		t.Fatalf("setup: encode revision: %v", err)
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	if c.newline {
		out = append(out, '\n')
	}
	return out
}

// acReadRevision decodes revision n and finds the encoding that reproduces its
// bytes exactly, so a tampered revision differs only in the members a test
// changes.
func acReadRevision(t *testing.T, dir string, n int) ([]byte, taskflow.Record, acCodec) {
	t.Helper()
	data := acRevisionBytes(t, dir, n)
	var rec taskflow.Record
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("setup: decode revision %d: %v", n, err)
	}
	indent := ""
	trimmed := bytes.TrimRight(data, "\n")
	if bytes.HasPrefix(trimmed, []byte("{\n")) {
		rest := trimmed[2:]
		i := 0
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
			i++
		}
		indent = string(rest[:i])
	}
	newline := bytes.HasSuffix(data, []byte("\n"))
	for _, esc := range []bool{true, false} {
		c := acCodec{indent: indent, escapeHTML: esc, newline: newline}
		if bytes.Equal(c.encode(t, rec), data) {
			return data, rec, c
		}
	}
	t.Fatalf("setup: re-encoding taskflow.Record does not reproduce revision %d byte for byte; the tamper recipe cannot be applied", n)
	return nil, taskflow.Record{}, acCodec{}
}

func acWriteRevision(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("setup: write %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("setup: chmod %s: %v", path, err)
	}
}

// acAppendRevision writes revision N+1 as a copy of revision N changed by
// mutate. As a positive control it first writes an unchanged copy and requires
// it to load, so a later load failure is caused by the mutation alone.
func acAppendRevision(t *testing.T, dir string, mutate func(*taskflow.Record)) {
	t.Helper()
	acAppendRevisionControlled(t, dir, nil, mutate)
}

// acAppendRevisionControlled is acAppendRevision whose positive control is
// the copy changed by control (unchanged when control is nil).
func acAppendRevisionControlled(t *testing.T, dir string, control, mutate func(*taskflow.Record)) {
	t.Helper()
	n := acLoad(t, dir).Revision
	data, rec, codec := acReadRevision(t, dir, n)
	sum := sha256.Sum256(data)
	rec.Revision = n + 1
	rec.PreviousSHA256 = hex.EncodeToString(sum[:])
	path := acRevisionPath(dir, n+1)
	controlRec := rec
	if control != nil {
		_, controlRec, _ = acReadRevision(t, dir, n)
		controlRec.Revision, controlRec.PreviousSHA256 = rec.Revision, rec.PreviousSHA256
		control(&controlRec)
	}
	acWriteRevision(t, path, codec.encode(t, controlRec))
	if _, err := taskflow.Load(dir); err != nil {
		t.Fatalf("setup: the positive-control copy of revision %d written as revision %d does not load (%v); the section 5 item 1 recipe does not hold", n, n+1, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("setup: %v", err)
	}
	mutate(&rec)
	acWriteRevision(t, path, codec.encode(t, rec))
}

// acRewriteLatest rewrites the latest revision in place (no later revision
// refers to its hash).
func acRewriteLatest(t *testing.T, dir string, mutate func(*taskflow.Record)) {
	t.Helper()
	n := acLoad(t, dir).Revision
	_, rec, codec := acReadRevision(t, dir, n)
	mutate(&rec)
	acWriteRevision(t, acRevisionPath(dir, n), codec.encode(t, rec))
}

// ---------------------------------------------------------------------------
// Checks, assessments and verify.

// acRun runs a check and returns the run it recorded, tolerating an error
// return for a check that ran and failed.
func acRun(t *testing.T, dir, id string) taskflow.CheckRun {
	t.Helper()
	before := len(acLoad(t, dir).Checks)
	run, err := taskflow.RunCheck(context.Background(), dir, id)
	if run.ID != "" {
		return run
	}
	rec := acLoad(t, dir)
	if len(rec.Checks) > before {
		for i := len(rec.Checks) - 1; i >= 0; i-- {
			if rec.Checks[i].CheckID == id {
				return rec.Checks[i]
			}
		}
	}
	t.Fatalf("RunCheck(%s) recorded no run: %v", id, err)
	return taskflow.CheckRun{}
}

func acRunPassed(t *testing.T, dir, id string) taskflow.CheckRun {
	t.Helper()
	run := acRun(t, dir, id)
	if run.Status != "passed" {
		t.Fatalf("setup: run of %s has status %q (failure %q); the fixture expects it to pass", id, run.Status, run.Failure)
	}
	return run
}

func acSourceRef(t *testing.T, root string) taskflow.SourceRef {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "internal", "a", "a.go"))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	sum := sha256.Sum256(data)
	return taskflow.SourceRef{Path: "internal/a/a.go", StartLine: 1, EndLine: 1, SHA256: hex.EncodeToString(sum[:])}
}

// acAssess assesses r1 satisfied with reqChecks and, when decisionChecks is
// non-nil, resolves the decision citing decisionChecks.
func acAssess(t *testing.T, dir, root string, reqChecks, decisionChecks []string) error {
	t.Helper()
	ref := acSourceRef(t, root)
	in := taskflow.AssessmentInput{Requirements: []taskflow.Assessment{{
		ID: "r1", Status: "satisfied", Evidence: []taskflow.SourceRef{ref}, CheckIDs: reqChecks,
	}}}
	if decisionChecks != nil {
		in.Decision = &taskflow.Decision{Status: "resolved", Reason: "the implementation meets the plan", Evidence: []taskflow.SourceRef{ref}, CheckIDs: decisionChecks}
	}
	_, err := taskflow.Assess(context.Background(), dir, in)
	return err
}

// acVerify runs verify and returns the verification it produced, whether or
// not Verify also reports incompleteness as an error.
func acVerify(t *testing.T, dir string) taskflow.Verification {
	t.Helper()
	v, err := taskflow.Verify(context.Background(), dir)
	if err == nil || !v.CheckedAt.IsZero() {
		return v
	}
	rec, lerr := taskflow.Load(dir)
	if lerr != nil || rec.Verification == nil {
		t.Fatalf("Verify: %v (and no stored verification)", err)
	}
	return *rec.Verification
}

func acHasProblem(v taskflow.Verification, substr string) bool {
	for _, p := range v.Problems {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Tests.

// acCases are the subtests of TestContractAddChecks, in source order.
var acCases = []struct {
	name string
	fn   func(*testing.T)
}{
	{"DoneCriterionMergeFailsVerifyThenPasses", acTestDoneCriterionMergeFailsVerifyThenPasses},
	{"PlanUnchanged", acTestPlanUnchanged},
	{"TamperedPlanFailsLoad", acTestTamperedPlanFailsLoad},
	{"Eligibility", acTestEligibility},
	{"MergeGate", acTestMergeGate},
	{"CheckValidation", acTestCheckValidation},
	{"RevisionShape", acTestRevisionShape},
	{"CreditedArgv", acTestCreditedArgv},
	{"NewCoverage", acTestNewCoverage},
	{"FreshIDs", acTestFreshIDs},
	{"Recorded", acTestRecorded},
	{"AppendOnlyLoad", acTestAppendOnlyLoad},
	{"AppendOnlyCountAndOrder", acTestAppendOnlyCountAndOrder},
	{"StoredAmendmentShape", acTestStoredAmendmentShape},
	{"ClearsVerification", acTestClearsVerification},
	{"RunAddedCheck", acTestRunAddedCheck},
	{"CitingAddedChecks", acTestCitingAddedChecks},
	{"VerifyCredit", acTestVerifyCredit},
	{"NoRecheckAfterAdmission", acTestNoRecheckAfterAdmission},
	{"NeverAmendedHasNoMember", acTestNeverAmendedHasNoMember},
	{"ConcurrentAdmission", acTestConcurrentAdmission},
}

// acTestDoneCriterionMergeFailsVerifyThenPasses fails when a
// task whose branch merged the base (changing go.mod and internal/b) does not
// fail verify (R13.c), or when an accepted amendment plus a passed run of the
// added check does not clear the go.mod problem and complete verify (R13.d,
// R12.a, R12.b), or the added check cannot run (R11.a).
func acTestDoneCriterionMergeFailsVerifyThenPasses(t *testing.T) {
	acSetup(t)
	t.Parallel()
	r, dir := acStandardTask(t, acPlanOpts{checkpointsOff: true})
	acRunPassed(t, dir, "unit")
	if err := acAssess(t, dir, r.root, []string{"unit"}, []string{"unit"}); err != nil {
		t.Fatalf("setup: assess before the amendment: %v", err)
	}
	v1 := acVerify(t, dir)
	if v1.Complete {
		t.Errorf("R13.c: verify complete after a base merge changed go.mod while only unit passed; problems %q", v1.Problems)
	}
	if !acHasProblem(v1, acGoModProblem) {
		t.Errorf("R13.c: verify problems %q lack %q", v1.Problems, acGoModProblem)
	}

	acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
	run := acRunPassed(t, dir, "widened")
	if run.CheckID != "widened" || !reflect.DeepEqual(run.Argv, acWidenedArgv) {
		t.Errorf("R11.a: run check_id %q argv %q, want widened %q", run.CheckID, run.Argv, acWidenedArgv)
	}
	if err := acAssess(t, dir, r.root, []string{"unit", "widened"}, []string{"widened"}); err != nil {
		t.Fatalf("R12.a/R12.b: assessment and decision citing added check widened refused: %v", err)
	}
	v2 := acVerify(t, dir)
	if acHasProblem(v2, acGoModProblem) {
		t.Errorf("R13.d: verify still reports %q after widened passed; problems %q", acGoModProblem, v2.Problems)
	}
	for _, p := range v2.Problems {
		if strings.Contains(p, "check argv differs from the explicit plan") {
			t.Errorf("R12.a: verify reports %q for the added check", p)
		}
		if strings.Contains(p, "decision") {
			t.Errorf("R12.b: verify reports a problem for the decision citing widened: %q", p)
		}
	}
	// Coordinator answer (tests-1b): full completion needs an accepted
	// independent review recorded through cw task bundle, which the CLI test
	// TestContractAddChecksCLI/DoneCriterion performs. Here every remaining
	// problem must be the independent-review requirement.
	for _, p := range v2.Problems {
		if !strings.Contains(strings.ToLower(p), "review") {
			t.Errorf("R13.d/R12.a: verify problem %q remains after the amendment and passed runs of unit and widened; only the independent-review requirement may remain", p)
		}
	}
}

// acTestPlanUnchanged fails when acceptance changes the stored
// plan (R1.a).
func acTestPlanUnchanged(t *testing.T) {
	acSetup(t)
	t.Parallel()
	_, dir := acStandardTask(t, acPlanOpts{})
	rec := acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
	first, ok1 := acRawMember(t, acRevisionBytes(t, dir, 1), "plan")
	latest, ok2 := acRawMember(t, acRevisionBytes(t, dir, rec.Revision), "plan")
	if !ok1 || !ok2 {
		t.Fatalf("R1.a: plan member missing (revision 1 %v, latest %v)", ok1, ok2)
	}
	if !bytes.Equal(first, latest) {
		t.Errorf("R1.a: plan changed by the amendment:\nrevision 1: %s\nlatest:     %s", first, latest)
	}
	if len(rec.Plan.Checks) != 1 || rec.Plan.Checks[0].ID != "unit" {
		t.Errorf("R1.a: plan.checks = %+v, want only unit", rec.Plan.Checks)
	}
}

// acTestTamperedPlanFailsLoad fails when a later revision of an
// amended record may change the plan (R1.b, R1.c, R1.d): load must fail and
// no task command may succeed on the record.
func acTestTamperedPlanFailsLoad(t *testing.T) {
	acSetup(t)
	t.Parallel()
	cases := []struct {
		name   string
		phrase string
		mutate func(*taskflow.Plan)
	}{
		{"R1.b plan gains check extra", "immutable task plan changed", func(p *taskflow.Plan) {
			p.Checks = append(append([]taskflow.Check(nil), p.Checks...),
				taskflow.Check{ID: "extra", Argv: []string{"go", "test", "./internal/c"}, TimeoutSeconds: 300})
		}},
		{"R1.c plan drops check unit", "", func(p *taskflow.Plan) {
			p.Checks = []taskflow.Check{}
			reqs := append([]taskflow.Requirement(nil), p.Requirements...)
			reqs[0].CheckIDs = []string{}
			p.Requirements = reqs
		}},
		{"R1.d plan narrows check unit", "", func(p *taskflow.Plan) {
			checks := append([]taskflow.Check(nil), p.Checks...)
			checks[0].Argv = []string{"go", "test", "-run", "TestA", "./internal/a"}
			p.Checks = checks
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, dir := acStandardTask(t, acPlanOpts{})
			acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
			acAppendRevision(t, dir, func(rec *taskflow.Record) { tc.mutate(&rec.Plan) })
			_, err := taskflow.Load(dir)
			if err == nil {
				t.Fatalf("%s: Load accepted a revision whose plan differs from revision 1", tc.name)
			}
			if tc.phrase != "" && !strings.Contains(err.Error(), tc.phrase) {
				t.Errorf("%s: Load error %q lacks %q", tc.name, err, tc.phrase)
			}
			if _, err := acAdd(dir, acInput("HEAD", acCheck("b-tests", "go", "test", "./internal/b"))); err == nil {
				t.Errorf("%s: AddChecks succeeded on a record whose plan changed", tc.name)
			}
		})
	}
}

// acTestEligibility fails when an ineligible task accepts an
// amendment or an eligible one refuses it (R2).
func acTestEligibility(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R2.a small plan refused (B1)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{small: true})
		acExpectRefused(t, dir, acInput("HEAD", acWidened()), "checks can be added only to a substantial task")
	})
	t.Run("R2.b no baseline manifest refused at load (B2, OD-5)", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		rec := acLoad(t, dir)
		if rec.Revision != 1 || rec.BaselineManifest == nil {
			t.Fatalf("setup: started record revision %d, baseline manifest present %v; want revision 1 with a manifest", rec.Revision, rec.BaselineManifest != nil)
		}
		acRewriteLatest(t, dir, func(rec *taskflow.Record) { rec.BaselineManifest = nil })
		r.standard(t)
		acExpectRefused(t, dir, acInput("HEAD", acWidened()), "task record has no baseline_manifest"+l4NewTask)
	})
	t.Run("R2.f abandoned record refused (B0)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		if _, err := taskflow.Abandon(dir, "contract fixture abandons the task"); err != nil {
			t.Fatalf("setup: Abandon: %v", err)
		}
		acExpectRefusedIs(t, dir, acInput("HEAD", acWidened()), taskflow.ErrAbandoned)
	})
	t.Run("B0 precedes B1: abandoned small plan names abandonment", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{small: true})
		if _, err := taskflow.Abandon(dir, "contract fixture abandons the task"); err != nil {
			t.Fatalf("setup: Abandon: %v", err)
		}
		acExpectRefusedIs(t, dir, acInput("HEAD", acWidened()), taskflow.ErrAbandoned)
	})
}

// acTestMergeGate fails when an amendment is accepted without a
// qualifying merge from the base, or refused with one (R3).
func acTestMergeGate(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R3.a merge M accepted with exactly one new revision", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		rec := acExpectAccepted(t, dir, acInput(r.M, acWidened()))
		if got := rec.Amendments[0].MergeCommit; got != r.M {
			t.Errorf("R3.a: merge_commit = %q, want %q", got, r.M)
		}
	})
	t.Run("R3.b merge reachable from baseline refused (C9)", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput(r.B, acWidened()), "merge commit "+r.B+" is reachable from the task baseline HEAD")
	})
	t.Run("R3.c merge not reachable from HEAD refused (C8)", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		acGit(t, r.root, "checkout", "-q", "-b", "other", r.B)
		acWrite(t, r.root, "internal/c/c.go", "package c\n\n// C returns a fixture value.\nfunc C() int { return 33 }\n")
		acCommitAll(t, r.root, "other work")
		acGit(t, r.root, "merge", "-q", "--no-ff", "--no-edit", "origin/main")
		q := acHead(t, r.root)
		acGit(t, r.root, "checkout", "-q", "feature")
		acExpectRefused(t, dir, acInput(q, acWidened()), "merge commit "+q+" is not reachable from HEAD")
	})
	t.Run("R3.d rebased branch without merge commit refused (C7)", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		r.commitF(t, nil)
		r.commitO(t, nil)
		acGit(t, r.root, "rebase", "-q", "origin/main")
		h := acHead(t, r.root)
		acExpectRefused(t, dir, acInput("HEAD", acWidened()), "commit "+h+" is not a merge commit", acNoFFHint)
	})
	t.Run("R3.e merge whose second parent is not from the base refused (C11)", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		acGit(t, r.root, "checkout", "-q", "-b", "topic")
		acWrite(t, r.root, "internal/b/b.go", "package b\n\n// B returns a fixture value.\nfunc B() int { return 200 }\n")
		acCommitAll(t, r.root, "T")
		acGit(t, r.root, "checkout", "-q", "feature")
		acGit(t, r.root, "merge", "-q", "--no-ff", "--no-edit", "topic")
		n := acHead(t, r.root)
		acExpectRefused(t, dir, acInput(n, acWidened()),
			"merge commit "+n+" has no parent after the first that is reachable from base_ref origin/main")
	})
	t.Run("R3.f base ref HEAD refused (C10)", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		in := acInput(r.M, acWidened())
		in.BaseRef = "HEAD"
		acExpectRefused(t, dir, in, "merge commit "+r.M+" is already reachable from base_ref HEAD", acNoFFHint)
	})
	t.Run("R3.g base ref feature refused (C10)", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		in := acInput(r.M, acWidened())
		in.BaseRef = "feature"
		acExpectRefused(t, dir, in, "merge commit "+r.M+" is already reachable from base_ref feature", acNoFFHint)
	})
	t.Run("R3.h base ref naming no commit refused (C6)", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		in := acInput(r.M, acWidened())
		in.BaseRef = "origin/does-not-exist"
		acExpectRefused(t, dir, in, "base_ref origin/does-not-exist does not name a commit")
	})
	t.Run("R3.i uncommitted merge in progress refused", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		r.commitF(t, nil)
		r.commitO(t, nil)
		acGit(t, r.root, "merge", "-q", "--no-ff", "--no-commit", "origin/main")
		if mh := acGit(t, r.root, "rev-parse", "MERGE_HEAD"); mh != r.O {
			t.Fatalf("setup: MERGE_HEAD = %s, want O %s", mh, r.O)
		}
		acExpectRefused(t, dir, acInput(r.O, acWidened()))
	})
	t.Run("R3.j selected path outside Git refused (C3)", func(t *testing.T) {
		t.Parallel()
		base := acBase(t)
		root := filepath.Join(base, "plain")
		store := filepath.Join(base, "store")
		if err := acWriteFiles(root, acModuleFiles()); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.MkdirAll(store, 0o700); err != nil {
			t.Fatalf("setup: %v", err)
		}
		rec, err := taskflow.StartWithPacks(context.Background(), root, store, acPlan(acPlanOpts{}), map[string]string{}, taskflow.SelectionExplicit, acPacks())
		if err != nil {
			t.Fatalf("setup: taskflow.StartWithPacks outside Git: %v", err)
		}
		acExpectRefused(t, rec.Directory, acInput(strings.Repeat("ab", 20), acWidened()),
			"adding checks needs a Git repository with a committed HEAD")
	})
	t.Run("R3.k dirty working tree accepted and recorded dirty", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		acWrite(t, r.root, "internal/a/a.go", "package a\n\n// A returns a fixture value.\nfunc A() int { return 11 }\n")
		rec := acExpectAccepted(t, dir, acInput(r.M, acWidened()))
		if !rec.Amendments[0].Snapshot.Dirty {
			t.Errorf("R3.k: amendments.0.snapshot.dirty = false, want true")
		}
	})
	t.Run("R3.l fast-forward to single-parent base commit refused (C7)", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		r.commitO(t, nil)
		acGit(t, r.root, "merge", "-q", "origin/main")
		if h := acHead(t, r.root); h != r.O {
			t.Fatalf("setup: merge did not fast-forward to O (HEAD %s, O %s)", h, r.O)
		}
		acExpectRefused(t, dir, acInput("HEAD", acWidened()), "commit "+r.O+" is not a merge commit", acNoFFHint)
	})
	t.Run("R3.l fast-forward to a base merge commit refused (C10)", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		acGit(t, r.root, "checkout", "-q", "-B", "mainline", "refs/remotes/origin/main")
		acGit(t, r.root, "checkout", "-q", "-b", "side2")
		acWrite(t, r.root, "go.mod", "module example.com/m\n\ngo 1.21\n\n// base change\n")
		acWrite(t, r.root, "internal/b/b.go", "package b\n\n// B returns a fixture value.\nfunc B() int { return 20 }\n")
		acCommitAll(t, r.root, "S2")
		acGit(t, r.root, "checkout", "-q", "mainline")
		acGit(t, r.root, "merge", "-q", "--no-ff", "--no-edit", "side2")
		o := acHead(t, r.root)
		acGit(t, r.root, "update-ref", "refs/remotes/origin/main", o)
		acGit(t, r.root, "checkout", "-q", "feature")
		acGit(t, r.root, "merge", "-q", "origin/main")
		if h := acHead(t, r.root); h != o {
			t.Fatalf("setup: merge did not fast-forward to the base merge (HEAD %s, want %s)", h, o)
		}
		acExpectRefused(t, dir, acInput("HEAD", acWidened()), "merge commit "+o+" is already reachable from base_ref origin/main", acNoFFHint)
	})
	t.Run("R3.m recovery with git merge --no-ff accepted", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		r.commitO(t, nil)
		acGit(t, r.root, "merge", "-q", "origin/main")
		acGit(t, r.root, "reset", "-q", "--keep", "ORIG_HEAD")
		if h := acHead(t, r.root); h != r.B {
			t.Fatalf("setup: reset --keep ORIG_HEAD left HEAD %s, want B %s", h, r.B)
		}
		acGit(t, r.root, "merge", "-q", "--no-ff", "--no-edit", "origin/main")
		m2 := acHead(t, r.root)
		rec := acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		if got := rec.Amendments[0].MergeCommit; got != m2 {
			t.Errorf("R3.m: merge_commit = %q, want M2 %q", got, m2)
		}
	})
}

// acTestCheckValidation fails when added checks escape plan
// check validation, the timeout default or the 256-check limit (R4), or when
// the input rows are not checked row-major in the section 1.6 order.
func acTestCheckValidation(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R4.a no checks refused (A1)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD"), "add at least one check")
		in := acInput("HEAD")
		in.Checks = []taskflow.Check{}
		acExpectRefused(t, dir, in, "add at least one check")
	})
	t.Run("R4.b omitted timeout defaults to 300", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		rec := acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		want := []taskflow.Check{{ID: "widened", Argv: acWidenedArgv, TimeoutSeconds: 300}}
		if got := rec.Amendments[0].Checks; !reflect.DeepEqual(got, want) {
			t.Errorf("R4.b: amendments.0.checks = %+v, want %+v", got, want)
		}
	})
	t.Run("R4.c timeout 3601 refused (A6)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		c := acWidened()
		c.TimeoutSeconds = 3601
		acExpectRefused(t, dir, acInput("HEAD", c), "check timeout must be 1..3600 seconds")
	})
	t.Run("R4.c negative timeout refused (A6)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		c := acWidened()
		c.TimeoutSeconds = -1
		acExpectRefused(t, dir, acInput("HEAD", c), "check timeout must be 1..3600 seconds")
	})
	t.Run("R4 boundary timeout 3600 accepted", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		c := acWidened()
		c.TimeoutSeconds = 3600
		rec := acExpectAccepted(t, dir, acInput("HEAD", c))
		if got := rec.Amendments[0].Checks[0].TimeoutSeconds; got != 3600 {
			t.Errorf("R4: timeout_seconds = %d, want 3600", got)
		}
	})
	t.Run("R4.d 256 plan checks refused (B5)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{planChecks: 256})
		acExpectRefused(t, dir, acInput("HEAD", acWidened()), "planned and added checks would exceed 256")
	})
	t.Run("R4.d boundary 255 plan checks plus one accepted", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{planChecks: 255})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
	})
	t.Run("R4.e shell argv refused (A7)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("widened", "sh", "-c", "go test ./...")),
			"check widened: ", "checks cannot invoke a shell")
	})
	// An added check takes env_passthrough under the plan's rules: kept in
	// the stored amendment, and refused for a name plan admission refuses.
	t.Run("env_passthrough kept in the amendment (A7)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		rec := acExpectAccepted(t, dir, acInput("HEAD", acPassthroughCheck(t, "DB_PASSWORD")))
		data, err := json.Marshal(rec.Amendments[0].Checks[0])
		if err != nil || !bytes.Contains(data, []byte(`"env_passthrough":["DB_PASSWORD"]`)) {
			t.Errorf("added check stored as %s (%v); want env_passthrough [DB_PASSWORD]", data, err)
		}
	})
	t.Run("env_passthrough of a publication token refused (A7)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acPassthroughCheck(t, "GITHUB_TOKEN")),
			"check widened: env_passthrough cannot name GITHUB_TOKEN")
	})
	t.Run("A5 precedes A6 row-major", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		first := acWidened()
		first.TimeoutSeconds = 3601
		acExpectRefused(t, dir, acInput("HEAD", first, acCheck("widened", "go", "test", "./internal/b")),
			"added check IDs must be distinct identifiers")
	})
	t.Run("A5 non-identifier ID refused", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("1widened", acWidenedArgv...)), "added check IDs must be distinct identifiers")
	})
	t.Run("A1 precedes A2", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		in := acInput("HEAD")
		in.Reason = "   "
		acExpectRefused(t, dir, in, "add at least one check")
	})
}

// acTestRevisionShape fails when a malformed or option-like
// revision is accepted or reaches Git as anything but "names no commit"
// (section 1.2, A2-A4, C5, C6).
func acTestRevisionShape(t *testing.T) {
	acSetup(t)
	t.Parallel()
	bad := map[string]string{
		"empty":            "",
		"leading dash":     "-HEAD",
		"option injection": "--output=injected",
		"space":            "HEAD ~1",
		"tab":              "HEAD\t",
		"newline":          "HEAD\n",
		"control":          "HE\x01AD",
		"DEL":              "HEAD\x7f",
		"257 bytes":        strings.Repeat("a", 257),
	}
	names := make([]string, 0, len(bad))
	for k := range bad {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		value := bad[name]
		t.Run("A3 merge_commit "+name, func(t *testing.T) {
			t.Parallel()
			_, dir := acStandardTask(t, acPlanOpts{})
			acExpectRefused(t, dir, acInput(value, acWidened()), "merge_commit must name a Git revision")
		})
		t.Run("A4 base_ref "+name, func(t *testing.T) {
			t.Parallel()
			_, dir := acStandardTask(t, acPlanOpts{})
			in := acInput("HEAD", acWidened())
			in.BaseRef = value
			acExpectRefused(t, dir, in, "base_ref must name a Git revision")
		})
	}
	t.Run("A2 precedes A3", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		in := acInput("-bad", acWidened())
		in.Reason = ""
		acExpectRefused(t, dir, in, "an amendment reason of 1 to 16384 bytes is required")
	})
	t.Run("A3 precedes A4", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		in := acInput("-bad", acWidened())
		in.BaseRef = "-bad"
		acExpectRefused(t, dir, in, "merge_commit must name a Git revision")
	})
	t.Run("C5 256-byte merge_commit names no commit", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		v := strings.Repeat("a", 256)
		acExpectRefused(t, dir, acInput(v, acWidened()), "merge_commit "+v+" does not name a commit")
	})
	t.Run("C5 unknown full ID names no commit", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		v := strings.Repeat("0123456789", 4)
		acExpectRefused(t, dir, acInput(v, acWidened()), "merge_commit "+v+" does not name a commit")
	})
	t.Run("C6 256-byte base_ref names no commit", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		v := strings.Repeat("b", 256)
		in := acInput("HEAD", acWidened())
		in.BaseRef = v
		acExpectRefused(t, dir, in, "base_ref "+v+" does not name a commit")
	})
}

// acTestCreditedArgv fails when an added argv the Go rule does
// not credit is admitted, or when cw's GOFLAGS affects admission (R5).
func acTestCreditedArgv(t *testing.T) {
	acSetup(t)
	t.Run("R5.a make test refused (B6)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("widened", "make", "test")),
			"check widened is not a direct go test that the Go evidence rule credits")
	})
	t.Run("R5.b go test -run refused (B6)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("widened", "go", "test", "-run", "TestX", "./...")),
			"check widened is not a direct go test that the Go evidence rule credits")
	})
	t.Run("R5.c GOFLAGS in the environment plays no part", func(t *testing.T) {
		_, dir := acStandardTask(t, acPlanOpts{})
		t.Setenv("GOFLAGS", "-run=TestX")
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
	})
}

// acTestNewCoverage fails when an added check that covers no
// new qualifying item is admitted, or one that does is refused (R6).
func acTestNewCoverage(t *testing.T) {
	acSetup(t)
	t.Parallel()
	bTests := acCheck("b-tests", "go", "test", "./internal/b")
	t.Run("R6.a widened covers go.mod and internal/b", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
	})
	t.Run("R6.b b-tests covers internal/b", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", bTests))
	})
	t.Run("R6.c again repeats the plan's coverage (C12)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("again", "go", "test", "./internal/a")), "check again "+acC12Tail)
	})
	t.Run("R6.d import-path pattern covers nothing (C12)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("widened", "go", "test", "example.com/m/...")), "check widened "+acC12Tail)
	})
	t.Run("R6.e package changed by F but not by M (C12)", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		r.commitF(t, map[string]string{"internal/d/d.go": "package d\n\n// D returns a fixture value.\nfunc D() int { return 40 }\n"})
		r.commitO(t, nil)
		r.mergeM(t)
		acExpectRefused(t, dir, acInput("HEAD", acCheck("d-tests", "go", "test", "./internal/d")), "check d-tests "+acC12Tail)
	})
	t.Run("R6.f package restored to baseline after M (C12)", func(t *testing.T) {
		t.Parallel()
		r := acNewRepo(t)
		dir := r.start(t, acPlanOpts{})
		r.commitF(t, nil)
		r.commitO(t, map[string]string{"internal/c/c.go": "package c\n\n// C returns a fixture value.\nfunc C() int { return 30 }\n"})
		r.mergeM(t)
		acWrite(t, r.root, "internal/c/c.go", acBaselineC)
		acCommitAll(t, r.root, "restore internal/c")
		acExpectRefused(t, dir, acInput(r.M, acCheck("c-tests", "go", "test", "./internal/c")), "check c-tests "+acC12Tail)
	})
	t.Run("R6.g later amendment repeating earlier coverage (C12)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acExpectRefused(t, dir, acInput("HEAD", bTests), "check b-tests "+acC12Tail)
		if n := len(acLoad(t, dir).Amendments); n != 1 {
			t.Errorf("R6.g: amendments = %d, want only the first", n)
		}
	})
	t.Run("R6.h widened then b-tests refused naming b-tests (C12)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acWidened(), bTests), "check b-tests "+acC12Tail)
	})
	t.Run("R6.i b-tests then widened accepted in order", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		rec := acExpectAccepted(t, dir, acInput("HEAD", bTests, acWidened()))
		var ids []string
		for _, c := range rec.Amendments[0].Checks {
			ids = append(ids, c.ID)
		}
		if !reflect.DeepEqual(ids, []string{"b-tests", "widened"}) {
			t.Errorf("R6.i: added check IDs = %q, want [b-tests widened] in input order", ids)
		}
	})
}

// acTestFreshIDs fails when an amendment can replace, remove or
// narrow a planned or added check (R7), or a repeated accepted input is
// accepted again (idempotency).
func acTestFreshIDs(t *testing.T) {
	acSetup(t)
	t.Parallel()
	unitArgv := []string{"go", "test", "./internal/a"}
	assertPlanKept := func(t *testing.T, dir string) {
		t.Helper()
		rec := acLoad(t, dir)
		if len(rec.Plan.Checks) == 0 || rec.Plan.Checks[0].ID != "unit" || !reflect.DeepEqual(rec.Plan.Checks[0].Argv, unitArgv) {
			t.Errorf("plan.checks = %+v, want unit with argv %q", rec.Plan.Checks, unitArgv)
		}
		if len(rec.Plan.Requirements) != 1 || !reflect.DeepEqual(rec.Plan.Requirements[0].CheckIDs, []string{"unit"}) {
			t.Errorf("plan.requirements = %+v, want r1 naming unit", rec.Plan.Requirements)
		}
	}
	t.Run("R7.a narrowing unit refused (B4)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("unit", "go", "test", "-run", "TestA", "./internal/a")),
			"check unit is already a planned or added check; checks cannot be replaced, removed or narrowed")
		assertPlanKept(t, dir)
	})
	t.Run("R7.b replacing unit refused (B4)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acCheck("unit", acWidenedArgv...)),
			"check unit is already a planned or added check; checks cannot be replaced, removed or narrowed")
		assertPlanKept(t, dir)
	})
	t.Run("R7.c narrowing an added check refused (B4)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acExpectRefused(t, dir, acInput("HEAD", acCheck("widened", "go", "test", "./internal/b")),
			"check widened is already a planned or added check; checks cannot be replaced, removed or narrowed")
		rec := acLoad(t, dir)
		if len(rec.Amendments) != 1 || !reflect.DeepEqual(rec.Amendments[0].Checks[0].Argv, acWidenedArgv) {
			t.Errorf("R7.c: amendments = %+v, want one with widened argv %q", rec.Amendments, acWidenedArgv)
		}
	})
	t.Run("R7.d duplicate ID in one input refused (A5)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectRefused(t, dir, acInput("HEAD", acWidened(), acCheck("widened", "go", "test", "./internal/b")),
			"added check IDs must be distinct identifiers")
	})
	t.Run("idempotency: repeating an accepted input is refused and changes nothing", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acExpectRefused(t, dir, acInput("HEAD", acWidened()), "check widened is already a planned or added check")
	})
}

// acTestRecorded fails when an accepted amendment does not
// store the section 1.3 members, or the reason bounds are not enforced (R8).
func acTestRecorded(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R8.a members", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		rec := acExpectAccepted(t, dir, acInput(r.M[:12], acWidened()))
		snap, err := projectctx.Capture(context.Background(), r.root)
		if err != nil {
			t.Fatalf("setup: Capture: %v", err)
		}
		a := rec.Amendments[0]
		want := []taskflow.Check{{ID: "widened", Argv: acWidenedArgv, TimeoutSeconds: 300}}
		if !reflect.DeepEqual(a.Checks, want) {
			t.Errorf("R8.a: checks = %+v, want %+v", a.Checks, want)
		}
		if a.MergeCommit != r.M {
			t.Errorf("R8.a: merge_commit = %q, want M's full ID %q", a.MergeCommit, r.M)
		}
		if a.BaseRef != "origin/main" {
			t.Errorf("R8.a: base_ref = %q, want origin/main", a.BaseRef)
		}
		if a.BaseCommit != r.O {
			t.Errorf("R8.a: base_commit = %q, want O's full ID %q", a.BaseCommit, r.O)
		}
		if a.Reason != acReason {
			t.Errorf("R8.a: reason = %q, want %q", a.Reason, acReason)
		}
		if a.CoordinatorID != "/root/coord" {
			t.Errorf("R8.a: coordinator_id = %q, want /root/coord", a.CoordinatorID)
		}
		if a.Snapshot != snap {
			t.Errorf("R8.a: snapshot = %+v, want Capture of the unchanged selected path %+v", a.Snapshot, snap)
		}
		if a.At.IsZero() {
			t.Errorf("R8.a: at is zero")
		}
		if a.At.Before(rec.CreatedAt) || a.At.After(rec.UpdatedAt) {
			t.Errorf("R8.a: at %v outside created_at %v .. updated_at %v", a.At, rec.CreatedAt, rec.UpdatedAt)
		}
		raw, ok := acRawMember(t, acRevisionBytes(t, dir, rec.Revision), "amendments")
		if !ok {
			t.Fatalf("R8.a: latest revision has no amendments member")
		}
		var members []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil || len(members) != 1 {
			t.Fatalf("R8.a: amendments member %s does not decode to one object (%v)", raw, err)
		}
		var at string
		if err := json.Unmarshal(members[0]["at"], &at); err != nil || !strings.HasSuffix(at, "Z") {
			t.Errorf("R8.a: stored at %s is not an RFC 3339 UTC time", members[0]["at"])
		}
		for _, k := range []string{"checks", "merge_commit", "base_ref", "base_commit", "reason", "coordinator_id", "snapshot", "at"} {
			if _, ok := members[0][k]; !ok {
				t.Errorf("R8.a: stored amendment lacks member %q", k)
			}
		}
	})
	t.Run("R8.b plan without coordinator_id records /root", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{noCoordinator: true})
		rec := acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		if got := rec.Amendments[0].CoordinatorID; got != "/root" {
			t.Errorf("R8.b: coordinator_id = %q, want /root", got)
		}
	})
	t.Run("R8.c blank reason refused (A2)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		in := acInput("HEAD", acWidened())
		in.Reason = "   "
		acExpectRefused(t, dir, in, "an amendment reason of 1 to 16384 bytes is required")
	})
	t.Run("R8.d reason of 16384 bytes accepted", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		in := acInput("HEAD", acWidened())
		in.Reason = strings.Repeat("r", 16384)
		rec := acExpectAccepted(t, dir, in)
		if got := len(rec.Amendments[0].Reason); got != 16384 {
			t.Errorf("R8.d: stored reason length %d, want 16384 verbatim", got)
		}
	})
	t.Run("R8.d reason of 16385 bytes refused (A2)", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		in := acInput("HEAD", acWidened())
		in.Reason = strings.Repeat("r", 16385)
		acExpectRefused(t, dir, in, "an amendment reason of 1 to 16384 bytes is required")
	})
}

// acTestAppendOnlyLoad fails when a stored revision may drop,
// alter or misidentify an earlier amendment and still load (R9).
func acTestAppendOnlyLoad(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R9.a later revision drops the amendment", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acAppendRevision(t, dir, func(rec *taskflow.Record) { rec.Amendments = nil })
		_, err := taskflow.Load(dir)
		if err == nil || !strings.Contains(err.Error(), "check amendments are append-only") {
			t.Errorf("R9.a: Load error = %v, want one containing %q", err, "check amendments are append-only")
		}
	})
	t.Run("R9.b later revision alters the amendment", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acAppendRevision(t, dir, func(rec *taskflow.Record) {
			am := append([]taskflow.Amendment(nil), rec.Amendments...)
			checks := append([]taskflow.Check(nil), am[0].Checks...)
			checks[0].Argv = []string{"go", "test", "./internal/b"}
			am[0].Checks = checks
			rec.Amendments = am
		})
		if _, err := taskflow.Load(dir); err == nil {
			t.Errorf("R9.b: Load accepted a revision that altered an earlier amendment")
		}
	})
	t.Run("R9.c stored amendment reuses plan ID unit", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acRewriteLatest(t, dir, func(rec *taskflow.Record) {
			am := append([]taskflow.Amendment(nil), rec.Amendments...)
			checks := append([]taskflow.Check(nil), am[0].Checks...)
			checks[0].ID = "unit"
			am[0].Checks = checks
			rec.Amendments = am
		})
		_, err := taskflow.Load(dir)
		if err == nil || !strings.Contains(err.Error(), "invalid stored check amendment") {
			t.Errorf("R9.c: Load error = %v, want one containing %q", err, "invalid stored check amendment")
		}
	})
}

// acCopyAmendment returns a deep copy of a with its first check renamed id.
func acCopyAmendment(a taskflow.Amendment, id string) taskflow.Amendment {
	checks := make([]taskflow.Check, len(a.Checks))
	for i, c := range a.Checks {
		checks[i] = taskflow.Check{ID: c.ID, Argv: append([]string(nil), c.Argv...), TimeoutSeconds: c.TimeoutSeconds}
	}
	checks[0].ID = id
	a.Checks = checks
	return a
}

// acTestAppendOnlyCountAndOrder fails when a stored revision
// may add more than one amendment at once or reorder earlier amendments and
// still load (R9, invariant 2).
func acTestAppendOnlyCountAndOrder(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R9 two amendments appended in one revision", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acCheck("b-tests", "go", "test", "./internal/b")))
		grow := func(ids ...string) func(*taskflow.Record) {
			return func(rec *taskflow.Record) {
				am := append([]taskflow.Amendment(nil), rec.Amendments...)
				for _, id := range ids {
					am = append(am, acCopyAmendment(rec.Amendments[0], id))
				}
				rec.Amendments = am
				rec.EvidenceRevision++
				rec.Verification = nil
			}
		}
		// Positive control: appending exactly one well-formed amendment loads,
		// so the failure below is caused by the second one.
		acAppendRevisionControlled(t, dir, grow("x2"), grow("x2", "x3"))
		_, err := taskflow.Load(dir)
		if err == nil || !strings.Contains(err.Error(), "check amendments are append-only") {
			t.Errorf("R9: Load error = %v, want one containing %q for two amendments added in one revision", err, "check amendments are append-only")
		}
	})
	t.Run("R9 two amendments reordered", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acCheck("b-tests", "go", "test", "./internal/b")))
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acAppendRevision(t, dir, func(rec *taskflow.Record) {
			rec.Amendments = []taskflow.Amendment{rec.Amendments[1], rec.Amendments[0]}
		})
		_, err := taskflow.Load(dir)
		if err == nil || !strings.Contains(err.Error(), "check amendments are append-only") {
			t.Errorf("R9: Load error = %v, want one containing %q for reordered amendments", err, "check amendments are append-only")
		}
	})
}

// acTestStoredAmendmentShape fails when a stored amendment that
// breaks an invariant 5 rule loads, or when load re-applies the admission or
// credit rules to a stored amendment.
func acTestStoredAmendmentShape(t *testing.T) {
	acSetup(t)
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(rec *taskflow.Record, a *taskflow.Amendment)
	}{
		{"merge_commit abbreviated", func(_ *taskflow.Record, a *taskflow.Amendment) { a.MergeCommit = a.MergeCommit[:12] }},
		{"merge_commit uppercase", func(_ *taskflow.Record, a *taskflow.Amendment) { a.MergeCommit = strings.ToUpper(a.MergeCommit) }},
		{"base_commit abbreviated", func(_ *taskflow.Record, a *taskflow.Amendment) { a.BaseCommit = a.BaseCommit[:12] }},
		{"base_ref malformed", func(_ *taskflow.Record, a *taskflow.Amendment) { a.BaseRef = "-origin/main" }},
		{"reason blank", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Reason = "   " }},
		{"reason over 16384 bytes", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Reason = strings.Repeat("r", 16385) }},
		{"coordinator_id not the effective coordinator", func(_ *taskflow.Record, a *taskflow.Amendment) { a.CoordinatorID = "/root/other" }},
		{"snapshot root differs from baseline", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Snapshot.Root += "-elsewhere" }},
		{"snapshot selected_path differs from baseline", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Snapshot.SelectedPath += "/internal" }},
		{"snapshot head empty", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Snapshot.Head = "" }},
		{"at before created_at", func(rec *taskflow.Record, a *taskflow.Amendment) { a.At = rec.CreatedAt.Add(-time.Hour) }},
		{"at after updated_at", func(rec *taskflow.Record, a *taskflow.Amendment) { a.At = rec.UpdatedAt.Add(time.Hour) }},
		{"no checks", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Checks = []taskflow.Check{} }},
		{"timeout 0", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Checks[0].TimeoutSeconds = 0 }},
		{"timeout 3601", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Checks[0].TimeoutSeconds = 3601 }},
		{"empty argv", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Checks[0].Argv = []string{} }},
		{"argv with NUL", func(_ *taskflow.Record, a *taskflow.Amendment) {
			a.Checks[0].Argv = []string{"go", "test", "./...\x00"}
		}},
		{"non-identifier check ID", func(_ *taskflow.Record, a *taskflow.Amendment) { a.Checks[0].ID = "1widened" }},
	}
	for _, tc := range cases {
		t.Run("invariant 5 "+tc.name, func(t *testing.T) {
			t.Parallel()
			_, dir := acStandardTask(t, acPlanOpts{})
			acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
			acRewriteLatest(t, dir, func(rec *taskflow.Record) {
				a := acCopyAmendment(rec.Amendments[0], rec.Amendments[0].Checks[0].ID)
				tc.mutate(rec, &a)
				rec.Amendments = []taskflow.Amendment{a}
			})
			if _, err := taskflow.Load(dir); err == nil {
				t.Errorf("invariant 5: Load accepted a stored amendment with %s", tc.name)
			}
		})
	}
	t.Run("invariant 5 load does not re-apply the credit rule", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acRewriteLatest(t, dir, func(rec *taskflow.Record) {
			a := acCopyAmendment(rec.Amendments[0], "widened")
			a.Checks[0].Argv = []string{"go", "test", "-run", "TestX", "./..."}
			rec.Amendments = []taskflow.Amendment{a}
		})
		if _, err := taskflow.Load(dir); err != nil {
			t.Errorf("invariant 5: Load re-applied the credit rule to a stored amendment: %v", err)
		}
	})
}

// acTestClearsVerification fails when acceptance keeps the
// stored verification or does not advance evidence_revision and revision by
// exactly one (R10.a, R10.c, invariant 7).
func acTestClearsVerification(t *testing.T) {
	acSetup(t)
	t.Parallel()
	r := acNewRepo(t)
	dir := r.start(t, acPlanOpts{checkpointsOff: true})
	r.commitF(t, nil)
	acRunPassed(t, dir, "unit")
	if err := acAssess(t, dir, r.root, []string{"unit"}, []string{"unit"}); err != nil {
		t.Fatalf("setup: assess: %v", err)
	}
	acVerify(t, dir)
	r.commitO(t, nil)
	r.mergeM(t)
	prev := acLoad(t, dir)
	if prev.Verification == nil {
		t.Fatalf("setup: verify stored no verification")
	}
	if !prev.Verification.Complete {
		t.Logf("note: the stored verification before the amendment is not complete: %q", prev.Verification.Problems)
	}
	rec := acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
	if rec.Verification != nil {
		t.Errorf("R10.a: verification kept after the amendment: %+v", rec.Verification)
	}
	if _, ok := acRawMember(t, acRevisionBytes(t, dir, rec.Revision), "verification"); ok {
		t.Errorf("R10.a: latest revision file still has a verification member")
	}
	if rec.EvidenceRevision != prev.EvidenceRevision+1 {
		t.Errorf("R10.c: evidence_revision = %d, want %d", rec.EvidenceRevision, prev.EvidenceRevision+1)
	}
	if rec.Revision != prev.Revision+1 {
		t.Errorf("R10.c: revision = %d, want %d", rec.Revision, prev.Revision+1)
	}
	if !reflect.DeepEqual(rec.Checks, prev.Checks) || !reflect.DeepEqual(rec.Assessments, prev.Assessments) || rec.Baseline != prev.Baseline {
		t.Errorf("invariant 7: the amendment changed check runs, assessments or the baseline")
	}
}

// acTestRunAddedCheck fails when an added check cannot be run
// by its ID, an unknown ID runs, or a stored run of an added check may carry a
// different argv (R11).
func acTestRunAddedCheck(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R11.a run widened", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		run := acRun(t, dir, "widened")
		if run.CheckID != "widened" || !reflect.DeepEqual(run.Argv, acWidenedArgv) {
			t.Errorf("R11.a: run check_id %q argv %q, want widened %q", run.CheckID, run.Argv, acWidenedArgv)
		}
		if run.Status == "" || run.Status == "running" || run.FinishedAt == nil {
			t.Errorf("R11.a: run status %q finished %v, want a terminal run", run.Status, run.FinishedAt)
		}
		rec := acLoad(t, dir)
		found := false
		for _, c := range rec.Checks {
			if c.ID == run.ID && c.CheckID == "widened" {
				found = true
			}
		}
		if !found {
			t.Errorf("R11.a: run %s of widened is not recorded", run.ID)
		}
	})
	t.Run("R11.b unknown ID refused", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		prev := acLoad(t, dir)
		_, err := taskflow.RunCheck(context.Background(), dir, "nonexistent")
		if err == nil || !strings.Contains(err.Error(), "check is not in the explicit coordinator plan") {
			t.Errorf("R11.b: RunCheck(nonexistent) error = %v, want %q", err, "check is not in the explicit coordinator plan")
		}
		after := acLoad(t, dir)
		if len(after.Checks) != len(prev.Checks) || after.Revision != prev.Revision {
			t.Errorf("R11.b: refused run changed the record (runs %d -> %d, revision %d -> %d)", len(prev.Checks), len(after.Checks), prev.Revision, after.Revision)
		}
	})
	t.Run("R11.c stored run of widened with another argv fails load", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		run := acRun(t, dir, "widened")
		acAppendRevision(t, dir, func(rec *taskflow.Record) {
			runs := append([]taskflow.CheckRun(nil), rec.Checks...)
			changed := false
			for i := range runs {
				if runs[i].ID == run.ID {
					runs[i].Argv = []string{"go", "test", "./internal/b"}
					changed = true
				}
			}
			if !changed {
				t.Fatalf("setup: run %s not in the latest revision", run.ID)
			}
			rec.Checks = runs
		})
		if _, err := taskflow.Load(dir); err == nil {
			t.Errorf("R11.c: Load accepted a stored run of widened whose argv differs from the amendment")
		}
	})
}

// acTestCitingAddedChecks fails when an added ID is not
// citable in a worker brief, an unknown one is, or the reviewer bundle omits
// or misorders the added check (R12.c assign step, R12.d, R12.e).
func acTestCitingAddedChecks(t *testing.T) {
	acSetup(t)
	t.Parallel()
	brief := func(checkIDs ...string) taskflow.WorkerBrief {
		return taskflow.WorkerBrief{
			ID: "impl-1", AgentID: "/root/worker", Role: "implementation", Stage: "implementation", Goal: "extend internal/b",
			OwnedPaths: []string{"internal/b"}, Constraints: []string{"contract fixture"},
			CheckIDs: checkIDs, StopCondition: "the cited checks pass",
		}
	}
	t.Run("R12.c brief citing widened is assigned", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{checkpointsOff: true})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		if _, err := taskflow.AssignWorker(context.Background(), dir, brief("widened")); err != nil {
			t.Errorf("R12.c: AssignWorker with check IDs [widened] refused: %v", err)
		}
	})
	t.Run("R12.d brief citing nonexistent is refused", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{checkpointsOff: true})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		_, err := taskflow.AssignWorker(context.Background(), dir, brief("nonexistent"))
		if err == nil || !strings.Contains(err.Error(), "worker brief references unplanned check nonexistent") {
			t.Errorf("R12.d: AssignWorker error = %v, want %q", err, "worker brief references unplanned check nonexistent")
		}
	})
	t.Run("R12.e bundle lists unit then widened", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acRunPassed(t, dir, "unit")
		run := acRunPassed(t, dir, "widened")
		b, err := taskflow.BuildBundle(context.Background(), dir, "review-1")
		if err != nil {
			t.Fatalf("R12.e: BuildBundle: %v", err)
		}
		if len(b.Checks) != 2 || b.Checks[0].CheckID != "unit" || b.Checks[1].CheckID != "widened" {
			t.Fatalf("R12.e: bundle checks = %+v, want unit then widened", b.Checks)
		}
		w := b.Checks[1]
		if w.RunID != run.ID || w.Status != "passed" || !w.Current {
			t.Errorf("R12.e: widened row = %+v, want run %s, status passed, current true", w, run.ID)
		}
	})
}

// acTestVerifyCredit fails when verify credits an added check's
// run wrongly (R13.e, R13.f, R13.g), ignores its failed latest run (R15.a), or
// lets it stand in for a plan check (R16.a, R16.b).
func acTestVerifyCredit(t *testing.T) {
	acSetup(t)
	t.Run("R13.e passed added check satisfies the fallback", func(t *testing.T) {
		t.Parallel()
		_, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acRunPassed(t, dir, "widened")
		if v := acVerify(t, dir); acHasProblem(v, acFallbackProblem) {
			t.Errorf("R13.e: verify reports %q although widened passed; problems %q", acFallbackProblem, v.Problems)
		}
	})
	t.Run("R13.f and R15.a widened passed then failed", func(t *testing.T) {
		r, dir := acStandardTask(t, acPlanOpts{})
		acRunPassed(t, dir, "unit")
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acRunPassed(t, dir, "widened")
		r.acMark(t, "b", true)
		if run := acRun(t, dir, "widened"); run.Status != "failed" {
			t.Fatalf("setup: second run of widened has status %q, want failed", run.Status)
		}
		r.acMark(t, "b", false)
		v := acVerify(t, dir)
		if !acHasProblem(v, acGoModProblem) {
			t.Errorf("R13.f: verify problems %q lack %q", v.Problems, acGoModProblem)
		}
		if v.Complete || !acHasProblem(v, "latest run of widened is failed") {
			t.Errorf("R15.a: verify complete=%v problems %q; want incomplete with %q", v.Complete, v.Problems, "latest run of widened is failed")
		}
	})
	t.Run("R13.g widened passed with compiler GOFLAGS", func(t *testing.T) {
		_, dir := acStandardTask(t, acPlanOpts{})
		acRunPassed(t, dir, "unit")
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		t.Setenv("GOFLAGS", "-gcflags=all=-N")
		acRunPassed(t, dir, "widened")
		t.Setenv("GOFLAGS", "")
		if v := acVerify(t, dir); !acHasProblem(v, acGoModProblem) {
			t.Errorf("R13.g: verify problems %q lack %q", v.Problems, acGoModProblem)
		}
	})
	t.Run("R16.a failed unit still blocks", func(t *testing.T) {
		r, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acRunPassed(t, dir, "widened")
		r.acMark(t, "a", true)
		if run := acRun(t, dir, "unit"); run.Status != "failed" {
			t.Fatalf("setup: run of unit has status %q, want failed", run.Status)
		}
		r.acMark(t, "a", false)
		v := acVerify(t, dir)
		if v.Complete || !acHasProblem(v, "latest run of unit is failed") {
			t.Errorf("R16.a: verify complete=%v problems %q; want incomplete with %q", v.Complete, v.Problems, "latest run of unit is failed")
		}
	})
	t.Run("R16.b assessment citing only widened refused", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acRunPassed(t, dir, "unit")
		acRunPassed(t, dir, "widened")
		err := acAssess(t, dir, r.root, []string{"widened"}, nil)
		if err == nil || !strings.Contains(err.Error(), "omits planned check unit") {
			t.Errorf("R16.b: Assess error = %v, want one containing %q", err, "omits planned check unit")
		}
	})
}

// acTestNoRecheckAfterAdmission fails when run or verify
// re-inspect the merge conditions after admission (R14).
func acTestNoRecheckAfterAdmission(t *testing.T) {
	acSetup(t)
	t.Parallel()
	t.Run("R14.a M unreachable after squash re-merge", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acGit(t, r.root, "reset", "-q", "--keep", r.F)
		acGit(t, r.root, "merge", "-q", "--squash", "origin/main")
		acGit(t, r.root, "commit", "-q", "-m", "squash origin/main")
		if out := exec.Command("git", "-C", r.root, "merge-base", "--is-ancestor", r.M, "HEAD").Run(); out == nil {
			t.Fatalf("setup: M is still reachable from HEAD")
		}
		run := acRun(t, dir, "widened")
		if run.CheckID != "widened" {
			t.Fatalf("R14.a: run check_id %q, want widened", run.CheckID)
		}
		if run.Status != "passed" {
			t.Fatalf("setup: run of widened status %q, want passed", run.Status)
		}
		acRunPassed(t, dir, "unit")
		v := acVerify(t, dir)
		if acHasProblem(v, acGoModProblem) || acHasProblem(v, acPkgBProblem) {
			t.Errorf("R14.a: verify problems %q include a Go problem after passed runs", v.Problems)
		}
	})
	t.Run("R14.b base advanced past M", func(t *testing.T) {
		t.Parallel()
		r, dir := acStandardTask(t, acPlanOpts{})
		acExpectAccepted(t, dir, acInput("HEAD", acWidened()))
		acGit(t, r.root, "update-ref", "refs/remotes/origin/main", r.M)
		run := acRun(t, dir, "widened")
		if run.CheckID != "widened" || run.Status != "passed" {
			t.Fatalf("R14.b: run check_id %q status %q, want a passed run of widened", run.CheckID, run.Status)
		}
		acRunPassed(t, dir, "unit")
		if v := acVerify(t, dir); acHasProblem(v, acGoModProblem) {
			t.Errorf("R14.b: verify problems %q include %q", v.Problems, acGoModProblem)
		}
	})
}

// acTestNeverAmendedHasNoMember fails when a never-amended
// record gains an amendments member (R21.a).
func acTestNeverAmendedHasNoMember(t *testing.T) {
	acSetup(t)
	t.Parallel()
	r := acNewRepo(t)
	dir := r.start(t, acPlanOpts{checkpointsOff: true})
	r.commitF(t, nil)
	acRunPassed(t, dir, "unit")
	if err := acAssess(t, dir, r.root, []string{"unit"}, []string{"unit"}); err != nil {
		t.Fatalf("setup: assess: %v", err)
	}
	acVerify(t, dir)
	rec := acLoad(t, dir)
	for n := 1; n <= rec.Revision; n++ {
		if _, ok := acRawMember(t, acRevisionBytes(t, dir, n), "amendments"); ok {
			t.Errorf("R21.a: revision %d of a never-amended record has an amendments member", n)
		}
	}
}

// acTestConcurrentAdmission fails when two concurrent identical
// amendments are both accepted (invariants 2 and 3: unique IDs, at most one
// amendment per revision).
func acTestConcurrentAdmission(t *testing.T) {
	acSetup(t)
	t.Parallel()
	_, dir := acStandardTask(t, acPlanOpts{})
	prev := acLoad(t, dir)
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = acAdd(dir, acInput("HEAD", acWidened()))
		}(i)
	}
	close(start)
	wg.Wait()
	accepted := 0
	for _, err := range errs {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("concurrent identical amendments: %d accepted (errors %v), want exactly 1", accepted, errs)
	}
	rec := acLoad(t, dir)
	if len(rec.Amendments) != 1 || rec.Revision != prev.Revision+1 {
		t.Errorf("concurrent identical amendments: %d amendments at revision %d, want 1 at revision %d", len(rec.Amendments), rec.Revision, prev.Revision+1)
	}
}
