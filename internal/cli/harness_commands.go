package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func harnessCommands() []uxCommand {
	json := uxOption("json", "", "return structured task state")
	brief := uxOption("brief", "", "return next action, ticket and evidence references as JSON")
	request := uxOption("request", "text", "original developer request")
	repair := uxOption("repair", "", "authorize source repairs after findings or failing assertions")
	commit := uxOption("commit", "", "delivery includes a local commit")
	publish := uxOption("publish-to", "remote/branch", "authorized remote branch whose commit must be confirmed")
	base := uxOption("pr-base", "branch", "require confirmation of an open GitHub PR into this branch")
	reviewBase := uxOption("review-base", "revision", "start commit of a read-only review range; requires --review-head")
	reviewHead := uxOption("review-head", "revision", "end commit of a read-only review range; requires --review-base")
	ticket := uxOption("ticket", "id", "action ticket returned by task next")
	return []uxCommand{
		{path: "task open", args: "<task> --until <outcome>", summary: "Pin a candidate and the developer's requested outcome", flags: append([]uxFlag{request, taskEntry, taskUntil, repair, commit, publish, base, reviewBase, reviewHead, requireReviewFlag(), brief, json}, reviewLimitFlags()...), examples: []string{`kitsu task open export-quoting --entry review --until verify --repair --request "Verify CSV quoting"`, `kitsu task open review-pr --until review --review-base main --review-head topic --request "Review the committed change"`}, run: harnessCommand("open")},
		{path: "task extend", args: "<task>", summary: "Explicitly extend a saved outcome or renew its time limit", flags: append([]uxFlag{taskUntil, repair, commit, publish, base, requireReviewFlag(), brief, json}, reviewLimitFlags()...), run: harnessCommand("extend")},
		{path: "task claim", args: "<task>", summary: "Reserve an action before launching a host worker", flags: []uxFlag{ticket, uxOption("actor", "id", "session reserving this action"), uxOption("worker", "id", "link the launched native reviewer or developer to an existing reservation"), uxOption("host", "host", "resolve specialist settings for a review, implementation or repair reservation", roles.Hosts...), brief, json}, run: harnessCommand("claim")},
		{path: "task release", args: "<task>", summary: "Release a claim after reconciling or cancelling its worker", flags: []uxFlag{ticket, brief, json}, run: harnessCommand("release")},
		{path: "task confirm", args: "<task>", summary: "Check every push destination and requested PR, or recheck a completed delivery", flags: []uxFlag{ticket, uxOption("pr", "url", "published pull request URL"), brief, json}, examples: []string{"kitsu task confirm export-quoting --ticket TICKET --pr https://github.com/example/repo/pull/17", "kitsu task confirm export-quoting --brief"}, run: harnessCommand("confirm")},
		{path: "task commit", args: "<task>", summary: "Commit the verified candidate already staged for authorized delivery", flags: []uxFlag{ticket, uxOption("message", "text", "commit message"), uxOption("include", "path", "explicitly include a pre-existing changed path; repeatable"), brief, json}, run: harnessCommand("commit")},
		{path: "task next", args: "<task>", summary: "Resume from current evidence and show the next action", flags: []uxFlag{brief, json}, run: harnessCommand("next")},
		{path: "task record", args: "<task> <action>", summary: "Record intake, a plan, implementation, repair or independent review", flags: []uxFlag{ticket, uxOption("claim-review", "host", "reserve the next independent review with this result", roles.Hosts...), uxOption("actor", "id", "native session identity"), uxOption("note", "text", "result and evidence or artifact reference"), uxOption("finding", "text", "unresolved review finding; repeatable"), brief, json}, examples: []string{`kitsu task record export-quoting intake --actor SESSION --ticket TICKET --note "Quoted fields should round-trip"`}, run: harnessCommand("record")},
		{path: "task test", args: "<task> [-- <program> [<argument>...]]", summary: "Execute the requested behavioral assertion and required repository checks", flags: []uxFlag{ticket, uxOption("expect", "text", "observable behavior asserted by this command"), uxOption("retry", "", "use the saved behavioral assertion on the current candidate"), uxOption("replace-reason", "text", "explain why a failing assertion needs a different command"), brief, json}, examples: []string{`kitsu task test export-quoting --ticket TICKET --expect "Quoted fields round-trip" -- go test ./csv`}, run: harnessCommand("test")},
	}
}

