package taskflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// Tests for FindOpen (contract gate-tests-v1, requirements R-FIND F1-F9) and
// SourceDigest (requirement R-DIGEST). Records are created with the approved
// scaffold (startIn, appendRevision, rewriteFirstRevision) and abandoned with
// Abandon; identify is a counting stub so each case can also assert which
// paths the content capture ran for.

// foStub is a counting SourceIdentifier that returns a fixed answer and
// records every path it was asked about.
type foStub struct {
	digest string
	err    error
	calls  []string
}

func (s *foStub) identify() SourceIdentifier {
	return func(_ context.Context, path string) (string, error) {
		s.calls = append(s.calls, path)
		return s.digest, s.err
	}
}

// Two distinct source digests in the format Verify stores.
var (
	foDigestA = strings.Repeat("a", 64)
	foDigestB = strings.Repeat("b", 64)
)

// foFixture returns a store path that does not exist yet and a repository
// directory for startIn.
func foFixture(t *testing.T) (store, repo string) {
	t.Helper()
	return filepath.Join(t.TempDir(), "tasks"), t.TempDir()
}

// foCanonicalDir returns a fresh directory in canonical form, usable as a
// root that no record in the test belongs to.
func foCanonicalDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// foAt rewrites the single revision of record so it was created at createdAt
// and last updated at updatedAt (both set explicitly).
func foAt(t *testing.T, record Record, createdAt, updatedAt time.Time) Record {
	t.Helper()
	return rewriteFirstRevision(t, record, func(next *Record) {
		next.CreatedAt = createdAt
		next.UpdatedAt = updatedAt
	})
}

// foStartAt starts a record and rewrites its creation and update time to at,
// so the order between records never depends on the wall clock.
func foStartAt(t *testing.T, store, repo, title, kind string, at time.Time) Record {
	t.Helper()
	return foAt(t, startIn(t, store, repo, title, kind), at, at)
}

// foVerify appends a revision carrying a verification of record with the
// given completeness and stored source digest. updated_at is unchanged.
func foVerify(t *testing.T, record Record, complete bool, sourceSHA256 string) Record {
	t.Helper()
	return appendRevision(t, record, func(next *Record) {
		problems := []string{}
		if !complete {
			problems = []string{"incomplete for the test"}
		}
		next.Verification = &Verification{TaskID: next.ID, Complete: complete, CurrentSnapshot: next.Baseline, SourceSHA256: sourceSHA256, Problems: problems, Limitations: []string{}, CheckedAt: next.UpdatedAt}
	})
}

// foCorrupt replaces the first revision of record with bytes that are not JSON.
func foCorrupt(t *testing.T, record Record) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(record.Directory, "record-000001.json"), []byte("this is not a record {"), 0600); err != nil {
		t.Fatal(err)
	}
}

// foBrokenChain starts an open substantial record updated at at and appends a
// second revision without a verification, so the record would gate if its
// chain were (wrongly) accepted after the caller breaks it.
func foBrokenChain(t *testing.T, store, repo, title string, at time.Time) Record {
	t.Helper()
	record := appendRevision(t, foStartAt(t, store, repo, title, "substantial", at), func(*Record) {})
	if record.Revision != 2 || record.Verification != nil {
		t.Fatalf("broken-chain fixture has revision %d, verification %v; want revision 2 and none", record.Revision, record.Verification)
	}
	return record
}

// foIn is the Repository of a command run in worktree whose baseline roots
// are roots.
func foIn(worktree string, roots ...string) Repository {
	return Repository{Worktree: worktree, Roots: roots}
}

// foAtRoot is the Repository of a command run in root itself.
func foAtRoot(root string) Repository {
	return foIn(root, root)
}

// foFind runs FindOpen and requires no error.
func foFind(t *testing.T, store string, repo Repository, now time.Time, identify SourceIdentifier) []OpenRecord {
	t.Helper()
	got, err := FindOpen(t.Context(), store, repo, now, identify)
	if err != nil {
		t.Fatalf("FindOpen: %v", err)
	}
	return got
}

// foWant is one expected FindOpen result: the record, whether it gates and
// whether it could not be read.
type foWant struct {
	record     Record
	gates      bool
	unreadable bool
}

func foGating(record Record) foWant     { return foWant{record: record, gates: true} }
func foCurrent(record Record) foWant    { return foWant{record: record, gates: false} }
func foUnreadable(record Record) foWant { return foWant{record: record, unreadable: true} }

// foDescribe renders results for failure messages.
func foDescribe(got []OpenRecord) string {
	parts := make([]string, 0, len(got))
	for _, open := range got {
		parts = append(parts, fmt.Sprintf("%s (%q, updated %s, problem %q)", open.Record.ID, open.Record.Plan.Title, open.Record.UpdatedAt.Format(time.RFC3339Nano), open.Problem))
	}
	return "[" + strings.Join(parts, "; ") + "]"
}

// foExpect requires exactly the wanted results in order, each with the wanted
// gating state; Gates() must agree with a non-empty Problem.
func foExpect(t *testing.T, got []OpenRecord, want ...foWant) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("FindOpen returned %d records %s, want %d", len(got), foDescribe(got), len(want))
	}
	for i, w := range want {
		open := got[i]
		if open.Record.ID != w.record.ID || open.Record.Directory != w.record.Directory || open.Record.Plan.Title != w.record.Plan.Title {
			t.Fatalf("FindOpen result %d is %s, want %s (%q); all results %s", i, open.Record.ID, w.record.ID, w.record.Plan.Title, foDescribe(got))
		}
		if open.Gates() != w.gates {
			t.Fatalf("FindOpen result %d (%q) Gates() = %v, want %v; problem %q", i, open.Record.Plan.Title, open.Gates(), w.gates, open.Problem)
		}
		if (open.Problem != "") != w.gates {
			t.Fatalf("FindOpen result %d (%q) has problem %q, inconsistent with gates %v", i, open.Record.Plan.Title, open.Problem, w.gates)
		}
		if open.NeedsConfirmation() != w.unreadable || (open.Unreadable != "") != w.unreadable {
			t.Fatalf("FindOpen result %d (%q) NeedsConfirmation() = %v with Unreadable %q, want %v", i, open.Record.Plan.Title, open.NeedsConfirmation(), open.Unreadable, w.unreadable)
		}
	}
}

