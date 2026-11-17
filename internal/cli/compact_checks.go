package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

type compactCheckOutcome struct {
	ID      string        `json:"id"`
	State   string        `json:"state"`
	Skipped bool          `json:"skipped"`
	Implied bool          `json:"implied,omitempty"`
	Record  *store.Record `json:"record,omitempty"`
}

type compactCheckMeta struct {
	Checks      []compactCheckOutcome `json:"checks"`
	Warnings    []string              `json:"warnings"`
	Verified    bool                  `json:"verified"`
	Missing     []string              `json:"missing"`
	Error       string                `json:"error,omitempty"`
	EvidenceKey string                `json:"evidence_key"`
	ExpiresAt   time.Time             `json:"evidence_expires_at"`
	LogEncoding string                `json:"log_text_encoding"`
}

type compactCheckCapture struct {
	compactCheckMeta
	current int
}

func newCompactCapture(ids []string) *compactCheckCapture {
	c := &compactCheckCapture{compactCheckMeta: compactCheckMeta{Checks: []compactCheckOutcome{}, Warnings: []string{}, Missing: []string{}, LogEncoding: "UTF-8 with replacement for invalid tool bytes; original bytes remain in the retained log"}}
	for _, id := range ids {
		c.Checks = append(c.Checks, compactCheckOutcome{ID: id, State: "not_attempted"})
	}
	return c
}

func (c *compactCheckCapture) begin(check *evidence.Check, force bool) {
	skipped := check.State == evidence.Satisfied && !force
	c.current = slices.IndexFunc(c.Checks, func(outcome compactCheckOutcome) bool { return outcome.ID == check.ID })
	current := compactCheckOutcome{ID: check.ID, State: "not_started", Skipped: skipped}
	if skipped {
		current.State, current.Implied, current.Record = check.State, check.Implied, check.Deciding
	}
	c.Checks[c.current] = current
}

func (c *compactCheckCapture) finish(record store.Record) {
	current := &c.Checks[c.current]
	current.Record, current.State = &record, record.Outcome
	current.Implied = false
}

func compactEvidenceKey(s *session) (string, error) {
	records, damaged, err := s.store.AllRecords()
	if err != nil {
		return "", err
	}
	issues := make([]string, 0, len(damaged))
	for _, d := range damaged {
		issues = append(issues, fmt.Sprintf("%s: %v", d.Path, d.Err))
	}
	return contextpack.Key(struct {
		Records []store.Record
		Damaged []string
	}{records, issues}), nil
}

func runCompactChecks(ctx context.Context, s *session, ids []string, force bool, base *evidence.Base, budget int) error {
	e, err := contextpack.Open(s.repo)
	if err != nil {
		return failf("%v", err)
	}
	inner := *s
	inner.cfg.Out, inner.cfg.Err = io.Discard, io.Discard
	inner.compact = newCompactCapture(ids)
	s.store.Sweep(ctx, time.Now())
	runErr := inner.executeCompactChecks(ctx, ids, force, base)
	state, stateErr := e.Snapshot(context.WithoutCancel(ctx))
	if stateErr != nil {
		return failf("checks recorded, but repository cannot be read: %v", stateErr)
	}
	inner.compact.EvidenceKey, err = compactEvidenceKey(&inner)
	if err != nil {
		return failf("%v", err)
	}
	verdict, judgeErr := inner.judgeWorktree(ctx, base)
	if judgeErr == nil && verdict.Tree != state.Tree {
		return failf("source changed while preparing check report")
	}
	if judgeErr != nil && runErr == nil {
		runErr = failf("%v", judgeErr)
	}
	if judgeErr != nil {
		inner.compact.Error = judgeErr.Error()
	}
	if judgeErr == nil {
		inner.compact.observeVerdict(verdict)
	}
	if runErr != nil && runErr.Error() != "" {
		inner.compact.Error = runErr.Error()
	}
	// The runner has already retained interrupted-run records. Finish local reporting even on cancellation.
	a, err := compactCheckArtifact(context.WithoutCancel(ctx), &inner, e, state, budget)
	if err != nil {
		return failf("checks recorded, but compact report unavailable: %v", err)
	}
	if err := validateCompactMetadata(ctx, s, a); err != nil {
		return failf("checks recorded, but report became stale: %v", err)
	}
	if err := printCompact(s.cfg, a, "", "", budget, false); err != nil {
		return err
	}
	return runErr
}

func (s *session) executeCompactChecks(ctx context.Context, ids []string, force bool, base *evidence.Base) error {
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		passed, err := s.runOne(ctx, id, force, base)
		if err != nil {
			return err
		}
		if !passed {
			failed++
		}
	}
	if failed > 0 {
		return exit(ExitNotVerified)
	}
	return nil
}

func compactCheckArtifact(ctx context.Context, s *session, e *contextpack.Engine, state contextpack.State, budget int) (contextpack.Artifact, error) {
	a := contextpack.Artifact{Kind: "checks", State: state, Items: []contextpack.Item{}}
	var err error
	slices.Sort(s.compact.Warnings)
	s.compact.Warnings = slices.Compact(s.compact.Warnings)
	a.Metadata, _ = json.Marshal(s.compact.compactCheckMeta)
	for _, check := range s.compact.Checks {
		item := contextpack.Item{ID: check.ID, Status: check.State}
		if check.Record != nil && !check.Implied {
			item.LogRunID = check.Record.RunID
			limit := budget
			if item.Status == "passed" || item.Status == "satisfied" {
				limit = 0
			}
			lines, total, err := compactLogCandidate(s, check.Record.RunID, limit)
			if err != nil {
				item.Status, item.Error = "partial", "retained log unavailable: "+err.Error()
			} else {
				item.LogChunks = total
				if limit > 0 && len(lines) == total {
					budget = max(0, budget-compactLogBytes(lines))
				}
				item.Lines = lines
			}
		}
		a.Items = append(a.Items, item)
	}
	// A concurrently completed check changes evidence even when it did not change source files.
	key, err := compactEvidenceKey(s)
	if err != nil || key != s.compact.EvidenceKey {
		return a, fmt.Errorf("check records changed while collecting logs: %v", err)
	}
	err = e.Save(ctx, &a)
	return a, err
}

func (c *compactCheckCapture) observeVerdict(verdict *evidence.Verdict) {
	c.Verified = verdict.Verified
	c.Warnings = append(c.Warnings, verdict.Warnings...)
	for _, check := range verdict.Missing() {
		c.Missing = append(c.Missing, check.ID)
	}
	for _, check := range verdict.Checks {
		if check.Deciding == nil || check.Deciding.EndedAt == nil {
			continue
		}
		expires := check.Deciding.EndedAt.Add(evidence.MaxAge)
		if c.ExpiresAt.IsZero() || expires.Before(c.ExpiresAt) {
			c.ExpiresAt = expires
		}
	}
}
