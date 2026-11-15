// Package changerecord_test holds the scenarios of the specification
// change-record
// (openspec/changes/add-role-workflow/specs/change-record/spec.md): the
// change folder, its stage, the rules of its files and `kitsu change check` as
// a check of the gate.
package changerecord_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// folder is the change folder of the shared fixture.
const folder = "openspec/changes/add-greeting/"

// The files of the shared fixture. The kitsu-roles.toml is the one of the
// role-definitions specification.
const (
	rolesFile = `version = 1

[paths]
tests = ["tests/**"]
`
	appTestFile = "greets_by_name\ngreets_without_name\n"

	proposalFile = `# Proposal: add-greeting

The app greets the person who runs it.
`

	specFile = `# greeting

## ADDED Requirements

### Requirement: Greeting text

The app SHALL greet the person by name when a name is given, and greet without a name otherwise.

Source: the owner's request.

#### Scenario: A name is given

- **WHEN** the app runs with the name Ada
- **THEN** it prints Hello, Ada

#### Scenario: No name is given

- **WHEN** the app runs without a name
- **THEN** it prints Hello
`

	designFile = `# Design: add-greeting

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| greeter | Makes the greeting. | none | yes | ` + "`src/app.txt`" + ` |
| app | Prints the greeting. | greeter | no | |

## Rules

| Rule | Checked by |
| --- | --- |
| The app prints only what the greeter makes. | review |
`

	tasksFile = `# Tasks: add-greeting

## 1. Greeting

- [x] 1.1 Build the greeter for Greeting text. Size: S. Depends on: none. Check: unit. Source: the specification.
- [x] 1.2 Print the greeting from the app. Size: S. Depends on: 1.1. Check: unit. Source: the design.
`

	testsFile = `# Tests: add-greeting

| Scenario | Test file | Text |
| --- | --- | --- |
| A name is given | tests/app_test.txt | greets_by_name |
| No name is given | tests/app_test.txt | greets_without_name |
`

	// reviewsTemplate takes the content ids spec, design, code, spec, design.
	reviewsTemplate = `# Reviews: add-greeting

## Reviews

| Stage | Round | Date | Content |
| --- | --- | --- | --- |
| spec | 1 | 2026-11-14 | %[1]s |
| design | 1 | 2026-11-14 | %[2]s |
| code | 1 | 2026-11-14 | %[3]s |

## Findings

| Id | Stage | Round | Severity | Where | Finding | Disposition | Reason |
| --- | --- | --- | --- | --- | --- | --- | --- |
%[4]s

## Approvals

| What | Date | Content |
| --- | --- | --- |
| spec | 2026-11-14 | %[1]s |
| design | 2026-11-14 | %[2]s |
`
)

// The finding of the fixture, and the parts of its row a scenario changes.
const (
	findingID  = "F1"
	findingRow = "| F1 | code | 1 | minor | src/app.txt | The greeting has no full stop. | resolved | |"
	addedID    = "F2"
)

// The lines of tasks.md and tests.md that scenarios change.
const (
	task11 = "- [x] 1.1 Build the greeter for Greeting text. Size: S. Depends on: none. Check: unit. Source: the specification."
	task12 = "- [x] 1.2 Print the greeting from the app. Size: S. Depends on: 1.1. Check: unit. Source: the design."
	rowNo  = "| No name is given | tests/app_test.txt | greets_without_name |"
)

// changeStatus is the JSON of `kitsu change status <task> --json`.
type changeStatus struct {
	Task         string            `json:"task"`
	Stage        string            `json:"stage"`
	Missing      []any             `json:"missing"`
	NotAutomated int               `json:"not_automated"`
	Content      map[string]string `json:"content"`
}

// missingText is what `missing` holds, one entry a line, whatever the type
// of an entry.
func (s changeStatus) missingText() string {
	var lines []string
	for _, entry := range s.Missing {
		if text, ok := entry.(string); ok {
			lines = append(lines, text)
			continue
		}
		data, _ := json.Marshal(entry)
		lines = append(lines, string(data))
	}
	return strings.Join(lines, "\n")
}

// record is the shared fixture of change-record.
type record struct {
	*kitsutest.Repo
	t *testing.T
}

// newRecord builds the shared fixture: demo with kitsu-roles.toml,
// tests/app_test.txt and the folder add-greeting, committed, with the
// content ids of `kitsu change status` in reviews.md.
func newRecord(t *testing.T) *record {
	t.Helper()
	r := &record{Repo: kitsutest.Demo(t), t: t}
	r.Write("kitsu-roles.toml", rolesFile)
	r.Write("tests/app_test.txt", appTestFile)
	r.Write(folder+"proposal.md", proposalFile)
	r.Write(folder+"specs/greeting/spec.md", specFile)
	r.Write(folder+"design.md", designFile)
	r.Write(folder+"tasks.md", tasksFile)
	r.Write(folder+"tests.md", testsFile)
	ids := r.status().Content
	for _, name := range []string{"spec", "design", "code"} {
		if ids[name] == "" {
			t.Fatalf("fixture: kitsu change status add-greeting --json gave no content id %q: %v", name, ids)
		}
	}
	r.Write(folder+"reviews.md", fmt.Sprintf(reviewsTemplate, ids["spec"], ids["design"], ids["code"], findingRow))
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "add-greeting")
	if result := r.Kitsu("change", "check"); result.Code != 0 {
		t.Fatalf("fixture: kitsu change check exited %d, want 0:\n%s", result.Code, result.Output())
	}
	if stage := r.status().Stage; stage != "done" {
		t.Fatalf("fixture: kitsu change status add-greeting gave the stage %q, want done:\n%s", stage, r.Kitsu("change", "status", "add-greeting").Output())
	}
	return r
}

