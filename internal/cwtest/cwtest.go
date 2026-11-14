// Package cwtest is the harness of the scenario tests: it builds cw once and
// drives it and git in temporary repositories that share nothing with the
// machine's own git configuration or home directory.
package cwtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
)

var binary struct {
	once sync.Once
	dir  string // holds only the cw program, so it can go first on PATH
	err  error
}

// Binary is the directory that holds the cw program under test and nothing
// else. CW_TEST_BINARY names a program built elsewhere; without it the
// harness builds cmd/cw of this module once per test process.
func Binary(t testing.TB) string {
	t.Helper()
	binary.once.Do(func() { binary.dir, binary.err = prepareBinary() })
	if binary.err != nil {
		t.Fatalf("cw cannot be built: %v", binary.err)
	}
	return binary.dir
}

func programName() string {
	if runtime.GOOS == "windows" {
		return "cw.exe"
	}
	return "cw"
}

func prepareBinary() (string, error) {
	dir, err := os.MkdirTemp("", "cw-under-test-")
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, programName())
	if given := os.Getenv("CW_TEST_BINARY"); given != "" {
		data, err := os.ReadFile(given)
		if err != nil {
			return "", err
		}
		syscall.ForkLock.RLock() // a program started now would hold the copy open, and the copy could not be run
		defer syscall.ForkLock.RUnlock()
		return dir, os.WriteFile(target, data, 0o755)
	}
	root, err := moduleRoot()
	if err != nil {
		return "", err
	}
	build := exec.Command("go", "build", "-o", target, "./cmd/cw")
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		return "", errors.New(strings.TrimSpace(string(output)) + ": " + err.Error())
	}
	return dir, nil
}

// moduleRoot is the directory of go.mod, found from the test's directory.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// Result is how a command ended.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Output is everything the command printed, standard output first.
func (r Result) Output() string { return r.Stdout + r.Stderr }

// Repo is a directory in which the tests run git and cw.
type Repo struct {
	Dir  string
	Home string // an empty home directory of its own
	t    testing.TB
	env  []string
}

// NewRepo creates an empty git repository on branch main. Its commands see
// no git configuration of the machine and an empty home directory; cw is
// first on their PATH.
func NewRepo(t testing.TB) *Repo {
	t.Helper()
	base := t.TempDir()
	// A temporary directory may be reached through a symbolic link; git prints the real path.
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	repo := &Repo{Dir: filepath.Join(base, "repo"), Home: filepath.Join(base, "home"), t: t}
	for _, dir := range []string{repo.Dir, repo.Home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "GIT_") || upper == "HOME" || upper == "USERPROFILE" || upper == "PATH" || strings.HasPrefix(upper, "XDG_") {
			continue
		}
		repo.env = append(repo.env, kv)
	}
	repo.env = append(repo.env,
		"PATH="+Binary(t)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+repo.Home, "USERPROFILE="+repo.Home,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	repo.Git("init", "-q", "-b", "main", ".")
	repo.Git("config", "maintenance.auto", "false") // its detached process would change .git while a test compares files
	return repo
}

// The files of the demo repository of the specifications.
const (
	DemoDeclaration = `version = 1

[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]

[[check]]
id = "lint"
run = ["sh", "lint.sh"]
`
	demoUnit = "echo unit.sh runs\ntest \"$(cat src/app.txt)\" = ok || exit 3\n"
	demoLint = "echo lint.sh runs\ntest ! -e BAD\n"
)

// Demo creates the repository `demo` that the specifications share: cw.toml
// declares `unit` (paths src/** and unit.sh; unit.sh exits 3 unless
// src/app.txt holds ok) and `lint` (no paths; lint.sh exits 0 unless a file
// BAD exists at the root); README.md; and a .gitignore that lists build/.
// Everything is in one commit on main. The hooks are not installed.
func Demo(t testing.TB) *Repo {
	t.Helper()
	repo := NewRepo(t)
	repo.Write("cw.toml", DemoDeclaration)
	repo.Write("src/app.txt", "ok\n")
	repo.Write("unit.sh", demoUnit)
	repo.Write("lint.sh", demoLint)
	repo.Write("README.md", "# demo\n")
	repo.Write(".gitignore", "build/\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "first")
	return repo
}

// In is the same environment in another directory, such as a linked
// worktree or a clone. A relative path is relative to the repository.
func (r *Repo) In(dir string) *Repo {
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.Dir, dir)
	}
	return &Repo{Dir: filepath.Clean(dir), Home: r.Home, t: r.t, env: r.env}
}

