package declaration_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Missing declaration", scenario "No file".
func TestMissingDeclarationPointsToInit(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	repo.Remove("kitsu.toml")

	result := repo.Kitsu("status")
	if result.Code != 1 {
		t.Errorf("kitsu status exited %d, want 1", result.Code)
	}
	for _, want := range []string{"kitsu.toml", "kitsu init"} {
		if !strings.Contains(result.Output(), want) {
			t.Errorf("the output does not hold %q:\n%s", want, result.Output())
		}
	}
}

// Requirement "Create a declaration", scenario "A new repository".
func TestInitCreatesADeclarationWithNoChecks(t *testing.T) {
	t.Parallel()
	repo := kitsutest.NewRepo(t)
	repo.Write("README.md", "# new\n")
	commitAll(repo, "first")

	if result := repo.Kitsu("init"); !repo.Exists("kitsu.toml") {
		t.Fatalf("kitsu init (exit %d) created no kitsu.toml:\n%s", result.Code, result.Output())
	}
	result := repo.Kitsu("status")
	if result.Code != 0 {
		t.Errorf("kitsu status exited %d, want 0", result.Code)
	}
	if !strings.Contains(strings.ToLower(result.Output()), "no check") {
		t.Errorf("kitsu status does not say that no checks are declared:\n%s", result.Output())
	}
}

// Requirement "Create a declaration", scenario "An existing declaration is
// kept".
func TestInitKeepsAnExistingDeclaration(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	before := sha256File(repo, "kitsu.toml")

	result := repo.Kitsu("init")
	if after := sha256File(repo, "kitsu.toml"); after != before {
		t.Errorf("kitsu init (exit %d) changed kitsu.toml:\n%s", result.Code, repo.Read("kitsu.toml"))
	}
}

// Requirement "Add a check from the command line", scenario "A check is
// appended".
func TestAddAppendsACheck(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	before := repo.Read("kitsu.toml")

	result := repo.Kitsu("add", "build", "--timeout", "5m", "--", "make", "-j4", "build")
	after := repo.Read("kitsu.toml")
	if !strings.HasPrefix(after, before) {
		t.Fatalf("kitsu add (exit %d) did not keep the previous content of kitsu.toml:\n%s\n%s", result.Code, after, result.Output())
	}
	var declaration struct {
		Check []struct {
			ID      string   `toml:"id"`
			Run     []string `toml:"run"`
			Timeout string   `toml:"timeout"`
		} `toml:"check"`
	}
	if err := toml.Unmarshal([]byte(after), &declaration); err != nil {
		t.Fatalf("kitsu.toml after kitsu add: %v\n%s", err, after)
	}
	if len(declaration.Check) == 0 {
		t.Fatalf("kitsu.toml declares no checks:\n%s", after)
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
	repo := kitsutest.Demo(t)
	sum := sha256File(repo, "kitsu.toml")
	before := fingerprint(t, repo.Dir, repo.Home)

	result := repo.Kitsu("add", "unit", "--", "true")
	if result.Code != 1 {
		t.Errorf("kitsu add exited %d, want 1", result.Code)
	}
	if !strings.Contains(result.Output(), "unit") {
		t.Errorf("the output does not name unit:\n%s", result.Output())
	}
	// Second challenge, finding U7: the user learns how to change a check.
	if !strings.Contains(result.Output(), "`kitsu add` only appends a check") {
		t.Errorf("the output does not say that `kitsu add` only appends a check:\n%s", result.Output())
	}
	if after := sha256File(repo, "kitsu.toml"); after != sum {
		t.Errorf("kitsu add changed kitsu.toml:\n%s", repo.Read("kitsu.toml"))
	}
	assertWroteNothing(t, repo, before)
}
