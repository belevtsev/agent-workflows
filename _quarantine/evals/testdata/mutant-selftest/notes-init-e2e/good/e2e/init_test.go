package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// notesBin is the binary built once by TestMain.
var notesBin string

// minimalPath holds only the Go toolchain and system binaries.
var minimalPath string

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "notes-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating temp dir:", err)
		return 1
	}
	defer os.RemoveAll(tmp)
	moduleRoot, err := filepath.Abs("..")
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolving module root:", err)
		return 1
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		fmt.Fprintln(os.Stderr, "finding go:", err)
		return 1
	}
	minimalPath = filepath.Dir(goTool) + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin"
	notesBin = filepath.Join(tmp, "notes")
	build := exec.Command(goTool, "build", "-o", notesBin, "./cmd/notes")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build ./cmd/notes: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// sandbox is a fresh temporary root with a working directory, HOME, TMPDIR
// and an unrelated directory outside the working directory.
type sandbox struct {
	root    string
	work    string
	home    string
	tmp     string
	outside string
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &sandbox{
		root:    root,
		work:    filepath.Join(root, "work"),
		home:    filepath.Join(root, "home"),
		tmp:     filepath.Join(root, "tmp"),
		outside: filepath.Join(root, "outside"),
	}
	for _, d := range []string{s.work, s.home, s.tmp, s.outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

type result struct {
	code   int
	stdout string
	stderr string
}

// run executes the built binary in dir with a controlled environment and
// the given stdin (empty when nil).
func (s *sandbox) run(t *testing.T, dir string, stdin io.Reader, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, notesBin, args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + s.home,
		"TMPDIR=" + s.tmp,
		"TERM=dumb",
		"LC_ALL=C",
		"PATH=" + minimalPath,
	}
	if stdin == nil {
		stdin = bytes.NewReader(nil)
	}
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("notes %v did not finish within 30s (waiting for input?)", args)
	}
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running notes %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func wantCode(t *testing.T, r result, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit status %d, want %d\nstdout: %q\nstderr: %q", r.code, want, r.stdout, r.stderr)
	}
}

// decodeOnly decodes stdout as exactly one JSON object and nothing else.
func decodeOnly(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("stdout %q is not a JSON object: %v", stdout, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout %q holds more than one JSON value", stdout)
	}
	return obj
}

func wantSuccessJSON(t *testing.T, stdout string, created bool, path string) {
	t.Helper()
	obj := decodeOnly(t, stdout)
	if len(obj) != 2 || obj["created"] != created || obj["path"] != path {
		t.Fatalf("stdout JSON = %v, want {\"created\": %v, \"path\": %q}", obj, created, path)
	}
}

func wantErrorJSON(t *testing.T, stdout string) {
	t.Helper()
	obj := decodeOnly(t, stdout)
	msg, ok := obj["error"].(string)
	if len(obj) != 1 || !ok || msg == "" {
		t.Fatalf("stdout JSON = %v, want {\"error\": \"<message>\"}", obj)
	}
}

func wantErrorLine(t *testing.T, stderr string) {
	t.Helper()
	line := strings.TrimSuffix(stderr, "\n")
	if !strings.HasPrefix(line, "notes: ") || strings.Contains(line, "\n") {
		t.Fatalf("stderr = %q, want one line starting with \"notes: \"", stderr)
	}
}

// wantPlainLine checks that stdout is exactly one line (a trailing newline
// is optional).
func wantPlainLine(t *testing.T, stdout, want string) {
	t.Helper()
	if strings.TrimSuffix(stdout, "\n") != want {
		t.Fatalf("stdout = %q, want the single line %q", stdout, want)
	}
}

func wantMode(t *testing.T, path string, dir bool, perm fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if info.IsDir() != dir || (!dir && !info.Mode().IsRegular()) {
		t.Fatalf("%s has mode %v, want a %s", path, info.Mode(), map[bool]string{true: "directory", false: "regular file"}[dir])
	}
	if got := info.Mode().Perm(); got != perm {
		t.Fatalf("%s has permissions %o, want %o", path, got, perm)
	}
}

func wantInitialized(t *testing.T, path string) {
	t.Helper()
	notesDir := filepath.Join(path, ".notes")
	wantMode(t, notesDir, true, 0o700)
	config := filepath.Join(notesDir, "config.json")
	wantMode(t, config, false, 0o600)
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"version\":1}\n" {
		t.Fatalf("config.json = %q, want %q", data, "{\"version\":1}\n")
	}
}

type entry struct {
	mode    fs.FileMode
	content string
	link    string
	mtime   time.Time
}

