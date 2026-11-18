package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

type uxViewArgs struct {
	host, task string
	json       bool
	roots      stringList
}

func uxParseView(name string, args []string, extra ...string) (parsed, uxViewArgs, error) {
	flags, options := newFlags(name), uxViewArgs{}
	flags.BoolVar(&options.json, "json", false, "")
	for _, field := range extra {
		switch field {
		case "host":
			flags.StringVar(&options.host, "host", "", "")
		case "task":
			flags.StringVar(&options.task, "task", "", "")
		case "root":
			flags.Var(&options.roots, "root", "")
		}
	}
	p, err := parseArgs(flags, args)
	if err == nil && (p.dash || options.task != "" && !taskName.MatchString(options.task)) {
		err = usagef("invalid arguments or task name")
	}
	return p, options, err
}

func runUXRoleShow(ctx context.Context, cfg Config, args []string) error {
	p, options, err := uxParseView("role show", args, "host", "task")
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || !roles.Known(p.pos[0]) && p.pos[0] != roles.Coordinator {
		return usagef("show requires one known role, including coordinator")
	}
	config, err := rolesConfig(ctx)
	if err != nil {
		return err
	}
	access, err := roleAdviserAccess(ctx, options.task)
	if err != nil {
		return failf("%v", err)
	}
	id := p.pos[0]
	selected := roles.Role{ID: id, Job: "Coordinates roles and records", Writes: "workflow coordination records", Models: map[string]roles.Model{}}
	for _, host := range roles.Hosts {
		selected.Models[host] = roles.Model{Model: "host session", Effort: "host setting"}
	}
	for _, role := range roles.All(config) {
		if role.ID == id {
			selected = role
		}
	}
	if options.host != "" && !slices.Contains(roles.Hosts, options.host) {
		return usagef("--host must be codex or claude")
	}
	if options.json {
		models, boundSkills := selected.Models, roleSkills(config, id)
		if options.host != "" {
			models = map[string]roles.Model{options.host: models[options.host]}
			boundSkills = map[string][]string{options.host: boundSkills[options.host]}
		}
		return printJSON(cfg, struct {
			Role     string                 `json:"role"`
			Job      string                 `json:"job"`
			Writes   string                 `json:"writes"`
			Models   map[string]roles.Model `json:"models"`
			Skills   map[string][]string    `json:"skills"`
			Advisers []string               `json:"advisers"`
		}{id, selected.Job, selected.Writes, models, boundSkills, allowedAdviserList(access, id)})
	}
	fmt.Fprintf(cfg.Out, "%s: %s\nMay change: %s\n", id, selected.Job, selected.Writes)
	for _, host := range roles.Hosts {
		if options.host != "" && host != options.host {
			continue
		}
		model := selected.Models[host]
		writeRoleLine(cfg.Out, fmt.Sprintf("%s: %s, reasoning %s", host, model.Model, model.Effort))
		writeRoleLine(cfg.Out, "  Skills: "+skillListText(selectedSkills(config, host, id)))
	}
	writeRoleLine(cfg.Out, "Advisers: "+adviserListText(access, id))
	return nil
}

