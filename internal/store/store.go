// Package store keeps what cw records: runs, their logs, hook decisions and
// the mode. Everything is under cw/ in the common git directory, which all
// linked working trees of a repository share.
package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// The two modes of the gate.
const (
	Enforce = "enforce"
	Observe = "observe"
)

// The ages after which cw run removes what it recorded.
const (
	LogAge    = 30 * 24 * time.Hour
	RecordAge = 365 * 24 * time.Hour
	tmpAge    = 24 * time.Hour
)

// Store is the directory cw/ of one repository.
type Store struct {
	Dir string
}

// Open names the store of the repository with the given common git
// directory. Nothing is created until something is written.
func Open(commonDir string) *Store {
	return &Store{Dir: filepath.Join(commonDir, "cw")}
}

// Tmp is where copies of the index live while git works on them.
func (s *Store) Tmp() string { return filepath.Join(s.Dir, "tmp") }

// LogPath is the file that holds the output of a run.
func (s *Store) LogPath(runID string) string {
	return filepath.Join(s.Dir, "logs", runID+".log")
}

// Abs turns a path a record holds, which is relative to the store, into a
// path a person can open.
func (s *Store) Abs(rel string) string {
	if rel == "" || filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(s.Dir, filepath.FromSlash(rel))
}

// Mode reads the mode. The gate observes only when the file holds exactly
// that word; anything else, and no file, is enforce. An error means the
// store cannot be read, and a hook then denies.
func (s *Store) Mode() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "mode"))
	if errors.Is(err, fs.ErrNotExist) {
		if _, statErr := os.Stat(s.Dir); statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return Enforce, statErr
		}
		return Enforce, nil
	}
	if err != nil {
		return Enforce, err
	}
	if bytes.Equal(bytes.TrimSuffix(data, []byte("\n")), []byte(Observe)) {
		return Observe, nil
	}
	return Enforce, nil
}

// SetMode writes the mode.
func (s *Store) SetMode(mode string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return writeSynced(filepath.Join(s.Dir, "mode"), []byte(mode+"\n"))
}

// Remove deletes the whole store.
func (s *Store) Remove() error { return os.RemoveAll(s.Dir) }

// writeSynced writes a file so that it is whole on disk or absent: a
// temporary file is written and synced, renamed into place, and the
// directory is synced.
func writeSynced(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		return err
	}
	syncDir(filepath.Dir(path))
	return nil
}

// syncDir makes a rename in the directory durable where the system can.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // not supported everywhere; the file itself is synced
		_ = d.Close()
	}
}