// Setenv sets a variable for the commands that follow.
func (r *Repo) Setenv(name, value string) { r.env = append(r.env, name+"="+value) }

// Env is the environment the commands run with.
func (r *Repo) Env() []string { return append([]string(nil), r.env...) }

// Path is a path inside the repository.
func (r *Repo) Path(rel string) string { return filepath.Join(r.Dir, filepath.FromSlash(rel)) }

// Write creates or replaces a file, with its directories.
func (r *Repo) Write(rel, content string) {
	r.t.Helper()
	path := r.Path(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// Read returns the content of a file.
func (r *Repo) Read(rel string) string {
	r.t.Helper()
	data, err := os.ReadFile(r.Path(rel))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

// Exists says whether a path exists.
func (r *Repo) Exists(rel string) bool {
	_, err := os.Lstat(r.Path(rel))
	return err == nil
}

// Remove deletes a file or a directory with what it holds.
func (r *Repo) Remove(rel string) {
	r.t.Helper()
	if err := os.RemoveAll(r.Path(rel)); err != nil {
		r.t.Fatal(err)
	}
}

// Command prepares a program to run in the repository with its environment,
// for a test that needs to start it itself.
func (r *Repo) Command(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	command.Dir = r.Dir
	command.Env = r.Env()
	return command
}

// Run runs a program and returns how it ended. input may be empty.
func (r *Repo) Run(input, name string, args ...string) Result {
	r.t.Helper()
	command := r.Command(name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	err := command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		result.Code = exit.ExitCode()
	default:
		r.t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	return result
}

// CW runs cw and returns how it ended.
func (r *Repo) CW(args ...string) Result {
	r.t.Helper()
	return r.Run("", filepath.Join(Binary(r.t), programName()), args...)
}

// TryGit runs git and returns how it ended.
func (r *Repo) TryGit(args ...string) Result {
	r.t.Helper()
	return r.Run("", "git", args...)
}

// Git runs git, fails the test when it does not exit 0, and returns what it
// printed without the final newline.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	result := r.TryGit(args...)
	if result.Code != 0 {
		r.t.Fatalf("git %s: exit status %d\n%s", strings.Join(args, " "), result.Code, result.Output())
	}
	return strings.TrimRight(result.Stdout, "\r\n")
}

// StoreDir is the directory cw/ in the common git directory, where cw keeps
// its records.
func (r *Repo) StoreDir() string {
	r.t.Helper()
	common := r.Git("rev-parse", "--git-common-dir")
	if !filepath.IsAbs(common) {
		common = filepath.Join(r.Dir, common)
	}
	return filepath.Join(filepath.Clean(common), "cw")
}

// RecordFiles lists the files that hold the run records of a check, on any
// content, oldest first by name.
func (r *Repo) RecordFiles(checkID string) []string {
	r.t.Helper()
	files, err := filepath.Glob(filepath.Join(r.StoreDir(), "runs", checkID, "*", "*.json"))
	if err != nil {
		r.t.Fatal(err)
	}
	return files
}

// EditRecord reads a run record, lets change alter its fields, and writes it
// back. The fields are those of the design: run_id, id, run, paths, tree,
// content_key, seq, outcome, exit_status, started_at, ended_at, log.
func (r *Repo) EditRecord(path string, change func(fields map[string]any)) {
	r.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(data, &fields); err != nil {
		r.t.Fatalf("%s: %v", path, err)
	}
	change(fields)
	out, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		r.t.Fatal(err)
	}
}
