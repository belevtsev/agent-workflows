package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

type taskVerificationOptions struct {
	task, from, workflow string
	json, compact        bool
	budget               int
}

func parseTaskVerification(args []string) (taskVerificationOptions, error) {
	o := taskVerificationOptions{}
	flags := newFlags("task verify")
	flags.StringVar(&o.from, "from", "", "")
	flags.StringVar(&o.workflow, "workflow", "", "")
	flags.BoolVar(&o.json, "json", false, "")
	flags.BoolVar(&o.compact, "compact", false, "")
	flags.IntVar(&o.budget, "max-bytes", contextpack.DefaultBytes, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return o, err
	}
	if p.dash || len(p.pos) != 1 || !taskName.MatchString(p.pos[0]) || o.workflow != "" && o.workflow != "small" && o.workflow != "full" {
		return o, usagef("usage: kitsu task verify <task> [--from <revision>] [--workflow full|small] [--json] [--compact]")
	}
	if uxFlagPresent(args, "from") && o.from == "" {
		return o, usagef("--from requires a starting revision")
	}
	if uxFlagPresent(args, "max-bytes") && !o.compact {
		return o, usagef("--max-bytes requires --compact")
	}
	o.task = p.pos[0]
	return o, compactBudget(o.budget)
}

type taskVerificationRun struct {
	s       *session
	options taskVerificationOptions
	ledger  *stageLedger
	plan    verification.Plan
	history verification.History
	skills  string
	refs    []verification.SelectionRef
	before  map[string]bool
	reused  int
	runErr  error
}

