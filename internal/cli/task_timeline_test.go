package cli_test

// Independent in-process tests for the error paths of `cw task timeline`
// (TL-8, with the read-only rule TL-9 on failing runs), authored from interface
// contract revision 2. Successful runs on a real task directory are covered
// end to end in cmd/cw/timeline_e2e_test.go.

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
)

type tlcEnv struct {
	root, home, work, state string
}

func tlcNewEnv(t *testing.T) tlcEnv {
	t.Helper()
	root := filepath.Join(t.TempDir(), "timeline cli root")
	env := tlcEnv{
		root:  root,
		home:  filepath.Join(root, "home"),
		work:  filepath.Join(root, "work"),
		state: filepath.Join(root, "state"),
	}
	for _, dir := range []string{env.home, env.work, env.state} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return env
}

// tlcRun executes cw in process with a closed environment: only HOME is set,
// and home and working directories live under the test's root.
func tlcRun(t *testing.T, env tlcEnv, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	vars := map[string]string{"HOME": env.home}
	err = cli.Execute(t.Context(), append([]string{"--state-dir", env.state}, args...), cli.Config{
		In:         strings.NewReader(""),
		Out:        &out,
		Err:        &errOut,
		HomeDir:    func() (string, error) { return env.home, nil },
		WorkingDir: func() (string, error) { return env.work, nil },
		Getenv:     func(name string) string { return vars[name] },
	})
	return out.String(), errOut.String(), err
}

