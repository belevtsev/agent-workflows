package pushgate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Requirement "Removal", scenario "Only `kitsu`'s own files go".
func TestOnlyCwsOwnFilesGo(t *testing.T) {
	t.Parallel()
	repo, _ := gated(t)
	checked(t, repo)
	hooks := hooksDir(t, repo)
	store := repo.StoreDir()
	for _, path := range []string{filepath.Join(hooks, "pre-commit"), filepath.Join(hooks, "pre-push"), store} {
		if !exists(path) {
			t.Fatalf("%s does not exist before kitsu uninstall", path)
		}
	}
	declaration := sha256Of(t, repo.Path("kitsu.toml"))

	result := repo.Kitsu("uninstall")
	if result.Code != 0 {
		t.Errorf("kitsu uninstall exited %d, want 0\n%s", result.Code, result.Output())
	}
	for _, path := range []string{filepath.Join(hooks, "pre-commit"), filepath.Join(hooks, "pre-push"), store} {
		if exists(path) {
			t.Errorf("%s is still there after kitsu uninstall", path)
		}
	}
	if after := sha256Of(t, repo.Path("kitsu.toml")); after != declaration {
		t.Errorf("kitsu uninstall changed kitsu.toml")
	}
}

// Requirement "Removal", scenario "Another tool's hook still calls `kitsu`".
func TestAnotherToolsHookStillCallsCw(t *testing.T) {
	t.Parallel()
	repo, _ := withOrigin(t)
	hooks := hooksDir(t, repo)
	preCommit := filepath.Join(hooks, "pre-commit")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho another tool >&2\nkitsu hook pre-commit \"$@\"\n"
	writeProgram(t, preCommit, script)
	// The hook of the other tool already calls kitsu, so nothing is left for
	// the user to edit and `kitsu init` exits 0 ("Hooks of other tools").
	mustKitsu(t, repo, 0, "init")
	checked(t, repo)
	store := repo.StoreDir()
	for _, path := range []string{filepath.Join(hooks, "pre-push"), store} {
		if !exists(path) {
			t.Fatalf("%s does not exist before kitsu uninstall", path)
		}
	}
	before := sha256Of(t, preCommit)

	result := repo.Kitsu("uninstall")
	if result.Code != 4 {
		t.Errorf("kitsu uninstall exited %d, want 4\n%s", result.Code, result.Output())
	}
	for _, path := range []string{filepath.Join(hooks, "pre-push"), store} {
		if exists(path) {
			t.Errorf("%s is still there after kitsu uninstall", path)
		}
	}
	if after := sha256Of(t, preCommit); after != before {
		t.Errorf("kitsu uninstall changed pre-commit")
	}
	output := result.Output()
	if !strings.Contains(output, preCommit) && !strings.Contains(output, ".git/hooks/pre-commit") {
		t.Errorf("the output does not hold the path of pre-commit:\n%s", output)
	}
	if !strings.Contains(output, `kitsu hook pre-commit "$@"`) {
		t.Errorf("the output does not hold the line to remove:\n%s", output)
	}
}
