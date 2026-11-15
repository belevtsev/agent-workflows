package reporting_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// observed is demo with the hooks installed in observe mode.
func observed(t *testing.T) *gatecubtest.Repo {
	t.Helper()
	repo := gatecubtest.Demo(t)
	if result := repo.Gatecub("init", "--observe"); result.Code != 0 {
		t.Fatalf("gatecub init --observe exited %d:\n%s", result.Code, result.Output())
	}
	return repo
}

// linesWith returns the lines of text that hold the word.
func linesWith(text, word string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, word) {
			out = append(out, line)
		}
	}
	return out
}

// Requirement "Observe mode" (publish-gate), scenario "A commit that would be
// denied goes through and is counted".
func TestACommitThatWouldBeDeniedGoesThroughAndIsCounted(t *testing.T) {
	t.Parallel()
	repo := observed(t)
	repo.Write("src/app.txt", "broken\n")
	if result := repo.Gatecub("run"); result.Code != 5 {
		t.Fatalf("gatecub run exited %d, want 5:\n%s", result.Code, result.Output())
	}
	before := head(repo)

	result := repo.TryGit("commit", "-a", "-m", "change")
	if result.Code != 0 || head(repo) == before {
		t.Errorf("the commit was not recorded (git exit %d):\n%s", result.Code, result.Output())
	}
	// The spec's own words: "would have been denied because `unit` failed".
	lines := linesWith(result.Stderr, "denied")
	if len(lines) != 1 {
		t.Errorf("standard error holds %d lines about the denial, want 1:\n%s", len(lines), result.Stderr)
	} else if _, ok := lineWith(lines[0], "would have been denied", "unit", "failed"); !ok {
		t.Errorf("the line does not say that the commit would have been denied because unit failed: %q", lines[0])
	}
	if n := statsJSON(t, repo).count("pre-commit", "observe", "denied"); n != 1 {
		t.Errorf("gatecub stats counts %d observed denials, want 1", n)
	}
}

// Requirement "Observe mode" (publish-gate), scenario "A cause that prevents
// a decision is observed too".
func TestACauseThatPreventsADecisionIsObservedToo(t *testing.T) {
	t.Parallel()
	repo := observed(t)
	repo.Write("gatecub.toml", `version = 1

[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]

[[check]]
tiemout = "5m"
id = "lint"
run = ["sh", "lint.sh"]
`)
	if line := strings.Split(repo.Read("gatecub.toml"), "\n")[8]; line != `tiemout = "5m"` {
		t.Fatalf("line 9 of gatecub.toml is %q", line)
	}
	repo.Git("add", "gatecub.toml")
	before := head(repo)

	result := repo.TryGit("commit", "-m", "change")
	if result.Code != 0 || head(repo) == before {
		t.Errorf("the commit was not recorded (git exit %d):\n%s", result.Code, result.Output())
	}
	// The spec gives no exact wording: one line must say "would have been
	// denied" and name the declaration as invalid.
	lines := linesWith(result.Stderr, "denied")
	if len(lines) != 1 {
		t.Errorf("standard error holds %d lines about the denial, want 1:\n%s", len(lines), result.Stderr)
	} else if _, ok := lineWith(lines[0], "would have been denied", "declaration", "invalid"); !ok {
		t.Errorf("the line does not say that the commit would have been denied because the declaration is invalid: %q", lines[0])
	}
}

// Requirement "Observe mode" (publish-gate), scenario "Enforcing again".
func TestEnforcingAgain(t *testing.T) {
	t.Parallel()
	repo := observed(t)
	if result := repo.Gatecub("init"); result.Code != 0 {
		t.Fatalf("gatecub init exited %d:\n%s", result.Code, result.Output())
	}
	statuses := hookStatuses(t, repo)
	repo.Write("src/app.txt", "broken\n")
	if result := repo.Gatecub("run"); result.Code != 5 {
		t.Fatalf("gatecub run exited %d, want 5:\n%s", result.Code, result.Output())
	}
	before := head(repo)

	// "Denied" in publish-gate: the hook exits 3, git records nothing, and
	// the reason is printed on standard error.
	result := repo.TryGit("commit", "-a", "-m", "change")
	if result.Code == 0 || head(repo) != before {
		t.Errorf("the commit was recorded (git exit %d):\n%s", result.Code, result.Output())
	}
	if got := statuses(); len(got) != 1 || got[0] != "pre-commit 3" {
		t.Errorf("gatecub hook ran with the exit statuses %q, want [pre-commit 3]", got)
	}
	if _, ok := lineWith(result.Stderr, "unit"); !ok {
		t.Errorf("standard error does not give the reason:\n%s", result.Stderr)
	}
}
