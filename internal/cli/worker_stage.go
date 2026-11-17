package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

type stageDispatch struct {
	workerDispatch
	DispatchID string `json:"dispatch_id"`
}

type stageCheck struct {
	ID    string `json:"id"`
	State string `json:"state"`
	RunID string `json:"run_id,omitempty"`
}

type stageReceipt struct {
	Version       int               `json:"version"`
	ID            string            `json:"receipt_id"`
	DispatchID    string            `json:"dispatch_id"`
	Task          string            `json:"task"`
	Status        string            `json:"status"`
	Created       time.Time         `json:"created_at"`
	State         contextpack.State `json:"state"`
	Policy        string            `json:"policy_key"`
	EvidenceKey   string            `json:"evidence_key"`
	ExpectChecks  string            `json:"expect_checks,omitempty"`
	Writes        bool              `json:"write_bounds_validated"`
	Verified      bool              `json:"checks_verified"`
	Checks        []stageCheck      `json:"checks"`
	Missing       []string          `json:"missing"`
	Warnings      []string          `json:"warnings"`
	EvidenceError string            `json:"evidence_error,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	Scope         string            `json:"scope"`
}

const stageReceiptScope = "Local write bounds and existing check evidence only; no model execution, skill loading, semantic TDD, independent review, human approval or task completion is certified. Hooks and the normal gate remain authoritative."

func runTaskPrepare(ctx context.Context, cfg Config, args []string) error {
	b, assignmentPath, budget, err := parseStagePrepare(args)
	if err != nil {
		return err
	}
	assignment, err := readWorkerAssignment(cfg.In, assignmentPath)
	if err != nil {
		return err
	}
	s, _, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	ledger, err := openStageLedger(ctx, s)
	if err != nil {
		return compactFailure(err)
	}
	defer ledger.close()
	active, err := ledger.active()
	if err != nil {
		return compactFailure(err)
	}
	if active != "" {
		return failf("unfinished stage %s; finish or explicitly abandon it before preparing another worker", active)
	}
	if err := checkWorkerDispatchBudget(s, b, budget, 81); err != nil {
		return err
	}
	p, err := prepareWorkerBrief(ctx, cfg, b, assignment)
	if err != nil {
		return err
	}
	checkPolicy, err := stageCheckPolicy(ctx, s, p.State.Tree)
	if err != nil {
		return compactFailure(err)
	}
	sources, err := stageRawSources(ctx, s, p.State.Tree)
	if err != nil {
		return compactFailure(err)
	}
	record := stageRecord{Version: 1, Worktree: s.repo.Root, Nonce: rand.Text(), Created: time.Now().UTC(), Checks: checkPolicy, Sources: sources, Prepared: p}
	id := contextpack.Key(record)
	dispatch := stageDispatch{workerDispatch: p.Dispatch, DispatchID: id}
	data, err := boundedStageJSON(dispatch, budget)
	if err != nil {
		return err
	}
	if err := verifySavedWorkerBrief(ctx, cfg, p); err != nil {
		return compactFailure(err)
	}
	if err := ledger.write(id+".json", record); err != nil {
		return compactFailure(err)
	}
	if err := ledger.write("active.json", id); err != nil {
		return compactFailure(err)
	}
	_, err = cfg.Out.Write(data)
	return err
}

func parseStagePrepare(args []string) (workerBrief, string, int, error) {
	flags := newFlags("task prepare")
	b := workerBrief{}
	flags.StringVar(&b.Host, "host", "", "")
	flags.StringVar(&b.Role, "role", "", "")
	flags.StringVar(&b.Stage, "stage", "", "")
	flags.IntVar(&b.Round, "round", 0, "")
	flags.StringVar(&b.Workflow, "workflow", "full", "")
	assignment := flags.String("assignment", "", "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return b, "", 0, err
	}
	if !validBriefArgs(p, b) || *assignment == "" {
		return b, "", 0, usagef("prepare requires a task, --role, --stage, positive --round, --host, --workflow full|small and --assignment <file|->")
	}
	b.Task = p.pos[0]
	return b, *assignment, *budget, compactBudget(*budget)
}

type stageFinishOptions struct {
	id, expect, reason string
	abandon            bool
	budget             int
}

func parseStageFinish(args []string) (stageFinishOptions, error) {
	flags := newFlags("task finish")
	o := stageFinishOptions{}
	flags.StringVar(&o.expect, "expect-checks", "any", "")
	flags.BoolVar(&o.abandon, "abandon", false, "")
	flags.StringVar(&o.reason, "reason", "", "")
	flags.IntVar(&o.budget, "max-bytes", contextpack.DefaultBytes, "")
	flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return o, err
	}
	if p.dash || len(p.pos) != 1 || !stageValidID(p.pos[0]) || o.expect != "pass" && o.expect != "fail" && o.expect != "any" {
		return o, usagef("finish requires a dispatch id and optional --expect-checks pass|fail|any")
	}
	explicitExpectation := false
	flags.Visit(func(f *flag.Flag) { explicitExpectation = explicitExpectation || f.Name == "expect-checks" })
	if o.abandon && (explicitExpectation || strings.TrimSpace(o.reason) == "") || !o.abandon && o.reason != "" || len(o.reason) > 1024 || !utf8.ValidString(o.reason) || strings.ContainsRune(o.reason, 0) {
		return o, usagef("--abandon requires --reason (at most 1024 UTF-8 bytes) and cannot combine with --expect-checks")
	}
	o.id = p.pos[0]
	return o, compactBudget(o.budget)
}

func runTaskFinish(ctx context.Context, cfg Config, args []string) error {
	o, err := parseStageFinish(args)
	if err != nil {
		return err
	}
	s, e, err := compactEngine(ctx, cfg)
	if err != nil {
		return err
	}
	ledger, err := openStageLedger(ctx, s)
	if err != nil {
		return compactFailure(err)
	}
	defer ledger.close()
	record, err := ledger.record(o.id, s.repo.Root)
	if err != nil {
		return compactFailure(err)
	}
	if receipt, err := existingStageReceipt(ctx, s, e, ledger, record, o); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return compactFailure(err)
		}
		return printStageReceipt(cfg, receipt, o.budget)
	}
	active, err := ledger.active()
	if err != nil {
		return compactFailure(err)
	}
	if active != o.id {
		return failf("dispatch is not the active unfinished stage")
	}
	receipt, err := finishStageReceipt(ctx, s, e, record, o)
	if err != nil {
		return compactFailure(err)
	}
	data, err := boundedStageJSON(receipt, o.budget)
	if err != nil {
		return err
	}
	if err := verifyStageBeforePublication(ctx, s, e, record, receipt); err != nil {
		return compactFailure(err)
	}
	if err := ledger.write(stageReceiptName(o), receipt); err != nil {
		return compactFailure(err)
	}
	if err := ledger.root.Remove("active.json"); err != nil {
		return compactFailure(err)
	}
	_, err = cfg.Out.Write(data)
	return err
}

func finishStageReceipt(ctx context.Context, s *session, e *contextpack.Engine, record stageRecord, o stageFinishOptions) (stageReceipt, error) {
	p := record.Prepared
	r := stageReceipt{Version: 1, DispatchID: o.id, Task: p.Brief.Task, Created: time.Now().UTC(), Checks: []stageCheck{}, Missing: []string{}, Warnings: []string{}, Scope: stageReceiptScope}
	state, err := e.Snapshot(ctx)
	if err != nil {
		return r, err
	}
	r.State, r.Policy = state, p.Brief.Policy
	if o.abandon {
		r.Status, r.Reason = "abandoned", o.reason
	} else {
		if err := validateStageWrites(ctx, s, record, state); err != nil {
			return r, err
		}
		r.Status, r.ExpectChecks, r.Writes = "finished", o.expect, true
		if err := observeStageChecks(ctx, s, &r); err != nil {
			return r, err
		}
		if err := stageExpectation(r, o.expect); err != nil {
			return r, err
		}
	}
	if current, err := e.Snapshot(ctx); err != nil || current != state {
		return r, fmt.Errorf("repository changed during stage finalization: %v", err)
	}
	r.ID = contextpack.Key(r)
	return r, nil
}

func validateStageWrites(ctx context.Context, s *session, record stageRecord, state contextpack.State) error {
	p := record.Prepared
	if time.Since(record.Created) > contextpack.Lifetime {
		return errors.New("stage record expired; abandon it and prepare a fresh worker")
	}
	if err := verifyWorkerBriefPolicy(s, p); err != nil {
		return err
	}
	if err := validateStageCheckPolicy(ctx, s, record, state.Tree); err != nil {
		return err
	}
	if state.SourcePolicy != p.State.SourcePolicy {
		return errors.New("source visibility policy changed during stage")
	}
	changes, err := s.repo.Diff(ctx, p.Brief.Baseline, state.Tree)
	if err != nil {
		return err
	}
	current, err := stageRawSources(ctx, s, state.Tree)
	if err != nil {
		return err
	}
	changes = append(changes, stageRawChanges(record.Sources, current)...)
	violations, err := roles.Check(p.Brief.Role, changes, p.Brief.TestPaths, p.Brief.Task)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("%s changed %d paths outside its recorded write limit; first: %s (%s)", p.Brief.Role, len(violations), violations[0].Path, violations[0].Why)
	}
	return nil
}

func observeStageChecks(ctx context.Context, s *session, r *stageReceipt) error {
	key, err := compactEvidenceKey(s)
	if err != nil {
		return err
	}
	verdict, err := evidence.Judge(ctx, s.repo, s.store, r.State.Tree, time.Now(), nil)
	if err != nil {
		if !errors.Is(err, evidence.ErrNoDeclaration) {
			return err
		}
		r.EvidenceError = err.Error()
	} else {
		r.Verified, r.Warnings = verdict.Verified, append(r.Warnings, verdict.Warnings...)
		for _, check := range verdict.Checks {
			item := stageCheck{ID: check.ID, State: check.State}
			if check.Running {
				item.State = "running"
			}
			if check.Deciding != nil {
				item.RunID = check.Deciding.RunID
			}
			r.Checks = append(r.Checks, item)
			if item.State != evidence.Satisfied {
				r.Missing = append(r.Missing, check.ID)
				r.Verified = false
			}
		}
	}
	if current, err := compactEvidenceKey(s); err != nil || current != key {
		return fmt.Errorf("check evidence changed during stage finalization: %v", err)
	}
	r.EvidenceKey = key
	return nil
}

func stageExpectation(r stageReceipt, expected string) error {
	if expected == "any" || expected == "pass" && r.Verified {
		return nil
	}
	if expected == "fail" && r.EvidenceError == "" && len(r.Warnings) == 0 {
		failed := false
		for _, check := range r.Checks {
			if check.State != evidence.Satisfied && check.State != evidence.Failed {
				return errors.New("expected current failed checks, but evidence is missing, stale, running or timed out")
			}
			failed = failed || check.State == evidence.Failed
		}
		if failed {
			return nil
		}
	}
	return fmt.Errorf("existing current check evidence does not meet --expect-checks %s; stage remains unfinished", expected)
}

func existingStageReceipt(ctx context.Context, s *session, e *contextpack.Engine, ledger *stageLedger, record stageRecord, o stageFinishOptions) (stageReceipt, error) {
	var r stageReceipt
	if err := ledger.read(stageReceiptName(o), &r); err != nil {
		return r, err
	}
	id := r.ID
	r.ID = ""
	if !stageValidID(id) || id != contextpack.Key(r) || r.DispatchID != o.id || r.Version != 1 {
		return r, errors.New("finished stage receipt was tampered with")
	}
	r.ID = id
	if err := validateStageReceiptSemantics(record, r); err != nil {
		return r, err
	}
	if o.abandon != (r.Status == "abandoned") || !o.abandon && o.expect != r.ExpectChecks || o.abandon && o.reason != r.Reason {
		return r, errors.New("repeat finish must use the original expectation or abandonment reason")
	}
	// Explicit abandonment grants no current write or quality approval. Retrying
	// it can safely recover its saved receipt after an interrupted pointer removal.
	if o.abandon {
		return r, releaseCompletedStage(ledger, o.id)
	}
	state, err := e.Snapshot(ctx)
	if err != nil {
		return r, err
	}
	if err := validateStageWrites(ctx, s, record, state); err != nil {
		return r, err
	}
	if err := verifyStageReceiptEvidence(ctx, s, record.Prepared, r); err != nil {
		return r, err
	}
	if state != r.State {
		return r, errors.New("finished stage receipt is stale: source or HEAD changed")
	}
	if err := releaseCompletedStage(ledger, o.id); err != nil {
		return r, err
	}
	return r, nil
}

func stageReceiptName(o stageFinishOptions) string {
	if o.abandon {
		return o.id + ".abandon.json"
	}
	return o.id + ".finish.json"
}

func validateStageReceiptSemantics(record stageRecord, r stageReceipt) error {
	if r.Task != record.Prepared.Brief.Task || r.Policy != record.Prepared.Brief.Policy || r.Scope != stageReceiptScope || r.Created.Before(record.Created) || r.Created.After(time.Now().Add(time.Minute)) {
		return errors.New("finished stage receipt does not match its original stage")
	}
	switch r.Status {
	case "finished":
		if !r.Writes || r.Reason != "" || r.ExpectChecks != "any" && r.ExpectChecks != "pass" && r.ExpectChecks != "fail" {
			return errors.New("invalid finished stage receipt semantics")
		}
		return stageExpectation(r, r.ExpectChecks)
	case "abandoned":
		if r.Writes || r.Verified || strings.TrimSpace(r.Reason) == "" || r.ExpectChecks != "" {
			return errors.New("invalid abandoned stage receipt semantics")
		}
		return nil
	default:
		return errors.New("invalid stage receipt status")
	}
}

func releaseCompletedStage(ledger *stageLedger, id string) error {
	active, err := ledger.active()
	if err != nil {
		return err
	}
	if active == id {
		return ledger.root.Remove("active.json")
	}
	return nil
}

func verifyStageBeforePublication(ctx context.Context, s *session, e *contextpack.Engine, record stageRecord, r stageReceipt) error {
	if r.Status == "finished" {
		if err := validateStageWrites(ctx, s, record, r.State); err != nil {
			return err
		}
		if err := verifyStageReceiptEvidence(ctx, s, record.Prepared, r); err != nil {
			return err
		}
	}
	state, err := e.Snapshot(ctx)
	if err != nil || state != r.State {
		return fmt.Errorf("repository changed before stage receipt publication: %v", err)
	}
	return nil
}

func verifyStageReceiptEvidence(ctx context.Context, s *session, p preparedWorkerBrief, r stageReceipt) error {
	if err := verifyWorkerBriefPolicy(s, p); err != nil {
		return err
	}
	current := stageReceipt{State: r.State, Checks: []stageCheck{}, Missing: []string{}, Warnings: []string{}}
	if err := observeStageChecks(ctx, s, &current); err != nil {
		return err
	}
	if current.EvidenceKey != r.EvidenceKey || current.Verified != r.Verified || current.EvidenceError != r.EvidenceError || contextpack.Key(current.Checks) != contextpack.Key(r.Checks) || contextpack.Key(current.Warnings) != contextpack.Key(r.Warnings) {
		return errors.New("finished stage receipt is stale: check evidence changed or expired")
	}
	return nil
}

func stageCheckPolicy(ctx context.Context, s *session, tree string) (string, error) {
	data, found, err := s.repo.Declaration(ctx, tree, "kitsu.toml")
	if err != nil {
		return "", err
	}
	return contextpack.Key(struct {
		Data  string
		Found bool
	}{string(data), found}), nil
}

func validateStageCheckPolicy(ctx context.Context, s *session, record stageRecord, tree string) error {
	current, err := stageCheckPolicy(ctx, s, tree)
	if err != nil {
		return err
	}
	if current != record.Checks {
		return errors.New("declared check policy changed during stage; abandon and prepare a fresh worker")
	}
	return nil
}

func boundedStageJSON(value any, budget int) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(data)+1 > budget {
		return nil, usagef("budget cannot hold mandatory stage metadata; increase --max-bytes")
	}
	return append(data, '\n'), nil
}

func printStageReceipt(cfg Config, r stageReceipt, budget int) error {
	data, err := boundedStageJSON(r, budget)
	if err != nil {
		return err
	}
	_, err = cfg.Out.Write(data)
	return err
}
