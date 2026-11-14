package taskflow

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Tests for Abandon (contract gate-tests-v1, requirements R-ABANDON A1-A6).
// Records are created with the approved scaffold (startIn, writeNextRevision).

// abStart starts an open substantial record in a fresh store and repository.
func abStart(t *testing.T, title string) Record {
	t.Helper()
	return startIn(t, filepath.Join(t.TempDir(), "tasks"), t.TempDir(), title, "substantial")
}

// abLoad loads the record at dir and requires success.
func abLoad(t *testing.T, dir string) Record {
	t.Helper()
	record, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	return record
}

// abExpectLatest requires that the latest revision at dir is want's revision
// with want's abandonment state.
func abExpectLatest(t *testing.T, dir string, want Record) {
	t.Helper()
	loaded := abLoad(t, dir)
	if loaded.Revision != want.Revision {
		t.Fatalf("record revision %d, want %d (no revision written)", loaded.Revision, want.Revision)
	}
	wantAbandonment := want.Abandonment
	switch {
	case wantAbandonment == nil && loaded.Abandonment != nil:
		t.Fatalf("record has abandonment %+v, want none", *loaded.Abandonment)
	case wantAbandonment != nil && loaded.Abandonment == nil:
		t.Fatalf("record has no abandonment, want %+v", *wantAbandonment)
	case wantAbandonment != nil && (loaded.Abandonment.Reason != wantAbandonment.Reason || !loaded.Abandonment.At.Equal(wantAbandonment.At)):
		t.Fatalf("record abandonment %+v, want %+v", *loaded.Abandonment, *wantAbandonment)
	}
}

// abAbandon abandons record with reason and requires success.
func abAbandon(t *testing.T, record Record, reason string) Record {
	t.Helper()
	abandoned, err := Abandon(record.Directory, reason)
	if err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	return abandoned
}

func TestAbandon(t *testing.T) {
	t.Run("appends one terminal revision", func(t *testing.T) {
		// A1: the returned record is one revision newer, carries the reason
		// and the time of Env's clock, in UTC, and Load returns the same
		// abandonment. The revision's updated_at is that time too.
		record := abStart(t, "to abandon")
		now := record.CreatedAt.Add(time.Hour).In(time.FixedZone("UTC+3", 3*60*60))
		abandoned, err := Env{Clock: fixedClock(now)}.Abandon(record.Directory, "superseded by another task")
		if err != nil {
			t.Fatalf("Abandon: %v", err)
		}
		if abandoned.Revision != record.Revision+1 {
			t.Fatalf("abandoned revision %d, want %d", abandoned.Revision, record.Revision+1)
		}
		if abandoned.ID != record.ID || abandoned.Directory != record.Directory {
			t.Fatalf("abandoned record %s at %s, want %s at %s", abandoned.ID, abandoned.Directory, record.ID, record.Directory)
		}
		if abandoned.Abandonment == nil {
			t.Fatal("abandoned record has no abandonment")
		}
		if abandoned.Abandonment.Reason != "superseded by another task" {
			t.Fatalf("abandonment reason %q", abandoned.Abandonment.Reason)
		}
		for name, at := range map[string]time.Time{"abandonment time": abandoned.Abandonment.At, "updated_at": abandoned.UpdatedAt} {
			if !at.Equal(now) || at.Location() != time.UTC {
				t.Fatalf("%s %v, want %v", name, at, now.UTC())
			}
		}
		loaded := abLoad(t, record.Directory)
		if loaded.Revision != abandoned.Revision {
			t.Fatalf("loaded revision %d, want %d", loaded.Revision, abandoned.Revision)
		}
		abExpectLatest(t, record.Directory, abandoned)
	})

	t.Run("blank reason is rejected", func(t *testing.T) {
		// A2: a blank or whitespace-only reason errors and writes nothing.
		for _, reason := range []string{"", " ", "   ", "\t\n", "\n"} {
			t.Run(fmt.Sprintf("reason %q", reason), func(t *testing.T) {
				record := abStart(t, "blank reason")
				if _, err := Abandon(record.Directory, reason); err == nil {
					t.Fatalf("Abandon accepted reason %q", reason)
				}
				abExpectLatest(t, record.Directory, record)
			})
		}
	})

	t.Run("second abandon is rejected", func(t *testing.T) {
		// A3: abandoning an abandoned record errors and writes nothing.
		record := abStart(t, "twice")
		abandoned := abAbandon(t, record, "first reason")
		if _, err := Abandon(record.Directory, "second reason"); err == nil {
			t.Fatal("Abandon accepted an already abandoned record")
		}
		abExpectLatest(t, record.Directory, abandoned)
	})

	t.Run("verify is refused after abandonment", func(t *testing.T) {
		// A4: Verify on an abandoned record errors and the abandoned revision
		// stays the latest.
		record := abStart(t, "verify after abandon")
		abandoned := abAbandon(t, record, "given up")
		if _, err := Verify(t.Context(), record.Directory); err == nil {
			t.Fatal("Verify accepted an abandoned record")
		}
		abExpectLatest(t, record.Directory, abandoned)
	})

	t.Run("hand-written revision after abandonment does not load", func(t *testing.T) {
		// A5: a correctly chained revision appended after the abandoned
		// revision makes Load fail, whether it keeps or clears the abandonment.
		tests := []struct {
			name   string
			mutate func(*Record)
		}{
			{"keeps the abandonment", func(*Record) {}},
			{"clears the abandonment", func(next *Record) { next.Abandonment = nil }},
			{"adds a verification", func(next *Record) {
				next.Verification = &Verification{TaskID: next.ID, Complete: true, CurrentSnapshot: next.Baseline, SourceSHA256: foDigestA, Problems: []string{}, Limitations: []string{}, CheckedAt: next.UpdatedAt}
			}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				record := abStart(t, "hand-written")
				abAbandon(t, record, "given up")
				// Control: the abandoned record itself loads.
				abandoned := abLoad(t, record.Directory)
				if abandoned.Abandonment == nil {
					t.Fatal("loaded record has no abandonment")
				}
				writeNextRevision(t, abandoned, test.mutate)
				if loaded, err := Load(record.Directory); err == nil {
					t.Fatalf("Load accepted revision %d appended after the abandoned revision %d", loaded.Revision, abandoned.Revision)
				}
			})
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		// A6: Abandon on a directory that does not exist errors.
		if _, err := Abandon(filepath.Join(t.TempDir(), "missing"), "reason"); err == nil {
			t.Fatal("Abandon accepted a nonexistent directory")
		}
	})
}
