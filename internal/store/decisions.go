package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
)

// The causes that prevent a hook from deciding.
const (
	CauseNoRefUpdates       = "no_ref_updates"
	CauseNoDeclaration      = "no_declaration"
	CauseInvalidDeclaration = "invalid_declaration"
	CauseTree               = "tree"
	CauseStore              = "store"
	CauseInternal           = "internal"
)

// Decision is one decision of a hook.
type Decision struct {
	Time    time.Time         `json:"time"`
	Hook    string            `json:"hook"`
	Mode    string            `json:"mode"`
	Tree    string            `json:"tree,omitempty"`
	Outcome string            `json:"outcome"` // "allowed" or "denied"
	Ref     string            `json:"ref,omitempty"`
	Checks  map[string]string `json:"checks,omitempty"` // check id to state, for the checks that were not satisfied
	Cause   string            `json:"cause,omitempty"`
}

// Record appends a decision to the file of its month. One line is one
// decision; a lock keeps two hooks from writing into each other's line.
func (s *Store) Record(ctx context.Context, d Decision) error {
	dir := filepath.Join(s.Dir, "decisions")
	var lock *fslock.Lock
	err := inDir(dir, func() (err error) {
		lock, err = fslock.Acquire(ctx, filepath.Join(dir, "write.lock"), fslock.Options{Wait: 5 * time.Second})
		return err
	})
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	line, err := json.Marshal(d)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, d.Time.UTC().Format("2006-01")+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Decisions reads the decisions made at or after since. A line that cannot
// be read is counted and skipped.
func (s *Store) Decisions(since time.Time) (decisions []Decision, unreadable int, err error) {
	dir := filepath.Join(s.Dir, "decisions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	first := since.UTC().Format("2006-01")
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.TrimSuffix(name, ".jsonl") < first {
			continue
		}
		file, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return nil, 0, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(nil, 1<<20)
		for scanner.Scan() {
			var d Decision
			if json.Unmarshal(scanner.Bytes(), &d) != nil || d.Hook == "" {
				unreadable++
				continue
			}
			if !d.Time.Before(since) {
				decisions = append(decisions, d)
			}
		}
		if scanner.Err() != nil {
			unreadable++ // the rest of the file could not be read
		}
		_ = file.Close()
	}
	return decisions, unreadable, nil
}

// Sweep removes what is too old to matter: logs of runs that ended more
// than 30 days ago, records and decisions older than 365 days, and index
// copies a dead kitsu left behind. A sweep that is already running is left to
// finish; nothing waits for it.
func (s *Store) Sweep(ctx context.Context, now time.Time) {
	if _, err := os.Stat(s.Dir); err != nil {
		return
	}
	lock, err := fslock.Acquire(ctx, filepath.Join(s.Dir, "gc.lock"), fslock.Options{Wait: fslock.NoWait})
	if err != nil {
		return
	}
	defer func() { _ = lock.Close() }()
	removeOlder(filepath.Join(s.Dir, "logs"), now.Add(-LogAge), false)
	removeOlder(filepath.Join(s.Dir, "tmp"), now.Add(-tmpAge), false)
	removeOlder(filepath.Join(s.Dir, "decisions"), now.Add(-RecordAge), false)
	removeOlder(filepath.Join(s.Dir, "runs"), now.Add(-RecordAge), true)
}

// removeOlder removes the files under dir that were last written before
// limit, and with prune the directories this leaves empty. A lock that
// processes share stays: taking it does not change its time, and a second
// process could lock a new file of the same name.
func removeOlder(dir string, limit time.Time, prune bool) {
	var dirs []string
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != dir {
				dirs = append(dirs, path)
			}
			return nil
		}
		if info, err := entry.Info(); err == nil && info.ModTime().Before(limit) && entry.Name() != "seq.lock" && entry.Name() != "write.lock" {
			// An index copy carries the time of the index it was made from. The file its process holds next to it tells its age.
			if hold, err := os.Stat(path + ".hold"); err != nil || hold.ModTime().Before(limit) {
				_ = os.Remove(path)
			}
		}
		return nil
	})
	if !prune {
		return
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // fails, as it should, when the directory still holds something
	}
}