func runTaskVerify(ctx context.Context, cfg Config, args []string) error {
	o, err := parseTaskVerification(args)
	if err != nil {
		return err
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	ledger, err := openStageLedger(ctx, s)
	if err != nil {
		return compactFailure(err)
	}
	defer ledger.close()
	if active, err := ledger.active(); err != nil || active != "" {
		return failf("finish or abandon the active managed interval before verification: %s %v", active, err)
	}
	run := taskVerificationRun{s: s, options: o, ledger: ledger}
	if err := run.prepare(ctx); err != nil {
		return compactFailure(err)
	}
	if current, err := observeTaskVerification(ctx, s, o.task); err != nil {
		return compactFailure(err)
	} else if run.canReuse(ctx, current) {
		return run.output(ctx, cfg, current)
	}
	inner := *s
	if o.json || o.compact {
		inner.cfg.Out, inner.cfg.Err = io.Discard, io.Discard
	}
	run.s = &inner
	if err := run.execute(ctx); err != nil {
		return compactFailure(err)
	}
	current, err := observeTaskVerification(context.WithoutCancel(ctx), s, o.task)
	if err != nil {
		return compactFailure(err)
	}
	return run.output(context.WithoutCancel(ctx), cfg, current)
}

func (r *taskVerificationRun) prepare(ctx context.Context) error {
	history, historyErr := verification.Load(r.s.repo.Root, r.options.task)
	if errors.Is(historyErr, verification.ErrPromotionPending) {
		historyErr = nil
	}
	if historyErr != nil && !errors.Is(historyErr, verification.ErrNoHistory) {
		return historyErr
	}
	if r.options.from == "" {
		if historyErr != nil {
			return errors.New("first verification requires --from <starting-revision>; it is never inferred from a completed task")
		}
		r.options.from = history.Attempt.BaseTree
	}
	workflow, err := verificationWorkflow(r.s, r.options.task, r.options.workflow, history, historyErr)
	if err != nil {
		return err
	}
	r.options.workflow, r.history = workflow, history
	r.skills, r.refs, err = taskVerificationSkills(r.s, r.options.task, workflow, true)
	if err != nil {
		return err
	}
	tree, err := r.s.repo.Snapshot(ctx, r.s.store.Tmp())
	if err != nil {
		return err
	}
	r.plan, err = verification.Build(ctx, r.s.repo, tree, r.options.from)
	if err != nil {
		return err
	}
	records, damaged, err := r.s.store.AllRecords()
	if err != nil || len(damaged) != 0 {
		return fmt.Errorf("verification needs readable local run records: %v (damaged: %d)", err, len(damaged))
	}
	r.before = map[string]bool{}
	for _, record := range records {
		r.before[record.RunID] = true
	}
	return nil
}

func (r *taskVerificationRun) canReuse(ctx context.Context, current *taskVerificationView) bool {
	if current == nil || !current.Ready || r.history.ID == "" || r.history.Attempt.Workflow != r.options.workflow || r.history.Attempt.BaseTree != r.plan.BaseTree || r.history.Attempt.PolicyKey != r.plan.PolicyKey {
		return false
	}
	v, err := r.s.judgeWorktree(ctx, nil)
	if err != nil || !v.Verified {
		return false
	}
	results := currentVerificationResults(r.s, v, r.plan, r.plan.AllIDs())
	if !verificationResultsPassed(results) {
		return false
	}
	r.reused = len(v.Checks)
	return true
}

func (r *taskVerificationRun) execute(ctx context.Context) error {
	sources, err := stageRawSources(ctx, r.s, r.plan.SourceTree)
	if err != nil {
		return err
	}
	raw := contextpack.Key(sources)
	previous, previousErr := readLocalVerification(r.s, r.options.task)
	rawChanged := previousErr == nil && previous.Raw != raw
	// A first local observation cannot retrospectively bind old Git-only runs
	// to original filesystem bytes. Establish that binding once, then reuse it.
	initial, err := r.s.judgeWorktree(ctx, nil)
	if err != nil {
		return err
	}
	bound := currentVerificationResults(r.s, initial, r.plan, r.plan.RequiredIDs())
	forceRequired := previousErr != nil || rawChanged || previous.Policy != r.plan.PolicyKey || previous.Skills != r.skills || !reflect.DeepEqual(previous.Results, bound)
	r.runIDs(ctx, r.plan.PriorityIDs(), forceRequired, true)
	if err := r.unchanged(ctx, r.plan.SourceTree, raw); err != nil {
		r.runErr = err
	}
	observing := context.WithoutCancel(ctx)
	verdict, err := r.s.judgeWorktree(observing, nil)
	if err != nil {
		return err
	}
	attempt := verification.Attempt{Version: 1, Task: r.options.task, Workflow: r.options.workflow,
		BaseTree: r.plan.BaseTree, SourceTree: r.plan.SourceTree, PolicyKey: r.plan.PolicyKey,
		SkillsKey: r.skills, Selections: r.refs, Results: currentVerificationResults(r.s, verdict, r.plan, r.plan.PriorityIDs())}
	r.history, err = verification.Publish(r.s.repo.Root, r.options.task, r.options.workflow, attempt)
	if err != nil {
		return err
	}
	if r.runErr != nil {
		return nil
	}
	// Publishing shared bookkeeping precedes the final content observations.
	tree, err := r.s.repo.Snapshot(ctx, r.s.store.Tmp())
	if err != nil {
		return err
	}
	if err := verifyPublicationSources(ctx, r.s, r.options.task, r.plan.SourceTree, tree, sources); err != nil {
		r.runErr = err
		return nil
	}
	raw, err = verificationRawKey(ctx, r.s, tree)
	if err != nil {
		return err
	}
	r.runIDs(ctx, r.plan.AllIDs(), rawChanged, false)
	if err := r.unchanged(observing, tree, raw); err != nil {
		r.runErr = err
		return nil
	}
	return r.recordLocal(observing, tree, raw)
}

func (r *taskVerificationRun) runIDs(ctx context.Context, ids []string, force, stopEarly bool) {
	for _, id := range ids {
		if ctx.Err() != nil {
			r.runErr = ctx.Err()
			return
		}
		v, err := r.s.judgeWorktree(ctx, nil)
		if err != nil {
			r.runErr = err
			return
		}
		mustRun := force && (!stopEarly || slices.Contains(r.plan.RequiredIDs(), id))
		for _, c := range v.Checks {
			if c.ID == id && c.State == evidence.Satisfied {
				observed := verificationCheckResult(r.s, c, verification.Result{ID: id})
				if observed.State != store.Passed {
					mustRun = true
				}
				if !mustRun {
					r.reused++
				}
			}
		}
		passed, err := r.s.runOne(ctx, id, mustRun, nil)
		if err != nil {
			r.runErr = err
			return
		}
		if !passed {
			r.runErr = exit(ExitNotVerified)
			if stopEarly {
				return
			}
		}
	}
}

func verificationResultsPassed(results []verification.Result) bool {
	return !slices.ContainsFunc(results, func(result verification.Result) bool { return result.State != store.Passed })
}

func (r *taskVerificationRun) unchanged(ctx context.Context, tree, raw string) error {
	current, err := r.s.repo.Snapshot(ctx, r.s.store.Tmp())
	if err != nil {
		return err
	}
	currentRaw, err := verificationRawKey(ctx, r.s, current)
	if err != nil {
		return err
	}
	if tree != current || raw != currentRaw {
		return errors.New("source changed during verification; no current observation was certified")
	}
	skillsKey, _, err := taskVerificationSkills(r.s, r.options.task, r.options.workflow, false)
	if err != nil || skillsKey != r.skills {
		return fmt.Errorf("selected original skills changed during verification: %v", err)
	}
	return nil
}

func (r *taskVerificationRun) recordLocal(ctx context.Context, tree, raw string) error {
	v, err := r.s.judgeWorktree(ctx, nil)
	if err != nil {
		return err
	}
	results := currentVerificationResults(r.s, v, r.plan, r.plan.RequiredIDs())
	if slices.ContainsFunc(results, func(result verification.Result) bool { return result.State != store.Passed }) {
		return nil
	}
	if v.Tree != tree {
		return errors.New("source changed before private verification publication")
	}
	if err := r.unchanged(ctx, tree, raw); err != nil {
		return err
	}
	observation := localVerification{Version: 1, Attempt: r.history.ID, Tree: tree, Raw: raw, Policy: r.plan.PolicyKey, Skills: r.skills, Results: results}
	observation.Key = observation.fingerprint()
	return r.ledger.write(localVerificationName(r.options.task), observation)
}
