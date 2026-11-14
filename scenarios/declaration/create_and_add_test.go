package declaration_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Missing declaration", scenario "No file".
func TestMissingDeclarationPointsToInit(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	repo.Remove("cw.toml")

	result := repo.CW("status")
	if result.Code != 1 {
		t.Errorf("cw status exited %d, want 1", result.Code)
	}
	for _, want := range []string{"cw.toml", "cw init"} {
		if !strings.Contains(result.Output(), want) {
			t.Errorf("the output does not hold %q:\n%s", want, result.Output())
		}
	}
}

// Requirement "Create a declaration", scenario "A new repository".
func TestInitCreatesADeclarationWithNoChecks(t *testing.T) {
	t.Parallel()
	repo := cwtest.NewRepo(t)
	repo.Write("README.md", "# new\n")
	commitAll(repo, "first")

	if result := repo.CW("init"); !repo.Exists("cw.toml") {
		t.Fatalf("cw init (exit %d) created no cw.toml:\n%s", result.Code, result.Output())
	}
	result := repo.CW("status")
	if result.Code != 0 {
		t.Errorf("cw status exited %d, want 0", result.Code)
	}
	if !strings.Contains(strings.ToLower(result.Output()), "no check") {
		t.Errorf("cw status does not say that no checks are declared:\n%s", result.Output())
	}
}

// Requirement "Create a declaration", scenario "An existing declaration is
// kept".
func TestInitKeepsAnExistingDeclaration(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	before := sha256File(repo, "cw.toml")

	result := repo.CW("init")
	if after := sha256File(repo, "cw.toml"); after != before {
		t.Errorf("cw init (exit %d) changed cw.toml:\n%s", result.Code, repo.Read("cw.toml"))
	}
}

// Requirement "Add a check from the command line", scenario "A check is
// appended".
func TestAddAppendsACheck(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	before := repo.Read("cw.toml")

	result := repo.CW("add", "build", "--timeout", "5m", "--", "make", "-j4", "build")
	after := repo.Read("cw.toml")
	if !strings.HasPrefix(after, before) {
		t.Fatalf("cw add (exit %d) did not keep the previous content of cw.toml:\n%s\n%s", result.Code, after, result.Output())
	}
	var declaration struct {
		Check []struct {
			ID      string   `toml:"id"`
			Run     []string `toml:"run"`
			Timeout string   `toml:"timeout"`
		} `toml:"check"`
	}
	if err := toml.Unmarshal([]byte(after), &declaration); err != nil {
		t.Fatalf("cw.toml after cw add: %v\n%s", err, after)
	}
	if len(declaration.Check) == 0 {
		t.Fatalf("cw.toml declares no checks:\n%s", after)
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
	repo := cwtest.Demo(t)
	sum := sha256File(repo, "cw.toml")
	before := fingerprint(t, repo.Dir, repo.Home)

	result := repo.CW("add", "unit", "--", "true")
	if result.Code != 1 {
		t.Errorf("cw add exited %d, want 1", result.Code)
	}
	if !strings.Contains(result.Output(), "unit") {
		t.Errorf("the output does not name unit:\n%s", result.Output())
	}
	if after := sha256File(repo, "cw.toml"); after != sum {
		t.Errorf("cw add changed cw.toml:\n%s", repo.Read("cw.toml"))
	}
	assertWroteNothing(t, repo, before)
}
