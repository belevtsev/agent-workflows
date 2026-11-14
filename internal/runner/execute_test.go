package runner

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/gitx"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
)

// isolatedRepo is an empty git repository that sees no git configuration of
// the machine. It uses t.Setenv, so a test that calls it cannot be parallel.
func isolatedRepo(t *testing.T) *gitx.Repo {
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
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CEILING_DIRECTORIES", base)
	cmd := exec.Command("git", "init", "-q", "-b", "main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	repo, err := gitx.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// A run whose log cannot be created is recorded as interrupted, with the
// cause, so that no run stays open; its program is not started (second
// challenge, finding A7).
func TestExecuteRecordsARunWhoseLogCannotBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder that cannot be written needs POSIX file modes")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes into a folder of mode 0500")
	}
	st := store.Open(t.TempDir())
	logs := filepath.Join(st.Dir, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logs, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(logs, 0o700) })

	job := Job{
		Check: decl.Check{ID: "unit", Run: []string{"no-such-program-of-this-test"}},
		Tree:  "0123456789abcdef0123456789abcdef01234567", Key: "k1",
		Out: io.Discard, Err: io.Discard,
	}
	// No repository: a run whose log cannot be written must not reach the
	// program or the snapshot after it.
	_, err := Execute(t.Context(), nil, st, job)
	if err == nil || !strings.Contains(err.Error(), "the log of `unit` cannot be written") {
		t.Errorf("Execute: error %v, want one that says the log of `unit` cannot be written", err)
	}

	records, damaged, err := st.Records("unit", "k1")
	if err != nil || len(damaged) != 0 || len(records) != 1 {
		t.Fatalf("Records = %v, %v, %v; want one record", records, damaged, err)
	}
	rec := records[0]
	if rec.Outcome != store.Interrupted || rec.EndedAt == nil || rec.Seq == 0 {
		t.Errorf("the record has outcome %q, ended_at %v and seq %d; want a finished run recorded as interrupted", rec.Outcome, rec.EndedAt, rec.Seq)
	}
	if !strings.HasPrefix(rec.Detail, "the log could not be written") {
		t.Errorf("the detail is %q, want it to start with %q", rec.Detail, "the log could not be written")
	}
	if rec.ExitStatus != nil {
		t.Errorf("the record holds exit status %d, want none", *rec.ExitStatus)
	}
}

// The log of a run is private to the user (second challenge, finding S10).
func TestExecuteWritesAPrivateLog(t *testing.T) {
	repo := isolatedRepo(t)
	st := store.Open(repo.CommonDir)
	tree, err := repo.Snapshot(t.Context(), st.Tmp())
	if err != nil {
		t.Fatal(err)
	}
	job := Job{
		Check: decl.Check{ID: "version", Run: []string{"git", "--version"}},
		Tree:  tree, Key: tree,
		Out: io.Discard, Err: io.Discard,
	}
	result, err := Execute(t.Context(), repo, st, job)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Record.Outcome != store.Passed {
		t.Fatalf("the run ended %q, want passed: %+v", result.Record.Outcome, result.Record)
	}
	log := st.Abs(result.Record.Log)
	data, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(data), "git version") {
		t.Errorf("the log %s holds %q, %v; want the output of git --version", log, data, err)
	}
	if runtime.GOOS == "windows" {
		return // no POSIX file modes
	}
	for path, want := range map[string]os.FileMode{log: 0o600, filepath.Dir(log): 0o700, st.Dir: 0o700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
		} else if info.Mode().Perm() != want {
			t.Errorf("%s has mode %v, want %v", path, info.Mode().Perm(), want)
		}
	}
}
