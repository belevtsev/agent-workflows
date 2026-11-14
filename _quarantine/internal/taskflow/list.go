package taskflow

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// States of a task that List reports.
const (
	// StateOpen is a readable task that is neither verified, abandoned nor
	// stale.
	StateOpen = "open"
	// StateVerified is a task whose latest cw task verify passed. Its source
	// may have changed since; the commit gate checks that, List does not.
	StateVerified = "verified"
	// StateAbandoned is a task closed with cw task abandon.
	StateAbandoned = "abandoned"
	// StateStale is an unabandoned task whose latest revision is older than
	// MaxOpenAge, so it no longer gates publication.
	StateStale = "stale"
	// StateUnreadable is a task directory Load refuses.
	StateUnreadable = "unreadable"
)

// Summary is one task in a store as List reports it. For an unreadable task
// the plan, root and update time are those of its newest revision that
// decodes, unvalidated, and empty when none does.
type Summary struct {
	ID        string    `json:"task_id"`
	Directory string    `json:"directory"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	Root      string    `json:"root"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	State     string    `json:"state"`
	// Error is why Load refuses an unreadable task, and empty otherwise.
	Error string `json:"error,omitempty"`
}

// List returns the tasks in store, newest latest revision first (then by ID).
// With roots, it returns only the tasks whose baseline root is one of them,
// judged from each task's newest revision that decodes, so a task with no
// such revision is left out; with nil roots it returns every task directory
// in the store, one whose name is a task ID. A store that does not exist is
// empty.
//
// A task's state is, in this order: unreadable when Load refuses it;
// abandoned; stale when its latest revision is older than MaxOpenAge at now;
// verified when its latest verification passed; else open.
func List(store string, roots []string, now time.Time) ([]Summary, error) {
	entries, err := os.ReadDir(store)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []Summary
	for _, entry := range entries {
		if !entry.IsDir() || !IsTaskID(entry.Name()) {
			continue
		}
		directory := filepath.Join(store, entry.Name())
		peeked, readable := peekNewestReadable(directory)
		if roots != nil && (!readable || !slices.Contains(roots, peeked.root)) {
			continue
		}
		tasks = append(tasks, summarize(directory, peeked, now))
	}
	slices.SortStableFunc(tasks, func(a, b Summary) int {
		if order := b.UpdatedAt.Compare(a.UpdatedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return tasks, nil
}

// summarize loads the task in directory, whose newest decodable revision is
// peeked, and reports its state.
func summarize(directory string, peeked head, now time.Time) Summary {
	record, err := Load(directory)
	if err != nil {
		return Summary{ID: filepath.Base(directory), Directory: directory, Title: peeked.title, Kind: peeked.kind,
			Root: peeked.root, UpdatedAt: peeked.updatedAt, State: StateUnreadable, Error: err.Error()}
	}
	value := Summary{ID: record.ID, Directory: record.Directory, Title: record.Plan.Title, Kind: record.Plan.Kind,
		Root: record.Baseline.Root, UpdatedAt: record.UpdatedAt, State: StateOpen}
	switch {
	case record.Abandonment != nil:
		value.State = StateAbandoned
	case now.Sub(record.UpdatedAt) > MaxOpenAge:
		value.State = StateStale
	case record.Verification != nil && record.Verification.Complete:
		value.State = StateVerified
	}
	return value
}
