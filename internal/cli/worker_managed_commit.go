package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/gate"
)

type managedCommitIntent struct {
	Message string   `json:"message"`
	Paths   []string `json:"paths"`
}

func validateManagedCommitOptions(o *stageFinishOptions) error {
	if o.message == "" && len(o.includes) == 0 {
		return nil
	}
	messageValid := strings.TrimSpace(o.message) != "" && len(o.message) <= 4096 &&
		utf8.ValidString(o.message) && !strings.ContainsRune(o.message, 0)
	if o.abandon || o.expect != "pass" || !messageValid || len(o.includes) == 0 {
		return usagef("configured git delivery requires --message, explicit --include paths and --expect-checks pass")
	}
	slices.Sort(o.includes)
	for i, path := range o.includes {
		if !returnPath(path) || i > 0 && path == o.includes[i-1] {
			return usagef("--include expects unique repository-relative file paths")
		}
	}
	return nil
}

func configuredGitStage(r stageRecord) bool {
	return r.Managed != nil && r.Prepared.Brief.Role == "git"
}

func validateManagedGitCandidate(ctx context.Context, s *session, r stageRecord, state contextpack.State, got managedStage) error {
	want := r.Managed
	before := r.Prepared.State
	if got.Head != state.Head || state.Tree != before.Tree || state.Raw != before.Raw || state.SourcePolicy != before.SourcePolicy {
		return errors.New("configured git worker candidate changed")
	}
	if got.Head == want.Head && got.MergeHeads == want.MergeHeads &&
		(got.Index == want.Index || got.Index == before.Tree) {
		return nil
	}
	if got.Head != "" && got.Head != want.Head && got.MergeHeads == "" && got.Index == before.Tree {
		tree, parents, err := s.repo.CommitContents(ctx, got.Head)
		if err != nil {
			return err
		}
		if tree == before.Tree && parents == strings.TrimSpace(want.Head+" "+want.MergeHeads) {
			return nil
		}
	}
	return errors.New("configured git stage no longer matches the authorized checkout, parents and index")
}