func requireReviewFlag() uxFlag {
	return uxOption("require-review", "", "require an independent review of the candidate")
}

func reviewLimitFlags() []uxFlag {
	return []uxFlag{
		uxOption("time-limit", "duration", "time remaining before the task pauses"),
		uxOption("review-limit", "count", "maximum total independent review starts, including released reservations"),
		uxOption("finish-reserve", "duration", "time before the deadline reserved from new reviews"),
	}
}

type reviewLimits struct {
	limit, reserve time.Duration
	reviews        int
}

type harnessOptions struct {
	id, host, action, request, actor, note, ticket, expect, replaceReason, worker string
	scope                                                                         harness.Scope
	findings, includes                                                            stringList
	argv                                                                          []string
	reviewLimits
	json, brief, retry, defaultDeliveryEntry bool
}

func parseHarness(verb string, args []string) (harnessOptions, error) {
	var o harnessOptions
	f := newFlags("task " + verb)
	f.BoolVar(&o.json, "json", false, "")
	f.BoolVar(&o.brief, "brief", false, "")
	switch verb {
	case "open", "extend":
		if verb == "open" {
			f.StringVar(&o.request, "request", "", "")
			taskEntry.bindChoice(f, &o.scope.Entry)
			bindReviewRangeFlags(f, &o.scope)
		}
		taskUntil.bindChoice(f, &o.scope.Until)
		f.BoolVar(&o.scope.RequireReview, "require-review", false, "")
		f.BoolVar(&o.scope.Repair, "repair", false, "")
		f.BoolVar(&o.scope.Commit, "commit", false, "")
		f.StringVar(&o.scope.Publish, "publish-to", "", "")
		f.StringVar(&o.scope.PRBase, "pr-base", "", "")
		o.bindLimitFlags(f)
	case "confirm":
		f.StringVar(&o.ticket, "ticket", "", "")
		f.StringVar(&o.note, "pr", "", "")
	case "claim":
		f.Func("worker", "", func(id string) error {
			if strings.TrimSpace(id) == "" {
				return errors.New("worker session ID must be nonempty")
			}
			o.worker = id
			return nil
		})
		f.StringVar(&o.host, "host", "", "")
		f.StringVar(&o.actor, "actor", "", "")
		f.StringVar(&o.ticket, "ticket", "", "")
	case "release":
		f.StringVar(&o.ticket, "ticket", "", "")
	case "commit":
		f.Var(&o.includes, "include", "")
		f.StringVar(&o.note, "message", "", "")
		f.StringVar(&o.ticket, "ticket", "", "")
	case "record":
		f.StringVar(&o.host, "claim-review", "", "")
		f.StringVar(&o.actor, "actor", "", "")
		f.StringVar(&o.note, "note", "", "")
		f.Var(&o.findings, "finding", "")
		f.StringVar(&o.ticket, "ticket", "", "")
	case "test":
		f.StringVar(&o.expect, "expect", "", "")
		f.BoolVar(&o.retry, "retry", false, "")
		f.StringVar(&o.replaceReason, "replace-reason", "", "")
		f.StringVar(&o.ticket, "ticket", "", "")
	}
	p, err := parseArgs(f, args)
	if err != nil {
		return o, err
	}
	if err := o.validateLimits(f); err != nil {
		return o, err
	}
	return validateHarnessOptions(verb, p, o)
}

func (o *reviewLimits) bindLimitFlags(f *flag.FlagSet) {
	f.DurationVar(&o.limit, "time-limit", 0, "")
	f.DurationVar(&o.reserve, "finish-reserve", 0, "")
	f.IntVar(&o.reviews, "review-limit", 0, "")
}

func (o reviewLimits) validateLimits(f *flag.FlagSet) error {
	var err error
	f.Visit(func(setting *flag.Flag) {
		if setting.Name == "review-limit" && o.reviews <= 0 || setting.Name == "finish-reserve" && o.reserve <= 0 || setting.Name == "time-limit" && o.limit <= 0 {
			err = usagef("--%s must be positive", setting.Name)
		}
	})
	return err
}

