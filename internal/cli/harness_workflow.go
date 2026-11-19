package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func workflowCommand() uxCommand {
	return uxCommand{
		path: "task run", args: "<request> | --resume <task>",
		summary:  "Run or resume the repository's configured role workflow",
		operands: [][2]string{{"request", "one complete developer request"}},
		flags: append([]uxFlag{
			uxOption("id", "task", "save a new task under this ID"),
			uxOption("resume", "task", "resume the saved role assignments and evidence"),
			uxOption("until", "outcome", "finish at plan, implement, review, verify or deliver"),
			uxOption("entry", "stage", "start at plan, implement, tests, review or verify"),
			uxOption("repair", "", "authorize source repairs on an existing candidate"),
			uxOption("commit", "", "deliver one verified local commit"),
			uxOption("include", "path", "include a pre-existing change in the authorized commit"),
			uxOption("expect", "text", "preauthorize an assertion with -- PROGRAM ARGS"),
			requireReviewFlag(),
			uxOption("native-host", "host", "offer compatible assignments to this authenticated host session", roles.Hosts...),
			uxOption("actor", "session", "current native parent session for --native-host"),
			uxOption("retry", "", "retry the current stage after resolving a reported problem"),
			uxOption("approve-assertion", "", "review and approve the saved assertion in your terminal"),
			uxOption("release", "attempt", "reconcile an outstanding attempt after confirming its worker stopped"),
			uxOption("rework-limit", "count", "maximum automatic test and code rework starts"),
			uxOption("json", "", "return a bounded task status or native assignment"),
		}, reviewLimitFlags()...),
		examples: []string{`kitsu task run "Fix CSV quoting" --until verify`, "kitsu task run --resume export-quoting"}, run: runConfiguredWorkflow,
	}
}

type workflowOptions struct {
	harnessOptions
	resume, nativeHost, actorID, release string
	reworkMax                            int
	approveAssertion                     bool
}

func parseWorkflow(args []string) (workflowOptions, error) {
	f := newFlags("task run")
	o := workflowOptions{harnessOptions: harnessOptions{scope: harness.Scope{Entry: "plan", Until: "verify"}}}
	f.StringVar(&o.id, "id", "", "")
	f.StringVar(&o.resume, "resume", "", "")
	f.StringVar(&o.scope.Entry, "entry", "plan", "")
	f.StringVar(&o.scope.Until, "until", "verify", "")
	f.BoolVar(&o.scope.RequireReview, "require-review", false, "")
	f.BoolVar(&o.scope.Repair, "repair", false, "")
	f.BoolVar(&o.scope.Commit, "commit", false, "")
	f.Var(&o.includes, "include", "")
	f.StringVar(&o.expect, "expect", "", "")
	f.StringVar(&o.nativeHost, "native-host", "", "")
	f.StringVar(&o.actorID, "actor", "", "")
	f.StringVar(&o.release, "release", "", "")
	f.IntVar(&o.reworkMax, "rework-limit", 0, "")
	f.BoolVar(&o.retry, "retry", false, "")
	f.BoolVar(&o.approveAssertion, "approve-assertion", false, "")
	f.BoolVar(&o.json, "json", false, "")
	o.bindLimitFlags(f)
	p, err := parseArgs(f, args)
	if err != nil {
		return o, err
	}
	if err := o.validateLimits(f); err != nil {
		return o, err
	}
	o.argv = p.rest
	if o.nativeHost != "" && (!slices.Contains(roles.Hosts, o.nativeHost) || !nativeSessionID.MatchString(o.actorID)) ||
		o.nativeHost == "" && o.actorID != "" {
		return o, usagef("--native-host and the current --actor session must be supplied together")
	}
	if o.reworkMax < 0 {
		return o, usagef("--rework-limit must be positive")
	}
	if o.resume != "" {
		if err := o.validateResume(f, p); err != nil {
			return o, err
		}
		o.id = o.resume
		return o, nil
	}
	return o.openRequest(p)
}

func (o workflowOptions) openRequest(p parsed) (workflowOptions, error) {
	if len(p.pos) != 1 || strings.TrimSpace(p.pos[0]) == "" || o.retry || o.release != "" || o.approveAssertion {
		return o, usagef("supply one request or use --resume TASK")
	}
	if o.id == "" {
		o.id = "task-" + strings.ToLower(rand.Text()[:10])
	}
	o.request = p.pos[0]
	if !validHarnessID(o.id) {
		return o, usagef("invalid task ID")
	}
	if err := harness.ValidateWorkflowScope(o.scope); err != nil {
		return o, usagef("%v", err)
	}
	needsAssertion := o.scope.Entry == "verify" || o.scope.Entry == "review" && o.scope.Until != "review"
	if (p.dash || o.expect != "") && (len(o.argv) == 0 || strings.TrimSpace(o.expect) == "") {
		return o, usagef("a supplied assertion requires --expect TEXT -- PROGRAM ARGS")
	}
	if needsAssertion && len(o.argv) == 0 {
		return o, usagef("verification entry requires an assertion after --")
	}
	return o, nil
}