// status runs `kitsu change status add-greeting --json`, requires exit 0 and
// returns its JSON.
func (r *record) status() changeStatus {
	r.t.Helper()
	result := r.Kitsu("change", "status", "add-greeting", "--json")
	if result.Code != 0 {
		r.t.Fatalf("kitsu change status add-greeting --json exited %d, want 0:\n%s", result.Code, result.Output())
	}
	var out changeStatus
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
		r.t.Fatalf("kitsu change status add-greeting --json printed no JSON: %v\n%s", err, result.Output())
	}
	return out
}

// statusText runs `kitsu change status add-greeting`, requires exit 0 and
// returns what it printed.
func (r *record) statusText() string {
	r.t.Helper()
	result := r.Kitsu("change", "status", "add-greeting")
	if result.Code != 0 {
		r.t.Fatalf("kitsu change status add-greeting exited %d, want 0:\n%s", result.Code, result.Output())
	}
	r.t.Logf("%s", result.Output())
	return result.Output()
}

// assertStage requires the stage in the JSON and in the printed text.
func (r *record) assertStage(want string) (changeStatus, string) {
	r.t.Helper()
	status := r.status()
	if status.Stage != want {
		r.t.Errorf("stage is %q, want %q; missing: %v", status.Stage, want, status.Missing)
	}
	text := r.statusText()
	if !namesWord(text, want) {
		r.t.Errorf("kitsu change status add-greeting does not print %q:\n%s", want, text)
	}
	return status, text
}

// check runs `kitsu change check`, requires the exit status and returns what it
// printed.
func (r *record) check(want int) string {
	r.t.Helper()
	result := r.Kitsu("change", "check")
	if result.Code != want {
		r.t.Errorf("kitsu change check exited %d, want %d:\n%s", result.Code, want, result.Output())
	}
	r.t.Logf("%s", result.Output())
	return result.Output()
}

// edit replaces the one occurrence of old in a file of the repository.
func (r *record) edit(rel, old, replacement string) {
	r.t.Helper()
	content := r.Read(rel)
	if n := strings.Count(content, old); n != 1 {
		r.t.Fatalf("%s holds %q %d times, want once", rel, old, n)
	}
	r.Write(rel, strings.Replace(content, old, replacement, 1))
}

// addFinding adds a row to the table of findings, after the fixture's one.
func (r *record) addFinding(row string) {
	r.t.Helper()
	r.edit(folder+"reviews.md", findingRow, findingRow+"\n"+row)
}

// lineOf is the 1-based number of the first line of a file that holds text.
func (r *record) lineOf(rel, text string) int {
	r.t.Helper()
	for i, line := range strings.Split(r.Read(rel), "\n") {
		if strings.Contains(line, text) {
			return i + 1
		}
	}
	r.t.Fatalf("%s holds no line with %q", rel, text)
	return 0
}

// lineWith says whether one line of text holds every one of the words.
func lineWith(text string, words ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		all := true
		for _, word := range words {
			if !strings.Contains(line, word) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// assertLineWith fails the test unless one line of text holds every word.
func assertLineWith(t *testing.T, text string, words ...string) {
	t.Helper()
	if !lineWith(text, words...) {
		t.Errorf("no line holds all of %q:\n%s", words, text)
	}
}

// assertFileLine fails the test unless one line of text names the file, the
// line number on its own and every word.
func assertFileLine(t *testing.T, text, file string, line int, words ...string) {
	t.Helper()
	number := regexp.MustCompile(`(^|[^0-9])` + fmt.Sprint(line) + `($|[^0-9])`)
	for _, l := range strings.Split(text, "\n") {
		if !strings.Contains(l, file) || !number.MatchString(l) {
			continue
		}
		if lineWith(l, words...) {
			return
		}
	}
	t.Errorf("no line names %s, the line %d and %q:\n%s", file, line, words, text)
}

// namesWord says whether text names a word on its own, such as a stage or a
// finding id.
func namesWord(text, word string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_.-])`).MatchString(text)
}

// assertNamesWord fails the test unless text names the word on its own.
func assertNamesWord(t *testing.T, text, word string) {
	t.Helper()
	if !namesWord(text, word) {
		t.Errorf("the output does not name %q:\n%s", word, text)
	}
}