func validateHarnessOptions(verb string, p parsed, o harnessOptions) (harnessOptions, error) {
	n := 1
	if verb == "record" {
		n = 2
	}
	if len(p.pos) != n || !taskName.MatchString(p.pos[0]) || len(p.pos[0]) > 100 || p.dash != (verb == "test" && !o.retry) {
		return o, usagef("invalid task name or arguments for task %s", verb)
	}
	o.id = p.pos[0]
	switch verb {
	case "open":
		if err := o.validateOpen(); err != nil {
			return o, err
		}
	case "record":
		if err := o.validateResult(p.pos[1]); err != nil {
			return o, err
		}
	case "test":
		o.argv = p.rest
		if o.retry {
			if o.ticket == "" || o.expect != "" || o.replaceReason != "" {
				return o, usagef("--retry requires a ticket and uses the saved assertion")
			}
		} else if len(o.argv) == 0 || o.argv[0] == "" || strings.TrimSpace(o.expect) == "" || o.ticket == "" {
			return o, usagef("test requires --ticket, --expect and a command after --")
		}
	}
	return o, nil
}

func harnessFile(id string) string { return "task-harness-" + id + ".json" }

func harnessCommand(verb string) func(context.Context, Config, []string) error {
	return func(ctx context.Context, cfg Config, args []string) error {
		o, err := parseHarness(verb, args)
		if err != nil {
			return err
		}
		s, err := open(ctx, cfg, true)
		if err != nil {
			return err
		}
		l, err := openStageLedger(ctx, s)
		if err != nil {
			return err
		}
		defer l.close()
		return runHarnessCommand(ctx, s, l, verb, o)
	}
}

func runHarnessCommand(ctx context.Context, s *session, l *stageLedger, verb string, o harnessOptions) error {
	active, err := l.active()
	if err != nil {
		return err
	} else if active != "" && verb != "next" {
		return failf("finish the active managed interval %s first", active)
	}
	task, err := loadHarness(ctx, s, l, o, verb)
	if err != nil {
		return err
	}
	full := o.json && !o.brief
	readOnly := verb == "open" || verb == "next"
	view, err := observeHarness(ctx, s, &task, full && readOnly)
	if err != nil {
		return err
	}
	if readOnly {
		view.checkManagedInterval(active)
		if verb == "next" && !o.brief {
			// The latest result already carries its complete note.
			view.PriorNotes = previousHarnessNotes(&task, view.Last)
		}
		return printHarness(s.cfg, o, view)
	}
	if verb == "record" && o.host != "" {
		return recordAndClaimReview(ctx, s, l, &task, o, view)
	}
	actionErr := applyHarness(ctx, s, l, &task, o, verb, view)
	if actionErr != nil && !errors.Is(actionErr, errPublicationUnconfirmed) {
		return actionErr
	}
	if err := l.write(harnessFile(o.id), task); err != nil {
		return err
	}
	view, err = observeHarness(ctx, s, &task, full)
	if err != nil {
		return err
	}
	view.ClaimCreated = verb == "claim" && o.worker == ""
	return errors.Join(actionErr, printHarnessResult(s.cfg, verb, o, view))
}

func previousHarnessNotes(task *harness.Task, last *harness.Event) map[string]string {
	notes := map[string]string{}
	for _, event := range []*harness.Event{task.Latest("intake"), task.Latest("plan"), task.Latest("implement", "repair")} {
		if event != nil && event != last {
			notes[event.Action] = event.Note
		}
	}
	return notes
}

func printHarnessResult(cfg Config, verb string, o harnessOptions, view harnessView) error {
	if verb == "test" && o.brief && view.VerificationLog == nil {
		view.VerificationLog = view.verificationLog()
	}
	if err := printHarness(cfg, o, view); err != nil {
		return err
	}
	if verb == "test" && !view.Verified || verb == "commit" && view.Delivery != "committed" {
		return exit(ExitNotVerified)
	}
	return nil
}

