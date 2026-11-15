package evidence_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "Running checks", scenario "Both checks run".
func TestBothChecksRunInDeclarationOrder(t *testing.T) {
	t.Parallel()
	repo, trace := tracedDemo(t)

	result := repo.Gatecub("run")

	if ran := executed(t, trace); !slices.Equal(ran, []string{"unit", "lint"}) {
		t.Errorf("scripts executed: %v, want [unit lint]", ran)
	}
	for _, id := range []string{"unit", "lint"} {
		if !lineWith(result.Output(), id, "passed") {
			t.Errorf("the output does not name %s as passed", id)
		}
	}
	if result.Code != 0 {
		t.Errorf("exit status %d, want 0", result.Code)
	}
	if t.Failed() {
		t.Logf("output:\n%s", result.Output())
	}
}

// Requirement "Running checks", scenario "A satisfied check is not run again".
func TestSatisfiedCheckIsNotRunAgain(t *testing.T) {
	t.Parallel()
	repo, trace := tracedDemo(t)
	checkedState(t, repo)
	if err := os.Remove(trace); err != nil {
		t.Fatal(err)
	}

	result := repo.Gatecub("run")

	if ran := executed(t, trace); len(ran) != 0 {
		t.Errorf("scripts executed: %v, want none", ran)
	}
	for _, id := range []string{"unit", "lint"} {
		if !lineWith(result.Output(), id, "satisfied") {
			t.Errorf("the output does not name %s as satisfied", id)
		}
	}
	if result.Code != 0 {
		t.Errorf("exit status %d, want 0", result.Code)
	}
	if t.Failed() {
		t.Logf("output:\n%s", result.Output())
	}
}

// Requirement "Running checks", scenario "Only the named check runs".
func TestOnlyTheNamedCheckRuns(t *testing.T) {
	t.Parallel()
	repo, trace := tracedDemo(t)
	checkedState(t, repo)
	if err := os.Remove(trace); err != nil {
		t.Fatal(err)
	}

	result := repo.Gatecub("run", "lint", "--force")

	if ran := executed(t, trace); !slices.Equal(ran, []string{"lint"}) {
		t.Fatalf("scripts executed: %v, want [lint]\noutput (exit status %d):\n%s", ran, result.Code, result.Output())
	}
}

// Requirement "Running checks", scenario "The environment reaches the check".
func TestEnvironmentReachesTheCheck(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	declare(t, repo, "[[check]]\nid = \"env\"\nrun = [\"env\"]\n")
	repo.Setenv("GIT_CONFIG_KEY_0", "user.name")
	repo.Setenv("TOKENIZERS_PARALLELISM", "false")

	result := repo.Gatecub("run", "env")

	record := lastRecord(t, repo, "env")
	log, _ := record["log"].(string)
	if log == "" {
		t.Fatalf("the record of env names no log: %v\noutput (exit status %d):\n%s", record, result.Code, result.Output())
	}
	data, err := os.ReadFile(logPath(repo, log))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"GIT_CONFIG_KEY_0=user.name", "TOKENIZERS_PARALLELISM=false"} {
		if !slices.Contains(strings.Split(string(data), "\n"), line) {
			t.Errorf("the log does not hold %s", line)
		}
	}
	if t.Failed() {
		t.Logf("log:\n%s", data)
	}
}

// Requirement "Running checks", scenario "An unknown check name is a usage
// error".
func TestUnknownCheckNameIsAUsageError(t *testing.T) {
	t.Parallel()
	repo, trace := tracedDemo(t)

	result := repo.Gatecub("run", "nosuch")

	if result.Code != 2 {
		t.Errorf("exit status %d, want 2", result.Code)
	}
	if !strings.Contains(result.Output(), "nosuch") {
		t.Errorf("the output does not name nosuch")
	}
	if ran := executed(t, trace); len(ran) != 0 {
		t.Errorf("scripts executed: %v, want none", ran)
	}
	if t.Failed() {
		t.Logf("output:\n%s", result.Output())
	}
}
