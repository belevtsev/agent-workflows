package gate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/evidence"
	"github.com/belevtsev/agent-workflows/v3/internal/gitx"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
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

var now = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// env is a temporary repository, its store and a gate.
type env struct {
	t    *testing.T
	base string
	dir  string
	repo *gitx.Repo
	gate *Gate
}

// newEnv creates an isolated repository on branch main. With files it makes
// one commit. It uses t.Setenv, so a test that calls it cannot be parallel.
func newEnv(t *testing.T, files map[string]string) *env {
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
	e := &env{t: t, base: base, dir: dir}
	e.git("init", "-q", "-b", "main", ".")
	if len(files) > 0 {
		for rel, content := range files {
			e.write(rel, content)
		}
		e.commit("first")
	}
	repo, err := gitx.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	e.repo = repo
	e.gate = &Gate{Repo: repo, Store: store.Open(repo.CommonDir), Now: now}
	return e
}

// demo is the repository of the specifications.
func demo(t *testing.T) *env {
	t.Helper()
	return newEnv(t, map[string]string{
		"cw.toml":     declaration,
		"src/app.txt": "ok\n",
		"unit.sh":     "test \"$(cat src/app.txt)\" = ok || exit 3\n",
		"lint.sh":     "test ! -e BAD\n",
		"README.md":   "# demo\n",
		".gitignore":  "build/\n",
	})
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

func (e *env) commit(message string) string {
	e.t.Helper()
	e.git("add", "-A")
	e.git("commit", "-q", "--allow-empty", "-m", message)
	return e.git("rev-parse", "HEAD")
}

// ran records a finished run of a declared check on a tree.
func (e *env) ran(tree, id, outcome string) store.Record {
	e.t.Helper()
	ctx := e.t.Context()
	file, err := evidence.Declaration(ctx, e.repo, tree)
	if err != nil {
		e.t.Fatal(err)
	}
	keys, err := evidence.Keys(ctx, e.repo, tree, file.Checks)
	if err != nil {
		e.t.Fatal(err)
	}
	check, ok := file.Check(id)
	if !ok {
		e.t.Fatalf("no check %s", id)
	}
	started := now.Add(-time.Hour)
	runID, err := store.NewRunID(started)
	if err != nil {
		e.t.Fatal(err)
	}
	active, err := e.gate.Store.Begin(ctx, store.Record{RunID: runID, ID: id, Run: check.Run, Paths: check.Paths, Tree: tree, ContentKey: keys[id], StartedAt: started})
	if err != nil {
		e.t.Fatal(err)
	}
	ended := now.Add(-time.Minute)
	rec, err := active.Finish(ctx, func(r *store.Record) {
		code := 0
		if outcome == store.Failed {
			code = 3
		}
		r.Outcome, r.EndedAt, r.ExitStatus = outcome, &ended, &code
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return rec
}

func (e *env) passAll(tree string) {
	e.t.Helper()
	e.ran(tree, "unit", store.Passed)
	e.ran(tree, "lint", store.Passed)
}

func (e *env) treeOf(rev string) string { return e.git("rev-parse", rev+"^{tree}") }

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func newRepoAt(t *testing.T, dir string) *gitx.Repo {
	t.Helper()
	repo, err := gitx.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
