package taskflow

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestListReportsTheStateOfEachTask(t *testing.T) {
	// Oracle: five tasks seeded in one repository, newest first: abandoned
	// (cw task abandon writes its revision at the wall clock, after the
	// others), open (1 hour old), verified (a passing verification, 2 hours
	// old), unreadable (a gap in its revision chain, 3 hours old, so Load
	// refuses it but its newest revision still names the repository) and
	// stale (15 days old, past MaxOpenAge). A task of another repository is
	// left out when roots name this one, and listed with nil roots.
	store, repo := foFixture(t)
	now := time.Now().UTC()
	open := foStartAt(t, store, repo, "open task", "substantial", now.Add(-time.Hour))
	verified := foVerify(t, foStartAt(t, store, repo, "verified task", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
	abandoned := foStartAt(t, store, repo, "abandoned task", "small", now.Add(-4*time.Hour))
	if _, err := Abandon(abandoned.Directory, "gave up for the test"); err != nil {
		t.Fatal(err)
	}
	unreadable := foBrokenChain(t, store, repo, "unreadable task", now.Add(-3*time.Hour))
	if err := os.Rename(filepath.Join(unreadable.Directory, "record-000002.json"), filepath.Join(unreadable.Directory, "record-000003.json")); err != nil {
		t.Fatal(err)
	}
	stale := foStartAt(t, store, repo, "stale task", "substantial", now.Add(-MaxOpenAge-24*time.Hour))
	other := foStartAt(t, store, t.TempDir(), "other repository", "substantial", now.Add(-30*time.Minute))

	type want struct {
		record Record
		state  string
	}
	wantRepo := []want{{abandoned, StateAbandoned}, {open, StateOpen}, {verified, StateVerified}, {unreadable, StateUnreadable}, {stale, StateStale}}
	check := func(t *testing.T, got []Summary, wants []want) {
		t.Helper()
		if len(got) != len(wants) {
			t.Fatalf("List returned %d tasks %+v, want %d", len(got), got, len(wants))
		}
		for i, w := range wants {
			task := got[i]
			if task.ID != w.record.ID || task.State != w.state || task.Title != w.record.Plan.Title || task.Kind != w.record.Plan.Kind ||
				task.Root != w.record.Baseline.Root || task.Directory == "" || filepath.Base(task.Directory) != w.record.ID {
				t.Fatalf("List result %d is %+v, want task %s (%q, %s) in state %s", i, task, w.record.ID, w.record.Plan.Title, w.record.Plan.Kind, w.state)
			}
			if (task.Error != "") != (w.state == StateUnreadable) {
				t.Fatalf("List result %d (%s) has error %q in state %s", i, task.Title, task.Error, task.State)
			}
		}
	}

	t.Run("one repository", func(t *testing.T) {
		got, err := List(store, []string{open.Baseline.Root}, now)
		if err != nil {
			t.Fatal(err)
		}
		check(t, got, wantRepo)
	})
	t.Run("every repository", func(t *testing.T) {
		got, err := List(store, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		check(t, got, slices.Insert(slices.Clone(wantRepo), 1, want{other, StateOpen}))
	})
	t.Run("missing store", func(t *testing.T) {
		got, err := List(filepath.Join(t.TempDir(), "absent"), nil, now)
		if err != nil || len(got) != 0 {
			t.Fatalf("List of a missing store = %+v, %v; want no tasks and no error", got, err)
		}
	})
}