func harnessSnapshot(ctx context.Context, s *session) (harness.Snapshot, error) {
	tree, err := s.repo.Snapshot(ctx, s.store.Tmp())
	if err != nil {
		return harness.Snapshot{}, err
	}
	c, err := harnessSource(ctx, s, tree)
	if err == nil {
		c.Index, err = s.repo.IndexTree(ctx, s.store.Tmp())
	}
	return c, err
}

func harnessSource(ctx context.Context, s *session, tree string) (harness.Snapshot, error) {
	c := harness.Snapshot{Tree: tree}
	var err error
	if c.Head, _, err = s.repo.CommitOf(ctx, "HEAD"); err != nil {
		return c, err
	}
	if c.MergeHeads, err = s.repo.MergeParents(ctx); err != nil {
		return c, err
	}
	c.Raw, err = verificationRawKey(ctx, s, tree)
	return c, err
}

func (s *session) checkHarnessInputs(want harness.Snapshot, target string) func(context.Context) error {
	return func(ctx context.Context) error {
		actual, err := harnessSource(ctx, s, want.Tree)
		if err != nil {
			return err
		}
		if !actual.SameSource(want) || !actual.SameBase(want) {
			return errors.New("working file bytes or commit parents changed")
		}
		return s.checkHarnessCommitTarget(ctx, target)
	}
}

func (s *session) checkHarnessCommitTarget(ctx context.Context, target string) error {
	if target == "" {
		return nil
	}
	actual, err := s.repo.CommitTarget(ctx)
	if err == nil && actual != target {
		return fmt.Errorf("checkout changed from %s to %s", target, actual)
	}
	return err
}

func actionTicket(task *harness.Task, next string, c harness.Snapshot) string {
	// Work can change source and staging, but its Git parents must still match.
	work, parents := next == "implement" || next == "repair", c.Parents()
	if work {
		c = harness.Snapshot{}
	}
	// A commit ticket already names the required tree. Staging that exact
	// tree is the expected next step. CommitStaged checks the live index.
	if next == "commit" {
		c.Index = c.Tree
	}
	ticket := contextpack.Key([]any{task.ID, task.Created, task.Scope, task.Generation, len(task.Events), next, c})
	// Previously reserved developers have a separately recorded and checked starting base.
	if work && (task.Claim == nil || task.Claim.Developer == nil || task.Claim.Ticket != ticket) {
		ticket = contextpack.Key([]string{ticket, parents})
	}
	// Older successful retries could leave a completed ticket current after
	// losing evidence. Preserve any action that already has a live claim.
	if task.Proof != nil && ticket == task.Proof.CompletionTicket && (task.Claim == nil || task.Claim.Ticket != ticket) {
		return contextpack.Key([]string{ticket, "verification-recovery"})
	}
	return ticket
}

func commitHarness(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, view harnessView) error {
	if view.CurrentCommitTarget != "" {
		return failf("%s", view.Delivery)
	}
	if view.Delivery == "committed" && task.Scope.Commit && (view.Next == "done" || view.Next == "publish") {
		return replayHarnessCommit(ctx, s, task, view, o.ticket)
	}
	if !task.Scope.Commit || view.Next != "commit" || strings.TrimSpace(o.note) == "" {
		return failf("commit requires explicit delivery scope, a verified candidate, ticket and message")
	}
	if o.ticket != view.Ticket {
		return failf("commit ticket no longer matches the verified candidate and target %s; inspect the checkout and run task next %s", view.CommitTarget, task.ID)
	}
	if view.Candidate.Index != view.Candidate.Tree {
		return failf("staged content differs from the verified candidate; inspect unrelated edits before staging")
	}
	if !view.Candidate.SameBase(task.Proof.Candidate) {
		return failf("HEAD changed after verification; reconcile before committing")
	}
	if err := checkCommitIncludes(ctx, s, task, view.Candidate.Tree, o.includes); err != nil {
		return err
	}
	// Recovery must know the intended destination even if Git finishes before
	// the coordinator can record the result.
	if task.Proof.CommitTarget != view.CommitTarget || task.CommitTarget != view.CommitTarget {
		task.CommitTarget, task.Proof.CommitTarget = view.CommitTarget, view.CommitTarget
		if err := l.write(harnessFile(task.ID), task); err != nil {
			return err
		}
	}
	if err := s.repo.CommitStaged(ctx, s.store.Tmp(), view.Candidate.Tree, view.Candidate.Head, view.Candidate.MergeHeads, view.CommitTarget, o.note); err != nil {
		return err
	}
	task.Events = append(task.Events, harness.Event{Action: "commit", Note: o.note, Ticket: o.ticket, Candidate: view.Candidate, At: time.Now().UTC()})
	return nil
}

