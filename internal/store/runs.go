package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/fslock"
)

// The outcomes of a run. The first three are decisive.
const (
	Passed        = "passed"
	Failed        = "failed"
	TimedOut      = "timed_out"
	SourceChanged = "source_changed"
	Interrupted   = "interrupted"
)

// Decisive says whether an outcome settles whether a check passes.
func Decisive(outcome string) bool {
	return outcome == Passed || outcome == Failed || outcome == TimedOut
}

// Record is one run of one check.
type Record struct {
	RunID      string     `json:"run_id"`
	ID         string     `json:"id"`
	Run        []string   `json:"run"`
	Paths      []string   `json:"paths"`
	Tree       string     `json:"tree"`
	ContentKey string     `json:"content_key"`
	Seq        int64      `json:"seq,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	ExitStatus *int       `json:"exit_status,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Log        string     `json:"log"`
	Differ     []string   `json:"differ,omitempty"`
	DifferMore int        `json:"differ_more,omitempty"`
	Detail     string     `json:"detail,omitempty"`

	// Running is set when the record has no outcome and its cw process is
	// still alive. It is not stored.
	Running bool `json:"-"`
}

// Damaged is a record file that cannot be read.
type Damaged struct {
	Path string
	Err  error
}

// NewRunID names a run by its start time and eight random hex digits.
func NewRunID(start time.Time) (string, error) {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return start.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random[:]), nil
}

func (s *Store) keyDir(checkID, key string) string {
	return filepath.Join(s.Dir, "runs", checkID, key)
}

// Run is a run that is executing: its lock is held until Finish.
type Run struct {
	store  *Store
	record Record
	lock   *fslock.Lock
}

// Begin takes the lock of a new run and then writes its record, without an
// outcome. The order matters: a record whose lock is free belongs to a cw
// process that died.
func (s *Store) Begin(ctx context.Context, record Record) (*Run, error) {
	dir := s.keyDir(record.ID, record.ContentKey)
	var lock *fslock.Lock
	err := inDir(dir, func() (err error) {
		lock, err = fslock.Acquire(ctx, filepath.Join(dir, record.RunID+".lock"), fslock.Options{Wait: fslock.NoWait})
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, "logs"), 0o755); err != nil {
		_ = lock.Close()
		return nil, err
	}
	record.Log = "logs/" + record.RunID + ".log"
	run := &Run{store: s, record: record, lock: lock}
	if err := run.write(); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return run, nil
}

// inDir creates the directory and then does the work in it. A command that
// only reads removes the directories it leaves empty, and the sweep removes
// empty directories of old runs; when that happens between the two steps
// they are repeated.
func inDir(dir string, work func() error) error {
	var err error
	for range 5 {
		if err = os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err = work(); !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return err
}

// Record is the record as it was last written.
func (r *Run) Record() Record { return r.record }

func (r *Run) path() string {
	return filepath.Join(r.store.keyDir(r.record.ID, r.record.ContentKey), r.record.RunID+".json")
}

func (r *Run) write() error {
	data, err := json.MarshalIndent(r.record, "", "  ")
	if err != nil {
		return err
	}
	return writeSynced(r.path(), append(data, '\n'))
}

// Finish records how the run ended and releases its lock. Under the lock of
// the directory the record gets the next sequence number, which is the order
// in which ends were recorded and does not depend on the clock. A decisive
// end also sets aside the damaged records of this checked content.
func (r *Run) Finish(ctx context.Context, update func(*Record)) (Record, error) {
	defer r.release()
	dir := r.store.keyDir(r.record.ID, r.record.ContentKey)
	order, err := fslock.Acquire(ctx, filepath.Join(dir, "seq.lock"), fslock.Options{Wait: 30 * time.Second})
	if err != nil {
		return r.record, fmt.Errorf("the end of the run cannot be recorded: %w", err)
	}
	defer func() { _ = order.Close() }()
	records, damaged, err := r.store.Records(r.record.ID, r.record.ContentKey)
	if err != nil {
		return r.record, err
	}
	var highest int64
	for _, rec := range records {
		highest = max(highest, rec.Seq)
	}
	update(&r.record)
	r.record.Seq = highest + 1
	if err := r.write(); err != nil {
		return r.record, err
	}
	if Decisive(r.record.Outcome) {
		for _, d := range damaged {
			_ = os.Rename(d.Path, d.Path+".damaged")
		}
	}
	return r.record, nil
}

// release gives up the lock of the run and removes its file. A lock file
// is as a rule never removed, because a second process could then lock a
// new file of the same name. Here the name belongs to this run alone, and a
// reader that finds the lock free or gone reads the record again before it
// calls the run interrupted.
func (r *Run) release() {
	_ = r.lock.Close()
	_ = os.Remove(filepath.Join(r.store.keyDir(r.record.ID, r.record.ContentKey), r.record.RunID+".lock"))
}

// Records reads the runs of one check on one checked content. A file that
// cannot be read is returned as damaged. A missing directory holds no runs.
func (s *Store) Records(checkID, key string) ([]Record, []Damaged, error) {
	dir := s.keyDir(checkID, key)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		// The directory may be absent because nothing ran, or unreachable
		// because the store itself cannot be read.
		if _, statErr := os.Stat(s.Dir); statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return nil, nil, statErr
		}
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var records []Record
	var damaged []Damaged
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		record, err := readRecord(path)
		if err != nil {
			damaged = append(damaged, Damaged{Path: path, Err: err})
			continue
		}
		records = append(records, record)
	}
	return records, damaged, nil
}

// readRecord reads one record. A record without an outcome is running when
// its lock is held and interrupted when it is not.
func readRecord(path string) (Record, error) {
	record, err := decodeRecord(path)
	if err != nil || record.Outcome != "" {
		return record, err
	}
	lockPath := strings.TrimSuffix(path, ".json") + ".lock"
	probe, lockErr := fslock.Acquire(context.Background(), lockPath, fslock.Options{Shared: true, NoCreate: true, Wait: fslock.NoWait})
	switch {
	case lockErr == nil:
		_ = probe.Close()
		record.Outcome = Interrupted
	case errors.Is(lockErr, fslock.ErrBusy):
		record.Running = true
	default:
		// The lock file is gone: the run ended between the two reads.
		if again, err := decodeRecord(path); err == nil && again.Outcome != "" {
			return again, nil
		}
		record.Outcome = Interrupted
	}
	return record, nil
}

func decodeRecord(path string) (Record, error) {
	var record Record
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("not a run record: %w", err)
	}
	if record.RunID == "" || record.ID == "" || record.ContentKey == "" || len(record.Run) == 0 {
		return record, errors.New("not a run record: a required field is missing")
	}
	if record.Outcome != "" && record.EndedAt == nil {
		return record, errors.New("not a run record: an outcome without an end time")
	}
	return record, nil
}

// AllRecords reads every run of the repository, for the statistics.
func (s *Store) AllRecords() ([]Record, []Damaged, error) {
	checks, err := os.ReadDir(filepath.Join(s.Dir, "runs"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var records []Record
	var damaged []Damaged
	for _, check := range checks {
		keys, err := os.ReadDir(filepath.Join(s.Dir, "runs", check.Name()))
		if err != nil {
			continue
		}
		for _, key := range keys {
			found, bad, err := s.Records(check.Name(), key.Name())
			if err != nil {
				return nil, nil, err
			}
			records = append(records, found...)
			damaged = append(damaged, bad...)
		}
	}
	return records, damaged, nil
}
