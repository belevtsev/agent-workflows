package taskflow

import (
	"errors"
	"testing"
)

// TestKilledPublisherLeavesPublishLockFree: a cw publish killed with SIGKILL
// while it holds .publish-lock does not lock out later runs. Oracle: while the
// holder lives, Lock fails at once with ErrPublicationLocked; once it is
// killed, the next Lock succeeds.
func TestKilledPublisherLeavesPublishLockFree(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	holder := startLockHolder(t, "publish", record.Directory)
	lock := func() error {
		task, err := OpenPublicationTask(record.Directory)
		if err != nil {
			t.Fatal(err)
		}
		defer task.Close()
		return task.Lock()
	}
	if err := lock(); !errors.Is(err, ErrPublicationLocked) {
		t.Fatalf("Lock while another run holds the lock: %v, want ErrPublicationLocked", err)
	}
	killLockHolder(t, holder)
	if err := lock(); err != nil {
		t.Fatalf("Lock after the holder was killed: %v", err)
	}
}