func checkManagedFinish(ctx context.Context, s *session, l *stageLedger, r stageRecord, o stageFinishOptions) error {
	if o.message != "" && !configuredGitStage(r) {
		return usagef("--message and --include apply only to an explicitly prepared configured git stage")
	}
	if r.Managed == nil {
		return nil
	}
	run, err := readManagedRun(l, o.id, r)
	if errors.Is(err, os.ErrNotExist) {
		if !o.abandon && r.Prepared.Brief.Role == "reviewer" {
			return errors.New("run the prepared review with native hook observation before finishing it")
		}
		if o.message != "" {
			return failf("run and inspect the configured git worker before authorizing its commit")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if o.abandon {
		return checkManagedWorkerStopped(ctx, s, l)
	}
	if run.Status != "returned" {
		return failf("configured worker is %s; inspect host session %q before abandoning the interval", run.Status, run.Session)
	}
	if run.Role == "reviewer" {
		_, err := readManagedHandback(l, r, run)
		return err
	}
	if o.message == "" {
		if configuredGitStage(r) {
			var intent managedCommitIntent
			if err := l.read(o.id+".commit.json", &intent); !errors.Is(err, os.ErrNotExist) {
				return errors.Join(err, errors.New("replay the authorized git stage with its original --message and --include paths"))
			}
		}
		return nil
	}
	report, err := readManagedHandback(l, r, run)
	if err != nil {
		return err
	}
	if len(report.Blockers) != 0 || len(report.Findings) != 0 || len(report.ChangedPaths) != 0 {
		return errors.New("configured git worker reported findings, blockers or writes; reconcile without committing")
	}
	return checkManagedCommitIntent(l, o)
}

// An available lock rules out a live supervisor. The caller accounts for
// the worker after an interrupted driver and supplies a reconciliation reason.
func checkManagedWorkerStopped(ctx context.Context, s *session, l *stageLedger) error {
	lock, err := privateStageLock(ctx, l.gitRoot, s.repo.GitDir, "kitsu-managed-worker.lock", fslock.NoWait)
	if errors.Is(err, fslock.ErrBusy) {
		return failf("configured worker is running; cancel or wait for it before abandoning")
	}
	if err != nil {
		return err
	}
	return lock.Close()
}

func checkManagedCommitIntent(l *stageLedger, o stageFinishOptions) error {
	var saved managedCommitIntent
	err := l.read(o.id+".commit.json", &saved)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if saved.Message != o.message || !slices.Equal(saved.Paths, o.includes) {
		return errors.New("replay the authorized git stage with its original --message and --include paths")
	}
	return nil
}

func finishManagedCommit(ctx context.Context, s *session, e *contextpack.Engine, l *stageLedger,
	r stageRecord, o stageFinishOptions, receipt stageReceipt,
) (stageReceipt, error) {
	if !receipt.Verified {
		return receipt, errors.New("configured git delivery requires current passing checks")
	}
	if err := checkManagedCommitPaths(ctx, s, r, o.includes); err != nil {
		return receipt, err
	}
	current, err := managedGitState(ctx, s)
	if err != nil {
		return receipt, err
	}
	if current.Index != receipt.State.Tree {
		return receipt, errors.New("stage exactly the explicitly included files before committing")
	}
	name := o.id + ".commit.json"
	var intent managedCommitIntent
	err = l.read(name, &intent)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return receipt, err
	}
	if current.Head != r.Managed.Head {
		if err != nil {
			return receipt, errors.New("Git advanced before Kitsu recorded a scoped commit attempt; inspect the checkout")
		}
		if err := validateManagedGitCandidate(ctx, s, r, receipt.State, current); err != nil {
			return receipt, err
		}
		return managedCommittedReceipt(receipt, receipt.State, current.Head), nil
	}
	if gate.State(ctx, s.repo) != gate.Installed {
		return receipt, errors.New("install the Kitsu Git gate before configured delivery")
	}
	if _, err := stageCommitOutputBudget(receipt, o.budget); err != nil {
		return receipt, err
	}
	if err := l.write(name, managedCommitIntent{Message: o.message, Paths: o.includes}); err != nil {
		return receipt, err
	}
	result, err := s.repo.CommitStaged(ctx, s.store.Tmp(), receipt.State.Tree, r.Managed.Head,
		r.Managed.MergeHeads, r.Managed.Target, o.message,
	)
	if err != nil {
		return receipt, err
	}
	after, err := e.Snapshot(ctx)
	if err != nil {
		return receipt, err
	}
	if after.Head != result.Commit || result.Commit == "" {
		return receipt, errors.New("Git committed, but the checkout changed before the result was recorded")
	}
	if err := validateManagedStageGit(ctx, s, r, after); err != nil {
		return receipt, err
	}
	return managedCommittedReceipt(receipt, after, result.Commit), nil
}

func stageCommitOutputBudget(receipt stageReceipt, budget int) ([]byte, error) {
	receipt.State.Head, receipt.Commit = strings.Repeat("0", 64), strings.Repeat("0", 64)
	return boundedStageJSON(receipt, budget)
}

func managedCommittedReceipt(receipt stageReceipt, state contextpack.State, commit string) stageReceipt {
	receipt.State, receipt.Commit, receipt.ID = state, commit, ""
	receipt.ID = contextpack.Key(receipt)
	return receipt
}

func checkManagedCommitPaths(ctx context.Context, s *session, r stageRecord, paths []string) error {
	base := r.Managed.Head
	if base == "" {
		var err error
		base, err = s.repo.EmptyTree(ctx)
		if err != nil {
			return err
		}
	}
	changes, err := s.repo.Diff(ctx, base, r.Prepared.State.Tree)
	if err != nil {
		return err
	}
	actual := make([]string, 0, len(changes))
	for _, change := range changes {
		actual = append(actual, change.Path)
	}
	slices.Sort(actual)
	if !slices.Equal(paths, actual) {
		for _, path := range actual {
			if _, found := slices.BinarySearch(paths, path); !found {
				return fmt.Errorf("staged candidate has no explicit --include for %q", path)
			}
		}
		return fmt.Errorf("included paths do not match the staged candidate; Git records %d changed paths and %d were explicitly included", len(actual), len(paths))
	}
	return nil
}