func replayHarnessCommit(ctx context.Context, s *session, task *harness.Task, view harnessView, ticket string) error {
	candidate, proof := view.Candidate, task.Proof
	target := task.AuthorizedCommitTarget()
	// The delivery view has already checked the verified tree, parents and saved target.
	// Attach an interrupted commit to its assertion without replacing the publication ticket.
	if assertion := task.Latest("test"); assertion != nil && assertion == &task.Events[len(task.Events)-1] &&
		assertion.Ticket == proof.Ticket && target != "" && candidate.Head != proof.Candidate.Head &&
		(assertion.ObservedCommit == "" || assertion.ObservedCommit == candidate.Head) {
		want := contextpack.Key([]string{actionTicket(task, "commit", proof.Candidate), target})
		if ticket == want {
			assertion.ObservedCommit = candidate.Head
			return nil
		}
	}
	if view.Next == "done" {
		return nil
	}
	if event := task.Latest("commit"); event != nil && ticket == event.Ticket &&
		event.Candidate.Head != candidate.Head && event.Candidate.SameSource(candidate) {
		tree, parents, err := s.repo.CommitContents(ctx, candidate.Head)
		if err != nil {
			return err
		}
		if event.Candidate.CommittedAs(candidate, tree, parents) {
			return nil
		}
	}
	return failf("commit ticket does not match the verified delivery; run task next %s", task.ID)
}

func checkCommitIncludes(ctx context.Context, s *session, task *harness.Task, candidate string, includes []string) error {
	base := task.Start.Head
	if base == "" {
		var err error
		base, err = s.repo.EmptyTree(ctx)
		if err != nil {
			return err
		}
	}
	original, err := s.repo.Diff(ctx, base, task.Start.Tree)
	if err != nil {
		return err
	}
	staged, err := s.repo.Diff(ctx, base, task.Start.Index)
	if err != nil {
		return err
	}
	changed, err := s.repo.Diff(ctx, base, candidate)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, item := range append(original, staged...) {
		existing[item.Path] = true
	}
	for _, item := range changed {
		if existing[item.Path] && !slices.Contains(includes, item.Path) {
			return failf("pre-existing edit %q requires explicit --include after confirming ownership", item.Path)
		}
	}
	return nil
}

func extendHarness(ctx context.Context, s *session, task *harness.Task, o harnessOptions) error {
	if task.PendingScope {
		if o.scope != (harness.Scope{}) || o.reviewLimits == (reviewLimits{}) {
			return usagef("bind the saved scope with task open before changing the requested outcome")
		}
		return o.setLimits(task)
	}
	if task.Claim != nil {
		return failf("reconcile the active claim before extending scope")
	}
	previousCommit := task.Scope.Commit
	task.Scope.RequireReview = task.Scope.RequireReview || o.scope.RequireReview || (task.Scope.Until == "review" && o.scope.Until != "" && o.scope.Until != "review")
	if o.scope.Until != "" {
		if slices.Index(harness.Outcomes, o.scope.Until) < slices.Index(harness.Outcomes, task.Scope.Until) {
			return usagef("--until must extend the existing outcome")
		}
		task.Scope.Until = o.scope.Until
	}
	task.Scope.Repair = task.Scope.Repair || o.scope.Repair
	task.Scope.Commit = task.Scope.Commit || o.scope.Commit
	if err := extendDeliveryScope(&task.Scope, o.scope); err != nil {
		return err
	}
	if !previousCommit && task.Scope.Commit {
		if err := pinHarnessCommitTarget(ctx, s, task); err != nil {
			return err
		}
	}
	return o.setLimits(task)
}

