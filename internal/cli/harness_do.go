package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/tickets"
)

var doEntry = uxOption("entry", "stage", "implement for your own change, verify for a supplied one", "implement", "verify")

func harnessDoCommand(request, commit, brief, json uxFlag) uxCommand {
	return uxCommand{
		path:    "task do",
		args:    "<task> [-- <program> [<argument>...]]",
		summary: "Open, record, verify and commit a task in one command, stopping where another session or a decision is needed",
		flags: []uxFlag{
			request,
			uxOption("from", "source", "bug ticket source URL or Markdown file"),
			uxOption("stdin", "", "read bug ticket Markdown from standard input and retain its source link"),
			doEntry,
			uxOption("expect", "text", "observable behavior asserted by the command after --"),
			uxOption("replace-reason", "text", "explain why a failing assertion needs a different command"),
			commit,
			uxOption("message", "text", "commit message; the verified candidate is committed with it"),
			uxOption("include", "path", "confirm a changed path that you are delivering; repeatable"),
			requireReviewFlag(),
			uxOption("actor", "id", "native session identity"),
			uxOption("claim-review", "host", "reserve the required independent review", roles.Hosts...),
			brief, json,
		},
		examples: []string{
			`kitsu task do export-json --request "Add JSON export" --expect "An empty export is []" -- go test ./export`,
			`kitsu task do export-json --message "Add JSON export" --include export.go`,
		},
		run: harnessCommand("do"),
	}
}

func (o *harnessOptions) bindDoFlags(f *flag.FlagSet) {
	f.StringVar(&o.request, "request", "", "")
	f.StringVar(&o.from, "from", "", "")
	f.BoolVar(&o.stdin, "stdin", false, "")
	doEntry.bindChoice(f, &o.scope.Entry)
	f.StringVar(&o.expect, "expect", "", "")
	f.StringVar(&o.replaceReason, "replace-reason", "", "")
	f.BoolVar(&o.scope.Commit, "commit", false, "")
	f.StringVar(&o.message, "message", "", "")
	f.Var(&o.includes, "include", "")
	f.BoolVar(&o.scope.RequireReview, "require-review", false, "")
	f.StringVar(&o.actor, "actor", "", "")
	f.StringVar(&o.host, "claim-review", "", "")
}

func (o harnessOptions) validateDo(dash bool) (harnessOptions, error) {
	if dash != (strings.TrimSpace(o.expect) != "") || dash && (len(o.argv) == 0 || o.argv[0] == "") {
		return o, usagef("task do takes --expect together with a command after --")
	}
	if o.host != "" && (!slices.Contains(roles.Hosts, o.host) || strings.TrimSpace(o.actor) == "") {
		return o, usagef("--claim-review requires a codex or claude host and --actor with this session's ID")
	}
	if o.from == "" && !o.stdin {
		return o, nil
	}
	if o.request != "" || o.scope.Entry != "" {
		return o, usagef("a bug ticket supplies its own request and starts at verification; leave out --request and --entry")
	}
	return o.withTicketSource()
}

// doHarness advances a task as far as one command can. It opens the task,
// records what the checkout already shows, runs the assertion and commits,
// each through the same step a separate command would take. It stops at the
// first step that needs another session, a decision or a change, so running
// the same command again continues from there.
func doHarness(ctx context.Context, s *session, l *stageLedger, o harnessOptions) error {
	if active, err := l.active(); err != nil {
		return err
	} else if active != "" {
		return failf("finish the active managed interval %s first", active)
	}
	if err := checkOtherWorkflow(l, o.id, "do"); err != nil {
		return err
	}
	task, err := loadDoHarness(ctx, s, l, o)
	if err != nil {
		return err
	}
	if task.Workflow != nil || task.Feature != nil || task.Bug != nil && task.Bug.Source.Check != nil {
		return failf("task do covers daily tasks and bug tickets; continue %s with task next", task.ID)
	}
	view, err := observeHarness(ctx, s, &task, false)
	if err != nil {
		return err
	}
	if err := saveObservedDelivery(l, &task, view); err != nil {
		return err
	}
	last, view, err := o.doSteps(ctx, s, l, &task, view)
	if err != nil {
		return err
	}
	return printDo(s.cfg, o, last, view)
}

