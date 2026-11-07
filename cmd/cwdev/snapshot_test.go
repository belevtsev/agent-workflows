package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// R9 of contract wp10-snapshot revision 1: verification options apply only to
// cwdev check, and --snapshot cannot be combined with release options.

func snapCLIIsolate(t *testing.T) string {
	t.Helper()
	if os.Getenv("GOCACHE") == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			t.Setenv("GOCACHE", filepath.Join(cache, "go-build"))
		}
	}
	if os.Getenv("GOPATH") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			t.Setenv("GOPATH", filepath.Join(home, "go"))
		}
	}
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOTOOLCHAIN", "local")
	tmp := t.TempDir()
	home := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR",
		"GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	return tmp
}

func snapCLIGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, output)
	}
}

// snapCLIRepository creates a fixture repository with a commit and an
// uncommitted change, so a snapshot of it would succeed.
func snapCLIRepository(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapCLIGit(t, root, "init", "-q")
	snapCLIGit(t, root, "config", "user.name", "Snapshot Fixture")
	snapCLIGit(t, root, "config", "user.email", "snapshot-fixture@example.invalid")
	snapCLIGit(t, root, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapCLIGit(t, root, "add", "-A")
	snapCLIGit(t, root, "commit", "-q", "--no-verify", "-m", "base")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// snapCLIRun runs cwdev with stdout and stderr captured.
func snapCLIRun(t *testing.T, arguments []string) (string, error) {
	t.Helper()
	capture, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	runErr := func() error {
		stdout, stderr := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = capture, capture
		defer func() { os.Stdout, os.Stderr = stdout, stderr }()
		return run(t.Context(), arguments)
	}()
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}

func TestRunRejectsSnapshotWithReleaseOptions(t *testing.T) {
	revision := strings.Repeat("0a1b2c3d4e", 4)
	cases := []struct {
		name          string
		arguments     func(dir string) []string
		wantErrorText []string
	}{
		{
			name: "check with release version",
			arguments: func(dir string) []string {
				return []string{"check", "--snapshot", "--release-version", "v1.0.1", "--source", dir}
			},
		},
		{
			name: "check with revision",
			arguments: func(dir string) []string {
				return []string{"check", "--snapshot", "--revision", revision, "--source", dir}
			},
		},
		{
			name: "release build with snapshot",
			arguments: func(string) []string {
				return []string{"release", "build", "--version", "v1.0.1", "--revision", revision, "--snapshot"}
			},
			wantErrorText: []string{"--snapshot", "apply only to check"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tmp := snapCLIIsolate(t)
			dir := snapCLIRepository(t)
			// Relative defaults (for example a release's source or dist) resolve
			// inside the disposable fixture rather than this repository.
			t.Chdir(dir)
			arguments := testCase.arguments(dir)
			output, err := snapCLIRun(t, arguments)
			if err == nil {
				t.Fatalf("R9: run(%q) returned nil, want an error; output:\n%s", arguments, output)
			}
			if strings.Contains(output, "Snapshot tree:") {
				t.Errorf("R9: run(%q) created a snapshot before rejecting the options; output:\n%s", arguments, output)
			}
			for _, text := range testCase.wantErrorText {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("R9: run(%q) error %q does not contain %q", arguments, err, text)
				}
			}
			entries, readErr := os.ReadDir(tmp)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "cwdev-snapshot-") {
					t.Errorf("R9: run(%q) left %s in TMPDIR", arguments, entry.Name())
				}
			}
		})
	}
}