// snapshot records every path under root without following links. Directory
// modification times are recorded only when dirTimes is set.
func snapshot(t *testing.T, root string, dirTimes bool) map[string]entry {
	t.Helper()
	out := map[string]entry{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		e := entry{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			e.link, err = os.Readlink(p)
			if err != nil {
				return err
			}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			e.content = string(data)
			e.mtime = info.ModTime()
		case info.IsDir() && dirTimes:
			e.mtime = info.ModTime()
		}
		out[filepath.ToSlash(rel)] = e
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diff(before, after map[string]entry) []string {
	var out []string
	for k, b := range before {
		a, ok := after[k]
		switch {
		case !ok:
			out = append(out, "removed "+k)
		case a.mode != b.mode || a.content != b.content || a.link != b.link || !a.mtime.Equal(b.mtime):
			out = append(out, "changed "+k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			out = append(out, "added "+k)
		}
	}
	sort.Strings(out)
	return out
}

func wantUnchanged(t *testing.T, what string, before, after map[string]entry) {
	t.Helper()
	if d := diff(before, after); len(d) != 0 {
		t.Fatalf("%s changed: %v", what, d)
	}
}

// R1: init in the current directory creates .notes (0700) and config.json
// (0600) with exactly {"version":1} and a newline, and exits 0.
func TestInitCreatesConfigInWorkingDir_R1(t *testing.T) {
	s := newSandbox(t)
	r := s.run(t, s.work, nil, "init")
	wantCode(t, r, 0)
	wantInitialized(t, s.work)
}

// R1 and R2: --dir names the target; plain output is the single line
// "initialized <absolute PATH>/.notes".
func TestInitWithDirFlag_R1_R2(t *testing.T) {
	s := newSandbox(t)
	proj := filepath.Join(s.work, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	r := s.run(t, s.outside, nil, "init", "--dir", proj)
	wantCode(t, r, 0)
	wantInitialized(t, proj)
	wantPlainLine(t, r.stdout, "initialized "+proj+"/.notes")
	if _, err := os.Lstat(filepath.Join(s.outside, ".notes")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("init --dir created .notes in the working directory too (err %v)", err)
	}
}

// R2: a relative --dir is reported as an absolute path.
func TestInitRelativeDirReportsAbsolutePath_R2(t *testing.T) {
	s := newSandbox(t)
	proj := filepath.Join(s.work, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	r := s.run(t, s.work, nil, "init", "--dir", "proj", "--json")
	wantCode(t, r, 0)
	wantSuccessJSON(t, r.stdout, true, proj+"/.notes")
	wantInitialized(t, proj)
}

// R2: plain output without --json is exactly one line.
func TestInitPlainOutput_R2(t *testing.T) {
	s := newSandbox(t)
	r := s.run(t, s.work, nil, "init")
	wantCode(t, r, 0)
	wantPlainLine(t, r.stdout, "initialized "+s.work+"/.notes")
}

// R2: --json prints exactly {"created": true, "path": "<absolute PATH>/.notes"}.
func TestInitJSONOutput_R2(t *testing.T) {
	s := newSandbox(t)
	r := s.run(t, s.work, nil, "init", "--json")
	wantCode(t, r, 0)
	wantSuccessJSON(t, r.stdout, true, s.work+"/.notes")
	wantInitialized(t, s.work)
}

// R3: a repeat init exits 0, reports created false and changes no file,
// mode or modification time.
func TestInitIsIdempotent_R3(t *testing.T) {
	s := newSandbox(t)
	wantCode(t, s.run(t, s.work, nil, "init"), 0)
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	for _, p := range []string{filepath.Join(s.work, ".notes", "config.json"), filepath.Join(s.work, ".notes"), s.work} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, s.work, true)

	r := s.run(t, s.work, nil, "init", "--json")
	wantCode(t, r, 0)
	wantSuccessJSON(t, r.stdout, false, s.work+"/.notes")
	wantUnchanged(t, "working directory after a repeat init --json", before, snapshot(t, s.work, true))

	r = s.run(t, s.work, nil, "init")
	wantCode(t, r, 0)
	wantUnchanged(t, "working directory after a repeat init", before, snapshot(t, s.work, true))
	wantInitialized(t, s.work)
}

// R4: .notes as a symbolic link to a directory is refused with exit 2 and
// nothing is written through or beside it.
func TestInitRefusesSymlinkedNotesDir_R4(t *testing.T) {
	s := newSandbox(t)
	if err := os.Symlink(s.outside, filepath.Join(s.work, ".notes")); err != nil {
		t.Fatal(err)
	}
	beforeWork := snapshot(t, s.work, true)
	beforeOutside := snapshot(t, s.outside, true)

	r := s.run(t, s.work, nil, "init")
	wantCode(t, r, 2)
	wantErrorLine(t, r.stderr)
	wantUnchanged(t, "working directory", beforeWork, snapshot(t, s.work, true))
	wantUnchanged(t, "symlink target", beforeOutside, snapshot(t, s.outside, true))

	r = s.run(t, s.work, nil, "init", "--json")
	wantCode(t, r, 2)
	wantErrorJSON(t, r.stdout)
	wantUnchanged(t, "working directory", beforeWork, snapshot(t, s.work, true))
	wantUnchanged(t, "symlink target", beforeOutside, snapshot(t, s.outside, true))
}

// R4: a dangling .notes symlink is refused and its target is not created.
func TestInitRefusesDanglingSymlink_R4(t *testing.T) {
	s := newSandbox(t)
	target := filepath.Join(s.outside, "not-there")
	if err := os.Symlink(target, filepath.Join(s.work, ".notes")); err != nil {
		t.Fatal(err)
	}
	beforeWork := snapshot(t, s.work, true)
	r := s.run(t, s.work, nil, "init", "--json")
	wantCode(t, r, 2)
	wantErrorJSON(t, r.stdout)
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("symlink target %s was created (err %v)", target, err)
	}
	wantUnchanged(t, "working directory", beforeWork, snapshot(t, s.work, true))
}

// R4: .notes as a regular file is refused and left untouched.
func TestInitRefusesRegularFile_R4(t *testing.T) {
	s := newSandbox(t)
	if err := os.WriteFile(filepath.Join(s.work, ".notes"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s.work, true)

	r := s.run(t, s.work, nil, "init")
	wantCode(t, r, 2)
	wantErrorLine(t, r.stderr)
	wantUnchanged(t, "working directory", before, snapshot(t, s.work, true))

	r = s.run(t, s.work, nil, "init", "--json")
	wantCode(t, r, 2)
	wantErrorJSON(t, r.stdout)
	wantUnchanged(t, "working directory", before, snapshot(t, s.work, true))
}

// R5: a missing PATH is an error with exit 2 and is never created.
func TestInitMissingDir_R5(t *testing.T) {
	s := newSandbox(t)
	missing := filepath.Join(s.work, "absent")
	before := snapshot(t, s.root, false)

	r := s.run(t, s.work, nil, "init", "--dir", missing)
	wantCode(t, r, 2)
	wantErrorLine(t, r.stderr)

	r = s.run(t, s.work, nil, "init", "--dir", missing, "--json")
	wantCode(t, r, 2)
	wantErrorJSON(t, r.stdout)

	if _, err := os.Lstat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing PATH %s was created (err %v)", missing, err)
	}
	wantUnchanged(t, "sandbox", before, snapshot(t, s.root, false))
}

// R6: init writes only PATH/.notes and its config.json; the working
// directory, HOME, TMPDIR and unrelated files stay byte-for-byte the same.
func TestInitWritesNothingElse_R6(t *testing.T) {
	s := newSandbox(t)
	proj := filepath.Join(s.work, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		filepath.Join(proj, "unrelated.txt"):      "unrelated\n",
		filepath.Join(s.work, "sibling.txt"):      "sibling\n",
		filepath.Join(s.home, ".notesrc"):         "home file\n",
		filepath.Join(s.outside, "untouched.txt"): "outside\n",
	} {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, s.root, false)

	r := s.run(t, s.outside, nil, "init", "--dir", proj)
	wantCode(t, r, 0)
	wantInitialized(t, proj)

	after := snapshot(t, s.root, false)
	d := diff(before, after)
	want := []string{"added work/proj/.notes", "added work/proj/.notes/config.json"}
	if strings.Join(d, "\n") != strings.Join(want, "\n") {
		t.Fatalf("filesystem changes = %v, want only %v", d, want)
	}
}

// R7: with TERM=dumb and non-terminal output there are no escape sequences,
// and the command never waits for input even when stdin stays open.
func TestNoEscapesAndNoInputWait_R7(t *testing.T) {
	s := newSandbox(t)
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinR.Close()
	defer stdinW.Close()

	results := []result{
		s.run(t, s.work, stdinR, "init"),
		s.run(t, s.work, stdinR, "init", "--json"),
		s.run(t, s.work, stdinR, "init", "--dir", filepath.Join(s.work, "absent")),
		s.run(t, s.work, stdinR, "init", "--dir", filepath.Join(s.work, "absent"), "--json"),
	}
	wantCode(t, results[0], 0)
	wantCode(t, results[1], 0)
	wantCode(t, results[2], 2)
	wantCode(t, results[3], 2)
	for i, r := range results {
		if strings.Contains(r.stdout, "\x1b") || strings.Contains(r.stderr, "\x1b") {
			t.Fatalf("run %d wrote an escape sequence: stdout %q stderr %q", i, r.stdout, r.stderr)
		}
	}
}