func (o reviewLimits) setLimits(task *harness.Task) error {
	if o.limit > 0 {
		task.Deadline = time.Now().UTC().Add(o.limit)
	}
	if o.reserve > 0 {
		if task.Deadline.IsZero() {
			return usagef("--finish-reserve requires a task deadline from --time-limit")
		}
		task.FinishReserve = o.reserve
	}
	if o.reviews > 0 {
		task.ReviewLimit = o.reviews
	}
	return nil
}

func recordHarness(task *harness.Task, o harnessOptions, view harnessView) error {
	if replay, err := replayedHarnessResult(task, o); replay {
		return err
	}
	allowPausedReturn(task, &view)
	if o.action != view.Next || o.ticket != view.Ticket || !slices.Contains([]string{"intake", "plan", "implement", "review", "repair"}, o.action) {
		return failf("stale or ineligible action; resume with task next %s", o.id)
	}
	if task.Claim != nil && task.Claim.Ticket != o.ticket {
		return failf("source changed during the claimed action; reconcile and release the claim")
	}
	if err := checkHarnessActor(task, o.action, o.actor); err != nil {
		return err
	}
	correcting := o.action == "repair" || o.action == "implement" && task.Latest("implement") != nil
	if correcting && task.RepairBase().SameSource(view.Candidate) {
		return failf("%s has no changed candidate; inspect the outstanding finding or verification failure", o.action)
	}
	reviewIndex := o.action == "review" && task.Scope.Until == "review" && task.Scope.ReviewBase == ""
	event := harness.Event{Action: o.action, ReviewIndex: reviewIndex, Note: o.note, Actor: o.actor, Findings: o.findings, Ticket: o.ticket, Candidate: view.Candidate, At: time.Now().UTC()}
	if task.Claim != nil {
		event.Reviewer = task.Claim.Reviewer
		event.Developer = task.Claim.Developer
	}
	task.Claim = nil
	task.Events = append(task.Events, event)
	return nil
}

func loadHarness(ctx context.Context, s *session, l *stageLedger, o harnessOptions, verb string) (harness.Task, error) {
	var task harness.Task
	var rangeStart harness.Snapshot
	if o.scope.ReviewBase != "" {
		var err error
		o.scope, rangeStart, err = resolveReviewRange(ctx, s, o.scope)
		if err != nil {
			return task, err
		}
	}
	err := l.read(harnessFile(o.id), &task)
	if task.Version == 1 && o.defaultDeliveryEntry {
		o.scope.Entry = "review"
	}
	if errors.Is(err, os.ErrNotExist) && verb == "open" {
		return createHarness(ctx, s, l, o, rangeStart)
	} else if err != nil {
		return task, fmt.Errorf("read saved task %s: %w", o.id, err)
	} else if !task.Valid(o.id) {
		return task, failf("invalid saved task %s", o.id)
	} else if task.PendingScope && verb == "open" {
		if err := bindHarnessScope(ctx, s, l, &task, o, rangeStart); err != nil {
			return task, err
		}
	} else if task.PendingScope && verb != "next" && verb != "extend" {
		return task, failf("bind the saved request with task open %s --entry STAGE --until OUTCOME", task.ID)
	} else if verb == "open" && (!o.matchesSavedScope(&task)) {
		return task, failf("task %s already exists with another request or scope; use task next or a new name", o.id)
	}
	if task.Scope.Commit {
		task.CommitTarget = task.AuthorizedCommitTarget()
	}
	return task, nil
}

func createHarness(ctx context.Context, s *session, l *stageLedger, o harnessOptions, start harness.Snapshot) (harness.Task, error) {
	t := harness.Task{Version: harness.CurrentVersion, ID: o.id, Scope: o.scope, Request: o.request, Created: time.Now().UTC(), Start: start}
	if strings.TrimSpace(o.request) == "" {
		return t, usagef("--request is required for a new task")
	}
	if start.Head == "" {
		var err error
		t.Start, err = harnessSnapshot(ctx, s)
		if err != nil {
			return t, err
		}
	}
	if err := pinHarnessCommitTarget(ctx, s, &t); err != nil {
		return t, err
	}
	if err := o.setLimits(&t); err != nil {
		return t, err
	}
	return t, l.write(harnessFile(o.id), t)
}

