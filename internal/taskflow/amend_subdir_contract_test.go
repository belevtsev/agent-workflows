package taskflow_test

// Independent contract tests for post-merge check amendment (interface
// contract revision 2, assignment tests-3). Expectations come only from the
// interface contract and the approved declaration extracts:
//
//   - section 1.4 "Path bases": AddChecks converts MergeFacts.Changed (relative
//     to the repository root) to paths relative to the selected path, dropping
//     paths outside it, when the selected path is a subdirectory of the work
//     tree (R6, R13 in a subdirectory layout);
//   - C7 with replace refs and grafts: a single-parent commit stays a
//     single-parent commit for admission, whatever a graft claims (R3, R19);
//   - C4 for a shallow clone (R19.o), through AddChecks and cw task add-checks.
//
// The tests are not parallel: each sets a hermetic environment with t.Setenv.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	subC12Phrase  = "covers no package or module file that the merge changed, that is still changed since the baseline, and that no earlier check covers"
	subNoFFHint   = "git merge --no-ff"
	subGoModProb  = "changed go.mod has no passed go test ./... run at the current source"
	subPkgBProb   = "changed package internal/b has no passed go test run at the current source"
	subC4Phrase   = "inspect merge commit: "
	subShallowMsg = "repository is shallow"
)

// subEnv makes Git, Go and cw hermetic for one non-parallel test. The Go build
// cache is the only shared location: it is content-addressed, and a cold cache
// would make every inner go test rebuild the runtime.
func subEnv(t *testing.T) {
	t.Helper()
	gocache := os.Getenv("GOCACHE")
	if gocache == "" {
		if out, err := exec.Command("go", "env", "GOCACHE").Output(); err == nil {
			gocache = strings.TrimSpace(string(out))
		}
	}
	home := subResolved(t, t.TempDir())
	gitConfig := filepath.Join(home, "gitconfig-empty")
	if err := os.WriteFile(gitConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	set := map[string]string{
		"HOME":                home,
		"XDG_CONFIG_HOME":     filepath.Join(home, "xdg"),
		"GIT_CONFIG_GLOBAL":   gitConfig,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Contract Author",
		"GIT_AUTHOR_EMAIL":    "author@example.com",
		"GIT_COMMITTER_NAME":  "Contract Author",
		"GIT_COMMITTER_EMAIL": "author@example.com",
		"GIT_TERMINAL_PROMPT": "0",
		"GOFLAGS":             "",
		"GOENV":               filepath.Join(home, "missing-goenv"),
		"GOTOOLCHAIN":         "local",
		"GOPROXY":             "off",
		"GOWORK":              "off",
		"GOPATH":              filepath.Join(home, "gopath"),
		"CW_MODEL_POLICY":     "",
		"TYPESAFE_API_KEY":    "",
		"SUBQ_API_KEY":        "",
		"OPENAI_API_KEY":      "",
	}
	if gocache != "" && gocache != "off" {
		set["GOCACHE"] = gocache
	}
	for k, v := range set {
		t.Setenv(k, v)
	}
	for _, k := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NO_REPLACE_OBJECTS",
		"GIT_REPLACE_REF_BASE", "GIT_GRAFT_FILE", "GIT_CEILING_DIRECTORIES",
		"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_COUNT",
	} {
		t.Setenv(k, "") // records the old value for restoration
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
}

func subResolved(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func subGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func subWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func subCommitAll(t *testing.T, dir, message string) string {
	t.Helper()
	subGit(t, dir, "add", "-A")
	subGit(t, dir, "commit", "-q", "--no-verify", "-m", message)
	return subGit(t, dir, "rev-parse", "HEAD")
}

func subPlan() taskflow.Plan {
	return taskflow.Plan{
		Title:         "post-merge amendment contract fixture",
		Kind:          "substantial",
		CoordinatorID: "/root/coord",
		Requirements: []taskflow.Requirement{{
			ID:          "r1",
			Description: "package a keeps working",
			CheckIDs:    []string{"unit"},
		}},
		Checks: []taskflow.Check{{
			ID:             "unit",
			Argv:           []string{"go", "test", "./internal/a"},
			TimeoutSeconds: 300,
		}},
		Checkpoints: &taskflow.CheckpointPolicy{
			Required: false,
			Reason:   "contract test fixture: amendments are independent of adviser checkpoints",
		},
	}
}

// subStore returns a new private (0700) task storage directory outside every
// project the test creates.
func subStore(t *testing.T) string {
	t.Helper()
	store := filepath.Join(subResolved(t, t.TempDir()), "store")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0o700); err != nil {
		t.Fatal(err)
	}
	return store
}

func subStart(t *testing.T, repo string) taskflow.Record {
	t.Helper()
	store := subStore(t)
	rec, err := taskflow.Start(context.Background(), repo, store, subPlan(), map[string]string{})
	if err != nil {
		t.Fatalf("Start(%s): %v", repo, err)
	}
	return rec
}

func subLoad(t *testing.T, dir string) taskflow.Record {
	t.Helper()
	rec, err := taskflow.Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	return rec
}

// subDirState hashes every regular file under the task directory, so a
// refusal can be shown to write no revision and no other file (invariant 7).
func subDirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			state[rel+"/"] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		state[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func subWidened() taskflow.Check {
	return taskflow.Check{ID: "widened", Argv: []string{"go", "test", "./..."}}
}

// subRefused asserts the "Refused" meaning of section 1.5 for AddChecks: a
// non-nil error holding every phrase, and no change in the task directory.
func subRefused(t *testing.T, dir string, input taskflow.AmendmentInput, phrases ...string) {
	t.Helper()
	beforeRev := subLoad(t, dir).Revision
	beforeState := subDirState(t, dir)
	rec, err := taskflow.AddChecks(context.Background(), dir, input)
	if err == nil {
		t.Fatalf("AddChecks(%+v) accepted (revision %d, %d amendments), want refusal containing %q",
			input, rec.Revision, len(rec.Amendments), phrases)
	}
	for _, phrase := range phrases {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("AddChecks error = %q, want it to contain %q", err, phrase)
		}
	}
	if got := subLoad(t, dir).Revision; got != beforeRev {
		t.Errorf("refused AddChecks moved revision %d -> %d", beforeRev, got)
	}
	if after := subDirState(t, dir); !reflect.DeepEqual(beforeState, after) {
		t.Errorf("refused AddChecks changed the task directory:\nbefore %v\nafter  %v", beforeState, after)
	}
}

func subAccepted(t *testing.T, dir string, input taskflow.AmendmentInput) taskflow.Record {
	t.Helper()
	before := subLoad(t, dir)
	rec, err := taskflow.AddChecks(context.Background(), dir, input)
	if err != nil {
		t.Fatalf("AddChecks(%+v) refused: %v", input, err)
	}
	after := subLoad(t, dir)
	if after.Revision != before.Revision+1 {
		t.Errorf("accepted AddChecks: revision %d -> %d, want exactly one new revision", before.Revision, after.Revision)
	}
	if len(after.Amendments) != len(before.Amendments)+1 {
		t.Fatalf("accepted AddChecks: amendments %d -> %d, want one more", len(before.Amendments), len(after.Amendments))
	}
	if rec.Revision != after.Revision {
		t.Errorf("AddChecks returned revision %d, stored latest is %d", rec.Revision, after.Revision)
	}
	return after
}

func subRunPassed(t *testing.T, dir, checkID string) {
	t.Helper()
	run, err := taskflow.Run(context.Background(), dir, checkID)
	if err != nil {
		t.Fatalf("Run(%s): %v", checkID, err)
	}
	if run.Status != "passed" || run.CheckID != checkID {
		t.Fatalf("Run(%s) = check %q status %q (failure %q), want a passed run of %s",
			checkID, run.CheckID, run.Status, run.Failure, checkID)
	}
}

// subVerifyText returns every problem verify reported, plus the error text if
// Verify reports an incomplete gate as an error, so both presence and absence
// assertions see everything verify said. It fails the test when Verify
// returned an error and no problem list can be observed, so an absence
// assertion never passes on a verify that did not run.
func subVerifyText(t *testing.T, dir string) []string {
	t.Helper()
	called := time.Now().Add(-time.Second)
	v, err := taskflow.Verify(context.Background(), dir)
	if err == nil {
		return v.Problems
	}
	problems := append([]string(nil), v.Problems...)
	if rec, lerr := taskflow.Load(dir); lerr == nil && rec.Verification != nil && rec.Verification.CheckedAt.After(called) {
		problems = append(problems, rec.Verification.Problems...)
	}
	if len(problems) == 0 {
		t.Fatalf("Verify(%s) returned %v and no observable problem list", dir, err)
	}
	return append(problems, err.Error())
}

func subMentions(lines []string, phrase string) bool {
	for _, line := range lines {
		if strings.Contains(line, phrase) {
			return true
		}
	}
	return false
}

// subSubdirFixture builds the subdirectory layout of item R1/section 1.4:
//
//	work tree wt/ (no go.mod at its top)
//	  README.md
//	  svc/go.mod, svc/internal/{a,b,c}/*.go   <- the selected path is wt/svc
//
// B (main, feature) is the baseline. O (origin/main, child of B) changes
// svc/go.mod, svc/internal/b/b.go, README.md and adds internal/c/c.go at the
// work-tree top, outside the selected path. The task starts at B on feature.
// The caller then calls subSubdirMerge, which commits F (changing
// svc/internal/c/c.go) and merges origin/main with --no-ff into M = (F, O).
//
// Relative to F, M changes README.md, internal/c/c.go, svc/go.mod and
// svc/internal/b/b.go (repository-root paths). Relative to the selected path,
// after conversion, only go.mod and internal/b/b.go remain: README.md and
// internal/c/c.go are outside svc/ and are dropped.
type subRepo struct {
	worktree string // work tree root
	selected string // selected path (svc)
	base     string // B
	upstream string // O
}

func subSubdirFixture(t *testing.T) subRepo {
	t.Helper()
	wt := subResolved(t, t.TempDir())
	subGit(t, wt, "init", "-q", "-b", "main")
	subWrite(t, wt, "README.md", "fixture\n")
	subWrite(t, wt, "svc/go.mod", "module example.com/svc\n\ngo 1.21\n")
	subWrite(t, wt, "svc/internal/a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	subWrite(t, wt, "svc/internal/b/b.go", "package b\n\nfunc B() int { return 1 }\n")
	subWrite(t, wt, "svc/internal/c/c.go", "package c\n\nfunc C() int { return 1 }\n")
	base := subCommitAll(t, wt, "B: baseline")
	subGit(t, wt, "branch", "feature")

	subWrite(t, wt, "README.md", "fixture, upstream\n")
	subWrite(t, wt, "svc/go.mod", "module example.com/svc\n\ngo 1.21\n\n// upstream change\n")
	subWrite(t, wt, "svc/internal/b/b.go", "package b\n\nfunc B() int { return 2 }\n")
	subWrite(t, wt, "internal/c/c.go", "package c\n\nfunc Outside() int { return 1 }\n")
	upstream := subCommitAll(t, wt, "O: upstream")
	subGit(t, wt, "update-ref", "refs/remotes/origin/main", upstream)
	subGit(t, wt, "checkout", "-q", "feature")
	if got := subGit(t, wt, "rev-parse", "HEAD"); got != base {
		t.Fatalf("feature HEAD = %s, want B %s", got, base)
	}
	return subRepo{worktree: wt, selected: filepath.Join(wt, "svc"), base: base, upstream: upstream}
}

// subSubdirMerge commits F and the --no-ff merge M; it returns M's full ID.
func subSubdirMerge(t *testing.T, r subRepo) string {
	t.Helper()
	subWrite(t, r.worktree, "svc/internal/c/c.go", "package c\n\nfunc C() int { return 3 }\n")
	feature := subCommitAll(t, r.worktree, "F: feature work in internal/c")
	subGit(t, r.worktree, "merge", "-q", "--no-ff", "--no-edit", "origin/main")
	merge := subGit(t, r.worktree, "rev-parse", "HEAD")
	if parents := subGit(t, r.worktree, "rev-list", "--parents", "-n", "1", merge); parents != merge+" "+feature+" "+r.upstream {
		t.Fatalf("fixture merge parents = %q, want M F O", parents)
	}
	changed := subGit(t, r.worktree, "diff", "--name-only", feature, merge)
	if want := "README.md\ninternal/c/c.go\nsvc/go.mod\nsvc/internal/b/b.go"; changed != want {
		t.Fatalf("fixture M changes relative to F = %q, want %q", changed, want)
	}
	return merge
}

// TestContractSubSelectedSubdirConvertsMergePaths guards section 1.4 "Path
// bases" through AddChecks: with the selected path a subdirectory of the work
// tree, M's changed files (repository-root relative: svc/go.mod,
// svc/internal/b/b.go) must be converted to go.mod and internal/b/b.go.
// Violations that fail it:
//   - no conversion: b-tests and widened cover nothing new and are refused (C12);
//   - outside paths kept instead of dropped: the work-tree file
//     internal/c/c.go would be read as the selected path's internal/c, and
//     c-tests would be admitted;
//   - a blanket accept: again (covered by the plan check) would be admitted;
//   - verify not crediting the added run in this layout: the go.mod and
//     internal/b problems stay after widened passed.
func TestContractSubSelectedSubdirConvertsMergePaths(t *testing.T) {
	subEnv(t)
	r := subSubdirFixture(t)
	started := subStart(t, r.selected)
	dir := started.Directory
	merge := subSubdirMerge(t, r)

	input := func(checks ...taskflow.Check) taskflow.AmendmentInput {
		return taskflow.AmendmentInput{
			MergeCommit: merge,
			BaseRef:     "origin/main",
			Reason:      "merged origin/main; svc/go.mod and svc/internal/b changed",
			Checks:      checks,
		}
	}

	// C12: covered by the plan check unit and unchanged: covers nothing new.
	subRefused(t, dir, input(taskflow.Check{ID: "again", Argv: []string{"go", "test", "./internal/a"}}),
		"check again "+subC12Phrase)
	// C12: internal/c is changed since the baseline (F), but M changed only the
	// work-tree file internal/c/c.go outside the selected path, which must be
	// dropped rather than read as svc/internal/c/c.go.
	subRefused(t, dir, input(taskflow.Check{ID: "c-tests", Argv: []string{"go", "test", "./internal/c"}}),
		"check c-tests "+subC12Phrase)

	// internal/b/b.go converted from svc/internal/b/b.go: b-tests covers internal/b.
	rec := subAccepted(t, dir, input(taskflow.Check{ID: "b-tests", Argv: []string{"go", "test", "./internal/b"}}))
	// go.mod converted from svc/go.mod: widened still covers the module file,
	// although internal/b is now covered by b-tests.
	rec = subAccepted(t, dir, input(subWidened()))

	if len(rec.Amendments) != 2 {
		t.Fatalf("amendments = %d, want 2", len(rec.Amendments))
	}
	for i, a := range rec.Amendments {
		if a.MergeCommit != merge || a.BaseCommit != r.upstream || a.BaseRef != "origin/main" {
			t.Errorf("amendment %d merge/base = %s/%s/%q, want %s/%s/origin/main",
				i, a.MergeCommit, a.BaseCommit, a.BaseRef, merge, r.upstream)
		}
		if a.Snapshot.Root != rec.Baseline.Root || a.Snapshot.SelectedPath != rec.Baseline.SelectedPath {
			t.Errorf("amendment %d snapshot root/selected = %q/%q, want the baseline's %q/%q",
				i, a.Snapshot.Root, a.Snapshot.SelectedPath, rec.Baseline.Root, rec.Baseline.SelectedPath)
		}
	}
	if rec.Baseline.Root == rec.Baseline.SelectedPath {
		t.Fatalf("fixture error: baseline root %q equals selected path; the layout is not a subdirectory", rec.Baseline.Root)
	}
	if got := rec.Amendments[1].Checks; len(got) != 1 || got[0].ID != "widened" || got[0].TimeoutSeconds != 300 {
		t.Errorf("second amendment checks = %+v, want widened with timeout 300", got)
	}

	// R13.c (no pack set): with only unit passed, the changed go.mod is unpaid.
	subRunPassed(t, dir, "unit")
	if text := subVerifyText(t, dir); !subMentions(text, subGoModProb) {
		t.Errorf("verify with only unit passed = %q, want it to report %q", text, subGoModProb)
	}
	// R13.d in this layout: after widened passed, neither Go problem remains.
	subRunPassed(t, dir, "widened")
	text := subVerifyText(t, dir)
	for _, phrase := range []string{subGoModProb, subPkgBProb, "changed go.mod", "changed package internal/b"} {
		if subMentions(text, phrase) {
			t.Errorf("verify after widened passed reports %q in %q", phrase, text)
		}
	}
}

// subCLI runs cw with explicit arguments and returns stdout, stderr and error.
func subCLI(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := cli.Execute(context.Background(), args, cli.Config{
		In:     strings.NewReader(stdin),
		Out:    &out,
		Err:    &errb,
		Getenv: os.Getenv,
	})
	return out.String(), errb.String(), err
}

func subWriteJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(subResolved(t, t.TempDir()), "input.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// subCLIRefused asserts the CLI "Refused" meaning: an error carrying every
// phrase (exit 1), nothing on standard output and no change in the task
// directory.
func subCLIRefused(t *testing.T, dir, inputPath string, phrases ...string) {
	t.Helper()
	beforeRev := subLoad(t, dir).Revision
	beforeState := subDirState(t, dir)
	out, stderr, err := subCLI(t, "", "task", "add-checks", "--task", dir, "--input", inputPath)
	if err == nil {
		t.Fatalf("cw task add-checks accepted, want refusal containing %q; stdout %q", phrases, out)
	}
	for _, phrase := range phrases {
		if !strings.Contains(err.Error()+"\n"+stderr, phrase) {
			t.Errorf("cw task add-checks error = %q (stderr %q), want it to contain %q", err, stderr, phrase)
		}
	}
	if out != "" {
		t.Errorf("refused cw task add-checks printed %q on standard output, want nothing", out)
	}
	if got := subLoad(t, dir).Revision; got != beforeRev {
		t.Errorf("refused cw task add-checks moved revision %d -> %d", beforeRev, got)
	}
	if after := subDirState(t, dir); !reflect.DeepEqual(beforeState, after) {
		t.Errorf("refused cw task add-checks changed the task directory:\nbefore %v\nafter  %v", beforeState, after)
	}
}

// TestContractSubSelectedSubdirThroughCLI drives the subdirectory layout
// through the CLI: cw task start --repo wt/svc (default pack selection, so the
// go pack is recorded) and cw task add-checks. Violations that fail it: a
// blanket accept (again admitted), widened refused, and verify still reporting
// the R13.a problems after widened passed. It does not by itself detect a
// missing path conversion, because the work-tree file internal/c/c.go maps to
// svc's internal/c without one and still lets widened qualify;
// TestContractSubSelectedSubdirConvertsMergePaths guards the conversion.
func TestContractSubSelectedSubdirThroughCLI(t *testing.T) {
	subEnv(t)
	r := subSubdirFixture(t)
	store := subStore(t)
	planPath := subWriteJSON(t, subPlan())

	out, stderr, err := subCLI(t, "", "task", "start", "--repo", r.selected, "--store", store, "--plan", planPath)
	if err != nil {
		t.Fatalf("cw task start: %v\n%s", err, stderr)
	}
	var started struct {
		Directory string `json:"directory"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &started); err != nil || started.Directory == "" {
		t.Fatalf("cw task start output %q: %v", out, err)
	}
	dir := started.Directory
	merge := subSubdirMerge(t, r)

	amend := func(checks ...taskflow.Check) string {
		return subWriteJSON(t, taskflow.AmendmentInput{
			MergeCommit: "HEAD",
			BaseRef:     "origin/main",
			Reason:      "merged origin/main; svc/go.mod and svc/internal/b changed",
			Checks:      checks,
		})
	}

	subCLIRefused(t, dir, amend(taskflow.Check{ID: "again", Argv: []string{"go", "test", "./internal/a"}}),
		"check again "+subC12Phrase)

	before := subLoad(t, dir).Revision
	out, stderr, err = subCLI(t, "", "task", "add-checks", "--task", dir, "--input", amend(subWidened()), "--summary")
	if err != nil {
		t.Fatalf("cw task add-checks widened refused: %v\n%s", err, stderr)
	}
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Errorf("--summary output %q is not one line", out)
	}
	var summary struct {
		Task        string   `json:"task"`
		Amendment   *int     `json:"amendment"`
		Checks      []string `json:"checks"`
		MergeCommit string   `json:"merge_commit"`
		BaseRef     string   `json:"base_ref"`
		BaseCommit  string   `json:"base_commit"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &summary); err != nil {
		t.Fatalf("--summary output %q: %v", out, err)
	}
	if summary.Amendment == nil || *summary.Amendment != 0 || !reflect.DeepEqual(summary.Checks, []string{"widened"}) ||
		summary.MergeCommit != merge || summary.BaseRef != "origin/main" || summary.BaseCommit != r.upstream ||
		summary.Task != dir {
		t.Errorf("--summary = %+v, want amendment 0, checks [widened], merge %s, base origin/main %s, task %s",
			summary, merge, r.upstream, dir)
	}
	if got := subLoad(t, dir).Revision; got != before+1 {
		t.Errorf("accepted cw task add-checks: revision %d -> %d, want one new revision", before, got)
	}

	// R13.a with the recorded go pack: only unit passed, both problems stand.
	subRunPassed(t, dir, "unit")
	text := subVerifyText(t, dir)
	for _, phrase := range []string{subGoModProb, subPkgBProb} {
		if !subMentions(text, phrase) {
			t.Errorf("verify with only unit passed = %q, want it to report %q", text, phrase)
		}
	}
	// R13.b in this layout: after widened passed, neither remains.
	subRunPassed(t, dir, "widened")
	text = subVerifyText(t, dir)
	for _, phrase := range []string{subGoModProb, subPkgBProb, "changed go.mod", "changed package internal/b"} {
		if subMentions(text, phrase) {
			t.Errorf("verify after widened passed reports %q in %q", phrase, text)
		}
	}
}

// subGoFixture builds the shared fixture with go.mod at the work-tree top:
// B (feature) with internal/a and internal/b, O (origin/main, child of B)
// changing go.mod and internal/b/b.go. HEAD is feature at B.
func subGoFixture(t *testing.T) subRepo {
	t.Helper()
	wt := subResolved(t, t.TempDir())
	subGit(t, wt, "init", "-q", "-b", "main")
	subWrite(t, wt, "go.mod", "module example.com/m\n\ngo 1.21\n")
	subWrite(t, wt, "internal/a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	subWrite(t, wt, "internal/b/b.go", "package b\n\nfunc B() int { return 1 }\n")
	base := subCommitAll(t, wt, "B: baseline")
	subGit(t, wt, "branch", "feature")
	subWrite(t, wt, "go.mod", "module example.com/m\n\ngo 1.21\n\n// upstream change\n")
	subWrite(t, wt, "internal/b/b.go", "package b\n\nfunc B() int { return 2 }\n")
	upstream := subCommitAll(t, wt, "O: upstream")
	subGit(t, wt, "update-ref", "refs/remotes/origin/main", upstream)
	subGit(t, wt, "checkout", "-q", "feature")
	return subRepo{worktree: wt, selected: wt, base: base, upstream: upstream}
}

// TestContractSubGraftedSquashIsNotMerge guards C7 against replace refs and
// grafts: a squash-style single-parent commit S (parent B, carrying O's
// changes) given a fake second parent O must still be refused as not a merge
// commit. If admission read the grafted parents (B, O), S would pass C7..C11
// and widened would cover go.mod and internal/b, so the amendment would be
// accepted: that acceptance fails this test.
func TestContractSubGraftedSquashIsNotMerge(t *testing.T) {
	for _, mode := range []string{"replace-graft", "info-grafts"} {
		t.Run(mode, func(t *testing.T) {
			subEnv(t)
			r := subGoFixture(t)
			dir := subStart(t, r.selected).Directory

			subGit(t, r.worktree, "merge", "-q", "--squash", "origin/main")
			squash := subCommitAll(t, r.worktree, "S: squash of origin/main")
			if got := subGit(t, r.worktree, "rev-list", "--parents", "-n", "1", squash); got != squash+" "+r.base {
				t.Fatalf("fixture: true parents of S = %q, want S B", got)
			}
			switch mode {
			case "replace-graft":
				subGit(t, r.worktree, "replace", "--graft", squash, r.base, r.upstream)
			case "info-grafts":
				grafts := filepath.Join(r.worktree, ".git", "info", "grafts")
				if err := os.MkdirAll(filepath.Dir(grafts), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(grafts, []byte(squash+" "+r.base+" "+r.upstream+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// Precondition: plain Git now reports the fake second parent.
			if got := subGit(t, r.worktree, "rev-list", "--parents", "-n", "1", squash); got != squash+" "+r.base+" "+r.upstream {
				if mode == "info-grafts" {
					t.Skipf("installed git ignores .git/info/grafts (rev-list shows %q); the grafts case cannot be exercised", got)
				}
				t.Fatalf("fixture: grafted parents of S = %q, want S B O", got)
			}

			subRefused(t, dir, taskflow.AmendmentInput{
				MergeCommit: squash,
				BaseRef:     "origin/main",
				Reason:      "squash-merged origin/main; go.mod and internal/b changed",
				Checks:      []taskflow.Check{subWidened()},
			}, "commit "+squash+" is not a merge commit", subNoFFHint)
			if rec := subLoad(t, dir); len(rec.Amendments) != 0 {
				t.Errorf("amendments = %d after refusal, want 0", len(rec.Amendments))
			}
		})
	}
}

// TestContractSubShallowCloneRefused guards C4 / R19.o through AddChecks and
// cw task add-checks: a task on a shallow clone holding an otherwise
// qualifying --no-ff merge of origin/main is refused with "inspect merge
// commit: " and "repository is shallow", and nothing is written. Without the
// shallow refusal the amendment would be accepted (M = (F, O') with O'
// carrying go.mod and internal/b), so acceptance or any other refusal phrase
// fails the test.
func TestContractSubShallowCloneRefused(t *testing.T) {
	subEnv(t)
	src := subResolved(t, t.TempDir())
	subGit(t, src, "init", "-q", "-b", "main")
	subWrite(t, src, "README.md", "history before the shallow boundary\n")
	subCommitAll(t, src, "B0: root")
	subWrite(t, src, "go.mod", "module example.com/m\n\ngo 1.21\n")
	subWrite(t, src, "internal/a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	subWrite(t, src, "internal/b/b.go", "package b\n\nfunc B() int { return 1 }\n")
	base := subCommitAll(t, src, "B: baseline")

	parent := subResolved(t, t.TempDir())
	subGit(t, parent, "clone", "-q", "--depth", "1", "file://"+src, "clone")
	clone := filepath.Join(parent, "clone")
	if got := subGit(t, clone, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("fixture: clone is-shallow-repository = %q, want true", got)
	}
	if got := subGit(t, clone, "rev-parse", "HEAD"); got != base {
		t.Fatalf("fixture: clone HEAD = %s, want B %s", got, base)
	}
	subGit(t, clone, "checkout", "-q", "-b", "feature")
	dir := subStart(t, clone).Directory

	// Base side O' (origin/main) changes go.mod and internal/b; feature
	// commits F and merges it with --no-ff, so M = (F, O') would qualify.
	subGit(t, clone, "checkout", "-q", "-b", "upstream")
	subWrite(t, clone, "go.mod", "module example.com/m\n\ngo 1.21\n\n// upstream change\n")
	subWrite(t, clone, "internal/b/b.go", "package b\n\nfunc B() int { return 2 }\n")
	upstream := subCommitAll(t, clone, "O: upstream")
	subGit(t, clone, "update-ref", "refs/remotes/origin/main", upstream)
	subGit(t, clone, "checkout", "-q", "feature")
	subWrite(t, clone, "internal/a/a.go", "package a\n\nfunc A() int { return 3 }\n")
	subCommitAll(t, clone, "F: feature work")
	subGit(t, clone, "merge", "-q", "--no-ff", "--no-edit", "origin/main")
	if got := subGit(t, clone, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("fixture: clone stopped being shallow")
	}

	input := taskflow.AmendmentInput{
		MergeCommit: "HEAD",
		BaseRef:     "origin/main",
		Reason:      "merged origin/main; go.mod and internal/b changed",
		Checks:      []taskflow.Check{subWidened()},
	}
	subRefused(t, dir, input, subC4Phrase, subShallowMsg)
	subCLIRefused(t, dir, subWriteJSON(t, input), subC4Phrase, subShallowMsg)
	if rec := subLoad(t, dir); len(rec.Amendments) != 0 || rec.Revision != 1 {
		t.Errorf("after refusals: revision %d, %d amendments; want revision 1 and none", rec.Revision, len(rec.Amendments))
	}
}
