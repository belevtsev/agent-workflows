package taskflow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// Tests for FindOpen (requirement R-FIND) and SourceDigest (requirement
// R-DIGEST). Records are created with the approved scaffold (startIn,
// appendRevision, rewriteFirstRevision); identify is a counting stub so each
// case can also assert when the content capture runs.

// foStub is a counting SourceIdentifier that returns a fixed answer and
// records every selected path it was asked about.
type foStub struct {
	digest string
	err    error
	calls  []string
}

func (s *foStub) identify() SourceIdentifier {
	return func(_ context.Context, selectedPath string) (string, error) {
		s.calls = append(s.calls, selectedPath)
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

// foAge rewrites the single revision of record so it was created at createdAt.
func foAge(t *testing.T, record Record, createdAt time.Time) Record {
	t.Helper()
	return rewriteFirstRevision(t, record, func(next *Record) { next.CreatedAt = createdAt })
}

// foStartAged starts a record and rewrites its creation time to createdAt, so
// the order between records never depends on the wall clock.
func foStartAged(t *testing.T, store, repo, title, kind string, createdAt time.Time) Record {
	t.Helper()
	return foAge(t, startIn(t, store, repo, title, kind), createdAt)
}

// foVerify appends a revision carrying a verification of record with the
// given completeness and stored source digest.
func foVerify(t *testing.T, record Record, complete bool, sourceSHA256 string) Record {
	t.Helper()
	return appendRevision(t, record, func(next *Record) {
		problems := []string{}
		if !complete {
			problems = []string{"incomplete for the test"}
		}
		next.Verification = &Verification{TaskID: next.ID, Complete: complete, CurrentSnapshot: next.Baseline, SourceSHA256: sourceSHA256, Problems: problems, Limitations: []string{}, CheckedAt: next.CreatedAt}
	})
}

// foCorrupt replaces the first revision of record with bytes that are not JSON.
func foCorrupt(t *testing.T, record Record) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(record.Directory, "record-000001.json"), []byte("this is not a record {"), 0600); err != nil {
		t.Fatal(err)
	}
}

// foBrokenChain starts an open substantial record created at createdAt and
// appends a second revision without a verification, so the record would gate
// if its chain were (wrongly) accepted after the caller breaks it.
func foBrokenChain(t *testing.T, store, repo, title string, createdAt time.Time) Record {
	t.Helper()
	record := appendRevision(t, foStartAged(t, store, repo, title, "substantial", createdAt), func(*Record) {})
	if record.Revision != 2 || record.Verification != nil {
		t.Fatalf("broken-chain fixture has revision %d, verification %v; want revision 2 and none", record.Revision, record.Verification)
	}
	return record
}

// foExpect runs FindOpen and requires no error and exactly the wanted record
// (nil means no record).
func foExpect(t *testing.T, store string, roots []string, now time.Time, identify SourceIdentifier, want *Record) {
	t.Helper()
	got, err := FindOpen(t.Context(), store, roots, now, identify)
	if err != nil {
		t.Fatalf("FindOpen: %v", err)
	}
	switch {
	case want == nil && got != nil:
		t.Fatalf("FindOpen returned %s (%q), want nil", got.ID, got.Plan.Title)
	case want != nil && got == nil:
		t.Fatalf("FindOpen returned nil, want %s (%q)", want.ID, want.Plan.Title)
	case want != nil && (got.ID != want.ID || got.Directory != want.Directory || got.Plan.Title != want.Plan.Title):
		t.Fatalf("FindOpen returned %s (%q), want %s (%q)", got.ID, got.Plan.Title, want.ID, want.Plan.Title)
	}
}

// foExpectCalls requires that identify was asked about exactly want.
func foExpectCalls(t *testing.T, stub *foStub, want ...string) {
	t.Helper()
	if !slices.Equal(stub.calls, want) {
		t.Fatalf("identify calls %q, want %q", stub.calls, want)
	}
}