// doSteps takes each step this command may take and returns the last one with
// the state it left. Every finished step is saved before the next one starts.
func (o harnessOptions) doSteps(ctx context.Context, s *session, l *stageLedger, task *harness.Task, view harnessView) (string, harnessView, error) {
	last := "next"
	// Each step changes the next action, so a task never needs this many.
	for range 8 {
		verb, step := o.doStep(task, view)
		if verb == "" {
			break
		}
		var err error
		if verb == "receipt" {
			err = saveBugReceipt(ctx, s, l, task, view)
		} else {
			err = applyHarness(ctx, s, l, task, step, verb, view)
		}
		if err != nil {
			return last, view, err
		}
		if view, err = observeSavedHarness(ctx, s, l, task, false); err != nil {
			return last, view, err
		}
		last, view.ClaimCreated = verb, verb == "claim"
		if verb == "claim" || verb == "test" && !view.Verified || verb == "commit" && view.Delivery != "committed" {
			break
		}
	}
	return last, view, nil
}

// loadDoHarness opens a new task from the command's own scope, or continues a
// saved one. A saved task keeps its request and scope: widening them stays an
// explicit task extend.
func loadDoHarness(ctx context.Context, s *session, l *stageLedger, o harnessOptions) (harness.Task, error) {
	var saved harness.Task
	err := l.read(harnessFile(o.id), &saved)
	commit := strings.TrimSpace(o.message) != ""
	if errors.Is(err, os.ErrNotExist) {
		o.scope.Entry, o.scope.Until = cmp.Or(o.scope.Entry, "implement"), "verify"
		if o.from != "" {
			o.scope = harness.Scope{Entry: "verify", Repair: true, Commit: true, RequireReview: o.scope.RequireReview}
		}
		if o.scope.Commit {
			o.scope.Until = "deliver"
		} else if commit {
			return saved, usagef("--message needs --commit: a commit must be in the task's scope")
		}
		if err := o.scope.Validate(); err != nil {
			return saved, usagef("%v", err)
		}
		return loadHarness(ctx, s, l, o, "open")
	}
	if err == nil && o.asksMoreThan(&saved) {
		return saved, failf("task %s already exists with another request or scope; use a new name or task extend", o.id)
	}
	// A message that commits nothing would read as a finished delivery.
	if err == nil && commit && !saved.Scope.Commit {
		return saved, failf("task %s has no commit in its scope; add one with task extend %s --until deliver --commit", o.id, o.id)
	}
	o.scope, o.from = harness.Scope{}, ""
	return loadHarness(ctx, s, l, o, "do")
}

// asksMoreThan says whether the command names a request, ticket or scope that
// the saved task does not have. Leaving them out continues the saved task.
func (o harnessOptions) asksMoreThan(saved *harness.Task) bool {
	if o.from != "" && (saved.Bug == nil || !tickets.SameSource(o.source, saved.Bug.Source)) {
		return true
	}
	scope := o.scope.Entry != "" && o.scope.Entry != saved.Scope.Entry || o.scope.Commit && !saved.Scope.Commit ||
		o.scope.RequireReview && !saved.Scope.RequireReview
	return scope || o.request != "" && o.request != saved.Request
}

