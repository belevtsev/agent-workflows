package changerecord_test

import (
	"os"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// The scenarios of the requirement "Usable as a check".

// Requirement "Usable as a check", scenario "A folder at the first stage".
func TestFolderAtFirstStage(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	for _, name := range []string{"design.md", "tasks.md", "tests.md", "reviews.md"} {
		r.Remove(folder + name)
	}
	r.check(0)
}

// Requirement "Usable as a check", scenario "The gate enforces the record".
func TestGateEnforcesRecord(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.Setenv("PATH", gatecubtest.Binary(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r.Write("gatecub.toml", gatecubtest.DemoDeclaration+`
[[check]]
id = "record"
run = ["gatecub", "change", "check"]
`)
	r.addFinding("| F2 | code | 1 | minor | src/app.txt | The greeting is too long. | open | |")
	if result := r.Gatecub("init"); result.Code != 0 {
		t.Fatalf("gatecub init exited %d:\n%s", result.Code, result.Output())
	}
	// The scenario asks only that gatecub run has ended, not how.
	run := r.Gatecub("run")
	t.Logf("gatecub run exited %d", run.Code)
	before := r.Git("rev-parse", "HEAD")
	result := r.TryGit("commit", "-a", "-m", "change")
	if result.Code == 0 {
		t.Errorf("git commit -a -m change exited 0, want a denial:\n%s", result.Output())
	}
	if after := r.Git("rev-parse", "HEAD"); after != before {
		t.Errorf("git commit moved HEAD from %s to %s", before, after)
	}
	assertNamesWord(t, result.Output(), "record")
}