func pinHarnessCommitTarget(ctx context.Context, s *session, task *harness.Task) error {
	if !task.Scope.Commit {
		task.CommitTarget = ""
		return nil
	}
	if task.CommitTarget != "" {
		return nil
	}
	var err error
	task.CommitTarget, err = s.repo.CommitTarget(ctx)
	return err
}

func (o *harnessOptions) matchesSavedScope(task *harness.Task) bool {
	scope := o.scope
	// Replaying the bind may omit a requirement already pinned at startup.
	scope.RequireReview = scope.RequireReview || task.Scope.RequireReview
	return task.Scope == scope && (o.request == "" || o.request == task.Request) &&
		(o.reviews == 0 || o.reviews == task.ReviewLimit) && (o.reserve == 0 || o.reserve == task.FinishReserve)
}

func applyHarness(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, verb string, view harnessView) error {
	if !task.Deadline.IsZero() && view.Next != "done" && (verb == "commit" || verb == "confirm" || verb == "test") {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, task.Deadline)
		defer cancel()
	}
	switch verb {
	case "confirm":
		return confirmHarness(ctx, s, task, o, view)
	case "claim":
		if o.worker != "" {
			return linkHarnessWorker(task, o)
		}
		return claimHarness(s.repo.Root, task, o, view)
	case "release":
		claim := task.Claim
		if claim == nil || claim.Ticket != o.ticket {
			return failf("release requires the saved claim ticket")
		}
		// A released developer may have left edits in the next review candidate.
		if claim.Developer != nil && claim.Worker != "" && !task.UsedSession(claim.Worker) {
			task.ReleasedDevelopers = append(task.ReleasedDevelopers, claim.Worker)
		}
		task.Claim = nil
		task.Generation++
	case "commit":
		return commitHarness(ctx, s, l, task, o, view)
	case "extend":
		return extendHarness(ctx, s, task, o)
	case "record":
		return recordHarness(task, o, view)
	case "test":
		return testHarnessAction(ctx, s, l, task, o, view)
	}
	return nil
}

// Both transitions share the same ledger lock and write. Replaying a result
// may recover the receipt, but must never launch or reclaim a second reviewer.
func recordAndClaimReview(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, view harnessView) error {
	reviewOwner := o.actor
	if task.Claim != nil && task.Claim.Developer != nil {
		reviewOwner = task.Claim.Owner
	}
	replay, _ := replayedHarnessResult(task, o)
	if err := recordHarness(task, o, view); err != nil {
		return err
	}
	updated, err := observeHarness(ctx, s, task, o.json && !o.brief)
	if err != nil {
		return err
	}
	if !replay && updated.Next == "review" {
		o.ticket, o.actor = updated.Ticket, reviewOwner
		if err := claimHarness(s.repo.Root, task, o, updated); err != nil {
			return err
		}
		updated.Claim, updated.ReviewStarts = task.Claim, task.StartedReviews()
		updated.ClaimCreated = true
	}
	if !replay {
		if err := l.write(harnessFile(task.ID), task); err != nil {
			return err
		}
	}
	return printHarness(s.cfg, o, updated)
}

func replayedHarnessResult(task *harness.Task, o harnessOptions) (bool, error) {
	for _, event := range task.Events {
		if event.Ticket != o.ticket {
			continue
		}
		if event.Action == o.action && event.Note == o.note && event.Actor == o.actor && slices.Equal(event.Findings, o.findings) {
			return true, nil
		}
		return true, failf("action ticket already recorded with different results")
	}
	return false, nil
}

func (o *harnessOptions) validateOpen() error {
	if o.scope.Until == "" {
		return usagef("task open requires --until with one of %s", strings.Join(taskUntil.choices, ", "))
	}
	if o.scope.Entry == "" {
		o.scope.Entry = o.scope.Until
		if o.scope.Entry == "deliver" {
			o.defaultDeliveryEntry = true
			o.scope.Entry = "verify"
		}
	}
	if err := o.scope.Validate(); err != nil {
		return usagef("%v", err)
	}
	return nil
}