func TestFindOpen(t *testing.T) {
	const day = 24 * time.Hour

	t.Run("no store", func(t *testing.T) {
		// R-FIND: a store that does not exist returns (nil, nil).
		stub := &foStub{digest: foDigestA}
		record, err := FindOpen(t.Context(), filepath.Join(t.TempDir(), "missing"), []string{foCanonicalDir(t)}, time.Now(), stub.identify())
		if err != nil || record != nil {
			t.Fatalf("FindOpen on a missing store = %v, %v; want nil, nil", record, err)
		}
		foExpectCalls(t, stub)
	})

	t.Run("empty store", func(t *testing.T) {
		// R-FIND: an existing empty store returns nil.
		store := filepath.Join(t.TempDir(), "tasks")
		if err := os.Mkdir(store, 0700); err != nil {
			t.Fatal(err)
		}
		stub := &foStub{digest: foDigestA}
		foExpect(t, store, []string{foCanonicalDir(t)}, time.Now(), stub.identify(), nil)
		foExpectCalls(t, stub)
	})

	t.Run("wrong root", func(t *testing.T) {
		// R-FIND: only records whose Baseline.Root is one of roots are
		// candidates.
		store, repo := foFixture(t)
		now := time.Now()
		record := foStartAged(t, store, repo, "wrong root", "substantial", now.Add(-time.Hour))
		foExpect(t, store, []string{foCanonicalDir(t)}, now, (&foStub{}).identify(), nil)
		// Positive control: the same record is found under its own root.
		foExpect(t, store, []string{record.Baseline.Root}, now, (&foStub{}).identify(), &record)
	})

	t.Run("non-substantial beside a substantial", func(t *testing.T) {
		// R-FIND: a newer small record is not a candidate, so the older
		// substantial record decides.
		store, repo := foFixture(t)
		now := time.Now()
		substantial := foStartAged(t, store, repo, "substantial", "substantial", now.Add(-2*time.Hour))
		small := foStartAged(t, store, repo, "small", "small", now.Add(-time.Hour))
		if small.Plan.Kind != "small" {
			t.Fatalf("scaffold plan kind %q", small.Plan.Kind)
		}
		stub := &foStub{}
		foExpect(t, store, []string{substantial.Baseline.Root}, now, stub.identify(), &substantial)
		foExpectCalls(t, stub)
	})

	t.Run("stale beyond 14 days", func(t *testing.T) {
		// R-FIND: an open record created 15 days ago is abandoned.
		store, repo := foFixture(t)
		now := time.Now()
		stale := foStartAged(t, store, repo, "stale", "substantial", now.Add(-15*day))
		foExpect(t, store, []string{stale.Baseline.Root}, now, (&foStub{}).identify(), nil)
	})

	t.Run("13 days still open", func(t *testing.T) {
		// R-FIND: an open record created 13 days ago still gates.
		store, repo := foFixture(t)
		now := time.Now()
		record := foStartAged(t, store, repo, "thirteen days", "substantial", now.Add(-13*day))
		foExpect(t, store, []string{record.Baseline.Root}, now, (&foStub{}).identify(), &record)
	})

	t.Run("age window is inclusive at both ends", func(t *testing.T) {
		// R-FIND: now - CreatedAt must lie within [0, MaxOpenAge]; a future
		// CreatedAt is excluded.
		if MaxOpenAge != 14*day {
			t.Fatalf("MaxOpenAge = %v, want 14 days", MaxOpenAge)
		}
		store, repo := foFixture(t)
		record := foStartAged(t, store, repo, "window", "substantial", time.Now().Add(-time.Hour))
		roots := []string{record.Baseline.Root}
		tests := []struct {
			name string
			now  time.Time
			open bool
		}{
			{"age zero", record.CreatedAt, true},
			{"age exactly MaxOpenAge", record.CreatedAt.Add(MaxOpenAge), true},
			{"one second past MaxOpenAge", record.CreatedAt.Add(MaxOpenAge + time.Second), false},
			{"created one second in the future", record.CreatedAt.Add(-time.Second), false},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				var want *Record
				if test.open {
					want = &record
				}
				foExpect(t, store, roots, test.now, (&foStub{}).identify(), want)
			})
		}
	})

	t.Run("future record does not hide an older open one", func(t *testing.T) {
		// R-FIND: a record created after now is not a candidate, so the
		// newest candidate is the older open record.
		store, repo := foFixture(t)
		now := time.Now()
		older := foStartAged(t, store, repo, "older", "substantial", now.Add(-time.Hour))
		foStartAged(t, store, repo, "future", "substantial", now.Add(time.Hour))
		foExpect(t, store, []string{older.Baseline.Root}, now, (&foStub{}).identify(), &older)
	})

	t.Run("completed and current", func(t *testing.T) {
		// R-FIND: a complete verification whose source digest equals what
		// identify reports closes the record; identify is asked exactly once,
		// for the record's Baseline.SelectedPath.
		store, repo := foFixture(t)
		now := time.Now()
		verified := foVerify(t, foStartAged(t, store, repo, "verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
		stub := &foStub{digest: foDigestA}
		foExpect(t, store, []string{verified.Baseline.Root}, now, stub.identify(), nil)
		foExpectCalls(t, stub, verified.Baseline.SelectedPath)
	})

	t.Run("completed but drifted", func(t *testing.T) {
		// R-FIND: a complete verification on bytes that changed since does
		// not close the record.
		store, repo := foFixture(t)
		now := time.Now()
		verified := foVerify(t, foStartAged(t, store, repo, "drifted", "substantial", now.Add(-time.Hour)), true, foDigestA)
		stub := &foStub{digest: foDigestB}
		foExpect(t, store, []string{verified.Baseline.Root}, now, stub.identify(), &verified)
		foExpectCalls(t, stub, verified.Baseline.SelectedPath)
	})

	t.Run("completed without SourceSHA256", func(t *testing.T) {
		// R-FIND: a record verified by an older cw (Complete with an empty
		// SourceSHA256) is open, and identify is not called.
		store, repo := foFixture(t)
		now := time.Now()
		legacy := foVerify(t, foStartAged(t, store, repo, "legacy", "substantial", now.Add(-time.Hour)), true, "")
		stub := &foStub{digest: ""}
		foExpect(t, store, []string{legacy.Baseline.Root}, now, stub.identify(), &legacy)
		foExpectCalls(t, stub)
	})

	t.Run("incomplete verification", func(t *testing.T) {
		// R-FIND: identify is never called for a record without a complete
		// verification, even when the stored digest would match.
		store, repo := foFixture(t)
		now := time.Now()
		incomplete := foVerify(t, foStartAged(t, store, repo, "incomplete", "substantial", now.Add(-time.Hour)), false, foDigestA)
		stub := &foStub{digest: foDigestA}
		foExpect(t, store, []string{incomplete.Baseline.Root}, now, stub.identify(), &incomplete)
		foExpectCalls(t, stub)
	})

	t.Run("newest decides", func(t *testing.T) {
		t.Run("newer closed beside older open", func(t *testing.T) {
			// R-FIND: older candidates never decide; a newer closed record
			// yields nil although an older one is open.
			store, repo := foFixture(t)
			now := time.Now()
			older := foStartAged(t, store, repo, "older open", "substantial", now.Add(-2*time.Hour))
			newer := foVerify(t, foStartAged(t, store, repo, "newer closed", "substantial", now.Add(-time.Hour)), true, foDigestA)
			stub := &foStub{digest: foDigestA}
			foExpect(t, store, []string{older.Baseline.Root}, now, stub.identify(), nil)
			foExpectCalls(t, stub, newer.Baseline.SelectedPath)
		})

		t.Run("newer open beside older closed", func(t *testing.T) {
			// R-FIND: a newer open record is returned although an older one
			// is closed; the older one is not consulted.
			store, repo := foFixture(t)
			now := time.Now()
			foVerify(t, foStartAged(t, store, repo, "older closed", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
			newer := foStartAged(t, store, repo, "newer open", "substantial", now.Add(-time.Hour))
			stub := &foStub{digest: foDigestA}
			foExpect(t, store, []string{newer.Baseline.Root}, now, stub.identify(), &newer)
			foExpectCalls(t, stub)
		})

		t.Run("three open records give the newest by CreatedAt", func(t *testing.T) {
			// R-FIND: the newest by CreatedAt wins, whatever order the
			// records were started in.
			store, repo := foFixture(t)
			now := time.Now()
			newest := foStartAged(t, store, repo, "started first, newest", "substantial", now.Add(-time.Hour))
			foStartAged(t, store, repo, "started second, oldest", "substantial", now.Add(-3*time.Hour))
			foStartAged(t, store, repo, "started third, middle", "substantial", now.Add(-2*time.Hour))
			foExpect(t, store, []string{newest.Baseline.Root}, now, (&foStub{}).identify(), &newest)
		})
	})

	t.Run("corrupt newest skipped", func(t *testing.T) {
		// R-FIND: a task directory whose record cannot be loaded is skipped
		// and the next newest candidate decides.
		tests := []struct {
			name    string
			corrupt func(t *testing.T, store, repo string, now time.Time)
		}{
			{"garbage JSON", func(t *testing.T, store, repo string, now time.Time) {
				foCorrupt(t, foStartAged(t, store, repo, "garbage", "substantial", now.Add(-time.Hour)))
			}},
			{"no revision files", func(t *testing.T, store, _ string, _ time.Time) {
				if err := os.Mkdir(filepath.Join(store, "empty-task"), 0700); err != nil {
					t.Fatal(err)
				}
			}},
			{"plain file in store", func(t *testing.T, store, _ string, _ time.Time) {
				if err := os.WriteFile(filepath.Join(store, "stray-file"), []byte("stray\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}},
			// R-FIND rev 3: the newest candidate's highest revision decodes,
			// but its revision chain has a sequence gap (revision 2 renamed to
			// record-000003.json), so the chain does not load and it is skipped.
			{"revision chain with a sequence gap", func(t *testing.T, store, repo string, now time.Time) {
				broken := foBrokenChain(t, store, repo, "gap", now.Add(-time.Hour))
				if err := os.Rename(filepath.Join(broken.Directory, "record-000002.json"), filepath.Join(broken.Directory, "record-000003.json")); err != nil {
					t.Fatal(err)
				}
			}},
			// R-FIND rev 3: the newest candidate's highest revision decodes,
			// but its first revision was replaced by a different valid record,
			// so the chain does not load and it is skipped.
			{"revision chain with a replaced first revision", func(t *testing.T, store, repo string, now time.Time) {
				broken := foBrokenChain(t, store, repo, "replaced", now.Add(-time.Hour))
				other := startIn(t, filepath.Join(t.TempDir(), "other-tasks"), repo, "other", "substantial")
				data, err := os.ReadFile(filepath.Join(other.Directory, "record-000001.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(broken.Directory, "record-000001.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}},
		}
		for _, test := range tests {
			t.Run(test.name+", next newest open", func(t *testing.T) {
				store, repo := foFixture(t)
				now := time.Now()
				good := foStartAged(t, store, repo, "good", "substantial", now.Add(-2*time.Hour))
				test.corrupt(t, store, repo, now)
				foExpect(t, store, []string{good.Baseline.Root}, now, (&foStub{}).identify(), &good)
			})
			t.Run(test.name+", next newest closed", func(t *testing.T) {
				store, repo := foFixture(t)
				now := time.Now()
				older := foStartAged(t, store, repo, "older open", "substantial", now.Add(-3*time.Hour))
				foVerify(t, foStartAged(t, store, repo, "closed", "substantial", now.Add(-2*time.Hour)), true, foDigestA)
				test.corrupt(t, store, repo, now)
				foExpect(t, store, []string{older.Baseline.Root}, now, (&foStub{digest: foDigestA}).identify(), nil)
			})
		}
	})

	t.Run("identify error on the newest", func(t *testing.T) {
		t.Run("next candidate decides", func(t *testing.T) {
			// R-FIND: a verified record whose identify call fails is skipped
			// and the next newest candidate decides.
			store, repo := foFixture(t)
			now := time.Now()
			older := foStartAged(t, store, repo, "older open", "substantial", now.Add(-2*time.Hour))
			newer := foVerify(t, foStartAged(t, store, repo, "newer verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
			stub := &foStub{err: errors.New("capture failed")}
			foExpect(t, store, []string{older.Baseline.Root}, now, stub.identify(), &older)
			foExpectCalls(t, stub, newer.Baseline.SelectedPath)
		})

		t.Run("no other candidate", func(t *testing.T) {
			// R-FIND: with no other candidate the result is nil.
			store, repo := foFixture(t)
			now := time.Now()
			only := foVerify(t, foStartAged(t, store, repo, "only verified", "substantial", now.Add(-time.Hour)), true, foDigestA)
			stub := &foStub{err: errors.New("capture failed")}
			foExpect(t, store, []string{only.Baseline.Root}, now, stub.identify(), nil)
			foExpectCalls(t, stub, only.Baseline.SelectedPath)
		})
	})

	t.Run("nil identify errors", func(t *testing.T) {
		// R-FIND: a nil identify is a caller error.
		store, repo := foFixture(t)
		now := time.Now()
		record := foStartAged(t, store, repo, "open", "substantial", now.Add(-time.Hour))
		if _, err := FindOpen(t.Context(), store, []string{record.Baseline.Root}, now, nil); err == nil {
			t.Fatal("FindOpen accepted a nil identify")
		}
	})

	t.Run("multiple roots", func(t *testing.T) {
		// R-FIND: a record whose root is the second element of roots is found.
		store, repo := foFixture(t)
		now := time.Now()
		record := foStartAged(t, store, repo, "second root", "substantial", now.Add(-time.Hour))
		foExpect(t, store, []string{foCanonicalDir(t), record.Baseline.Root}, now, (&foStub{}).identify(), &record)
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
