package reporting_test

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Requirement "Status", scenario "Everything is satisfied".
func TestEverythingIsSatisfied(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	checked(t, repo)

	result := repo.CW("status")
	if result.Code != 0 {
		t.Errorf("cw status exited %d, want 0", result.Code)
	}
	for _, id := range []string{"unit", "lint"} {
		if _, ok := lineWith(result.Stdout, id, "satisfied"); !ok {
			t.Errorf("cw status prints no line with %q as satisfied:\n%s", id, result.Output())
		}
	}
}

// Requirement "Status", scenario "Something is missing".
func TestSomethingIsMissing(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	repo.Write("src/app.txt", "broken\n")
	if result := repo.CW("run"); result.Code == 0 {
		t.Fatalf("cw run exited 0 on broken content:\n%s", result.Output())
	}
	object, _ := statusJSON(t, repo)
	log := object.check(t, "unit").Log
	if log == "" {
		t.Fatalf("cw status --json gives no log for unit: %+v", object.Checks)
	}
	if _, err := os.Stat(log); err != nil {
		t.Errorf("the log of unit cannot be read: %v", err)
	}

	result := repo.CW("status")
	if result.Code != 5 {
		t.Errorf("cw status exited %d, want 5", result.Code)
	}
	if _, ok := lineWith(result.Stdout, "unit", "failed", log); !ok {
		t.Errorf("cw status prints no line with unit as failed and the log %s:\n%s", log, result.Output())
	}
	if _, ok := lineWith(result.Stdout, "lint", "satisfied"); !ok {
		t.Errorf("cw status prints no line with lint as satisfied:\n%s", result.Output())
	}
	if !strings.Contains(result.Output(), "cw run") {
		t.Errorf("cw status does not print the command `cw run`:\n%s", result.Output())
	}
}

// Requirement "Status", scenario "No checks are declared".
func TestNoChecksAreDeclared(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	repo.Write("cw.toml", "version = 1\n")

	result := repo.CW("status")
	if result.Code != 0 {
		t.Errorf("cw status exited %d, want 0", result.Code)
	}
	// The spec gives no wording; the output must say "no check(s)" and
	// that commits are "allowed".
	text := strings.ToLower(result.Output())
	if !strings.Contains(text, "no check") {
		t.Errorf("cw status does not say that no checks are declared:\n%s", result.Output())
	}
	if !strings.Contains(text, "every commit") || !strings.Contains(text, "allowed") {
		t.Errorf("cw status does not say that every commit is allowed:\n%s", result.Output())
	}
}

// Requirement "Status", scenario "The index file is not rewritten".
func TestTheIndexFileIsNotRewritten(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	checked(t, repo)
	index := repo.Path(".git/index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	// A rewrite now would show in the modification time even on a file
	// system with a coarse clock.
	time.Sleep(20 * time.Millisecond)

	if result := repo.CW("status"); result.Code != 0 {
		t.Errorf("cw status exited %d in the checked state, want 0:\n%s", result.Code, result.Output())
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("cw status changed the bytes of .git/index")
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Errorf("cw status changed the modification time of .git/index from %v to %v", beforeInfo.ModTime(), afterInfo.ModTime())
	}
}

// Requirement "Staged content", scenario "Part of the work is staged".
func TestPartOfTheWorkIsStaged(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	repo.Write("README.md", "# demo, edited\n")
	// unit.sh compares the output of cat, which drops trailing newlines,
	// so this edit keeps unit passing.
	repo.Write("src/app.txt", "ok\n\n")
	checked(t, repo)
	repo.Git("add", "README.md")

	result := repo.CW("status")
	if result.Code != 0 {
		t.Errorf("cw status exited %d, want 0", result.Code)
	}
	// The spec gives no wording: src/app.txt must be listed after the words
	// "not staged", and a line must say that a commit of the index would be
	// denied.
	_, rest, found := strings.Cut(result.Stdout, "not staged")
	listed := slices.ContainsFunc(strings.Split(rest, "\n"), func(line string) bool {
		return strings.TrimSpace(line) == "src/app.txt"
	})
	if !found || !listed {
		t.Errorf("cw status does not list src/app.txt as not staged:\n%s", result.Output())
	}
	if line, ok := lineWith(result.Stdout, "README.md"); ok {
		t.Errorf("cw status lists the staged README.md: %q\n%s", line, result.Output())
	}
	if _, ok := lineWith(result.Stdout, "index", "denied"); !ok {
		t.Errorf("cw status does not say that a commit of the index alone would be denied:\n%s", result.Output())
	}
}

// Requirement "Gate state", scenario "A clone without hooks".
func TestACloneWithoutHooks(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	checked(t, repo)
	clone := repo.In(t.TempDir() + "/clone")
	repo.Git("clone", "-q", repo.Dir, clone.Dir)

	result := clone.CW("status")
	// The spec gives no wording: one line must be a warning that speaks of
	// the hooks and names `cw init`.
	if _, ok := lineWith(strings.ToLower(result.Output()), "warn", "hook", "cw init"); !ok {
		t.Errorf("cw status in the clone does not warn about the hooks and name `cw init`:\n%s", result.Output())
	}
}

// Requirement "Gate state", scenario "A hooks directory that does not call
// `cw`".
func TestAHooksDirectoryThatDoesNotCallCw(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	repo.Git("config", "core.hooksPath", ".githooks")
	if err := os.MkdirAll(repo.Path(".githooks"), 0o755); err != nil {
		t.Fatal(err)
	}

	object, result := statusJSON(t, repo)
	if object.Hooks != "missing" {
		t.Errorf("hooks is %q, want missing:\n%s", object.Hooks, result.Output())
	}
}

// Requirement "Status as JSON", scenario "The object for a failed check".
func TestTheObjectForAFailedCheck(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	repo.Write("src/app.txt", "broken\n")
	if result := repo.CW("run"); result.Code == 0 {
		t.Fatalf("cw run exited 0 on broken content:\n%s", result.Output())
	}

	object, result := statusJSON(t, repo)
	if object.Verified == nil || *object.Verified {
		t.Errorf("verified is not false:\n%s", result.Stdout)
	}
	if object.Mode != "enforce" {
		t.Errorf("mode is %q, want enforce", object.Mode)
	}
	unit := object.check(t, "unit")
	if unit.State != "failed" {
		t.Errorf("unit has state %q, want failed", unit.State)
	}
	if unit.Outcome != "failed" {
		t.Errorf("unit has outcome %q, want failed", unit.Outcome)
	}
	if unit.ExitStatus == nil || *unit.ExitStatus != 3 {
		t.Errorf("unit has exit_status %v, want 3:\n%s", unit.ExitStatus, result.Stdout)
	}
}