func (o workflowOptions) validateResume(f *flag.FlagSet, p parsed) error {
	if !validHarnessID(o.resume) || len(p.pos) != 0 || p.dash || o.release != "" && !nativeSessionID.MatchString(o.release) {
		return usagef("resume requires a saved task ID and no new request")
	}
	var err error
	f.Visit(func(f *flag.Flag) {
		if !slices.Contains([]string{"resume", "native-host", "actor", "retry", "release", "rework-limit", "json", "include", "approve-assertion"}, f.Name) {
			err = usagef("use task extend to change scope on a saved task")
		}
	})
	return err
}

func runConfiguredWorkflow(ctx context.Context, cfg Config, args []string) error {
	o, err := parseWorkflow(args)
	if err != nil {
		return err
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	if err := workflowIntake(ctx, s, o); err != nil {
		return err
	}
	if o.approveAssertion {
		if err := approveWorkflowAssertion(ctx, s, o); err != nil {
			return err
		}
	}
	if o.resume == "" {
		fmt.Fprintf(cfg.Err, "Kitsu task %s. Resume with kitsu task run --resume %s\n", o.id, o.id)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		step, err := advanceWorkflow(ctx, s, o)
		if err != nil {
			return err
		}
		if step.attempt == nil {
			if step.again {
				continue
			}
			return reportWorkflow(s, o, step)
		}
		if step.attempt.Native {
			return reportWorkflow(s, o, step)
		}
		if err := launchWorkflowAttempt(ctx, s, o, step); err != nil {
			return err
		}
	}
}

func workflowIntake(ctx context.Context, s *session, o workflowOptions) error {
	l, err := openStageLedger(ctx, s)
	if err != nil {
		return err
	}
	defer l.close()
	var task harness.Task
	err = l.read(harnessFile(o.id), &task)
	if o.resume != "" {
		if err != nil {
			return err
		}
		if !task.Valid(o.id) || task.Workflow == nil {
			return failf("task %s has no configured role workflow", o.id)
		}
		return reconcileWorkflowOptions(ctx, s, l, &task, o)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return err
		}
		return failf("task %s already exists; use --resume %s", o.id, o.id)
	}
	roleConfig, err := dailyRoleConfig(s.repo.Root)
	if err != nil {
		return err
	}
	assigned, err := roleConfig.ResolveWorkflow(s.repo.Root)
	if err != nil {
		return err
	}
	task = harness.Task{
		Version: harness.WorkflowVersion, ID: o.id, Request: o.request, Scope: o.scope, Created: time.Now().UTC(),
		Workflow: &harness.Workflow{Roles: assigned, TestPolicy: roleConfig.Tests, Includes: o.includes, ReworkMax: 2},
	}
	if o.reworkMax > 0 {
		task.Workflow.ReworkMax = o.reworkMax
	}
	if len(o.argv) > 0 {
		task.Workflow.Test = &harness.WorkflowAssertion{Argv: o.argv, Expect: o.expect}
		task.Workflow.ApprovedArgv = workflowArgvKey(o.argv)
	}
	task.Start, err = harnessCandidate(ctx, s, &task)
	if err != nil {
		return err
	}
	task.Workflow.Candidate = &task.Start
	if err = pinHarnessCommitTarget(ctx, s, &task); err != nil {
		return err
	}
	if err = pinHarnessPolicy(ctx, s, &task); err != nil {
		return err
	}
	if err = o.setLimits(&task); err != nil {
		return err
	}
	task.Events = append(task.Events, harness.Event{
		Action: "intake", Note: o.request, Candidate: task.Start, At: task.Created,
		Ticket: actionTicket(&task, "intake", task.Start),
	})
	return l.write(harnessFile(o.id), &task)
}

