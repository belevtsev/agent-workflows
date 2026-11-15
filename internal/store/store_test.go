package store

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return Open(t.TempDir())
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestOpenAndPaths(t *testing.T) {
	common := t.TempDir()
	s := Open(common)
	if s.Dir != filepath.Join(common, "kitsu") {
		t.Errorf("Dir = %q", s.Dir)
	}
	if exists(s.Dir) {
		t.Error("Open created the store")
	}
	if s.Tmp() != filepath.Join(s.Dir, "tmp") {
		t.Errorf("Tmp = %q", s.Tmp())
	}
	if s.LogPath("r1") != filepath.Join(s.Dir, "logs", "r1.log") {
		t.Errorf("LogPath = %q", s.LogPath("r1"))
	}
	if got := s.Abs("logs/r1.log"); got != filepath.Join(s.Dir, "logs", "r1.log") {
		t.Errorf("Abs = %q", got)
	}
	if got := s.Abs(""); got != "" {
		t.Errorf("Abs(\"\") = %q", got)
	}
	abs := filepath.Join(common, "x.log")
	if got := s.Abs(abs); got != abs {
		t.Errorf("Abs(abs) = %q", got)
	}
}

func TestModeWithoutFile(t *testing.T) {
	s := newStore(t)
	mode, err := s.Mode()
	if err != nil || mode != Enforce {
		t.Errorf("Mode() = %q, %v; want enforce", mode, err)
	}
	if exists(s.Dir) {
		t.Error("Mode created the store")
	}
}

func TestModeContents(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"observe", Observe},
		{"observe\n", Observe},
		{"enforce\n", Enforce},
		{"", Enforce},
		{"Observe\n", Enforce},
		{" observe\n", Enforce},
		{"observe \n", Enforce},
		{"observe\n\n", Enforce},
		{"observed\n", Enforce},
		{"obs", Enforce},
		{"anything", Enforce},
	}
	for _, tc := range cases {
		s := newStore(t)
		writeFile(t, filepath.Join(s.Dir, "mode"), []byte(tc.content))
		mode, err := s.Mode()
		if err != nil || mode != tc.want {
			t.Errorf("Mode() with %q = %q, %v; want %q", tc.content, mode, err, tc.want)
		}
	}
}

func TestSetMode(t *testing.T) {
	s := newStore(t)
	for _, mode := range []string{Observe, Enforce, Observe} {
		if err := s.SetMode(mode); err != nil {
			t.Fatalf("SetMode(%q): %v", mode, err)
		}
		got, err := s.Mode()
		if err != nil || got != mode {
			t.Errorf("after SetMode(%q), Mode() = %q, %v", mode, got, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "mode"))
	if err != nil || string(data) != "observe\n" {
		t.Errorf("mode file = %q, %v", data, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(s.Dir, ".tmp-*"))
	if len(leftovers) > 0 {
		t.Errorf("temporary files left: %v", leftovers)
	}
}

func TestModeStoreUnreadable(t *testing.T) {
	common := t.TempDir()
	// The store is a file, so the mode under it cannot be read.
	writeFile(t, filepath.Join(common, "kitsu"), []byte("x"))
	mode, err := Open(common).Mode()
	if err == nil {
		t.Errorf("Mode() = %q with no error for a store that is a file", mode)
	}
	if mode != Enforce {
		t.Errorf("Mode() = %q, want enforce with the error", mode)
	}
}

func TestRemove(t *testing.T) {
	s := newStore(t)
	if err := s.SetMode(Observe); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(); err != nil {
		t.Fatal(err)
	}
	if exists(s.Dir) {
		t.Error("the store is still there")
	}
}

func TestNewRunID(t *testing.T) {
	start := time.Date(2026, 11, 14, 15, 30, 0, 0, time.FixedZone("x", 3600))
	id, err := NewRunID(start)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^20261114T143000Z-[0-9a-f]{8}$`).MatchString(id) {
		t.Errorf("NewRunID = %q", id)
	}
	other, _ := NewRunID(start)
	if other == id {
		t.Errorf("two run ids are equal: %q", id)
	}
}

func TestDecisive(t *testing.T) {
	for _, o := range []string{Passed, Failed, TimedOut} {
		if !Decisive(o) {
			t.Errorf("Decisive(%q) = false", o)
		}
	}
	for _, o := range []string{SourceChanged, Interrupted, ""} {
		if Decisive(o) {
			t.Errorf("Decisive(%q) = true", o)
		}
	}
}
