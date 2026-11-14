package changerecord_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// Requirement "Folder", scenario "The folder is found".
func TestFolderIsFound(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	result := r.CW("change", "status")
	if result.Code != 0 {
		t.Errorf("cw change status exited %d, want 0:\n%s", result.Code, result.Output())
	}
	lines := 0
	for _, line := range strings.Split(result.Stdout, "\n") {
		if namesWord(line, "add-greeting") {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("cw change status printed %d lines for add-greeting, want 1:\n%s", lines, result.Output())
	}
}

// Requirement "Folder", scenario "A task that does not exist".
func TestTaskThatDoesNotExist(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	result := r.CW("change", "status", "no-such-task")
	if result.Code != 1 {
		t.Errorf("cw change status no-such-task exited %d, want 1:\n%s", result.Code, result.Output())
	}
	if !strings.Contains(result.Output(), "openspec/changes/no-such-task") {
		t.Errorf("the output does not name openspec/changes/no-such-task/:\n%s", result.Output())
	}
}

// Requirement "Stage from the files", scenario "The design is missing".
func TestDesignIsMissing(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.Remove(folder + "design.md")
	r.Remove(folder + "tasks.md")
	status, text := r.assertStage("design")
	if !strings.Contains(text, "design.md") {
		t.Errorf("cw change status does not name design.md as missing:\n%s", text)
	}
	if !strings.Contains(status.missingText(), "design.md") {
		t.Errorf("missing does not name design.md: %v", status.Missing)
	}
}

// Requirement "Stage from the files", scenario "A finding is still open".
func TestFindingIsStillOpen(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.addFinding("| F2 | code | 1 | minor | src/app.txt | The greeting is too long. | open | |")
	status, text := r.assertStage("review")
	assertNamesWord(t, text, addedID)
	assertNamesWord(t, status.missingText(), addedID)
}

// Requirement "Stage from the files", scenario "A task is not checked".
func TestTaskIsNotChecked(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"tasks.md", "- [x] 1.2", "- [ ] 1.2")
	status := r.status()
	if status.Stage != "code" {
		t.Errorf("stage is %q, want code; missing: %v", status.Stage, status.Missing)
	}
	assertNamesWord(t, status.missingText(), "1.2")
}

// Requirement "Stage from the files", scenario "The record is not committed".
func TestRecordIsNotCommitted(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.Write(folder+"reviews.md", r.Read(folder+"reviews.md")+"\n")
	_, text := r.assertStage("commit")
	if !strings.Contains(text, "HEAD") {
		t.Errorf("cw change status does not say that the folder differs from HEAD:\n%s", text)
	}
}

// Requirement "Stage from the files", scenario "Reading changes nothing".
func TestReadingChangesNothing(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	before := r.tree()
	if result := r.CW("change", "status"); result.Code != 0 {
		t.Errorf("cw change status exited %d, want 0:\n%s", result.Code, result.Output())
	}
	if after := r.tree(); after != before {
		t.Errorf("cw status --json prints the tree %q after cw change status, %q before", after, before)
	}
}

// tree is the field tree of `cw status --json`.
func (r *record) tree() string {
	r.t.Helper()
	result := r.CW("status", "--json")
	var out struct {
		Tree string `json:"tree"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil || out.Tree == "" {
		r.t.Fatalf("cw status --json (exit %d) printed no tree: %v\n%s", result.Code, err, result.Output())
	}
	return out.Tree
}
