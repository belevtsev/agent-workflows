package declaration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Invalid declaration", scenario "A misspelt key is refused".
func TestMisspeltKeyIsRefusedWithItsLine(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	// The scripts leave a mark outside the repository when they execute.
	marks := t.TempDir()
	repo.Write("unit.sh", "touch '"+filepath.Join(marks, "unit")+"'\n")
	repo.Write("lint.sh", "touch '"+filepath.Join(marks, "lint")+"'\n")
	repo.Write("cw.toml", `version = 1

[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]

[[check]]
tiemout = "5m"
id = "lint"
run = ["sh", "lint.sh"]
`)
	if line := strings.Split(repo.Read("cw.toml"), "\n")[8]; line != `tiemout = "5m"` {
		t.Fatalf("line 9 of cw.toml is %q", line)
	}
	commitAll(repo, "misspelt key")
	before := fingerprint(t, repo.Dir, repo.Home)

	result := repo.CW("run")
	if result.Code != 1 {
		t.Errorf("cw run exited %d, want 1", result.Code)
	}
	for _, want := range []string{"cw.toml:9", "tiemout"} {
		if !strings.Contains(result.Output(), want) {
			t.Errorf("the output does not hold %q:\n%s", want, result.Output())
		}
	}
	entries, err := os.ReadDir(marks)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("the check %s was executed", entry.Name())
	}
	assertWroteNothing(t, repo, before)
}

// Requirement "Invalid declaration", scenario "A duplicate id is refused".
func TestDuplicateIDIsRefusedWithBothLines(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	// The checks begin on lines 11 and 21; their ids are on lines 12 and 22.
	repo.Write("cw.toml", `version = 1

# a
# b
# c
# d
# e
# f
# g

[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]

# h
# i
# j
# k

[[check]]
id = "unit"
run = ["sh", "lint.sh"]
`)
	lines := strings.Split(repo.Read("cw.toml"), "\n")
	if lines[10] != "[[check]]" || lines[11] != `id = "unit"` || lines[20] != "[[check]]" || lines[21] != `id = "unit"` {
		t.Fatalf("the layout of cw.toml is not the one the test expects:\n%s", repo.Read("cw.toml"))
	}
	commitAll(repo, "duplicate id")
	before := fingerprint(t, repo.Dir, repo.Home)

	result := repo.CW("status")
	if result.Code != 1 {
		t.Errorf("cw status exited %d, want 1", result.Code)
	}
	output := result.Output()
	if !strings.Contains(output, "unit") {
		t.Errorf("the output does not name unit:\n%s", output)
	}
	// The line of a check is taken as that of its [[check]] or of its id.
	if !holdsNumber(output, 11) && !holdsNumber(output, 12) {
		t.Errorf("the output does not name the line of the first check (11 or 12):\n%s", output)
	}
	if !holdsNumber(output, 21) && !holdsNumber(output, 22) {
		t.Errorf("the output does not name the line of the second check (21 or 22):\n%s", output)
	}
	assertWroteNothing(t, repo, before)
}

// Requirement "Invalid declaration", scenario "A command written as one
// string is refused".
func TestRunWrittenAsOneStringIsRefused(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	repo.Write("cw.toml", cwtest.DemoDeclaration+`
[[check]]
id = "gotest"
run = "go test ./..."
`)
	commitAll(repo, "run as a string")
	before := fingerprint(t, repo.Dir, repo.Home)

	result := repo.CW("run")
	if result.Code != 1 {
		t.Errorf("cw run exited %d, want 1", result.Code)
	}
	output := strings.ToLower(result.Output())
	for _, want := range []string{"run", "array", "string"} {
		if !strings.Contains(output, want) {
			t.Errorf("the output does not say that run must be an array of strings (no %q):\n%s", want, result.Output())
		}
	}
	assertWroteNothing(t, repo, before)
}
