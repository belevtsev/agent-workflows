package harness_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// A task command holds the task ledger while its assertion runs. A second
// command of the same checkout waits for it instead of failing at once.
func TestATaskCommandWaitsForARunningAssertion(t *testing.T) {
	r := kitsutest.Demo(t)
	started := filepath.Join(t.TempDir(), "started")
	kitsu := filepath.Join(kitsutest.Binary(t), "kitsu")
	running := r.Command(kitsu, "task", "do", "waited", "--request", "Check the slow behavior", "--expect", "the slow check ends",
		"--", "sh", "-c", "touch '"+filepath.ToSlash(started)+"' && sleep 2")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- running.Wait() }()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the assertion did not start")
		}
	}

	next := r.Kitsu("task", "next", "waited", "--brief")
	if next.Code != 0 {
		t.Fatalf("task next beside a running assertion exited %d\n%s", next.Code, next.Output())
	}
	if err := <-finished; err != nil {
		t.Fatalf("the command with the assertion failed: %v", err)
	}
}