func runUXSkillShow(ctx context.Context, cfg Config, args []string) error {
	p, options, err := uxParseView("skill show", args, "host", "root")
	if err != nil {
		return err
	}
	if len(p.pos) != 1 {
		return usagef("show requires one skill reference")
	}
	root, err := skillWorkingRoot(ctx)
	if err != nil {
		return failf("%v", err)
	}
	if filepath.IsAbs(p.pos[0]) {
		dir := p.pos[0]
		if filepath.Base(dir) == "SKILL.md" {
			dir = filepath.Dir(dir)
		}
		options.roots = append(options.roots, dir)
	}
	catalog, err := skills.DiscoverWithRoots(root, options.host, options.roots)
	if err != nil {
		return failf("%v", err)
	}
	var found []skills.Entry
	for _, entry := range catalog.Skills {
		if entry.ID == p.pos[0] || uxSameSkillPath(entry.Path, p.pos[0]) {
			found = []skills.Entry{entry}
			break
		}
		if entry.Name == p.pos[0] {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		return failf("skill %q is missing or ambiguous; use an id from `kitsu skill list`", p.pos[0])
	}
	catalog.Skills = found
	return printSkills(cfg, options.host, catalog, options.json)
}

func uxSameSkillPath(path, ref string) bool {
	if !filepath.IsAbs(ref) {
		return false
	}
	if filepath.Base(ref) != "SKILL.md" {
		ref = filepath.Join(ref, "SKILL.md")
	}
	physical := func(value string) string {
		if resolved, err := filepath.EvalSymlinks(value); err == nil {
			return filepath.Clean(resolved)
		}
		return filepath.Clean(value)
	}
	return physical(path) == physical(ref)
}

// Daily list rows summarize the ledger without inspecting the current checkout.
type dailyTaskSummary struct {
	Request    string `json:"request"`
	LastAction string `json:"last_action,omitempty"`
}

func (v harnessView) printResumeNotes(out io.Writer) {
	if v.PriorNotes == nil {
		return
	}
	fmt.Fprintln(out, "Request:", decl.Plain(v.Request))
	v.printResumeScope(out)
	if v.ReviewLimit > 0 {
		fmt.Fprintf(out, "Review starts: %d of %d\n", v.ReviewStarts, v.ReviewLimit)
	}
	if !v.Deadline.IsZero() {
		fmt.Fprintln(out, "Deadline:", v.Deadline.Format(time.RFC3339Nano))
		if v.FinishReserve > 0 {
			fmt.Fprintln(out, "Finish reserve:", v.FinishReserve)
		}
	}
	if claim := v.Claim; claim != nil && claim.Candidate != nil {
		fmt.Fprintf(out, "Reviewer recovery label: kitsu-task: %s review %d\n", v.Task, v.ReviewStarts)
		if !v.Deadline.IsZero() {
			fmt.Fprintln(out, "Reviewer return by:", v.Deadline.Add(-v.FinishReserve).Format(time.RFC3339Nano))
		}
		if v.Next == "review" && claim.Ticket == v.Ticket {
			fmt.Fprintf(out, "Reviewer input: kitsu task show %s --review-ticket %s\n", v.Task, v.Ticket)
		}
	}
	if v.Claim != nil && v.Next == "paused" {
		fmt.Fprintln(out, "Reserved ticket:", v.Claim.Ticket)
	}
	for _, action := range []string{"intake", "plan", "implement", "repair"} {
		if note, ok := v.PriorNotes[action]; ok {
			fmt.Fprintf(out, "Recorded %s: %s\n", action, decl.Plain(note))
		}
	}
}

func (v harnessView) printResumeScope(out io.Writer) {
	var flags []string
	if v.Scope.Entry != "" {
		flags = append(flags, "--entry", v.Scope.Entry)
	}
	if v.Scope.Until != "" {
		flags = append(flags, "--until", v.Scope.Until)
	}
	if v.Scope.Repair {
		flags = append(flags, "--repair")
	}
	if v.Scope.Commit {
		flags = append(flags, "--commit")
	}
	if v.Scope.Publish != "" {
		flags = append(flags, "--publish-to", decl.Plain(v.Scope.Publish))
	}
	if v.Scope.PRBase != "" {
		flags = append(flags, "--pr-base", decl.Plain(v.Scope.PRBase))
	}
	if v.Scope.RequireReview {
		flags = append(flags, "--require-review")
	}
	if len(flags) > 0 {
		fmt.Fprintln(out, "Saved scope:", strings.Join(flags, " "))
	}
	if v.RepositoryReview {
		fmt.Fprintln(out, "Repository requires independent review.")
	}
}

// The opening candidate can already be dirty. Compare the saved snapshots so
// the diagnostic distinguishes new drift from the developer's original edits.
func planInputChange(opening, current harness.Snapshot) string {
	var changes []string
	if !opening.SameSource(current) {
		changes = append(changes, "working files")
	}
	if opening.Index != current.Index {
		changes = append(changes, "staged files")
	}
	if !opening.SameBase(current) {
		changes = append(changes, "Git parents")
	}
	return "Read-only planning inputs changed since opening (" + strings.Join(changes, ", ") + "). Open a new plan for the changed inputs, or resume this task when the original inputs match."
}

// Report commit readiness after routing and managed ownership have selected
// the action the caller can take. Existing commits keep their observed status.
func (v *harnessView) describeCommitReadiness() {
	if v.Next == "commit" && v.Ticket != "" && v.Candidate.Tree == v.Candidate.Index && v.Delivery == "verified working tree" {
		v.Delivery = "ready to commit"
	}
}

func (v harnessView) printVerification(out io.Writer) {
	if v.Proof != nil {
		fmt.Fprintf(out, "Behavior: %s (%s), run %s\n", decl.Plain(v.Proof.Expect), v.Behavior, v.Proof.Run)
	}
	if log := v.VerificationLog; log != nil {
		if log.Detail != "" {
			fmt.Fprintln(out, decl.Plain(log.Detail))
		}
		fmt.Fprintf(out, "Verification log for %s: %s\n", decl.Plain(log.CheckID), decl.Plain(log.Path))
	}
	fmt.Fprintf(out, "Verified on current content: %t · repository checks: %s · %s\n", v.Verified, v.Gate, v.Delivery)
	if v.CommitEvidence == "verified_existing_commit" {
		fmt.Fprintln(out, "The current verification ran with this commit already checked out.")
	}
}

func sourceChangeSummary(record store.Record) string {
	if record.Outcome != store.SourceChanged {
		return ""
	}
	return boundedSummary(decl.Plain(record.Detail), 240)
}

func boundedSummary(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

func summarizeDailyTask(task *harness.Task) *dailyTaskSummary {
	summary := &dailyTaskSummary{Request: boundedSummary(strings.Join(strings.Fields(task.Request), " "), 120)}
	if n := len(task.Events); n != 0 {
		summary.LastAction = task.Events[n-1].Action
	}
	return summary
}

type uxTaskView struct {
	DailyError   string                `json:"daily_error,omitempty"`
	Daily        *dailyTaskSummary     `json:"daily,omitempty"`
	Harness      *harnessView          `json:"harness,omitempty"`
	History      []harness.Event       `json:"events,omitempty"`
	Task         string                `json:"task"`
	Workflow     string                `json:"workflow"`
	Stage        string                `json:"stage"`
	Report       *change.Report        `json:"report,omitempty"`
	Verification *taskVerificationView `json:"verification,omitempty"`
	UsageBudget  []string              `json:"usage_budget_reference,omitempty"`
}

func uxTaskViews(ctx context.Context, s *session, named string) ([]uxTaskView, error) {
	tasks := []string{named}
	if named == "" {
		var err error
		tasks, err = uxSavedTasks(s.repo.Root)
		if err != nil {
			return nil, err
		}
	}
	views := []uxTaskView{}
	snapshot := sync.OnceValues(func() (string, error) { return s.repo.Snapshot(ctx, s.store.Tmp()) })
	verify := taskListVerification(ctx, s, named, snapshot)
	var gitOf func(*change.Folder) (change.Git, error)
	for _, task := range tasks {
		problems, err := skills.ValidateSelections(s.repo.Root, task)
		if err != nil || len(problems) > 0 {
			return nil, fmt.Errorf("unsafe or invalid skill history for %s: %v %s", task, err, strings.Join(problems, "; "))
		}
		folder, err := change.Read(s.repo.Root, task)
		if errors.Is(err, change.ErrNoFolder) {
			view, err := uxSmallTaskView(s, task, verify)
			if err != nil {
				return nil, err
			}
			views = append(views, view)
			continue
		}
		if err != nil {
			return nil, err
		}
		if gitOf == nil {
			tree, err := snapshot()
			if err != nil {
				return nil, err
			}
			gitOf, err = s.changeGitAt(ctx, tree)
			if err != nil {
				return nil, err
			}
		}
		report, err := uxTaskReport(s, folder, gitOf)
		if err != nil {
			return nil, err
		}
		v, err := verify(task)
		if err != nil {
			return nil, err
		}
		applyTaskVerification(report, v)
		views = append(views, uxTaskView{Task: task, Workflow: "full", Stage: report.Stage, Report: report, Verification: v})
	}
	return appendUsageBudgetReferences(s.repo.Root, views)
}

// Each list reads one policy for tasks with no verification history. An active
// policy or a historical record still gets the full freshness checks.
func taskListVerification(ctx context.Context, s *session, named string, snapshot func() (string, error)) func(string) (*taskVerificationView, error) {
	enabled := sync.OnceValues(func() (bool, error) {
		tree, err := snapshot()
		if err != nil {
			return false, err
		}
		policy, err := evidence.Declaration(ctx, s.repo, tree)
		if errors.Is(err, evidence.ErrNoDeclaration) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return slices.ContainsFunc(policy.Checks, func(c decl.Check) bool { return c.Verify }), nil
	})
	return func(task string) (*taskVerificationView, error) {
		if named == "" {
			on, err := enabled()
			if err != nil {
				return nil, err
			}
			if !on {
				has, err := hasVerificationHistory(s.repo.Root, task)
				if err != nil || !has {
					return nil, err
				}
			}
		}
		return observeTaskVerification(ctx, s, task)
	}
}

func uxTaskReport(s *session, folder *change.Folder, gitOf func(*change.Folder) (change.Git, error)) (*change.Report, error) {
	state, err := gitOf(folder)
	if err != nil {
		return nil, err
	}
	report, err := change.Status(folder, state)
	if err == nil {
		err = applyAdviserRequirements(s.repo.Root, report, state.Committed)
	}
	return report, err
}

func runUXTaskList(ctx context.Context, cfg Config, args []string) error {
	return uxPrintTasks(ctx, cfg, args, false)
}

func runUXTaskShow(ctx context.Context, cfg Config, args []string) error {
	return runCoordinatorTaskShow(ctx, cfg, args)
}

func uxPrintTasks(ctx context.Context, cfg Config, args []string, named bool) error {
	p, options, err := uxParseView("task", args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 && !named || named && (len(p.pos) != 1 || !taskName.MatchString(p.pos[0])) {
		return usagef("list accepts no task; show requires one valid task name")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	task := ""
	if named {
		task = p.pos[0]
	}
	views, err := combinedTaskViews(ctx, s, task)
	if err != nil && (named || views == nil) {
		return err
	}
	if options.json {
		if named {
			return printJSON(cfg, views[0])
		}
		reply := map[string]any{"tasks": views}
		if err != nil {
			reply["list_error"] = err.Error()
		}
		return errors.Join(err, printJSON(cfg, reply))
	}
	if len(views) == 0 && err == nil {
		fmt.Fprintln(cfg.Out, "No saved task records.")
	}
	return errors.Join(err, printTaskViews(cfg, views, named))
}

func printTaskViews(cfg Config, views []uxTaskView, named bool) error {
	for _, view := range views {
		fmt.Fprintf(cfg.Out, "%s: %s (%s)\n", view.Task, view.Stage, view.Workflow)
		if view.DailyError != "" {
			fmt.Fprintln(cfg.Out, "  Daily record:", decl.Plain(view.DailyError))
		}
		if view.Daily != nil {
			fmt.Fprintln(cfg.Out, "  "+decl.Plain(view.Daily.Request))
			if view.Daily.LastAction != "" {
				fmt.Fprintln(cfg.Out, "  Last recorded action:", decl.Plain(view.Daily.LastAction))
			}
		}
		if named {
			printTaskVerificationSummary(cfg, view.Verification)
		}
		if view.Harness != nil && named {
			return printHarness(cfg, harnessOptions{}, *view.Harness)
		} else if view.Workflow == "small" {
			fmt.Fprintln(cfg.Out, "  Saved history only; the workflow stage cannot be recovered from these files.")
		} else if named {
			for _, missing := range view.Report.Missing {
				fmt.Fprintln(cfg.Out, "  "+missing)
			}
			fmt.Fprintf(cfg.Out, "content ids: spec %s, design %s, code %s\n", orNone(view.Report.Content.Spec), orNone(view.Report.Content.Design), orNone(view.Report.Content.Code))
			if view.Report.NotAutomated > 0 || view.Report.BrokenRules > 0 {
				fmt.Fprintf(cfg.Out, "scenarios not automated: %d; broken rules: %d (kitsu task check %s)\n", view.Report.NotAutomated, view.Report.BrokenRules, view.Task)
			}
		}
	}
	return nil
}

func runUXCheckList(ctx context.Context, cfg Config, args []string) error {
	p, options, err := uxParseView("check list", args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 {
		return usagef("list accepts no arguments")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(s.repo.Root, decl.FileName))
	if err != nil {
		return failf("%v", err)
	}
	file, err := decl.Parse(data)
	if err != nil {
		return failf("%v", err)
	}
	if options.json {
		return printJSON(cfg, file)
	}
	for _, check := range file.Checks {
		fmt.Fprintf(cfg.Out, "%s  %s  (timeout %s)\n", check.ID, decl.Plain(strings.Join(check.Run, " ")), check.Timeout)
		if len(check.When) > 0 || check.Verify || check.Expect != "" {
			fmt.Fprintf(cfg.Out, "  when: %s; verify: %t; expect: %s\n", decl.Plain(strings.Join(check.When, ", ")), check.Verify, decl.Plain(check.Expect))
		}
	}
	return nil
}

// Preserve existing shared records even if a local task has the same name.
func combinedTaskViews(ctx context.Context, s *session, id string) ([]uxTaskView, error) {
	shared, sharedErr := uxTaskViews(ctx, s, id)
	if id != "" && sharedErr != nil && !errors.Is(sharedErr, change.ErrNoFolder) {
		return nil, sharedErr
	}
	views, dailyErr := appendDailyTaskViews(ctx, s, id, slices.Clone(shared))
	if dailyErr != nil && views == nil && id == "" && len(shared) > 0 {
		views = shared
	}
	if errors.Is(sharedErr, change.ErrNoFolder) && id != "" && len(views) > 0 {
		sharedErr = nil
	}
	if sharedErr != nil && id == "" {
		sharedErr = fmt.Errorf("cannot list shared tasks: %w", sharedErr)
	}
	return views, errors.Join(sharedErr, dailyErr)
}

func appendDailyTaskViews(ctx context.Context, s *session, id string, views []uxTaskView) ([]uxTaskView, error) {
	_, statErr := os.Lstat(filepath.Join(s.repo.GitDir, "kitsu-stages"))
	if errors.Is(statErr, os.ErrNotExist) {
		return views, nil
	}
	if statErr != nil {
		return nil, statErr
	}
	l, openErr := readOnlyStageLedger(s.repo.GitDir)
	if openErr != nil {
		return nil, openErr
	}
	defer l.close()
	entries, readErr := os.ReadDir(l.root.Name())
	if readErr != nil {
		return nil, readErr
	}
	shared := make(map[string]int, len(views))
	for i := range views {
		shared[views[i].Task] = i
	}
	unreadable := 0
	for _, entry := range entries {
		name := harnessRecordName(entry.Name())
		if name == "" || id != "" && name != id {
			continue
		}
		i, exists := shared[name]
		if !exists {
			views = append(views, uxTaskView{Task: name, Workflow: "harness", Stage: "saved"})
			i = len(views) - 1
		}
		if recordErr := readDailyTaskView(ctx, s, l, &views[i], id != ""); recordErr != nil {
			if id != "" {
				return nil, fmt.Errorf("read daily task %s: %w", name, recordErr)
			}
			views[i].DailyError = recordErr.Error()
			if !exists {
				views[i].Stage = "unreadable"
			}
			unreadable++
			continue
		}
	}
	if unreadable > 0 {
		if rootErr := l.verify(); rootErr != nil {
			return nil, rootErr
		}
		return views, fmt.Errorf("incomplete task list (unreadable daily records: %d)", unreadable)
	}
	return views, nil
}

func readDailyTaskView(ctx context.Context, s *session, ledger *stageLedger, view *uxTaskView, named bool) error {
	var task harness.Task
	if err := ledger.read(harnessFile(view.Task), &task); err != nil {
		return err
	}
	if !task.Valid(view.Task) {
		return errors.New("invalid task record")
	}
	if !named {
		view.Daily = summarizeDailyTask(&task)
		return nil
	}
	current, err := observeHarness(ctx, s, &task, true)
	if err != nil {
		return err
	}
	active, err := ledger.active()
	if err != nil {
		return err
	}
	current.checkManagedInterval(active)
	current.describeCommitReadiness()
	view.Harness, view.History = &current, task.Events
	if view.Workflow == "harness" {
		view.Stage = current.Next
	}
	return nil
}

func harnessRecordName(file string) string {
	name, ok := strings.CutPrefix(file, "task-harness-")
	if !ok {
		return ""
	}
	name, ok = strings.CutSuffix(name, ".json")
	if !ok || !taskName.MatchString(name) {
		return ""
	}
	return name
}

// Opening a local harness task opts this worktree into host usage notices.
func hasLocalHarnessTasks(repo *gitx.Repo) bool {
	ledger, err := readOnlyStageLedger(repo.GitDir)
	if err != nil {
		return false
	}
	defer ledger.close()
	entries, err := os.ReadDir(ledger.root.Name())
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Type().IsRegular() && harnessRecordName(e.Name()) != "" })
}