// doStep names the next step this command may take itself, with its options,
// or no step.
func (o harnessOptions) doStep(task *harness.Task, view harnessView) (string, harnessOptions) {
	step := harnessOptions{id: o.id, ticket: view.Ticket, brief: true, actor: cmp.Or(o.actor, "task-do")}
	claim := task.Claim
	switch next := view.Next; {
	case next == "intake":
		step.action, step.note = next, cmp.Or(o.expect, "The request is saved with the task")
		return "record", step
	case next == "review" && claim == nil && o.host != "":
		step.actor, step.host = o.actor, o.host
		return "claim", step
	case next == "review" && claim != nil && claim.ReviewID != "" && claim.Ticket == view.Ticket:
		// Only a review that the host observed is recorded here.
		step.action, step.actor, step.observedReview = next, "", true
		return "record", step
	case claim != nil:
	case next == "implement" || next == "repair" || next == "verify":
		return o.doCandidate(task, view, step)
	case next == "commit" && strings.TrimSpace(o.message) != "":
		step.note, step.includes = o.message, o.includes
		return "commit", step
	case next == "receipt":
		return "receipt", step
	}
	return "", step
}

// doCandidate hands the candidate to verification once the command carries an
// assertion or the task has saved one. A correction with no changed candidate
// only repeats the assertion.
func (o harnessOptions) doCandidate(task *harness.Task, view harnessView, step harnessOptions) (string, harnessOptions) {
	p := task.Proof
	if len(o.argv) == 0 && p == nil {
		return "", step
	}
	test := step
	test.argv, test.expect, test.replaceReason = o.argv, o.expect, o.replaceReason
	// The saved assertion, named again or left out, is a retry.
	test.retry = len(o.argv) == 0 || p != nil && slices.Equal(o.argv, p.Argv) && o.expect == p.Expect
	if view.Next == "verify" || canRetestBeforeEdit(task, test, view) {
		return "test", test
	}
	correcting := view.Next == "repair" || task.CurrentFeatureEvent(task.Latest("implement"))
	if correcting && task.RepairBase().SameSource(view.Candidate) {
		return "", step
	}
	step.action, step.note = view.Next, "Candidate handed to task do for verification"
	return "record", step
}

// The reply of task do holds what is true now and the one thing left to do.
var doReplyKeys = []string{
	"task", "next", "verified", "delivery", "repository_checks", "failed_check", "ticket_markdown",
	"claim", "claim_created", "review_input", "review_status", "review_findings", "pause", "resume", "delivery_receipt",
}

// doHint says what the caller does before running the command again.
func doHint(view harnessView, failed bool) string {
	switch next := view.Next; {
	case failed:
		return "read the log, correct the candidate and run the command again"
	case next == "review" && view.Claim == nil:
		return "run again with --actor SESSION and --claim-review HOST"
	case next == "review":
		return "launch the reviewer on review_input, then run the command again"
	case next == "commit":
		return "stage the verified candidate and run again with --message"
	case next == "implement" || next == "repair" || next == "verify":
		return "change the candidate and run again with --expect and a command after --; a corrected command needs --replace-reason"
	}
	return ""
}

func printDo(cfg Config, o harnessOptions, verb string, view harnessView) error {
	if o.brief || o.json {
		return printHarnessResult(cfg, verb, o, view)
	}
	view.describeCommitReadiness()
	full, reply := view.brief(), map[string]any{}
	for _, key := range doReplyKeys {
		if value, ok := full[key]; ok {
			reply[key] = value
		}
	}
	if work := view.Next == "implement" || view.Next == "repair"; work && view.Review != nil && len(view.Review.Findings) > 0 {
		reply["findings"] = view.Review.Findings
	}
	if view.Delivery == "committed" && view.Scope.Commit {
		reply["commit"] = view.Candidate.Head
	}
	failed := verb == "test" && !view.Verified
	if log := view.verificationLog(); failed && log != nil {
		reply["log"] = log.Path
	}
	if hint := doHint(view, failed); hint != "" {
		reply["then"] = hint
	}
	if err := json.NewEncoder(cfg.Out).Encode(reply); err != nil {
		return err
	}
	if failed || verb == "commit" && view.Delivery != "committed" {
		return exit(ExitNotVerified)
	}
	return nil
}