// tlcSnapshot lists every entry under root with its mode, its bytes for a
// regular file and its target for a symlink, so any create, modify, rename,
// remove or chmod shows up as a difference.
func tlcSnapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		line := fmt.Sprintf("%s %v", rel, info.Mode())
		switch {
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += fmt.Sprintf(" %q", data)
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			line += " -> " + target
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TL-8: a missing or empty --task, a positional argument and an unknown flag
// are usage errors (exit status 2) that write nothing to standard output and
// touch nothing on disk; the missing --task message names --task. Fails if the
// command accepts an empty task, maps a usage error to 1, or prints output.
func TestTaskTimelineTL8UsageErrors(t *testing.T) {
	// taskDir stands for a valid-looking --task: an owner-only directory
	// under the test root, so in the positional and unknown-flag cases only
	// that argument can cause the usage error (test review T3).
	const taskDir = "\x00task-dir"
	cases := []struct {
		name      string
		args      []string
		namesTask bool
	}{
		{"no --task", []string{"task", "timeline"}, true},
		{"no --task with --summary", []string{"task", "timeline", "--summary"}, true},
		{"empty --task", []string{"task", "timeline", "--task", ""}, true},
		{"empty --task= form", []string{"task", "timeline", "--task="}, true},
		{"no --task with --dry-run", []string{"--dry-run", "task", "timeline"}, true},
		{"positional argument without --task", []string{"task", "timeline", "extra"}, false},
		{"unknown flag without --task", []string{"task", "timeline", "--no-such-flag"}, false},
		{"positional argument with a valid-looking --task", []string{"task", "timeline", "--task", taskDir, "extra"}, false},
		{"positional argument before --task", []string{"task", "timeline", "extra", "--task", taskDir}, false},
		{"unknown flag with a valid-looking --task", []string{"task", "timeline", "--task", taskDir, "--no-such-flag"}, false},
		{"unknown flag with --summary and a valid-looking --task", []string{"task", "timeline", "--summary", "--no-such-flag", "--task", taskDir}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := tlcNewEnv(t)
			dir := filepath.Join(env.root, "owner only task")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			args := make([]string, len(tc.args))
			for i, arg := range tc.args {
				if arg == taskDir {
					arg = dir
				}
				args[i] = arg
			}
			before := tlcSnapshot(t, env.root)
			stdout, _, err := tlcRun(t, env, args...)
			if err == nil {
				t.Fatalf("cw %v succeeded, want a usage error", tc.args)
			}
			if code := cli.ExitCode(err); code != 2 {
				t.Errorf("exit status = %d, want 2 (usage); error: %v", code, err)
			}
			if tc.namesTask && !strings.Contains(err.Error(), "--task") {
				t.Errorf("error %q does not name --task", err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing on error", stdout)
			}
			if after := tlcSnapshot(t, env.root); after != before {
				t.Errorf("failing command changed the file system:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// TL-8 and TL-9: a --task path that is missing, not a directory, a symlink,
// group- or other-accessible, or an owner-only directory without a valid
// record is a runtime error (exit status 1) with nothing on standard output,
// with or without --summary and --dry-run, and the file system is unchanged.
// Each case is also a control for the usage cases above: these arguments are
// well formed, so a status of 2 here means the command misparsed them.
func TestTaskTimelineTL8RuntimeErrors(t *testing.T) {
	type layout func(t *testing.T, root string) string
	cases := []struct {
		name  string
		setup layout
	}{
		{"missing directory", func(t *testing.T, root string) string {
			return filepath.Join(root, "missing task")
		}},
		{"regular file", func(t *testing.T, root string) string {
			path := filepath.Join(root, "task file")
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"symlink to an owner-only directory", func(t *testing.T, root string) string {
			target := filepath.Join(root, "real task")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "task link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			return link
		}},
		{"group and other permission bits", func(t *testing.T, root string) string {
			path := filepath.Join(root, "open task")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o755); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"owner-only directory without a record", func(t *testing.T, root string) string {
			path := filepath.Join(root, "empty task")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		// No revision file: these names are not record-NNNNNN.json, so the
		// directory holds no revision at all. Requires the runtime error of a
		// directory without a valid record: exit status 1, nothing on
		// standard output and the directory unchanged. Fails if the command
		// succeeds (for example with an empty timeline), exits with another
		// status, prints anything, or changes a file.
		{"owner-only directory with only non-revision names", func(t *testing.T, root string) string {
			path := filepath.Join(root, "no revision task")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"record.json", "revision-000001.json", "000001.json"} {
				if err := os.WriteFile(filepath.Join(path, name), []byte("{not json"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			return path
		}},
		// Corrupt revision 1 (code review C1): record-000001.json, the sole
		// revision, has a real revision name and owner-only mode, but its
		// content is not JSON. Requires exit status 1, nothing on standard
		// output and the directory unchanged. Fails if the command succeeds,
		// exits with another status, prints anything, or rewrites or removes
		// the undecodable file. A corrupt revision after a valid one is
		// covered end to end by TestTimelineE2ECorruptRevisionAfterValid in
		// cmd/cw/timeline_e2e_test.go.
		{"owner-only directory with a corrupt revision 1 (invalid JSON)", func(t *testing.T, root string) string {
			path := filepath.Join(root, "corrupt revision task")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "record-000001.json"), []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		// Corrupt revision 1: record-000001.json, the sole revision, is valid
		// JSON but not a JSON object, so it cannot be decoded as a revision of
		// the stored format. Requires exit status 1, nothing on standard
		// output and the directory unchanged. Fails if the command succeeds,
		// exits with another status, prints anything, or changes a file. A
		// corrupt revision after a valid one is covered end to end by
		// TestTimelineE2ECorruptRevisionAfterValid in
		// cmd/cw/timeline_e2e_test.go.
		{"owner-only directory with a corrupt revision 1 (JSON array, not an object)", func(t *testing.T, root string) string {
			path := filepath.Join(root, "non-object revision task")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "record-000001.json"), []byte("[]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	}
	variants := [][]string{nil, {"--summary"}}
	for _, tc := range cases {
		for _, extra := range variants {
			for _, dryRun := range []bool{false, true} {
				name := fmt.Sprintf("%s/extra=%v/dry-run=%v", tc.name, extra, dryRun)
				t.Run(name, func(t *testing.T) {
					env := tlcNewEnv(t)
					dir := tc.setup(t, env.root)
					before := tlcSnapshot(t, env.root)
					args := []string{"task", "timeline", "--task", dir}
					args = append(args, extra...)
					if dryRun {
						args = append([]string{"--dry-run"}, args...)
					}
					stdout, _, err := tlcRun(t, env, args...)
					if err == nil {
						t.Fatalf("cw %v succeeded with stdout %q, want a runtime error", args, stdout)
					}
					if code := cli.ExitCode(err); code != 1 {
						t.Errorf("exit status = %d, want 1 (runtime); error: %v", code, err)
					}
					if stdout != "" {
						t.Errorf("stdout = %q, want nothing on error", stdout)
					}
					if after := tlcSnapshot(t, env.root); after != before {
						t.Errorf("failing command changed the file system:\nbefore:\n%s\nafter:\n%s", before, after)
					}
				})
			}
		}
	}
}

// tlcSummaryUsage returns the help text of the --summary flag: its flag line
// plus any continuation lines up to the next flag line or blank line, so a
// wrapped usage string is kept whole and other flags' text is left out.
func tlcSummaryUsage(help string) string {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "-") || !strings.Contains(trimmed, "--summary") {
			continue
		}
		usage := []string{trimmed}
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if next == "" || strings.HasPrefix(next, "-") {
				break
			}
			usage = append(usage, next)
		}
		return strings.Join(usage, " ")
	}
	return ""
}

// TL-7 (revision 3): `task timeline --help` describes --summary with its own
// text saying it prints JSON Lines, not the shared "one-line summary" text.
// Fails if the command reuses the shared summary flag help.
func TestTaskTimelineTL7SummaryHelpText(t *testing.T) {
	env := tlcNewEnv(t)
	stdout, stderr, _ := tlcRun(t, env, "task", "timeline", "--help")
	help := stdout + stderr
	usage := tlcSummaryUsage(help)
	if usage == "" {
		t.Fatalf("help text has no --summary flag:\n%s", help)
	}
	if !strings.Contains(strings.ToLower(usage), "json lines") {
		t.Errorf("--summary help %q does not say it prints JSON Lines", usage)
	}
	if strings.Contains(strings.ToLower(usage), "one-line summary") {
		t.Errorf("--summary help %q uses the shared \"one-line summary\" text", usage)
	}
}