func testHarnessAction(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, view harnessView) error {
	prior := task.Proof.FailedAssertion()
	if o.retry {
		switch {
		case prior != nil:
			o.expect, o.argv = prior.Expect, prior.Argv
		case task.Proof != nil:
			o.expect, o.argv = task.Proof.Expect, task.Proof.Argv
		}
		if len(o.argv) == 0 || o.argv[0] == "" || strings.TrimSpace(o.expect) == "" {
			return failf("no saved behavioral assertion; supply --expect and a command after --")
		}
	}
	if task.Proof != nil && view.Verified && slices.Equal(o.argv, task.Proof.Argv) && o.expect == task.Proof.Expect && (o.ticket == task.Proof.Ticket || o.ticket == task.Proof.CompletionTicket) {
		return nil
	}
	if o.ticket != view.Ticket || (view.Next != "verify" && !canRetestBeforeEdit(task, o, view)) {
		return failf("test needs the current verification ticket or an unclaimed correction ticket for the same failed candidate")
	}
	if prior != nil && !slices.Equal(o.argv, prior.Argv) && strings.TrimSpace(o.replaceReason) == "" {
		return failf("assertion from run %s has an unresolved failure; use --retry or explain the corrected command with --replace-reason", prior.Run)
	}
	return runHarnessTest(ctx, s, l, task, o, view)
}

// A mistaken or transient assertion can be retried before changing the source.
func canRetestBeforeEdit(task *harness.Task, o harnessOptions, view harnessView) bool {
	p := task.Proof
	if (view.Next != "repair" && view.Next != "implement") || task.Claim != nil || p == nil || task.Events[len(task.Events)-1].Action != "test" {
		return false
	}
	correction := p.FailedAssertion() != nil && strings.TrimSpace(o.replaceReason) != "" && !slices.Equal(o.argv, p.Argv)
	return (o.retry || correction) && view.Candidate.SameSource(p.Candidate) && view.Candidate.SameBase(p.Candidate)
}

func (o *harnessOptions) validateResult(action string) error {
	o.action = action
	if o.host != "" && (!slices.Contains(roles.Hosts, o.host) || !slices.Contains([]string{"intake", "implement", "repair"}, action)) {
		return usagef("--claim-review requires a codex or claude host on intake, implement or repair")
	}
	if strings.TrimSpace(o.note) == "" || strings.TrimSpace(o.actor) == "" || o.ticket == "" {
		return usagef("record requires --note, --actor and --ticket")
	}
	if len(o.findings) > 0 && o.action != "review" {
		return usagef("--finding is a review result")
	}
	for _, item := range o.findings {
		if strings.TrimSpace(item) == "" {
			return usagef("empty finding")
		}
	}
	return nil
}

func bindHarnessScope(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o harnessOptions, rangeStart harness.Snapshot) error {
	if task.Scope.Until != "" && task.Scope.Until != o.scope.Until {
		return failf("requested stopping point is %s", task.Scope.Until)
	}
	if o.request != "" && o.request != task.Request {
		return failf("request differs from the original task")
	}
	if !task.Deadline.IsZero() && o.limit > 0 || task.ReviewLimit > 0 && o.reviews > 0 && task.ReviewLimit != o.reviews || task.FinishReserve > 0 && o.reserve > 0 && task.FinishReserve != o.reserve {
		return failf("limits were pinned at startup; bind the scope and use task extend for an authorized change")
	}
	o.scope.RequireReview = o.scope.RequireReview || task.Scope.RequireReview
	if err := o.scope.Validate(); err != nil {
		return usagef("%v", err)
	}
	task.PendingScope = false
	task.Scope = o.scope
	if rangeStart.Head != "" {
		task.Start = rangeStart
	}
	if err := pinHarnessCommitTarget(ctx, s, task); err != nil {
		return err
	}
	if err := o.setLimits(task); err != nil {
		return err
	}
	return l.write(harnessFile(o.id), task)
}
