package evidence_test

import (
	"os"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "Log limit", scenario "A very long log".
func TestVeryLongLogIsCutAndStillSatisfies(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	// 50 MiB of output, and exit status 0.
	declare(t, repo, "[[check]]\nid = \"loud\"\nrun = [\"sh\", \"-c\", \"yes 0123456789abcdef | head -c 52428800\"]\n")
	const limit = 10 << 20
	const markerAllowance = 4 << 10 // the marker's size is not specified; a few lines

	first := repo.Gatecub("run")

	record := lastRecord(t, repo, "loud")
	if record["outcome"] != "passed" {
		t.Errorf("the first run ended %v, want passed", record["outcome"])
	}
	log, _ := record["log"].(string)
	if info, err := os.Stat(logPath(repo, log)); err != nil {
		t.Errorf("the log cannot be found: %v", err)
	} else if info.Size() > limit+markerAllowance {
		t.Errorf("the log holds %d bytes, want at most 10 MiB plus the marker", info.Size())
	}
	before := map[string]int{}
	for _, id := range []string{"unit", "lint", "loud"} {
		before[id] = len(repo.RecordFiles(id))
	}

	second := repo.Gatecub("run")

	for _, id := range []string{"unit", "lint", "loud"} {
		if after := len(repo.RecordFiles(id)); after != before[id] {
			t.Errorf("the second gatecub run executed %s (%d records, %d before)", id, after, before[id])
		}
	}
	if t.Failed() {
		t.Logf("first gatecub run exit status %d; second gatecub run (exit status %d):\n%s", first.Code, second.Code, second.Output())
	}
}
