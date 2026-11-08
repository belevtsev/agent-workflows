//go:build unix

package projectctx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGitDir writes a git wrapper that runs the real git unless one of its
// arguments equals match, in which case it records the hit in marker and
// runs action instead. It returns the wrapper's directory.
func fakeGitDir(t *testing.T, realGit, match, action, marker string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"for arg do\n" +
		"  if [ \"$arg\" = '" + match + "' ]; then\n" +
		"    echo hit >> '" + marker + "'\n" +
		"    " + action + "\n" +
		"  fi\n" +
		"done\n" +
		"exec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestCaptureReportsAbortedProbes requires that a Git probe which runs past
// its budget or is killed ends capture with an error naming the cause,
// instead of treating the repository as a plain folder or dropping HEAD or
// the branch, while a directory that is not a repository, or a host without
// Git, keeps the folder-only identity.
func TestCaptureReportsAbortedProbes(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	const budget = time.Second
	const sleep = "exec sleep 30"
	const kill = "kill -KILL $$"
	const folderOnly = "Git identity is unavailable; content identity covers the selected folder only"
	cases := []struct {
		name    string
		repo    bool   // a committed Git repository rather than a plain folder
		noGit   bool   // PATH holds no git at all
		match   string // the argument that selects the slow or killed probe
		action  string
		wantErr string // "" means capture must succeed folder-only
	}{
		{name: "toplevel probe times out", repo: true, match: "--show-toplevel", action: sleep, wantErr: "Git inspection timed out after 1s (git rev-parse)"},
		{name: "toplevel probe is killed", repo: true, match: "--show-toplevel", action: kill, wantErr: "Git inspection was killed before it finished (git rev-parse)"},
		{name: "HEAD probe times out", repo: true, match: "--verify", action: sleep, wantErr: "Git inspection timed out after 1s (git rev-parse)"},
		{name: "branch probe times out", repo: true, match: "symbolic-ref", action: sleep, wantErr: "Git inspection timed out after 1s (git symbolic-ref)"},
		{name: "index listing times out", repo: true, match: "--stage", action: sleep, wantErr: "Git inspection timed out after 1s (git ls-files)"},
		{name: "HEAD tree listing times out", repo: true, match: "ls-tree", action: sleep, wantErr: "Git inspection timed out after 1s (git ls-tree)"},
		{name: "untracked listing times out", repo: true, match: "--others", action: sleep, wantErr: "Git inspection timed out after 1s (git ls-files)"},
		{name: "plain folder", match: "--never-passed", action: sleep},
		{name: "plain folder without git", noGit: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			selected, err := canonicalDirectory(dir)
			if err != nil {
				t.Fatal(err)
			}
			if tc.repo {
				selected = gitFixture(t)
			} else {
				writeFile(t, selected, "main.go", "package main\n")
			}
			marker := filepath.Join(t.TempDir(), "hits")
			if tc.noGit {
				t.Setenv("PATH", t.TempDir())
			} else {
				t.Setenv("PATH", fakeGitDir(t, realGit, tc.match, tc.action, marker)+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			started := time.Now()
			got, err := capture(t.Context(), selected, budget)
			if elapsed := time.Since(started); elapsed > 20*time.Second {
				t.Errorf("capture took %s; an aborted probe must not wait for the slow git", elapsed)
			}
			if tc.wantErr != "" {
				if _, statErr := os.Stat(marker); statErr != nil {
					t.Fatalf("the fake git never reached the %s probe: %v", tc.match, statErr)
				}
				if err == nil {
					t.Fatalf("capture succeeded with root %q, head %q, branch %q, warnings %q; want error %q",
						got.snapshot.Root, got.snapshot.Head, got.snapshot.Branch, got.warnings, tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("capture error = %q, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("capture of a plain folder failed: %v", err)
			}
			if got.partial || got.snapshot.Root != selected || got.snapshot.Head != "" || !strings.HasPrefix(got.snapshot.Fingerprint, "sha256:") {
				t.Fatalf("plain folder snapshot = %+v, partial %t; want complete folder-only identity", got.snapshot, got.partial)
			}
			if len(got.warnings) != 1 || got.warnings[0] != folderOnly {
				t.Fatalf("plain folder warnings = %q, want only %q", got.warnings, folderOnly)
			}
		})
	}
}
