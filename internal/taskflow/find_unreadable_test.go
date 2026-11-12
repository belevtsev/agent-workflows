package taskflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A governing record that Load refuses, for any reason but ErrLegacyRecord,
// must not vanish from FindOpen: the hook asks about it and cw publish
// refuses (D4). Before this, each case below returned no record at all, so
// the hook allowed the commit.
func TestFindOpenReturnsRecordsItCannotRead(t *testing.T) {
	tests := []struct {
		name string
		// damage breaks the open record; newer says ErrNewerRecord results.
		damage func(t *testing.T, record Record)
		newer  bool
		// reason is part of the load error Unreadable must carry.
		reason string
	}{
		// The review's repro: echo '{' > <task>/record-000002.json.
		{"garbage newest revision", func(t *testing.T, record Record) {
			foWrite(t, record.Directory, "record-000002.json", "{\n")
		}, false, "invalid task JSON"},
		{"newer version", func(t *testing.T, record Record) {
			writeNextRevision(t, record, func(next *Record) { next.Version = Version + 1 })
		}, true, "upgrade cw"},
		{"revision readable by others", func(t *testing.T, record Record) {
			if err := os.Chmod(filepath.Join(record.Directory, "record-000001.json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, false, "private regular file"},
		{"task directory readable by others", func(t *testing.T, record Record) {
			if err := os.Chmod(record.Directory, 0o755); err != nil {
				t.Fatal(err)
			}
		}, false, "task directory must be private"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Oracle: exactly one OpenRecord, the damaged one by ID, directory
			// and title, that does not gate and has Unreadable set to the load
			// error; Newer only for a record a newer cw wrote.
			store, repo := foFixture(t)
			now := time.Now()
			record := foStartAt(t, store, repo, "damaged", "substantial", now.Add(-time.Hour))
			test.damage(t, record)
			got := foFind(t, store, foAtRoot(record.Baseline.Root), now, (&foStub{}).identify())
			foExpect(t, got, foUnreadable(record))
			if !strings.Contains(got[0].Unreadable, test.reason) || got[0].Newer != test.newer {
				t.Fatalf("Unreadable %q, Newer %v; want an error containing %q and Newer %v", got[0].Unreadable, got[0].Newer, test.reason, test.newer)
			}
		})
	}
}
