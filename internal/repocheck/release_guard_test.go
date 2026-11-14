package repocheck

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGh stands in for gh. It writes its arguments to the file $FAKE_GH_ARGS,
// prints the file $FAKE_GH_RUNS and exits with $FAKE_GH_EXIT.
const fakeGh = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_GH_ARGS"
cat "$FAKE_GH_RUNS"
exit "${FAKE_GH_EXIT:-0}"
`

// guardRepo is a repository with a commit on main and a commit on another
// branch, and a fake gh first on PATH.
type guardRepo struct {
	t       *testing.T
	dir     string
	env     []string
	onMain  string
	offMain string
}

func newGuardRepo(t *testing.T) *guardRepo {
	t.Helper()
	base := t.TempDir()
	r := &guardRepo{t: t, dir: filepath.Join(base, "repo")}
	bin := filepath.Join(base, "bin")
	home := filepath.Join(base, "home")
	for _, dir := range []string{r.dir, bin, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGh), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") || strings.HasPrefix(name, "GH_") || strings.HasPrefix(name, "XDG_") || name == "HOME" || name == "PATH" {
			continue
		}
		r.env = append(r.env, kv)
	}
	r.env = append(r.env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"FAKE_GH_ARGS="+filepath.Join(base, "gh-args"),
		"FAKE_GH_RUNS="+filepath.Join(base, "gh-runs"),
	)
	r.git("init", "-q", "-b", "main", ".")
	r.git("config", "maintenance.auto", "false")
	r.git("commit", "-q", "--allow-empty", "-m", "on main")
	r.onMain = r.git("rev-parse", "HEAD")
	r.git("update-ref", "refs/remotes/origin/main", r.onMain)
	r.git("switch", "-q", "-c", "side")
	r.git("commit", "-q", "--allow-empty", "-m", "not on main")
	r.offMain = r.git("rev-parse", "HEAD")
	r.runs("")
	return r
}

func (r *guardRepo) run(name string, args ...string) (string, int) {
	r.t.Helper()
	command := exec.Command(name, args...)
	command.Dir = r.dir
	command.Env = r.env
	var out bytes.Buffer
	command.Stdout, command.Stderr = &out, &out
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &exit):
		return out.String(), exit.ExitCode()
	default:
		r.t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
		return "", 0
	}
}

func (r *guardRepo) git(args ...string) string {
	r.t.Helper()
	out, code := r.run("git", args...)
	if code != 0 {
		r.t.Fatalf("git %s: exit status %d\n%s", strings.Join(args, " "), code, out)
	}
	return strings.TrimSpace(out)
}

// runs sets what the fake gh prints: one line per run.
func (r *guardRepo) runs(lines string) {
	r.t.Helper()
	if err := os.WriteFile(r.envValue("FAKE_GH_RUNS"), []byte(lines), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *guardRepo) guard(commit string) (string, int) {
	r.t.Helper()
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "release-guard.sh"))
	if err != nil {
		r.t.Fatal(err)
	}
	return r.run("sh", script, commit)
}

// The release job runs on Linux only, so the guard is tested there and on
// macOS, not on Windows.
func TestReleaseGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the release job does not run on Windows")
	}
	tests := []struct {
		name    string
		offMain bool
		runs    string // %s stands for the commit
		ghExit  string
		allowed bool
		reason  string
	}{
		{name: "commit not on main", offMain: true, runs: "%s push completed success\n", reason: "is not on main"},
		{name: "no run", reason: "No CI run on a push or by hand exists"},
		{name: "run of another commit", runs: "0000000000000000000000000000000000000000 push completed success\n", reason: "No CI run on a push or by hand exists"},
		{name: "only a pull request run passed", runs: "%s pull_request completed success\n", reason: "No CI run on a push or by hand exists"},
		{name: "run failed", runs: "%s push completed failure\n", reason: "has passed"},
		{name: "run in progress", runs: "%s push in_progress \n", reason: "has passed"},
		{name: "gh fails", runs: "%s push completed success\n", ghExit: "1", reason: "GitHub did not answer"},
		{name: "run passed", runs: "%s push completed failure\n%s push completed success\n", allowed: true},
		{name: "run by hand passed", runs: "%s workflow_dispatch completed success\n", allowed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newGuardRepo(t)
			commit := r.onMain
			if test.offMain {
				commit = r.offMain
			}
			r.runs(strings.ReplaceAll(test.runs, "%s", commit))
			if test.ghExit != "" {
				r.env = append(r.env, "FAKE_GH_EXIT="+test.ghExit)
			}
			out, code := r.guard(commit)
			if test.allowed {
				if code != 0 {
					t.Fatalf("the guard refused the commit with exit status %d: %s", code, out)
				}
				args, err := os.ReadFile(r.envValue("FAKE_GH_ARGS"))
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"run\nlist\n", "--workflow\nci.yml\n", "--commit\n" + commit + "\n"} {
					if !strings.Contains(string(args), want) {
						t.Errorf("gh was called with %q, want %q in it", args, want)
					}
				}
				return
			}
			if code == 0 {
				t.Fatalf("the guard allowed the commit: %s", out)
			}
			if !strings.Contains(out, test.reason) {
				t.Errorf("the guard said %q, want %q", out, test.reason)
			}
			if lines := strings.Count(strings.TrimSpace(out), "\n"); lines != 0 {
				t.Errorf("the guard said %d lines, want one sentence: %q", lines+1, out)
			}
		})
	}
}

// envValue is the last value of a variable in the environment of the commands.
func (r *guardRepo) envValue(name string) string {
	value := ""
	for _, kv := range r.env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			value = v
		}
	}
	return value
}
