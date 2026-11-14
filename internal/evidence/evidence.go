// Package evidence decides, from recorded runs alone, whether a check is
// satisfied for a tree and whether a tree is verified.
package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"slices"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/gitx"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
)

// The states of a check for a tree.
const (
	Satisfied = "satisfied"
	NotRun    = "not run"
	Failed    = "failed"
	TimedOut  = "timed out"
)

// ErrNoDeclaration is a tree without cw.toml at its root.
var ErrNoDeclaration = errors.New("the tree has no declaration: there is no " + decl.FileName + " at its root; `cw init` creates one")

// StoreError is a store that cannot be read.
type StoreError struct {
	Dir string
	Err error
}

func (e *StoreError) Error() string {
	return fmt.Sprintf("the records in %s cannot be read: %v", e.Dir, e.Err)
}

func (e *StoreError) Unwrap() error { return e.Err }

// Check is one declared check and its state for the tree.
type Check struct {
	decl.Check
	Key   string
	State string
	// Deciding is the run that decides the state, when one does.
	Deciding *store.Record
	// Last is the most recent run on this content that decides nothing: one
	// that was interrupted or changed the source.
	Last *store.Record
	// Running says that a run on this content is executing now.
	Running bool
	// Implied says that the check counts as passed on the base revision.
	Implied bool
}

// Verdict is the judgement of one tree.
type Verdict struct {
	Tree     string
	Checks   []Check
	Verified bool
	Warnings []string
}

// Missing lists the checks that are not satisfied.
func (v *Verdict) Missing() []Check {
	var missing []Check
	for _, c := range v.Checks {
		if c.State != Satisfied {
			missing = append(missing, c)
		}
	}
	return missing
}

// Declaration reads the declaration of a tree. The error is
// ErrNoDeclaration, a *decl.Error, or what git reported.
func Declaration(ctx context.Context, repo *gitx.Repo, tree string) (*decl.File, error) {
	data, found, err := repo.Declaration(ctx, tree, decl.FileName)
	switch {
	case !found && err == nil:
		return nil, ErrNoDeclaration
	case found && err != nil:
		return nil, &decl.Error{Problems: []decl.Problem{{Text: err.Error()}}}
	case err != nil:
		return nil, err
	}
	return decl.Parse(data)
}

// Keys computes the content key of each check for a tree. Two trees hold the
// same checked content for a check exactly when the keys are equal. A check
// without paths depends on everything, so its key is the tree itself. With
// paths, the key is the SHA-256 of the tree's raw entries whose path matches.
func Keys(ctx context.Context, repo *gitx.Repo, tree string, checks []decl.Check) (map[string]string, error) {
	keys := make(map[string]string, len(checks))
	hashers := map[string]hash.Hash{}
	for _, c := range checks {
		if len(c.Paths) == 0 {
			keys[c.ID] = tree
		} else {
			hashers[c.ID] = sha256.New()
		}
	}
	if len(hashers) == 0 {
		return keys, nil
	}
	err := repo.Entries(ctx, tree, func(raw []byte, path string) {
		for _, c := range checks {
			if h, ok := hashers[c.ID]; ok && decl.MatchAny(c.Paths, path) {
				h.Write(raw)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	for id, h := range hashers {
		keys[id] = hex.EncodeToString(h.Sum(nil))
	}
	return keys, nil
}

// Base holds the checks a base revision declares and their content keys.
// `cw run --base` counts such a check as passed on that content.
type Base struct {
	checks map[string]Check
}

// LoadBase reads the declaration of the tree of a base revision. A base
// without a valid declaration implies nothing.
func LoadBase(ctx context.Context, repo *gitx.Repo, rev string) (*Base, error) {
	tree, found, err := repo.TreeOf(ctx, rev)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("the base revision `%s` names no tree in this repository", rev)
	}
	base := &Base{checks: map[string]Check{}}
	file, err := Declaration(ctx, repo, tree)
	if err != nil {
		var invalid *decl.Error
		if errors.Is(err, ErrNoDeclaration) || errors.As(err, &invalid) {
			return base, nil
		}
		return nil, err
	}
	keys, err := Keys(ctx, repo, tree, file.Checks)
	if err != nil {
		return nil, err
	}
	for _, c := range file.Checks {
		base.checks[c.ID] = Check{Check: c, Key: keys[c.ID]}
	}
	return base, nil
}

func (b *Base) implies(c decl.Check, key string) bool {
	if b == nil {
		return false
	}
	at, ok := b.checks[c.ID]
	return ok && at.Key == key && slices.Equal(at.Run, c.Run) && slices.Equal(at.Paths, c.Paths)
}

// Judge decides the state of every check a tree declares. base may be nil.
func Judge(ctx context.Context, repo *gitx.Repo, st *store.Store, tree string, now time.Time, base *Base) (*Verdict, error) {
	file, err := Declaration(ctx, repo, tree)
	if err != nil {
		return nil, err
	}
	keys, err := Keys(ctx, repo, tree, file.Checks)
	if err != nil {
		return nil, err
	}
	verdict := &Verdict{Tree: tree, Verified: true}
	for _, c := range file.Checks {
		check := Check{Check: c, Key: keys[c.ID], State: NotRun}
		records, damaged, err := st.Records(c.ID, check.Key)
		if err != nil {
			return nil, &StoreError{Dir: st.Dir, Err: err}
		}
		warnings := check.decide(records, now)
		for _, d := range damaged {
			check.State, check.Deciding = NotRun, nil
			warnings = append(warnings, fmt.Sprintf("the record %s cannot be read (%v); `%s` does not count as run on this content until it runs again", d.Path, d.Err, c.ID))
		}
		if check.State == NotRun && check.Deciding == nil && len(damaged) == 0 && len(warnings) == 0 && base.implies(c, check.Key) {
			check.State, check.Implied = Satisfied, true
		}
		verdict.Warnings = append(verdict.Warnings, warnings...)
		verdict.Verified = verdict.Verified && check.State == Satisfied
		verdict.Checks = append(verdict.Checks, check)
	}
	return verdict, nil
}

// decide sets the state from the records of the check's content. Among the
// decisive runs with the same command and paths, the one whose end was
// recorded last is the most recent; of two with the same sequence number,
// the one that did not pass. It decides when it ended within the last 30
// days and not later than now.
func (c *Check) decide(records []store.Record, now time.Time) []string {
	var deciding, last *store.Record
	for i := range records {
		r := &records[i]
		if !slices.Equal(r.Run, c.Run) || !slices.Equal(r.Paths, c.Paths) {
			continue
		}
		switch {
		case r.Running:
			c.Running = true
		case !store.Decisive(r.Outcome):
			if last == nil || r.Seq > last.Seq {
				last = r
			}
		case deciding == nil || r.Seq > deciding.Seq || (r.Seq == deciding.Seq && r.Outcome != store.Passed):
			deciding = r
		}
	}
	if deciding == nil || (last != nil && last.Seq > deciding.Seq) {
		c.Last = last
	}
	if deciding == nil {
		return nil
	}
	if deciding.EndedAt.After(now) {
		return []string{fmt.Sprintf("run %s of `%s` is dated in the future (%s); it does not count", deciding.RunID, c.ID, deciding.EndedAt.Format(time.RFC3339))}
	}
	if now.Sub(*deciding.EndedAt) > store.LogAge {
		return nil // too old to satisfy
	}
	c.Deciding = deciding
	switch deciding.Outcome {
	case store.Passed:
		c.State = Satisfied
	case store.Failed:
		c.State = Failed
	case store.TimedOut:
		c.State = TimedOut
	}
	return nil
}