func reconcileWorkflowOptions(ctx context.Context, s *session, l *stageLedger, task *harness.Task, o workflowOptions) error {
	if len(o.includes) == 0 && o.release == "" && !o.retry && o.reworkMax == 0 {
		return nil
	}
	if task.CompletedDelivery() != nil {
		return failf("delivery is complete; open a new configured task for more work")
	}
	if err := addWorkflowIncludes(task, o); err != nil {
		return err
	}
	if err := recoverWorkflowAttempt(ctx, s, task, o); err != nil {
		return err
	}
	w := task.Workflow
	if o.reworkMax > 0 {
		if o.reworkMax < w.Rework {
			return failf("rework limit cannot be less than starts already recorded")
		}
		w.ReworkMax = o.reworkMax
	}
	return l.write(harnessFile(task.ID), task)
}

func addWorkflowIncludes(task *harness.Task, o workflowOptions) error {
	if len(o.includes) == 0 {
		return nil
	}
	w := task.Workflow
	if w.Active != nil || o.nativeHost != "" || !task.Scope.Commit {
		return failf("confirm pre-existing edits for the authorized commit from your terminal after workers stop")
	}
	for _, name := range o.includes {
		if !slices.Contains(w.Includes, name) {
			w.Includes = append(w.Includes, name)
		}
	}
	return nil
}

func recoverWorkflowAttempt(ctx context.Context, s *session, task *harness.Task, o workflowOptions) error {
	w := task.Workflow
	if o.release != "" {
		if w.Active == nil || w.Active.ID != o.release {
			return failf("release must name the saved outstanding attempt")
		}
		if o.nativeHost != "" || !onboardingTerminal(s.cfg) {
			return failf("confirm the worker stopped and release it from your terminal")
		}
		finishWorkflowAttempt(task, "released")
		task.Generation++
	}
	if o.retry {
		if w.Active != nil {
			return failf("reconcile the outstanding worker before retrying")
		}
		c, err := harnessCandidate(ctx, s, task)
		if err != nil {
			return err
		}
		if w.Candidate != nil && *w.Candidate != c {
			return failf("restore the accepted candidate before retrying")
		}
		w.Problem = ""
	}
	return nil
}

type workflowStep struct {
	attempt    *harness.WorkflowAttempt
	settings   roles.SpecialistSettings
	view       harnessView
	assignment string
	deadline   time.Time
	again      bool
	message    string
}

func reportWorkflow(s *session, o workflowOptions, step workflowStep) error {
	if step.attempt != nil || o.json {
		v := step.view.brief()
		if step.attempt != nil {
			v["assignment"], v["attempt"], v["role_settings"] = step.assignment, step.attempt, step.settings
		}
		if step.message != "" {
			v["diagnostic"] = step.message
		}
		if err := printJSON(s.cfg, v); err != nil {
			return err
		}
	} else {
		if step.message != "" {
			fmt.Fprintln(s.cfg.Err, step.message)
		}
		if err := printHarness(s.cfg, o.harnessOptions, step.view); err != nil {
			return err
		}
	}
	if step.attempt != nil || step.view.Next == "done" {
		return nil
	}
	return exit(ExitNotVerified)
}

const workflowActiveFile = "workflow-active.json"

type workflowWorktreeClaim struct {
	Task    string `json:"task"`
	Attempt string `json:"attempt"`
}

// An outstanding daily assignment owns this worktree until its worker is
// accounted for, including when the process holding the ledger lock has exited.
func workflowActiveTask(l *stageLedger, exceptID string) (string, error) {
	var claim workflowWorktreeClaim
	if err := l.read(workflowActiveFile, &claim); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if !validHarnessID(claim.Task) || !nativeSessionID.MatchString(claim.Attempt) {
		return "", failf("invalid configured worktree claim")
	}
	if claim.Task == exceptID {
		return "", nil
	}
	task, err := readWorkflow(l, claim.Task)
	if err != nil {
		return "", err
	}
	if active := task.Workflow.Active; active != nil {
		if active.ID != claim.Attempt {
			return "", failf("configured worktree claim does not match its saved attempt")
		}
		return claim.Task, nil
	}
	return "", nil
}

func finishWorkflowAttempt(task *harness.Task, outcome string) {
	w := task.Workflow
	if attempt := w.Active; attempt != nil {
		attempt.Outcome = outcome
		if attempt.Elapsed == 0 {
			attempt.Elapsed = time.Since(attempt.Started)
		}
		attempt.Sources = nil
		w.Attempts = append(w.Attempts, *attempt)
		w.Active = nil
	}
	task.Claim = nil
}

// All coordinator output is tied to the saved task. Worker narratives go to
// the ledger, while the CLI emits a single bounded action or outcome.
func workflowCheckOutput(cfg Config, id, action string) io.Writer {
	if cfg.Err != nil {
		fmt.Fprintf(cfg.Err, "Kitsu task %s · %s\n", id, action)
	}
	return io.Discard
}
