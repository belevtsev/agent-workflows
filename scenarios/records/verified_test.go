package records_test

import (
	"strings"
	"testing"
	"time"
)

// Requirement "Verified tree", scenario "Every check is satisfied".
func TestTreeWithEveryCheckSatisfiedIsVerified(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)

	if verified := statusJSON(t, repo)["verified"]; verified != true {
		t.Errorf("verified is %v, want true", verified)
	}
}

// Requirement "Verified tree", scenario "One check is not satisfied".
func TestTreeWithACheckNotSatisfiedIsNotVerified(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	repo.Write("README.md", "# demo\n\nedited\n")

	if verified := statusJSON(t, repo)["verified"]; verified != false {
		t.Errorf("verified is %v, want false", verified)
	}
}

// Requirement "Old runs", scenario "A run ages out".
func TestRunOlderThanThirtyDaysAgesOut(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	ended := time.Now().Add(-31 * 24 * time.Hour).UTC()
	for _, id := range []string{"unit", "lint"} {
		files := repo.RecordFiles(id)
		if len(files) == 0 {
			t.Fatalf("no record of %s", id)
		}
		for _, path := range files {
			repo.EditRecord(path, func(fields map[string]any) {
				fields["started_at"] = ended.Add(-time.Second).Format(time.RFC3339)
				fields["ended_at"] = ended.Format(time.RFC3339)
			})
		}
	}

	status := repo.Gatecub("status")
	assertState(t, status, "unit", "not run")
	assertState(t, status, "lint", "not run")
}

// Requirement "Old runs", scenario "A run dated in the future".
func TestRunDatedInTheFutureDoesNotSatisfy(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	passed := recordWithOutcome(t, repo, "lint", "passed")
	repo.EditRecord(passed, func(fields map[string]any) {
		fields["ended_at"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	})

	status := repo.Gatecub("status")
	assertState(t, status, "lint", "not run")
	if !strings.Contains(strings.ToLower(status.Output()), "future") {
		t.Errorf("gatecub status does not warn that the run is dated in the future:\n%s", status.Output())
	}
}
