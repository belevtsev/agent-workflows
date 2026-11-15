package evidence

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

const declaration = `version = 1

[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]

[[check]]
id = "lint"
run = ["sh", "lint.sh"]
`

var (
	now     = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	unitRun = []string{"sh", "unit.sh"}
	lintRun = []string{"sh", "lint.sh"}
	unitPat = []string{"src/**", "unit.sh"}
)

// env is a temporary repository and its store.
type env struct {
	t     *testing.T
	dir   string
	repo  *gitx.Repo
	store *store.Store
}

// newEnv creates the repository demo of the specifications with one commit.
// It uses t.Setenv, so a test that calls it cannot be parallel.
func newEnv(t *testing.T) *env {
	t.Helper()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	home := filepath.Join(base, "home")
	dir := filepath.Join(base, "repo")
	for _, d := range []string{home, dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	global := filepath.Join(base, "gitconfig")
	if err := os.WriteFile(global, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
	for name, value := range map[string]string{
		"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": global, "GIT_CEILING_DIRECTORIES": base,
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(name, value)
	}
	e := &env{t: t, dir: dir}
	e.git("init", "-q", "-b", "main", ".")
	e.write("kitsu.toml", declaration)
	e.write("src/app.txt", "ok\n")
	e.write("unit.sh", "test \"$(cat src/app.txt)\" = ok || exit 3\n")
	e.write("lint.sh", "test ! -e BAD\n")
	e.write("README.md", "# demo\n")
	e.commit("first")
	repo, err := gitx.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	e.repo, e.store = repo, store.Open(repo.CommonDir)
	return e
}

func (e *env) git(args ...string) string {
	e.t.Helper()
	cmd := exec.CommandContext(e.t.Context(), "git", args...)
	cmd.Dir = e.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		e.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func (e *env) write(rel, content string) {
	e.t.Helper()
	path := filepath.Join(e.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// commit records everything and returns the new tree.
func (e *env) commit(message string) string {
	e.t.Helper()
	e.git("add", "-A")
	e.git("commit", "-q", "--allow-empty", "-m", message)
	return e.git("rev-parse", "HEAD^{tree}")
}

func (e *env) tree() string { return e.git("rev-parse", "HEAD^{tree}") }

func (e *env) keys(tree string) map[string]string {
	e.t.Helper()
	file, err := Declaration(e.t.Context(), e.repo, tree)
	if err != nil {
		e.t.Fatal(err)
	}
	keys, err := Keys(e.t.Context(), e.repo, tree, file.Checks)
	if err != nil {
		e.t.Fatal(err)
	}
	return keys
}

// ran records a finished run of a check on a tree.
func (e *env) ran(tree, id string, run, paths []string, outcome string, endedAgo time.Duration) store.Record {
	e.t.Helper()
	key := e.keys(tree)[id]
	started := now.Add(-endedAgo - time.Minute)
	runID, err := store.NewRunID(started)
	if err != nil {
		e.t.Fatal(err)
	}
	active, err := e.store.Begin(e.t.Context(), store.Record{RunID: runID, ID: id, Run: run, Paths: paths, Tree: tree, ContentKey: key, StartedAt: started})
	if err != nil {
		e.t.Fatal(err)
	}
	ended := now.Add(-endedAgo)
	rec, err := active.Finish(e.t.Context(), func(r *store.Record) {
		r.Outcome, r.EndedAt = outcome, &ended
		code := 0
		if outcome == store.Failed {
			code = 3
		}
		if outcome != store.TimedOut {
			r.ExitStatus = &code
		}
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return rec
}

func (e *env) passBoth(tree string) {
	e.t.Helper()
	e.ran(tree, "unit", unitRun, unitPat, store.Passed, time.Hour)
	e.ran(tree, "lint", lintRun, nil, store.Passed, time.Hour)
}

func (e *env) judge(tree string, base *Base) *Verdict {
	e.t.Helper()
	v, err := Judge(e.t.Context(), e.repo, e.store, tree, now, base)
	if err != nil {
		e.t.Fatalf("Judge: %v", err)
	}
	return v
}

func state(v *Verdict, id string) Check {
	for _, c := range v.Checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func expect(t *testing.T, v *Verdict, states map[string]string) {
	t.Helper()
	verified := true
	for id, want := range states {
		if got := state(v, id).State; got != want {
			t.Errorf("%s is %q, want %q", id, got, want)
		}
		verified = verified && want == Satisfied
	}
	if v.Verified != verified {
		t.Errorf("Verified = %v, want %v", v.Verified, verified)
	}
}

func TestSatisfiedOnSameTree(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	expect(t, e.judge(tree, nil), map[string]string{"unit": NotRun, "lint": NotRun})
	e.passBoth(tree)
	v := e.judge(tree, nil)
	expect(t, v, map[string]string{"unit": Satisfied, "lint": Satisfied})
	if len(v.Warnings) != 0 || len(v.Missing()) != 0 {
		t.Errorf("warnings %v, missing %v", v.Warnings, v.Missing())
	}
	if c := state(v, "unit"); c.Deciding == nil || c.Deciding.Outcome != store.Passed || c.Line != 3 {
		t.Errorf("unit = %+v", c)
	}
	if v.Tree != tree {
		t.Errorf("Tree = %s", v.Tree)
	}
}

func TestKeys(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	keys := e.keys(tree)
	if keys["lint"] != tree {
		t.Errorf("the key of a check without paths is %s, want the tree %s", keys["lint"], tree)
	}
	if len(keys["unit"]) != 64 || keys["unit"] == tree {
		t.Errorf("the key of a check with paths is %q", keys["unit"])
	}
	e.write("README.md", "changed\n")
	other := e.commit("readme")
	if got := e.keys(other); got["unit"] != keys["unit"] || got["lint"] == keys["lint"] {
		t.Errorf("after a change outside paths, keys %v -> %v", keys, got)
	}
}

func TestChangeOutsidePathsKeepsCheckSatisfied(t *testing.T) {
	e := newEnv(t)
	e.passBoth(e.tree())
	e.write("README.md", "# edited\n")
	tree := e.commit("readme")
	expect(t, e.judge(tree, nil), map[string]string{"unit": Satisfied, "lint": NotRun})
}

func TestChangeInsidePathsDoesNot(t *testing.T) {
	e := newEnv(t)
	e.passBoth(e.tree())
	e.write("src/app.txt", "ok \n")
	tree := e.commit("app")
	expect(t, e.judge(tree, nil), map[string]string{"unit": NotRun, "lint": NotRun})
}

func TestFileMovedOutOfPaths(t *testing.T) {
	e := newEnv(t)
	e.passBoth(e.tree())
	e.git("mv", "src/app.txt", "app.txt")
	tree := e.commit("move")
	expect(t, e.judge(tree, nil), map[string]string{"unit": NotRun, "lint": NotRun})
}

func TestContentCheckedEarlierIsRecognised(t *testing.T) {
	e := newEnv(t)
	first := e.tree()
	e.passBoth(first)
	e.write("src/app.txt", "broken\n")
	e.commit("broken")
	e.write("src/app.txt", "ok\n")
	again := e.commit("ok")
	if again != first {
		t.Fatalf("the tree changed: %s != %s", again, first)
	}
	expect(t, e.judge(again, nil), map[string]string{"unit": Satisfied, "lint": Satisfied})
}

func TestChangedRunNeedsNewRun(t *testing.T) {
	e := newEnv(t)
	e.passBoth(e.tree())
	e.write("kitsu.toml", strings.Replace(declaration, `run = ["sh", "unit.sh"]`, `run = ["sh", "unit.sh", "-v"]`, 1))
	tree := e.commit("verbose")
	expect(t, e.judge(tree, nil), map[string]string{"unit": NotRun, "lint": NotRun})
}

func TestChangedPathsNeedsNewRun(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	// A run recorded with other paths on the same content key does not count.
	e.ran(tree, "unit", unitRun, []string{"src/**"}, store.Passed, time.Hour)
	expect(t, e.judge(tree, nil), map[string]string{"unit": NotRun, "lint": NotRun})
}

func TestHighestSeqDecides(t *testing.T) {
	cases := []struct {
		name     string
		outcomes []string
		want     string
	}{
		{"a later failure beats an earlier pass", []string{store.Passed, store.Failed}, Failed},
		{"a later pass beats an earlier failure", []string{store.Failed, store.Passed}, Satisfied},
		{"a later timeout beats an earlier pass", []string{store.Passed, store.TimedOut}, TimedOut},
		{"a non-decisive run decides nothing", []string{store.Passed, store.SourceChanged, store.Interrupted}, Satisfied},
		{"only non-decisive runs", []string{store.SourceChanged}, NotRun},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			tree := e.tree()
			for _, outcome := range tc.outcomes {
				e.ran(tree, "lint", lintRun, nil, outcome, time.Hour)
			}
			if got := state(e.judge(tree, nil), "lint").State; got != tc.want {
				t.Errorf("lint is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOrderOfRecordingNotClock(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.ran(tree, "lint", lintRun, nil, store.Passed, time.Hour)
	e.ran(tree, "lint", lintRun, nil, store.Failed, 2*time.Hour) // recorded later, dated earlier
	c := state(e.judge(tree, nil), "lint")
	if c.State != Failed || c.Deciding == nil || c.Deciding.Outcome != store.Failed {
		t.Errorf("lint = %q, deciding %+v; want failed", c.State, c.Deciding)
	}
}

func TestSameSeqCountsTheRunThatDidNotPass(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	pass := e.ran(tree, "lint", lintRun, nil, store.Passed, time.Hour)
	fail := e.ran(tree, "lint", lintRun, nil, store.Failed, time.Hour)
	// Give the failure the sequence number of the pass.
	dir := filepath.Join(e.store.Dir, "runs", "lint", pass.ContentKey)
	path := filepath.Join(dir, fail.RunID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"seq": 2`), []byte(`"seq": 1`), 1)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := state(e.judge(tree, nil), "lint").State; got != Failed {
		t.Errorf("lint is %q, want failed when two runs share a seq", got)
	}
}

func TestOldRunDoesNotSatisfy(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.ran(tree, "lint", lintRun, nil, store.Passed, 31*24*time.Hour)
	e.ran(tree, "unit", unitRun, unitPat, store.Passed, 29*24*time.Hour)
	expect(t, e.judge(tree, nil), map[string]string{"unit": Satisfied, "lint": NotRun})
}

func TestOldDecidingRunLetsNoEarlierPassDecide(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.ran(tree, "lint", lintRun, nil, store.Passed, time.Hour)
	e.ran(tree, "lint", lintRun, nil, store.Failed, 31*24*time.Hour) // the later end, aged out
	c := state(e.judge(tree, nil), "lint")
	if c.State != NotRun || c.Deciding != nil {
		t.Errorf("lint = %q, deciding %+v; want not run, with no earlier pass deciding", c.State, c.Deciding)
	}
}

func TestFutureRunDoesNotSatisfy(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.ran(tree, "lint", lintRun, nil, store.Passed, time.Hour)
	e.ran(tree, "lint", lintRun, nil, store.Passed, -24*time.Hour) // dated a day ahead
	v := e.judge(tree, nil)
	if c := state(v, "lint"); c.State != NotRun || c.Deciding != nil {
		t.Errorf("lint = %q, deciding %+v; want not run", c.State, c.Deciding)
	}
	if len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], "future") {
		t.Errorf("warnings = %q, want one about a run dated in the future", v.Warnings)
	}
}

func TestDamagedRecordMakesCheckNotSatisfied(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.passBoth(tree)
	fail := e.ran(tree, "lint", lintRun, nil, store.Failed, time.Hour/2)
	path := filepath.Join(e.store.Dir, "runs", "lint", fail.ContentKey, fail.RunID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	v := e.judge(tree, nil)
	expect(t, v, map[string]string{"unit": Satisfied, "lint": NotRun})
	if c := state(v, "lint"); c.Deciding != nil {
		t.Errorf("lint is decided by %+v", c.Deciding)
	}
	if len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], path) {
		t.Errorf("warnings = %q, want one naming %s", v.Warnings, path)
	}

	// A later decisive run sets the damaged record aside.
	e.ran(tree, "lint", lintRun, nil, store.Passed, time.Hour/4)
	v = e.judge(tree, nil)
	expect(t, v, map[string]string{"unit": Satisfied, "lint": Satisfied})
	if len(v.Warnings) != 0 {
		t.Errorf("warnings after a new run = %q", v.Warnings)
	}
}

func TestRunningAndLast(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.ran(tree, "lint", lintRun, nil, store.Passed, time.Hour)
	changed := e.ran(tree, "lint", lintRun, nil, store.SourceChanged, time.Hour/2)
	runID, _ := store.NewRunID(now)
	active, err := e.store.Begin(t.Context(), store.Record{RunID: runID, ID: "lint", Run: lintRun, Tree: tree, ContentKey: tree, StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	c := state(e.judge(tree, nil), "lint")
	if c.State != Satisfied || !c.Running || c.Last == nil || c.Last.RunID != changed.RunID {
		t.Errorf("lint = %q, running %v, last %+v", c.State, c.Running, c.Last)
	}
	ended := now
	if _, err := active.Finish(t.Context(), func(r *store.Record) { r.Outcome, r.EndedAt = store.Interrupted, &ended }); err != nil {
		t.Fatal(err)
	}
}

func TestNoDeclaration(t *testing.T) {
	e := newEnv(t)
	e.git("rm", "-q", "kitsu.toml")
	tree := e.commit("remove")
	if _, err := Judge(t.Context(), e.repo, e.store, tree, now, nil); !errors.Is(err, ErrNoDeclaration) {
		t.Errorf("Judge = %v, want ErrNoDeclaration", err)
	}
}

func TestInvalidDeclaration(t *testing.T) {
	e := newEnv(t)
	e.write("kitsu.toml", declaration+"tiemout = \"5m\"\n")
	tree := e.commit("typo")
	_, err := Judge(t.Context(), e.repo, e.store, tree, now, nil)
	var invalid *decl.Error
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "kitsu.toml:11") {
		t.Errorf("Judge = %v, want a *decl.Error naming kitsu.toml:11", err)
	}
}

func TestDeclarationThatIsNotAFile(t *testing.T) {
	e := newEnv(t)
	e.git("rm", "-q", "kitsu.toml")
	e.write("kitsu.toml/x", "x\n")
	tree := e.commit("dir")
	_, err := Judge(t.Context(), e.repo, e.store, tree, now, nil)
	var invalid *decl.Error
	if !errors.As(err, &invalid) {
		t.Errorf("Judge = %v, want a *decl.Error", err)
	}
}

func TestEmptyDeclarationIsVerified(t *testing.T) {
	e := newEnv(t)
	e.write("kitsu.toml", "version = 1\n")
	tree := e.commit("empty")
	v := e.judge(tree, nil)
	if !v.Verified || len(v.Checks) != 0 {
		t.Errorf("a declaration without checks: %+v", v)
	}
}

func TestStoreErrorUnwraps(t *testing.T) {
	inner := errors.New("denied")
	err := &StoreError{Dir: "/x/kitsu", Err: inner}
	if !errors.Is(err, inner) || !strings.Contains(err.Error(), "/x/kitsu") {
		t.Errorf("StoreError = %v", err)
	}
}

func TestBaseImpliesUnchangedCheck(t *testing.T) {
	e := newEnv(t)
	e.git("tag", "base")
	e.write("README.md", "# docs only\n")
	tree := e.commit("docs")
	base, err := LoadBase(t.Context(), e.repo, "base")
	if err != nil {
		t.Fatal(err)
	}
	v := e.judge(tree, base)
	if c := state(v, "unit"); c.State != Satisfied || !c.Implied {
		t.Errorf("unit = %q, implied %v; want implied by the base", c.State, c.Implied)
	}
	if c := state(v, "lint"); c.State != NotRun || c.Implied {
		t.Errorf("lint = %q, implied %v; want not run", c.State, c.Implied)
	}
	if v.Verified {
		t.Error("the tree is verified while lint did not run")
	}
	// Nothing is written for an implied check.
	if exists(filepath.Join(e.store.Dir, "runs")) {
		t.Error("Judge wrote a record")
	}
}

func TestBaseDoesNotImplyChangedOrNewCheck(t *testing.T) {
	e := newEnv(t)
	e.git("tag", "base")
	e.write("kitsu.toml", strings.Replace(declaration, `run = ["sh", "unit.sh"]`, `run = ["sh", "unit.sh", "-v"]`, 1)+
		"\n[[check]]\nid = \"build\"\nrun = [\"make\"]\npaths = [\"src/**\"]\n")
	tree := e.commit("change checks")
	base, err := LoadBase(t.Context(), e.repo, "base")
	if err != nil {
		t.Fatal(err)
	}
	v := e.judge(tree, base)
	for _, id := range []string{"unit", "lint", "build"} {
		if c := state(v, id); c.State != NotRun || c.Implied {
			t.Errorf("%s = %q, implied %v; want not run", id, c.State, c.Implied)
		}
	}
}

func TestBaseDoesNotOverrideOwnRecord(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.ran(tree, "unit", unitRun, unitPat, store.Failed, time.Hour)
	base, err := LoadBase(t.Context(), e.repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if c := state(e.judge(tree, base), "unit"); c.State != Failed || c.Implied {
		t.Errorf("unit = %q, implied %v; want failed", c.State, c.Implied)
	}
}

// design.md, Records, --base: the base implies a check only when it has "no
// decisive record of its own in that directory"; check-evidence, Base
// revision: the base counts "earlier than every recorded run". A failure
// that aged out is still a recorded decisive run.
func TestBaseDoesNotImplyOverAgedOutRecord(t *testing.T) {
	e := newEnv(t)
	tree := e.tree()
	e.ran(tree, "unit", unitRun, unitPat, store.Failed, 31*24*time.Hour)
	base, err := LoadBase(t.Context(), e.repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if c := state(e.judge(tree, base), "unit"); c.State == Satisfied || c.Implied {
		t.Errorf("unit = %q, implied %v; want not satisfied: a decisive record of its own exists", c.State, c.Implied)
	}
}

func TestBaseWithoutDeclarationImpliesNothing(t *testing.T) {
	e := newEnv(t)
	e.git("rm", "-q", "kitsu.toml")
	e.commit("no declaration")
	e.git("tag", "bare")
	base, err := LoadBase(t.Context(), e.repo, "bare")
	if err != nil {
		t.Fatalf("LoadBase of a tree without a declaration: %v", err)
	}
	e.git("checkout", "-q", "HEAD~1", "--", "kitsu.toml")
	tree := e.commit("back")
	if c := state(e.judge(tree, base), "unit"); c.Implied {
		t.Error("a base without a declaration implied a check")
	}
	if _, err := LoadBase(t.Context(), e.repo, "nosuch"); err == nil {
		t.Error("LoadBase of an unknown revision gave no error")
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
