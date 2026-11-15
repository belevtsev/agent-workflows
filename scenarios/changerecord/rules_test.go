package changerecord_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scenarios of the requirements "Specification rules", "Task rules",
// "Test table" and "Design rules".

// Requirement "Specification rules", scenario "A requirement without a
// scenario".
func TestRequirementWithoutScenario(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	spec := folder + "specs/greeting/spec.md"
	content := r.Read(spec)
	cut := strings.Index(content, "#### Scenario: A name is given")
	r.Write(spec, strings.TrimRight(content[:cut], "\n")+"\n")
	line := r.lineOf(spec, "### Requirement: Greeting text")
	out := r.check(5)
	assertFileLine(t, strings.ToLower(out), "specs/greeting/spec.md", line, "scenario")
}

// Requirement "Specification rules", scenario "A requirement that exists
// already".
func TestRequirementThatExistsAlready(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.Write("openspec/specs/greeting/spec.md", `# greeting

## Requirements

### Requirement: Greeting text

The app SHALL greet the person.

Source: the owner's request.

#### Scenario: Someone runs the app

- **WHEN** the app runs
- **THEN** it prints Hello
`)
	out := r.check(5)
	assertLineWith(t, out, "Greeting text", "MODIFIED")
}

// Requirement "Task rules", scenario "A requirement that no task builds".
func TestRequirementThatNoTaskBuilds(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"tasks.md", "the greeter for Greeting text.", "the greeter.")
	out := r.check(5)
	assertLineWith(t, out, "Greeting text")
}

// Requirement "Task rules", scenario "A task without a check".
func TestTaskWithoutCheck(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"tasks.md", task12, strings.Replace(task12, " Check: unit.", "", 1))
	line := r.lineOf(folder+"tasks.md", "1.2 ")
	out := r.check(5)
	assertFileLine(t, out, "tasks.md", line, "Check:")
}

// Requirement "Test table", scenario "A test is renamed".
func TestTestIsRenamed(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit("tests/app_test.txt", "greets_by_name", "greets_name")
	out := r.check(5)
	assertLineWith(t, out, "A name is given", "tests/app_test.txt")
}

// Requirement "Test table", scenario "A test file outside the repository".
func TestTestFileOutsideTheRepository(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	// A file beside the repository holds the text of the row.
	outside := filepath.Join(filepath.Dir(r.Dir), "outside_test.txt")
	if err := os.WriteFile(outside, []byte(appTestFile), 0o644); err != nil {
		t.Fatal(err)
	}
	r.edit(folder+"tests.md", "| A name is given | tests/app_test.txt |", "| A name is given | ../outside_test.txt |")
	out := r.check(5)
	assertLineWith(t, out, "A name is given", "../outside_test.txt")

	// The same file reached through a symbolic link inside the repository.
	link := filepath.Join(r.Dir, "tests", "link_test.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("this system makes no symbolic link: %v", err)
	}
	r.edit(folder+"tests.md", "| A name is given | ../outside_test.txt |", "| A name is given | tests/link_test.txt |")
	out = r.check(5)
	assertLineWith(t, out, "A name is given", "tests/link_test.txt")
}

// Requirement "Test table", scenario "A scenario without a test, when the
// work is called finished".
func TestScenarioWithoutTestWhenWorkIsCalledFinished(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"tests.md", rowNo+"\n", "")
	out := r.check(5)
	assertLineWith(t, out, "No name is given")
}

// Requirement "Test table", scenario "A scenario without a test, while the
// work goes on".
func TestScenarioWithoutTestWhileWorkGoesOn(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"tests.md", rowNo+"\n", "")
	r.edit(folder+"tasks.md", "- [x] 1.2", "- [ ] 1.2")
	r.check(0)
}

// Requirement "Test table", scenario "A scenario that is not automated".
func TestScenarioThatIsNotAutomated(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"tests.md", rowNo, "| No name is given | not automated | A person reads the greeting. |")
	r.Git("commit", "-q", "-a", "-m", "not automated")
	status, _ := r.assertStage("done")
	if status.NotAutomated != 1 {
		t.Errorf("not_automated is %d, want 1", status.NotAutomated)
	}
}

// Requirement "Design rules", scenario "Two blocks depend on each other".
func TestTwoBlocksDependOnEachOther(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"design.md", "| greeter | Makes the greeting. | none |", "| greeter | Makes the greeting. | app |")
	out := r.check(5)
	assertLineWith(t, out, "greeter", "app")
}

// Requirement "Design rules", scenario "A new block without a look at what
// exists".
func TestNewBlockWithoutLookAtWhatExists(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"design.md", "| yes | `src/app.txt` |", "| yes | |")
	out := r.check(5)
	assertNamesWord(t, out, "greeter")
}

// Requirement "Design rules", scenario "A rule that a check enforces".
func TestRuleThatCheckEnforces(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"design.md", "makes. | review |", "makes. | lint |")
	r.check(0)
}

// Requirement "Design rules", scenario "A rule that names a check nobody
// declared".
func TestRuleThatNamesCheckNobodyDeclared(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"design.md", "makes. | review |", "makes. | layers |")
	out := r.check(5)
	assertLineWith(t, out, "gatecub.toml", "layers")
}
