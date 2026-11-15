package store

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The clean-up keeps a lock that processes share, however old it is:
// taking a lock does not change its time, and removing a held lock would let
// a second process lock a new file of the same name (second challenge,
// finding A6).
func TestSweepKeepsSharedLocks(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	old := now.Add(-2 * RecordAge)
	files := []string{
		"runs/unit/k/seq.lock",
		"runs/unit/k/r1.json",
		"runs/unit/k/r1.lock",
		"decisions/write.lock",
		"decisions/2024-01.jsonl",
		"logs/seq.lock",
		"logs/write.lock",
		"logs/r1.log",
	}
	for _, rel := range files {
		path := filepath.Join(s.Dir, filepath.FromSlash(rel))
		writeFile(t, path, nil)
		setAge(t, path, old)
	}
	s.Sweep(t.Context(), now)

	kept := []string{"runs/unit/k/seq.lock", "decisions/write.lock", "logs/seq.lock", "logs/write.lock"}
	gone := []string{"runs/unit/k/r1.json", "runs/unit/k/r1.lock", "decisions/2024-01.jsonl", "logs/r1.log"}
	for _, rel := range kept {
		if !exists(filepath.Join(s.Dir, filepath.FromSlash(rel))) {
			t.Errorf("Sweep removed the lock %s", rel)
		}
	}
	for _, rel := range gone {
		if exists(filepath.Join(s.Dir, filepath.FromSlash(rel))) {
			t.Errorf("Sweep kept the old file %s", rel)
		}
	}
}

// A decisions file that cannot be read to its end counts as unreadable; the
// decisions before the fault are read (second challenge, finding A13). A
// line longer than the reader takes is such a fault.
func TestDecisionsCountsAReadError(t *testing.T) {
	s := newStore(t)
	at := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	if err := s.Record(t.Context(), Decision{Time: at, Hook: "pre-commit", Mode: Enforce, Outcome: "allowed"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, "decisions", "2026-11.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(bytes.Repeat([]byte("x"), 2<<20), '\n')); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	decisions, unreadable, err := s.Decisions(time.Time{})
	if err != nil {
		t.Fatalf("Decisions: %v", err)
	}
	if len(decisions) != 1 || unreadable != 1 {
		t.Errorf("Decisions = %d decisions and %d unreadable, want 1 and 1", len(decisions), unreadable)
	}
}

// WriteFile gives the file the mode asked for, replaces an earlier file
// whole, and leaves no temporary file behind when it fails.
func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gatecub.toml")
	for _, mode := range []fs.FileMode{0o600, 0o644, 0o755} {
		data := []byte("mode " + mode.String() + "\n")
		if err := WriteFile(path, data, mode); err != nil {
			t.Fatalf("WriteFile with mode %v: %v", mode, err)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("after WriteFile with mode %v the file holds %q, %v; want %q", mode, got, err, data)
		}
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(path); err != nil {
				t.Error(err)
			} else if info.Mode().Perm() != mode {
				t.Errorf("after WriteFile with mode %v the mode is %v", mode, info.Mode().Perm())
			}
		}
	}
	assertNoTemporary(t, dir)

	// The rename fails: a folder that holds a file has the name.
	blocked := filepath.Join(dir, "blocked")
	writeFile(t, filepath.Join(blocked, "inside.txt"), []byte("x"))
	if err := WriteFile(blocked, []byte("data"), 0o600); err == nil {
		t.Error("WriteFile over a folder that holds a file gave no error")
	}
	assertNoTemporary(t, dir)

	// The temporary file cannot be created: the folder does not exist.
	if err := WriteFile(filepath.Join(dir, "missing", "f"), []byte("data"), 0o600); err == nil {
		t.Error("WriteFile into a folder that does not exist gave no error")
	}
	assertNoTemporary(t, dir)
}

func assertNoTemporary(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Errorf("a temporary file is left behind: %s", entry.Name())
		}
	}
}

// The store is private to the user: its folders are created 0700, and the
// records, the decisions and the mode 0600 (second challenge, finding S10).
// The runner creates the logs 0600; its own test checks that.
func TestStoreIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX file modes")
	}
	s := newStore(t)
	if err := s.SetMode(Enforce); err != nil {
		t.Fatal(err)
	}
	rec := newRecord(t, "unit", "k1")
	run := begin(t, s, rec)
	finish(t, run, ended(Passed, 0, start.Add(time.Minute)))
	if err := s.Record(t.Context(), Decision{Time: start, Hook: "pre-commit", Mode: Enforce, Outcome: "allowed"}); err != nil {
		t.Fatal(err)
	}

	err := filepath.WalkDir(s.Dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.Dir, path)
		switch {
		case entry.IsDir():
			if info.Mode().Perm() != 0o700 {
				t.Errorf("the folder %s has mode %v, want 0700", rel, info.Mode().Perm())
			}
		case strings.HasSuffix(rel, ".json"), strings.HasSuffix(rel, ".jsonl"), rel == "mode":
			if info.Mode().Perm() != 0o600 {
				t.Errorf("the file %s has mode %v, want 0600", rel, info.Mode().Perm())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"mode", "logs", "decisions/" + start.Format("2006-01") + ".jsonl"} {
		if !exists(filepath.Join(s.Dir, filepath.FromSlash(rel))) {
			t.Errorf("%s was not written", rel)
		}
	}
	if records, _, err := s.Records("unit", "k1"); err != nil || len(records) != 1 {
		t.Errorf("Records = %v, %v; want the one record", records, err)
	}
}
