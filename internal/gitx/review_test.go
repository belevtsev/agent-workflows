package gitx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Every git call of gatecub takes no optional lock and fetches no object that a
// partial clone lacks, so that gatecub rewrites no index and starts no connection
// (second challenge, finding S6). The variables are checked in the
// environment gatecub builds and in the one git hands to a command it starts.
func TestEveryGitCallTakesNoLockAndFetchesNothing(t *testing.T) {
	dir, _ := committed(t, map[string]string{"a.txt": "a\n"})
	for _, foreign := range []bool{false, true} {
		env := environment(foreign, "EXTRA=1")
		for _, want := range []string{"GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "EXTRA=1"} {
			if !slices.Contains(env, want) {
				t.Errorf("environment(foreign=%v) lacks %s", foreign, want)
			}
		}

		// An alias that starts a program shows the environment git runs in.
		out, err := run(t.Context(), call{dir: dir, foreign: foreign}, "-c", "alias.gatecubenv=!env", "gatecubenv")
		if err != nil {
			t.Fatalf("git gatecubenv (foreign=%v): %v", foreign, err)
		}
		lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
		for _, want := range []string{"GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1"} {
			if !slices.Contains(lines, want) {
				t.Errorf("git ran without %s (foreign=%v)", want, foreign)
			}
		}

		// git's file system monitor is off for every call.
		out, err = run(t.Context(), call{dir: dir, foreign: foreign}, "config", "core.fsmonitor")
		if err != nil || strings.TrimSpace(string(out)) != "false" {
			t.Errorf("core.fsmonitor in a call of gatecub (foreign=%v) = %q, %v; want false", foreign, out, err)
		}
	}
}

// Only exit status 1 of `git rev-parse --verify --quiet` means that a
// revision names nothing. Any other failure, such as an object git cannot
// read, is an error and not "absent" (second challenge, finding A8).
func TestResolveReportsAnObjectThatCannotBeRead(t *testing.T) {
	dir, repo := committed(t, map[string]string{"a.txt": "a\n"})
	head := git(t, dir, "rev-parse", "HEAD")
	object := filepath.Join(dir, ".git", "objects", head[:2], head[2:])
	if err := os.Chmod(object, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(object, []byte("not a git object\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, resolve := range map[string]func() (string, bool, error){
		"TreeOf(HEAD)":     func() (string, bool, error) { return repo.TreeOf(t.Context(), "HEAD") },
		"CommitOf(HEAD)":   func() (string, bool, error) { return repo.CommitOf(t.Context(), "HEAD") },
		"TreeOf(<commit>)": func() (string, bool, error) { return repo.TreeOf(t.Context(), head) },
	} {
		got, found, err := resolve()
		var gitErr *Error
		if !errors.As(err, &gitErr) || gitErr.Code == 1 {
			t.Errorf("%s = %q, %v, %v; want a git error with a status other than 1", name, got, found, err)
		}
		if found {
			t.Errorf("%s says it found %q", name, got)
		}
	}

	// A name that names nothing is still "absent", without an error.
	if got, found, err := repo.TreeOf(t.Context(), "nosuch"); err != nil || found || got != "" {
		t.Errorf("TreeOf(nosuch) = %q, %v, %v; want absent without an error", got, found, err)
	}
}

// DiffPaths lists at most MaxPaths paths and says how many more differ
// (second challenge, finding A10).
func TestDiffPaths(t *testing.T) {
	files := map[string]string{}
	for i := range MaxPaths + 5 {
		files[fmt.Sprintf("f%02d.txt", i)] = "1\n"
	}
	dir, repo := committed(t, files)
	from := git(t, dir, "rev-parse", "HEAD^{tree}")

	cases := []struct {
		changed int
		more    int
	}{
		{0, 0},
		{3, 0},
		{MaxPaths, 0},
		{MaxPaths + 1, 1},
		{MaxPaths + 5, 5},
	}
	for _, tc := range cases {
		for i := range tc.changed {
			write(t, dir, fmt.Sprintf("f%02d.txt", i), fmt.Sprintf("changed %d\n", tc.changed))
		}
		git(t, dir, "add", "-A")
		to := git(t, dir, "write-tree")
		paths, more, err := repo.DiffPaths(t.Context(), from, to)
		if err != nil {
			t.Fatalf("%d changed: DiffPaths: %v", tc.changed, err)
		}
		wantLen := min(tc.changed, MaxPaths)
		if len(paths) != wantLen || more != tc.more {
			t.Errorf("%d changed: DiffPaths gives %d paths and %d more, want %d and %d", tc.changed, len(paths), more, wantLen, tc.more)
		}
		for i, path := range paths {
			if want := fmt.Sprintf("f%02d.txt", i); path != want {
				t.Errorf("%d changed: path %d is %q, want %q", tc.changed, i, path, want)
			}
		}
		git(t, dir, "reset", "-q", "--hard")
	}
	if _, _, err := repo.DiffPaths(t.Context(), from, "nosuch"); err == nil {
		t.Error("DiffPaths with an unknown tree gave no error")
	}
}