// foExpectCalls requires that identify was asked about exactly want.
func foExpectCalls(t *testing.T, stub *foStub, want ...string) {
	t.Helper()
	if !slices.Equal(stub.calls, want) {
		t.Fatalf("identify calls %q, want %q", stub.calls, want)
	}
}

// foExpectCallsAtMostOnce requires that identify was asked about path at most
// once and about no other path.
func foExpectCallsAtMostOnce(t *testing.T, stub *foStub, path string) {
	t.Helper()
	if len(stub.calls) > 1 {
		t.Fatalf("identify calls %q, want at most one call for %q", stub.calls, path)
	}
	for _, call := range stub.calls {
		if call != path {
			t.Fatalf("identify calls %q, want only %q", stub.calls, path)
		}
	}
}

func TestFindOpen(t *testing.T) {
	const day = 24 * time.Hour

	t.Run("no store", func(t *testing.T) {
		// F7: a store that does not exist returns no record and no error.
		stub := &foStub{digest: foDigestA}
		root := foCanonicalDir(t)
		got, err := FindOpen(t.Context(), filepath.Join(t.TempDir(), "missing"), foAtRoot(root), time.Now(), stub.identify())
		if err != nil || len(got) != 0 {
			t.Fatalf("FindOpen on a missing store = %s, %v; want nothing and nil", foDescribe(got), err)
		}
		foExpectCalls(t, stub)
	})

	t.Run("empty store", func(t *testing.T) {
		// F7: an existing empty store returns no record.
		store := filepath.Join(t.TempDir(), "tasks")
		if err := os.Mkdir(store, 0700); err != nil {
			t.Fatal(err)
		}
		stub := &foStub{digest: foDigestA}
		foExpect(t, foFind(t, store, foAtRoot(foCanonicalDir(t)), time.Now(), stub.identify()))
		foExpectCalls(t, stub)
	})

	t.Run("wrong root", func(t *testing.T) {
		// F7: only records whose Baseline.Root is in Roots are returned.
		store, repo := foFixture(t)
		now := time.Now()
		record := foStartAt(t, store, repo, "wrong root", "substantial", now.Add(-time.Hour))
		foExpect(t, foFind(t, store, foAtRoot(foCanonicalDir(t)), now, (&foStub{}).identify()))
		// Positive control: the same record is found under its own root.
		foExpect(t, foFind(t, store, foAtRoot(record.Baseline.Root), now, (&foStub{}).identify()), foGating(record))
	})

	t.Run("multiple roots", func(t *testing.T) {
		// F7: a record whose root is the second element of Roots is returned.
		store, repo := foFixture(t)
		now := time.Now()
		record := foStartAt(t, store, repo, "second root", "substantial", now.Add(-time.Hour))
		worktree := foCanonicalDir(t)
		foExpect(t, foFind(t, store, foIn(worktree, worktree, record.Baseline.Root), now, (&foStub{}).identify()), foGating(record))
	})

	t.Run("non-substantial beside a substantial", func(t *testing.T) {
		// F7: a newer small record is never returned; the substantial one is.
		store, repo := foFixture(t)
		now := time.Now()
		substantial := foStartAt(t, store, repo, "substantial", "substantial", now.Add(-2*time.Hour))
		small := foStartAt(t, store, repo, "small", "small", now.Add(-time.Hour))
		if small.Plan.Kind != "small" {
			t.Fatalf("scaffold plan kind %q", small.Plan.Kind)
		}
		foExpect(t, foFind(t, store, foAtRoot(substantial.Baseline.Root), now, (&foStub{}).identify()), foGating(substantial))
	})

	t.Run("every candidate is returned newest updated_at first", func(t *testing.T) {
		t.Run("older open beside newer verified and current", func(t *testing.T) {
			// F1, F2, F3: there is no newest-decides rule; both records are
			// returned, the newer verified one first and not gating, the older
			// open one gating.
			store, repo := foFixture(t)
			now := time.Now()
			older := foStartAt(t, store, repo, "older open", "substantial", now.Add(-2*time.Hour))
			newer := foVerify(t, foStartAt(t, store, repo, "newer verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
			stub := &foStub{digest: foDigestA}
			foExpect(t, foFind(t, store, foAtRoot(older.Baseline.Root), now, stub.identify()), foCurrent(newer), foGating(older))
			foExpectCalls(t, stub, newer.Baseline.SelectedPath)
		})

		t.Run("newer open beside older verified and current", func(t *testing.T) {
			// F1: the older verified record is returned too, after the newer.
			store, repo := foFixture(t)
			now := time.Now()
			older := foVerify(t, foStartAt(t, store, repo, "older verified", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
			newer := foStartAt(t, store, repo, "newer open", "substantial", now.Add(-time.Hour))
			foExpect(t, foFind(t, store, foAtRoot(newer.Baseline.Root), now, (&foStub{digest: foDigestA}).identify()), foGating(newer), foCurrent(older))
		})

		t.Run("three open records in updated_at order", func(t *testing.T) {
			// F1: the order is by updated_at descending, whatever order the
			// records were started in.
			store, repo := foFixture(t)
			now := time.Now()
			newest := foStartAt(t, store, repo, "started first, newest", "substantial", now.Add(-time.Hour))
			oldest := foStartAt(t, store, repo, "started second, oldest", "substantial", now.Add(-3*time.Hour))
			middle := foStartAt(t, store, repo, "started third, middle", "substantial", now.Add(-2*time.Hour))
			foExpect(t, foFind(t, store, foAtRoot(newest.Baseline.Root), now, (&foStub{}).identify()), foGating(newest), foGating(middle), foGating(oldest))
		})

		t.Run("updated_at orders, not created_at", func(t *testing.T) {
			// F1, F5: a record created earlier but updated later comes first.
			store, repo := foFixture(t)
			now := time.Now()
			createdEarly := foAt(t, startIn(t, store, repo, "created early, updated late", "substantial"), now.Add(-5*time.Hour), now.Add(-time.Hour))
			createdLate := foStartAt(t, store, repo, "created late, updated early", "substantial", now.Add(-3*time.Hour))
			foExpect(t, foFind(t, store, foAtRoot(createdEarly.Baseline.Root), now, (&foStub{}).identify()), foGating(createdEarly), foGating(createdLate))
		})
	})

	t.Run("verified and current does not gate", func(t *testing.T) {
		// F2, F6: a complete verification whose SourceSHA256 equals what
		// identify reports is returned with an empty Problem; with Worktree
		// equal to the record's root, identify is asked once, for the record's
		// Baseline.SelectedPath.
		store, repo := foFixture(t)
		now := time.Now()
		verified := foVerify(t, foStartAt(t, store, repo, "verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
		stub := &foStub{digest: foDigestA}
		foExpect(t, foFind(t, store, foAtRoot(verified.Baseline.Root), now, stub.identify()), foCurrent(verified))
		foExpectCalls(t, stub, verified.Baseline.SelectedPath)
	})

	t.Run("records without a current passing verification gate", func(t *testing.T) {
		// F3: no verification, an incomplete verification, a verification
		// without SourceSHA256 (even when identify also reports ""), and a
		// SourceSHA256 that differs from the current digest all gate.
		tests := []struct {
			name     string
			verify   func(t *testing.T, record Record) Record
			identify string
		}{
			{"no verification", func(_ *testing.T, record Record) Record { return record }, foDigestA},
			{"incomplete verification", func(t *testing.T, record Record) Record { return foVerify(t, record, false, foDigestA) }, foDigestA},
			{"complete without SourceSHA256", func(t *testing.T, record Record) Record { return foVerify(t, record, true, "") }, ""},
			{"complete but drifted", func(t *testing.T, record Record) Record { return foVerify(t, record, true, foDigestA) }, foDigestB},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				store, repo := foFixture(t)
				now := time.Now()
				record := test.verify(t, foStartAt(t, store, repo, test.name, "substantial", now.Add(-time.Hour)))
				stub := &foStub{digest: test.identify}
				got := foFind(t, store, foAtRoot(record.Baseline.Root), now, stub.identify())
				foExpect(t, got, foGating(record))
				foExpectCallsAtMostOnce(t, stub, record.Baseline.SelectedPath)
			})
		}
	})

	t.Run("abandoned records are never returned", func(t *testing.T) {
		// F4: a record abandoned with Abandon is not returned, whether it was
		// open or verified; an open record beside it still is.
		store, repo := foFixture(t)
		now := time.Now()
		open := foStartAt(t, store, repo, "still open", "substantial", now.Add(-3*time.Hour))
		abandonedOpen := foStartAt(t, store, repo, "abandoned open", "substantial", now.Add(-2*time.Hour))
		abandonedVerified := foVerify(t, foStartAt(t, store, repo, "abandoned verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
		for _, record := range []Record{abandonedOpen, abandonedVerified} {
			if _, err := Abandon(record.Directory, "gave up for the test"); err != nil {
				t.Fatalf("Abandon %q: %v", record.Plan.Title, err)
			}
		}
		foExpect(t, foFind(t, store, foAtRoot(open.Baseline.Root), time.Now(), (&foStub{digest: foDigestA}).identify()), foGating(open))
	})

	t.Run("ageing by the latest revision's updated_at", func(t *testing.T) {
		// F5 (F8b): created_at no longer decides age; the latest revision's
		// updated_at does.
		tests := []struct {
			name   string
			record func(t *testing.T, store, repo string, now time.Time) Record
			open   bool
		}{
			{"created 20 days ago, updated an hour ago", func(t *testing.T, store, repo string, now time.Time) Record {
				return foAt(t, startIn(t, store, repo, "old but active", "substantial"), now.Add(-20*day), now.Add(-time.Hour))
			}, true},
			{"created 20 days ago, latest revision updated an hour ago", func(t *testing.T, store, repo string, now time.Time) Record {
				record := foStartAt(t, store, repo, "old with a recent revision", "substantial", now.Add(-20*day))
				return appendRevision(t, record, func(next *Record) { next.UpdatedAt = now.Add(-time.Hour) })
			}, true},
			{"created and updated 15 days ago", func(t *testing.T, store, repo string, now time.Time) Record {
				return foStartAt(t, store, repo, "stale", "substantial", now.Add(-15*day))
			}, false},
			{"created and updated 13 days ago", func(t *testing.T, store, repo string, now time.Time) Record {
				return foStartAt(t, store, repo, "thirteen days", "substantial", now.Add(-13*day))
			}, true},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				store, repo := foFixture(t)
				now := time.Now()
				record := test.record(t, store, repo, now)
				var want []foWant
				if test.open {
					want = append(want, foGating(record))
				}
				foExpect(t, foFind(t, store, foAtRoot(record.Baseline.Root), now, (&foStub{}).identify()), want...)
			})
		}
	})

	t.Run("age window ends at MaxOpenAge, inclusive", func(t *testing.T) {
		// F5: now - UpdatedAt must be at most MaxOpenAge; a latest revision
		// dated after now governs like one written now (stamps run ahead of
		// a clock that stepped back). created_at lies far outside the
		// window so only updated_at can decide.
		if MaxOpenAge != 14*day {
			t.Fatalf("MaxOpenAge = %v, want 14 days", MaxOpenAge)
		}
		store, repo := foFixture(t)
		start := time.Now()
		record := foAt(t, startIn(t, store, repo, "window", "substantial"), start.Add(-30*day), start.Add(-time.Hour))
		tests := []struct {
			name string
			now  time.Time
			open bool
		}{
			{"age zero", record.UpdatedAt, true},
			{"age exactly MaxOpenAge", record.UpdatedAt.Add(MaxOpenAge), true},
			{"one second past MaxOpenAge", record.UpdatedAt.Add(MaxOpenAge + time.Second), false},
			{"updated one second in the future", record.UpdatedAt.Add(-time.Second), true},
			{"updated an hour in the future", record.UpdatedAt.Add(-time.Hour), true},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				var want []foWant
				if test.open {
					want = append(want, foGating(record))
				}
				foExpect(t, foFind(t, store, foAtRoot(record.Baseline.Root), test.now, (&foStub{}).identify()), want...)
			})
		}
	})

	t.Run("a record dated after now gates beside an older open one", func(t *testing.T) {
		// F5: a record updated after now governs like one written now, and
		// hides nothing; both are returned, newest latest revision first.
		store, repo := foFixture(t)
		now := time.Now()
		older := foStartAt(t, store, repo, "older", "substantial", now.Add(-time.Hour))
		future := foStartAt(t, store, repo, "future", "substantial", now.Add(time.Hour))
		foExpect(t, foFind(t, store, foAtRoot(older.Baseline.Root), now, (&foStub{}).identify()), foGating(future), foGating(older))
	})

	t.Run("digested path", func(t *testing.T) {
		// F6 (F8c): when Worktree differs from the record's Baseline.Root,
		// identify is called with Worktree, once for all records.
		tests := []struct {
			name  string
			found string
			gates bool
		}{
			{"different digest at the linked worktree gates", foDigestB, true},
			{"identical digest at the linked worktree does not gate", foDigestA, false},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				store, repo := foFixture(t)
				now := time.Now()
				first := foVerify(t, foStartAt(t, store, repo, "first verified", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
				second := foVerify(t, foStartAt(t, store, repo, "second verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
				worktree := foCanonicalDir(t)
				if worktree == first.Baseline.Root {
					t.Fatal("fixture worktree equals the record root")
				}
				stub := &foStub{digest: test.found}
				got := foFind(t, store, foIn(worktree, worktree, first.Baseline.Root), now, stub.identify())
				foExpect(t, got, foWant{record: second, gates: test.gates}, foWant{record: first, gates: test.gates})
				foExpectCalls(t, stub, worktree)
				if test.gates {
					for _, open := range got {
						if !strings.Contains(open.Problem, open.Record.Baseline.Root) || !strings.Contains(open.Problem, worktree) {
							t.Fatalf("problem %q does not name both the record root %s and the worktree %s", open.Problem, open.Record.Baseline.Root, worktree)
						}
					}
				}
			})
		}
	})

	t.Run("identify runs at most once per path", func(t *testing.T) {
		// F6: several verified records digested at the same path share one
		// identify call.
		store, repo := foFixture(t)
		now := time.Now()
		first := foVerify(t, foStartAt(t, store, repo, "first", "substantial", now.Add(-3*time.Hour)), true, foDigestA)
		second := foVerify(t, foStartAt(t, store, repo, "second", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
		third := foVerify(t, foStartAt(t, store, repo, "third", "substantial", now.Add(-time.Hour)), true, foDigestB)
		stub := &foStub{digest: foDigestA}
		foExpect(t, foFind(t, store, foAtRoot(first.Baseline.Root), now, stub.identify()), foGating(third), foCurrent(second), foCurrent(first))
		foExpectCalls(t, stub, first.Baseline.SelectedPath)
	})

	t.Run("linked worktree with real git", func(t *testing.T) {
		// F6 (F8c) real-git variant: a record verified in the main checkout
		// gates a linked worktree whose eligible bytes differ, and stops
		// gating once they are identical.
		store := filepath.Join(t.TempDir(), "tasks")
		main := foCanonicalDir(t)
		foGit(t, main, "init", "-q")
		foWrite(t, main, "source.txt", "source\n")
		foGit(t, main, "add", "source.txt")
		foGit(t, main, "commit", "-q", "-m", "initial")
		linked := filepath.Join(foCanonicalDir(t), "linked")
		foGit(t, main, "worktree", "add", "-q", linked)
		// Untracked, non-ignored bytes present only in the main checkout.
		foWrite(t, main, "extra.txt", "only in main\n")

		record := startIn(t, store, main, "verified in main", "substantial")
		if record.Baseline.Root != main {
			t.Fatalf("record root %q, want main checkout %q", record.Baseline.Root, main)
		}
		record = foVerify(t, record, true, foDigest(t, record.Baseline.SelectedPath))
		repository := foIn(linked, linked, main)
		// Control: in the main checkout the verified record does not gate.
		foExpect(t, foFind(t, store, foAtRoot(main), time.Now(), SourceDigest), foCurrent(record))

		got := foFind(t, store, repository, time.Now(), SourceDigest)
		foExpect(t, got, foGating(record))
		if problem := got[0].Problem; !strings.Contains(problem, main) || !strings.Contains(problem, linked) {
			t.Fatalf("problem %q does not name both the record root %s and the linked worktree %s", problem, main, linked)
		}

		foWrite(t, linked, "extra.txt", "only in main\n")
		foExpect(t, foFind(t, store, repository, time.Now(), SourceDigest), foCurrent(record))
	})

	t.Run("unloadable task directories", func(t *testing.T) {
		// F8: a task directory with no revision that decodes is skipped, and
		// the other records are still returned. A record whose newest
		// decodable revision governs but whose chain does not load is
		// returned too, unreadable, by its updated_at like any other.
		tests := []struct {
			name string
			// corrupt returns the unreadable result it causes, if any.
			corrupt func(t *testing.T, store, repo string, now time.Time) []foWant
		}{
			{"garbage JSON", func(t *testing.T, store, repo string, now time.Time) []foWant {
				foCorrupt(t, foStartAt(t, store, repo, "garbage", "substantial", now.Add(-time.Hour)))
				return nil
			}},
			{"no revision files", func(t *testing.T, store, _ string, _ time.Time) []foWant {
				if err := os.Mkdir(filepath.Join(store, "empty-task"), 0700); err != nil {
					t.Fatal(err)
				}
				return nil
			}},
			{"plain file in store", func(t *testing.T, store, _ string, _ time.Time) []foWant {
				if err := os.WriteFile(filepath.Join(store, "stray-file"), []byte("stray\n"), 0600); err != nil {
					t.Fatal(err)
				}
				return nil
			}},
			// The highest revision decodes, but the chain has a sequence gap
			// (revision 2 renamed to record-000003.json).
			{"revision chain with a sequence gap", func(t *testing.T, store, repo string, now time.Time) []foWant {
				broken := foBrokenChain(t, store, repo, "gap", now.Add(-time.Hour))
				if err := os.Rename(filepath.Join(broken.Directory, "record-000002.json"), filepath.Join(broken.Directory, "record-000003.json")); err != nil {
					t.Fatal(err)
				}
				return []foWant{foUnreadable(broken)}
			}},
			// The highest revision decodes, but the first revision was
			// replaced by a different valid record.
			{"revision chain with a replaced first revision", func(t *testing.T, store, repo string, now time.Time) []foWant {
				broken := foBrokenChain(t, store, repo, "replaced", now.Add(-time.Hour))
				other := startIn(t, filepath.Join(t.TempDir(), "other-tasks"), repo, "other", "substantial")
				data, err := os.ReadFile(filepath.Join(other.Directory, "record-000001.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(broken.Directory, "record-000001.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				return []foWant{foUnreadable(broken)}
			}},
		}
		for _, test := range tests {
			t.Run(test.name+", open record still returned", func(t *testing.T) {
				store, repo := foFixture(t)
				now := time.Now()
				good := foStartAt(t, store, repo, "good", "substantial", now.Add(-2*time.Hour))
				want := append(test.corrupt(t, store, repo, now), foGating(good))
				foExpect(t, foFind(t, store, foAtRoot(good.Baseline.Root), now, (&foStub{}).identify()), want...)
			})
			t.Run(test.name+", verified record still returned", func(t *testing.T) {
				store, repo := foFixture(t)
				now := time.Now()
				older := foStartAt(t, store, repo, "older open", "substantial", now.Add(-3*time.Hour))
				verified := foVerify(t, foStartAt(t, store, repo, "verified", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
				want := append(test.corrupt(t, store, repo, now), foCurrent(verified), foGating(older))
				foExpect(t, foFind(t, store, foAtRoot(older.Baseline.Root), now, (&foStub{digest: foDigestA}).identify()), want...)
			})
		}
	})

	t.Run("identify error fails open for that record", func(t *testing.T) {
		t.Run("other records still returned", func(t *testing.T) {
			// F9: a verified record whose identify call fails is skipped; an
			// open record that needs no digest is still returned.
			store, repo := foFixture(t)
			now := time.Now()
			older := foStartAt(t, store, repo, "older open", "substantial", now.Add(-2*time.Hour))
			newer := foVerify(t, foStartAt(t, store, repo, "newer verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
			stub := &foStub{err: errors.New("capture failed")}
			foExpect(t, foFind(t, store, foAtRoot(older.Baseline.Root), now, stub.identify()), foGating(older))
			foExpectCallsAtMostOnce(t, stub, newer.Baseline.SelectedPath)
			if len(stub.calls) != 1 {
				t.Fatalf("identify calls %q, want one call for the verified record", stub.calls)
			}
		})

		t.Run("no other record", func(t *testing.T) {
			// F9: with no other record the result is empty and there is no
			// error.
			store, repo := foFixture(t)
			now := time.Now()
			only := foVerify(t, foStartAt(t, store, repo, "only verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
			stub := &foStub{err: errors.New("capture failed")}
			foExpect(t, foFind(t, store, foAtRoot(only.Baseline.Root), now, stub.identify()))
			foExpectCalls(t, stub, only.Baseline.SelectedPath)
		})
	})

	t.Run("nil identify errors", func(t *testing.T) {
		// F9: a nil identify is a caller error.
		store, repo := foFixture(t)
		now := time.Now()
		record := foStartAt(t, store, repo, "open", "substantial", now.Add(-time.Hour))
		if _, err := FindOpen(t.Context(), store, foAtRoot(record.Baseline.Root), now, nil); err == nil {
			t.Fatal("FindOpen accepted a nil identify")
		}
	})
}

// foGit runs git in dir without the user's global or system configuration.
func foGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

var foHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// foDigest returns SourceDigest of path and requires 64 lowercase hex digits.
func foDigest(t *testing.T, path string) string {
	t.Helper()
	digest, err := SourceDigest(t.Context(), path)
	if err != nil {
		t.Fatalf("SourceDigest(%s): %v", path, err)
	}
	if !foHex64.MatchString(digest) {
		t.Fatalf("SourceDigest(%s) = %q, want 64 lowercase hex digits", path, digest)
	}
	return digest
}

// foWrite writes content to dir/name.
func foWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSourceDigest(t *testing.T) {
	t.Run("equals BaselineManifest.SHA256 after Start", func(t *testing.T) {
		// R-DIGEST: for a source unchanged since Start, SourceDigest equals
		// the record's baseline manifest digest.
		tests := []struct {
			name string
			git  bool
		}{
			{"plain directory", false},
			{"git repository", true},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				store, repo := foFixture(t)
				if test.git {
					foGit(t, repo, "init", "-q")
				}
				foWrite(t, repo, "source.txt", "source\n")
				record := startIn(t, store, repo, "digest", "substantial")
				if record.BaselineManifest == nil {
					t.Fatal("record has no baseline manifest")
				}
				if got := foDigest(t, record.Baseline.SelectedPath); got != record.BaselineManifest.SHA256 {
					t.Fatalf("SourceDigest = %s, want BaselineManifest.SHA256 %s", got, record.BaselineManifest.SHA256)
				}
			})
		}
	})

	t.Run("content identity in a git repository", func(t *testing.T) {
		// R-DIGEST: git add, git commit and a HEAD change leave the digest
		// unchanged; editing a file or adding a new one changes it.
		repo := foCanonicalDir(t)
		foGit(t, repo, "init", "-q")
		foWrite(t, repo, "source.txt", "source\n")
		original := foDigest(t, repo)

		foGit(t, repo, "add", "source.txt")
		if got := foDigest(t, repo); got != original {
			t.Fatalf("digest after git add = %s, want unchanged %s", got, original)
		}
		foGit(t, repo, "commit", "-q", "-m", "x")
		if got := foDigest(t, repo); got != original {
			t.Fatalf("digest after git commit = %s, want unchanged %s", got, original)
		}
		foGit(t, repo, "commit", "-q", "--allow-empty", "-m", "y")
		if got := foDigest(t, repo); got != original {
			t.Fatalf("digest after moving HEAD = %s, want unchanged %s", got, original)
		}

		foWrite(t, repo, "source.txt", "edited\n")
		edited := foDigest(t, repo)
		if edited == original {
			t.Fatal("digest unchanged after editing source.txt")
		}
		foWrite(t, repo, "added.txt", "new file\n")
		if added := foDigest(t, repo); added == edited {
			t.Fatal("digest unchanged after adding added.txt")
		}
	})

	t.Run("missing path errors", func(t *testing.T) {
		// R-DIGEST: a path that does not exist is an error.
		if digest, err := SourceDigest(t.Context(), filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatalf("SourceDigest of a missing path = %q, nil; want an error", digest)
		}
	})

	t.Run("Verify stores SourceSHA256", func(t *testing.T) {
		// R-DIGEST: Verify stores SourceSHA256 equal to SourceDigest of the
		// record's selected path, also on an incomplete record.
		store, repo := foFixture(t)
		foWrite(t, repo, "source.txt", "source\n")
		record := startIn(t, store, repo, "verify", "substantial")
		verification, err := Verify(t.Context(), record.Directory)
		if err == nil {
			t.Fatal("Verify of an unassessed record returned nil")
		}
		if verification.Complete {
			t.Fatal("Verify reported an unassessed record complete")
		}
		if want := foDigest(t, record.Baseline.SelectedPath); verification.SourceSHA256 != want {
			t.Fatalf("Verification.SourceSHA256 = %q, want SourceDigest %s", verification.SourceSHA256, want)
		}
	})
}

// foManifest returns record's BaselineManifest.SHA256 and requires one.
func foManifest(t *testing.T, record Record) string {
	t.Helper()
	if record.BaselineManifest == nil || record.BaselineManifest.SHA256 == "" {
		t.Fatalf("record %q has no baseline manifest digest", record.Plan.Title)
	}
	return record.BaselineManifest.SHA256
}

func TestFindOpenFinishedRecords(t *testing.T) {
	const day = 24 * time.Hour

	// F10 with a stub identify: record A carries a complete verification
	// whose SourceSHA256 D is record B's BaselineManifest.SHA256, and identify
	// reports another digest, so A would gate as drifted unless B finishes it.
	tests := []struct {
		name string
		// b starts record B in store for repo (or elsewhere) at time at.
		b func(t *testing.T, store, repo string, at time.Time) Record
		// aComplete is whether A's verification is complete.
		aComplete bool
		// aVerifiedAgo is how long before now A was created and verified
		// (its Verification.CheckedAt); zero means two hours. A's latest
		// revision is always updated two hours before now.
		aVerifiedAgo time.Duration
		// current is the digest identify reports; "" means foDigestB (drift).
		current string
		// want lists the expected results given A and B, newest first.
		want func(a, b Record) []foWant
	}{
		{
			// F10: an open substantial B in the same root finishes drifted A.
			name: "substantial open B finishes drifted A",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				return foStartAt(t, store, repo, "B substantial", "substantial", at)
			},
			aComplete: true,
			want:      func(_, b Record) []foWant { return []foWant{foGating(b)} },
		},
		{
			// F10b (rev 6): a small B does not govern, so it does not finish
			// A; A gates and B is not returned.
			name: "small B does not finish drifted A",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				return foStartAt(t, store, repo, "B small", "small", at)
			},
			aComplete: true,
			want:      func(a, _ Record) []foWant { return []foWant{foGating(a)} },
		},
		{
			// F10b (rev 6): an abandoned B does not govern, so it does not
			// finish A; A gates and B is not returned.
			name: "abandoned B does not finish drifted A",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				b := foStartAt(t, store, repo, "B abandoned", "substantial", at)
				if _, err := Abandon(b.Directory, "abandoned for the test"); err != nil {
					t.Fatalf("Abandon B: %v", err)
				}
				return b
			},
			aComplete: true,
			want:      func(a, _ Record) []foWant { return []foWant{foGating(a)} },
		},
		{
			// F10b (rev 6): a B whose latest revision is older than
			// MaxOpenAge does not govern, so it does not finish A, although
			// it was created after A's CheckedAt (A verified 50 days ago, B
			// created 30 days ago; F10a alone would let it finish A). A
			// gates and B is not returned.
			name: "B older than MaxOpenAge does not finish drifted A",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				return foStartAt(t, store, repo, "B stale", "substantial", at.Add(-30*day))
			},
			aComplete:    true,
			aVerifiedAgo: 50 * day,
			want:         func(a, _ Record) []foWant { return []foWant{foGating(a)} },
		},
		{
			// F10b (rev 6) control for the stale case: the same A beside a
			// governing (substantial, unabandoned, in-window) B created after
			// A's CheckedAt is finished.
			name: "in-window B finishes drifted A verified long ago",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				return foStartAt(t, store, repo, "B in window", "substantial", at)
			},
			aComplete:    true,
			aVerifiedAgo: 50 * day,
			want:         func(_, b Record) []foWant { return []foWant{foGating(b)} },
		},
		{
			// F10a: B created before A's CheckedAt does not finish A, even
			// with A's verified bytes as its baseline; both gate.
			name: "B created before A's verification does not finish A",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				return foStartAt(t, store, repo, "B earlier", "substantial", at.Add(-2*time.Hour))
			},
			aComplete: true,
			want:      func(a, b Record) []foWant { return []foWant{foGating(a), foGating(b)} },
		},
		{
			// F10: B must have a root in repo.Roots; B of another root does
			// not finish A, which gates as drifted.
			name: "B of another root does not finish A",
			b: func(t *testing.T, store, _ string, at time.Time) Record {
				return foStartAt(t, store, t.TempDir(), "B other root", "substantial", at)
			},
			aComplete: true,
			want:      func(a, _ Record) []foWant { return []foWant{foGating(a)} },
		},
		{
			// F10: an unverified A (incomplete verification) is never
			// finished.
			name: "incomplete A is not finished",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				return foStartAt(t, store, repo, "B substantial", "substantial", at)
			},
			aComplete: false,
			want:      func(a, b Record) []foWant { return []foWant{foGating(b), foGating(a)} },
		},
		{
			// F2 with F10: an A whose current digest still equals D is not
			// drifted, so it is returned and does not gate.
			name: "current A is returned beside B",
			b: func(t *testing.T, store, repo string, at time.Time) Record {
				return foStartAt(t, store, repo, "B substantial", "substantial", at)
			},
			aComplete: true,
			current:   "same as D",
			want:      func(a, b Record) []foWant { return []foWant{foGating(b), foCurrent(a)} },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, repo := foFixture(t)
			now := time.Now()
			b := test.b(t, store, repo, now.Add(-time.Hour))
			d := foManifest(t, b)
			verifiedAgo := test.aVerifiedAgo
			if verifiedAgo == 0 {
				verifiedAgo = 2 * time.Hour
			}
			// foVerify sets CheckedAt to A's updated_at, here now-verifiedAgo.
			a := foVerify(t, foStartAt(t, store, repo, "A verified", "substantial", now.Add(-verifiedAgo)), test.aComplete, d)
			if verifiedAgo != 2*time.Hour {
				a = appendRevision(t, a, func(next *Record) { next.UpdatedAt = now.Add(-2 * time.Hour) })
			}
			current := foDigestB
			if test.current != "" {
				current = d
			}
			got := foFind(t, store, foAtRoot(a.Baseline.Root), now, (&foStub{digest: current}).identify())
			foExpect(t, got, test.want(a, b)...)
		})
	}

	t.Run("no B leaves drifted A gating", func(t *testing.T) {
		// F10: when no record started from A's verified bytes, A gates.
		store, repo := foFixture(t)
		now := time.Now()
		other := foStartAt(t, store, repo, "B on other bytes", "substantial", now.Add(-time.Hour))
		a := foVerify(t, foStartAt(t, store, repo, "A verified", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
		if foManifest(t, other) == foDigestA {
			t.Fatal("fixture manifest collides with the stub digest")
		}
		foExpect(t, foFind(t, store, foAtRoot(a.Baseline.Root), now, (&foStub{digest: foDigestB}).identify()), foGating(other), foGating(a))
	})

	t.Run("real git: B started on A's verified bytes finishes A", func(t *testing.T) {
		// F10 real-git case: A started and verified on bytes X, B started on
		// X with no edits between, then a file is edited: only B is returned,
		// gating.
		store := filepath.Join(t.TempDir(), "tasks")
		repo := foCanonicalDir(t)
		foGit(t, repo, "init", "-q")
		foWrite(t, repo, "source.txt", "bytes X\n")
		now := time.Now()
		a := foAt(t, startIn(t, store, repo, "A", "substantial"), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
		x := foDigest(t, a.Baseline.SelectedPath)
		a = foVerify(t, a, true, x)
		b := foAt(t, startIn(t, store, repo, "B", "substantial"), now.Add(-time.Hour), now.Add(-time.Hour))
		if foManifest(t, b) != x {
			t.Fatalf("B baseline manifest %s, want A's verified digest %s", foManifest(t, b), x)
		}
		foWrite(t, repo, "source.txt", "edited\n")
		foExpect(t, foFind(t, store, foAtRoot(repo), time.Now(), SourceDigest), foGating(b))
	})

	t.Run("real git control: B started on changed bytes does not finish A", func(t *testing.T) {
		// F10 real-git control: A verified on X, a file edited, then B
		// started (baseline differs from X): both are returned and gate.
		store := filepath.Join(t.TempDir(), "tasks")
		repo := foCanonicalDir(t)
		foGit(t, repo, "init", "-q")
		foWrite(t, repo, "source.txt", "bytes X\n")
		now := time.Now()
		a := foAt(t, startIn(t, store, repo, "A", "substantial"), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
		x := foDigest(t, a.Baseline.SelectedPath)
		a = foVerify(t, a, true, x)
		foWrite(t, repo, "source.txt", "edited\n")
		b := foAt(t, startIn(t, store, repo, "B", "substantial"), now.Add(-time.Hour), now.Add(-time.Hour))
		if foManifest(t, b) == x {
			t.Fatal("B baseline manifest equals A's verified digest after an edit")
		}
		foExpect(t, foFind(t, store, foAtRoot(repo), time.Now(), SourceDigest), foGating(b), foGating(a))
	})

	t.Run("real git: records verified after both started do not finish each other", func(t *testing.T) {
		// F10a real-git mutual case: A and B started on the same bytes X,
		// both verified on X afterwards with no edits, then a file edited:
		// both are returned and both gate.
		store := filepath.Join(t.TempDir(), "tasks")
		repo := foCanonicalDir(t)
		foGit(t, repo, "init", "-q")
		foWrite(t, repo, "source.txt", "bytes X\n")
		now := time.Now()
		a := foAt(t, startIn(t, store, repo, "A", "substantial"), now.Add(-3*time.Hour), now.Add(-3*time.Hour))
		b := foAt(t, startIn(t, store, repo, "B", "substantial"), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
		x := foDigest(t, repo)
		if foManifest(t, a) != x || foManifest(t, b) != x {
			t.Fatalf("baseline manifests %s and %s, want both %s", foManifest(t, a), foManifest(t, b), x)
		}
		a = foVerifyAt(t, a, x, now.Add(-90*time.Minute))
		b = foVerifyAt(t, b, x, now.Add(-time.Hour))
		// Control: before the edit both are current.
		foExpect(t, foFind(t, store, foAtRoot(repo), time.Now(), SourceDigest), foCurrent(b), foCurrent(a))
		foWrite(t, repo, "source.txt", "edited\n")
		foExpect(t, foFind(t, store, foAtRoot(repo), time.Now(), SourceDigest), foGating(b), foGating(a))
	})
}

// foVerifyAt appends a revision updated at at that carries a complete
// verification of record checked at at, with the given source digest.
func foVerifyAt(t *testing.T, record Record, sourceSHA256 string, at time.Time) Record {
	t.Helper()
	return appendRevision(t, record, func(next *Record) {
		next.UpdatedAt = at
		next.Verification = &Verification{TaskID: next.ID, Complete: true, CurrentSnapshot: next.Baseline, SourceSHA256: sourceSHA256, Problems: []string{}, Limitations: []string{}, CheckedAt: at}
	})
}

func TestFindOpenIdentifyOncePerWorktree(t *testing.T) {
	// foSubdirRecords starts two records in a Git repository, one for the
	// repository and one for its subdirectory sub, and requires the same
	// Baseline.Root and different SelectedPaths.
	foSubdirRecords := func(t *testing.T) (store, repo string, top, sub Record) {
		t.Helper()
		store = filepath.Join(t.TempDir(), "tasks")
		repo = foCanonicalDir(t)
		foGit(t, repo, "init", "-q")
		foWrite(t, repo, "source.txt", "source\n")
		if err := os.Mkdir(filepath.Join(repo, "sub"), 0700); err != nil {
			t.Fatal(err)
		}
		foWrite(t, filepath.Join(repo, "sub"), "inner.txt", "inner\n")
		now := time.Now()
		top = foAt(t, startIn(t, store, repo, "top", "substantial"), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
		sub = foAt(t, startIn(t, store, filepath.Join(repo, "sub"), "sub", "substantial"), now.Add(-time.Hour), now.Add(-time.Hour))
		if top.Baseline.Root != sub.Baseline.Root || top.Baseline.Root != repo {
			t.Fatalf("roots %q and %q, want both %q", top.Baseline.Root, sub.Baseline.Root, repo)
		}
		if top.Baseline.SelectedPath == sub.Baseline.SelectedPath {
			t.Fatalf("both records select %q, want different selected paths", top.Baseline.SelectedPath)
		}
		return store, repo, top, sub
	}

	t.Run("one identify call for two selected paths", func(t *testing.T) {
		// F11: two verified records with the same root and different
		// SelectedPaths cause one identify call, with one of their
		// SelectedPaths.
		store, repo, top, sub := foSubdirRecords(t)
		top = foVerify(t, top, true, foDigestA)
		sub = foVerify(t, sub, true, foDigestA)
		stub := &foStub{digest: foDigestA}
		foExpect(t, foFind(t, store, foAtRoot(repo), time.Now(), stub.identify()), foCurrent(sub), foCurrent(top))
		if len(stub.calls) != 1 || (stub.calls[0] != top.Baseline.SelectedPath && stub.calls[0] != sub.Baseline.SelectedPath) {
			t.Fatalf("identify calls %q, want one call for %q or %q", stub.calls, top.Baseline.SelectedPath, sub.Baseline.SelectedPath)
		}
	})

	t.Run("real git digest covers the whole worktree", func(t *testing.T) {
		// F11: either selected path gives the same Git digest, so both
		// records verified on the worktree's digest are current.
		store, repo, top, sub := foSubdirRecords(t)
		if a, b := foDigest(t, top.Baseline.SelectedPath), foDigest(t, sub.Baseline.SelectedPath); a != b {
			t.Fatalf("SourceDigest differs between %s (%s) and %s (%s)", top.Baseline.SelectedPath, a, sub.Baseline.SelectedPath, b)
		}
		digest := foDigest(t, repo)
		top = foVerify(t, top, true, digest)
		sub = foVerify(t, sub, true, digest)
		foExpect(t, foFind(t, store, foAtRoot(repo), time.Now(), SourceDigest), foCurrent(sub), foCurrent(top))
	})
}

func TestFindOpenOrderTieBreak(t *testing.T) {
	// Result order: newest UpdatedAt first; equal UpdatedAt orders by record
	// ID ascending. F12 (rev 6): with twenty-four tied records and the order
	// asserted over twenty FindOpen calls, an order that does not use the ID
	// tie-break passes only by chance (at most 1/24! per call if the order
	// comes from map iteration), so removing the tie-break fails with
	// negligible probability of passing. Twenty-four records also take an
	// unstable sort past its stable insertion-sort range for small inputs.
	const tiedCount, calls = 24, 20
	store, repo := foFixture(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	tied := make([]Record, 0, tiedCount)
	for i := range tiedCount {
		tied = append(tied, foStartAt(t, store, repo, fmt.Sprintf("tie %02d", i), "substantial", at))
	}
	newest := foStartAt(t, store, repo, "newest", "substantial", now.Add(-time.Minute))
	for _, record := range tied {
		if !record.UpdatedAt.Equal(tied[0].UpdatedAt) {
			t.Fatalf("fixture updated_at values differ: %v and %v", record.UpdatedAt, tied[0].UpdatedAt)
		}
	}
	slices.SortFunc(tied, func(x, y Record) int { return strings.Compare(x.ID, y.ID) })
	want := []foWant{foGating(newest)}
	for _, record := range tied {
		want = append(want, foGating(record))
	}
	for range calls {
		foExpect(t, foFind(t, store, foAtRoot(newest.Baseline.Root), now, (&foStub{}).identify()), want...)
	}
}
