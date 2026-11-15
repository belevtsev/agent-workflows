package changerecord_test

import (
	"strings"
	"testing"
)

// The scenarios of the requirements "Review records" and "Reviewed content".

// Requirement "Review records", scenario "A major finding is put off".
func TestMajorFindingIsPutOff(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.addFinding("| F2 | code | 1 | major | src/app.txt | The greeting is wrong. | deferred | Later. |")
	out := r.check(5)
	assertNamesWord(t, out, addedID)
}

// Requirement "Review records", scenario "A finding is rejected without a
// reason".
func TestFindingIsRejectedWithoutReason(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"reviews.md", "| resolved | |", "| rejected | |")
	out := r.check(5)
	assertNamesWord(t, out, findingID)
}

// Requirement "Review records", scenario "Finished work with an open
// finding".
func TestFinishedWorkWithOpenFinding(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.addFinding("| F2 | code | 1 | minor | src/app.txt | The greeting is too long. | open | |")
	out := r.check(5)
	assertNamesWord(t, out, addedID)
}

// Requirement "Review records", scenario "A stage that does not exist".
func TestStageThatDoesNotExist(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"reviews.md", "| F1 | code |", "| F1 | final |")
	line := r.lineOf(folder+"reviews.md", "| F1 |")
	out := r.check(5)
	assertFileLine(t, out, "reviews.md", line, "final")
}

// Requirement "Reviewed content", scenario "The specification changes after
// its approval".
func TestSpecificationChangesAfterItsApproval(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"specs/greeting/spec.md", "The app SHALL greet", "The app MUST greet")
	_, text := r.assertStage("spec")
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "review") || !strings.Contains(lower, "approv") {
		t.Errorf("kitsu change status does not say that the review and the approval of the specification were given for other content:\n%s", text)
	}
}

// Requirement "Reviewed content", scenario "The code changes after its
// review".
func TestCodeChangesAfterItsReview(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.Write(folder+"reviews.md", r.Read(folder+"reviews.md")+"\n")
	r.Write("src/app.txt", "ok\nmore\n")
	_, text := r.assertStage("review")
	if !lineWith(strings.ToLower(text), "code", "review") {
		t.Errorf("kitsu change status does not say that the review of the code was given for other content:\n%s", text)
	}
}

// Requirement "Reviewed content", scenario "A checked task does not undo the
// approval of the design".
func TestCheckedTaskDoesNotUndoApprovalOfDesign(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.edit(folder+"tasks.md", "- [x] 1.2", "- [ ] 1.2")
	status, text := r.assertStage("code")
	for _, out := range []string{strings.ToLower(text), strings.ToLower(status.missingText())} {
		if lineWith(out, "design", "review") || lineWith(out, "design", "approv") {
			t.Errorf("kitsu change status names the review or the approval of the design:\n%s", out)
		}
	}
}

// Requirement "Reviewed content", scenario "Later work leaves a finished
// task done".
func TestLaterWorkLeavesFinishedTaskDone(t *testing.T) {
	t.Parallel()
	r := newRecord(t)
	r.Write("README.md", "# demo\n\nmore\n")
	r.Git("commit", "-q", "-a", "-m", "readme")
	r.Write("src/app.txt", "ok\nmore\n")
	r.assertStage("done")
}
