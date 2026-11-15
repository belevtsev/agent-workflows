package declaration_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "Missing declaration", scenario "No file".
func TestMissingDeclarationPointsToInit(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	repo.Remove("gatecub.toml")

	result := repo.Gatecub("status")
	if result.Code != 1 {
		t.Errorf("gatecub status exited %d, want 1", result.Code)
	}
	for _, want := range []string{"gatecub.toml", "gatecub init"} {
		if !strings.Contains(result.Output(), want) {
			t.Errorf("the output does not hold %q:\n%s", want, result.Output())
		}
	}
}

// Requirement "Create a declaration", scenario "A new repository".
func TestInitCreatesADeclarationWithNoChecks(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.NewRepo(t)
	repo.Write("README.md", "# new\n")
	commitAll(repo, "first")

	if result := repo.Gatecub("init"); !repo.Exists("gatecub.toml") {
		t.Fatalf("gatecub init (exit %d) created no gatecub.toml:\n%s", result.Code, result.Output())
	}
	result := repo.Gatecub("status")
	if result.Code != 0 {
		t.Errorf("gatecub status exited %d, want 0", result.Code)
	}
	if !strings.Contains(strings.ToLower(result.Output()), "no check") {
		t.Errorf("gatecub status does not say that no checks are declared:\n%s", result.Output())
	}
}

// Requirement "Create a declaration", scenario "An existing declaration is
// kept".
func TestInitKeepsAnExistingDeclaration(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	before := sha256File(repo, "gatecub.toml")

	result := repo.Gatecub("init")
	if after := sha256File(repo, "gatecub.toml"); after != before {
		t.Errorf("gatecub init (exit %d) changed gatecub.toml:\n%s", result.Code, repo.Read("gatecub.toml"))
	}
}

// Requirement "Add a check from the command line", scenario "A check is
// appended".
func TestAddAppendsACheck(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	before := repo.Read("gatecub.toml")

	result := repo.Gatecub("add", "build", "--timeout", "5m", "--", "make", "-j4", "build")
	after := repo.Read("gatecub.toml")
	if !strings.HasPrefix(after, before) {
		t.Fatalf("gatecub add (exit %d) did not keep the previous content of gatecub.toml:\n%s\n%s", result.Code, after, result.Output())
	}
	var declaration struct {
		Check []struct {
			ID      string   `toml:"id"`
			Run     []string `toml:"run"`
			Timeout string   `toml:"timeout"`
		} `toml:"check"`
	}
	if err := toml.Unmarshal([]byte(after), &declaration); err != nil {
		t.Fatalf("gatecub.toml after gatecub add: %v\n%s", err, after)
	}
	if len(declaration.Check) == 0 {
		t.Fatalf("gatecub.toml declares no checks:\n%s", after)
	}
	last := declaration.Check[len(declaration.Check)-1]
	if last.ID != "build" {
		t.Errorf("the last check has id %q, want build", last.ID)
	}
	if !slices.Equal(last.Run, []string{"make", "-j4", "build"}) {
		t.Errorf("the last check has run %q, want [make -j4 build]", last.Run)
	}
	if last.Timeout != "5m" {
		t.Errorf("the last check has timeout %q, want 5m", last.Timeout)
	}
	if !strings.Contains(after[len(before):], "build") {
		t.Errorf("the appended text does not hold the check build:\n%s", after[len(before):])
	}
}

// Requirement "Add a check from the command line", scenario "A second check
// with the same id is refused".
func TestAddRefusesAnIDThatExists(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	sum := sha256File(repo, "gatecub.toml")
	before := fingerprint(t, repo.Dir, repo.Home)

	result := repo.Gatecub("add", "unit", "--", "true")
	if result.Code != 1 {
		t.Errorf("gatecub add exited %d, want 1", result.Code)
	}
	if !strings.Contains(result.Output(), "unit") {
		t.Errorf("the output does not name unit:\n%s", result.Output())
	}
	// Second challenge, finding U7: the user learns how to change a check.
	if !strings.Contains(result.Output(), "`gatecub add` only appends a check") {
		t.Errorf("the output does not say that `gatecub add` only appends a check:\n%s", result.Output())
	}
	if after := sha256File(repo, "gatecub.toml"); after != sum {
		t.Errorf("gatecub add changed gatecub.toml:\n%s", repo.Read("gatecub.toml"))
	}
	assertWroteNothing(t, repo, before)
}
