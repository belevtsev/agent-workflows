package platform_test

import (
	"testing"
)

// Requirement "Programs that a system lacks", scenario "A program that does
// not exist".
func TestProgramThatDoesNotExist(t *testing.T) {
	t.Parallel()
	repo := portable(t, "[[check]]\nid = \"ghost\"\nrun = [\"kitsu-no-such-program\"]\n")

	result := repo.Kitsu("run", "ghost")

	if got := outcome(t, repo, "ghost"); got != "failed" {
		t.Errorf("ghost ended %q, want failed", got)
	}
	if !lineWith(result.Output(), "kitsu-no-such-program", "not found") {
		t.Errorf("no line of the output names kitsu-no-such-program and says it was not found")
	}
	if result.Code != 5 {
		t.Errorf("kitsu run exit status %d, want 5", result.Code)
	}
	if t.Failed() {
		t.Logf("kitsu run ghost (exit status %d):\n%s", result.Code, result.Output())
	}
}
