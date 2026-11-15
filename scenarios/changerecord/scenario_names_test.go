package changerecord_test

import (
	"fmt"
	"strings"
	"testing"
)

const anotherGreeting = `
### Requirement: Another greeting

The app SHALL greet another person.

Source: the owner's request.

#### Scenario: A name is given

- **WHEN** the app runs with the name Grace
- **THEN** it prints Hello, Grace
`

// One tests.md row must not satisfy different scenarios with the same name.
func TestDuplicateScenarioNames(t *testing.T) {
	t.Parallel()
	const firstFile = folder + "specs/greeting/spec.md"
	for _, tt := range []struct {
		name          string
		duplicateFile string
		prepare       func(*record)
	}{
		{
			name:          "within one requirement",
			duplicateFile: firstFile,
			prepare: func(r *record) {
				r.edit(firstFile, "#### Scenario: No name is given", "#### Scenario: A name is given")
				r.edit(folder+"tests.md", rowNo+"\n", "")
			},
		},
		{
			name:          "across requirements",
			duplicateFile: firstFile,
			prepare: func(r *record) {
				r.Write(firstFile, specFile+anotherGreeting)
				r.edit(folder+"tasks.md", "for Greeting text.", "for Greeting text and Another greeting.")
			},
		},
		{
			name:          "across capabilities",
			duplicateFile: folder + "specs/salutation/spec.md",
			prepare: func(r *record) {
				r.Write("openspec/specs/salutation/spec.md", "# salutation\n\n## Requirements\n"+anotherGreeting)
				r.Write(folder+"specs/salutation/spec.md", "# salutation\n\n## MODIFIED Requirements\n"+anotherGreeting)
				r.edit(folder+"tasks.md", "for Greeting text.", "for Greeting text and Another greeting.")
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRecord(t)
			tt.prepare(r)
			r.approveAndCommit()

			const heading = "#### Scenario: A name is given"
			content := r.Read(tt.duplicateFile)
			duplicate := strings.LastIndex(content, heading)
			if duplicate < 0 {
				t.Fatal("fixture has no duplicate scenario")
			}
			line := strings.Count(content[:duplicate], "\n") + 1
			first := fmt.Sprintf("%s:%d", firstFile, r.lineOf(firstFile, heading))
			out := r.check(5)
			assertFileLine(t, out, tt.duplicateFile, line, "A name is given", first, "rename", "tests.md")
			status, _ := r.assertStage("spec")
			assertLineWith(t, status.missingText(), "A name is given", first, "rename", "tests.md")

			// A distinct name and its own row retain the existing simple-name format.
			r.Write(tt.duplicateFile, content[:duplicate]+strings.Replace(content[duplicate:], heading, "#### Scenario: Another name is given", 1))
			r.Write(folder+"tests.md", r.Read(folder+"tests.md")+"| Another name is given | tests/app_test.txt | greets_by_name |\n")
			r.approveAndCommit()
			r.check(0)
			r.assertStage("done")
		})
	}
}

func TestRemovedScenarioNameNeedsNoTestMapping(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.Write(folder+"specs/salutation/spec.md", "# salutation\n\n## REMOVED Requirements\n"+anotherGreeting)
	r.edit(folder+"tasks.md", "for Greeting text.", "for Greeting text and Another greeting.")
	r.approveAndCommit()
	r.check(0)
	r.assertStage("done")
}

// approveAndCommit makes every other stage complete, so only the rule under
// test can prevent the fixture from being reported as done.
func (r *record) approveAndCommit() {
	r.t.Helper()
	ids := r.status().Content
	r.Write(folder+"reviews.md", fmt.Sprintf(reviewsTemplate, ids["spec"], ids["design"], ids["code"], findingRow))
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "update change record")
}
