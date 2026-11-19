package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

// tokensDir holds the token records of a clone.
func tokensDir(repo *gitx.Repo) string {
	return filepath.Join(store.Open(repo.CommonDir).Dir, "tokens")
}

// runTokens reports the tokens that were recorded, or records them.
func runTokens(ctx context.Context, cfg Config, args []string) error {
	if len(args) > 0 && args[0] == "record" {
		return runTokensRecord(ctx, cfg, args[1:])
	}
	flags := newFlags("tokens")
	task := flags.String("task", "", "")
	asJSON := flags.Bool("json", false, "")
	diagnostics := flags.Bool("diagnostics", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash {
		return usagef("usage: kitsu tokens [--task <task>] [--json], or kitsu tokens record [--host claude|codex]")
	}
	s, err := open(ctx, cfg, false)
	if err != nil {
		return err
	}
	if *diagnostics {
		return runUsageDiagnostics(ctx, cfg, *task, *asJSON)
	}
	starts, damaged, err := tokens.Load(tokensDir(s.repo))
	if err != nil {
		return failf("%v", err)
	}
	for _, file := range damaged {
		fmt.Fprintf(cfg.Err, "kitsu: warning: the token record %s cannot be read; its tokens are in no sum\n", file)
	}
	report := tokens.Build(starts, damaged)
	items, err := loadAdviserTokenActivity(s.repo.Root)
	if err != nil {
		return failf("%v", err)
	}
	activity := attachAdviserTokenActivity(report, items)
	if *task == "" {
		if *asJSON {
			return printJSON(cfg, adviserGlobalTokenJSON(report, activity))
		}
		printTokens(cfg.Out, report)
		printAdviserTokenUsage(cfg.Out, activity.all)
		return nil
	}
	return printNamedUsage(cfg, s.repo, *task, *asJSON, report, activity.byTask[*task])
}

func printNamedUsage(cfg Config, repo *gitx.Repo, task string, asJSON bool, report *tokens.Report, advisers *adviserTokenUsage) error {
	found, sessions, unlabelled, ok := report.Find(task)
	if !ok || found.Starts == 0 {
		daily, err := savedUsageTask(repo, task)
		if err != nil {
			return failf("read daily task usage: %v", err)
		}
		if daily != nil {
			return printDailySessionUsage(cfg, daily.ID, actorSessionUsage(daily.Events, report.Sessions), advisers, asJSON)
		}
	}
	if !ok {
		return failf("no start is recorded for the task `%s`", task)
	}
	if asJSON {
		return printJSON(cfg, struct {
			*tokens.Task
			Unlabelled []tokens.Unlabelled `json:"unlabelled"`
			Sessions   []tokens.Session    `json:"sessions"`
			Advisers   *adviserTokenUsage  `json:"advisers,omitempty"`
		}{found, unlabelled, sessions, advisers})
	}
	if found.Starts > 0 || advisers == nil {
		printTask(cfg.Out, found, sessions, unlabelled)
	} else {
		fmt.Fprintln(cfg.Out, found.Task+": no measured host token usage")
	}
	printAdviserTokenUsage(cfg.Out, advisers)
	return nil
}

func printJSON(cfg Config, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return failf("%v", err)
	}
	fmt.Fprintln(cfg.Out, string(data))
	return nil
}

// runTokensRecord reads the notice a host gives its hook when a role or a
// session ends, and stores the counts of the records the notice names. It
// records only in a repository with a declaration or an opened local harness task.
func runTokensRecord(ctx context.Context, cfg Config, args []string) error {
	if os.Getenv("KITSU_ADVISER_CALL") != "" {
		return nil
	}
	flags := newFlags("tokens record")
	host := flags.String("host", tokens.Claude, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash || (*host != tokens.Claude && *host != tokens.Codex) {
		return usagef("usage: kitsu tokens record [--host claude|codex], with the host's notice on standard input")
	}
	var notice tokens.Notice
	data, err := io.ReadAll(io.LimitReader(cfg.In, 1<<20))
	bad := err != nil || json.Unmarshal(data, &notice) != nil
	dir, err := notice.Dir()
	if err != nil {
		return failf("%v", err)
	}
	repo, err := gitx.Open(ctx, dir)
	if errors.Is(err, gitx.ErrNotRepo) || (err == nil && repo.Root == "") {
		return nil
	}
	if err != nil {
		return failf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, decl.FileName)); err != nil && !hasLocalHarnessTasks(repo) {
		if !bad {
			return observeManagedReview(ctx, repo, *host, notice)
		}
		return nil
	}
	if bad {
		return failf("the notice on standard input is not a JSON object")
	}
	switch notice.Event {
	case "SubagentStart":
		return errors.Join(observeNativeReview(ctx, repo, *host, notice), observeManagedReview(ctx, repo, *host, notice))
	case "SubagentStop":
		return errors.Join(observeNativeReview(ctx, repo, *host, notice), observeManagedReview(ctx, repo, *host, notice),
			tokens.Record(ctx, tokensDir(repo), *host, notice),
		)
	}
	if err := tokens.Record(ctx, tokensDir(repo), *host, notice); err != nil {
		return failf("%v", err)
	}
	return nil
}

// commas writes a number with a comma between its thousands.
func commas(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var out strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 && digits[i-1] != '-' {
			out.WriteByte(',')
		}
		out.WriteRune(d)
	}
	return out.String()
}

func amount(a tokens.Amount) string {
	if a.Unknown {
		return "unknown"
	}
	return commas(a.N)
}

// atLeast marks a sum that leaves a start out.
func atLeast(e tokens.Entry) string {
	if (e.UnknownStarts > 0 || e.PartialStarts > 0) && !e.Total.Unknown {
		return "at least " + amount(e.Total)
	}
	return amount(e.Total)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// printTokens prints one line for each task, then the sessions and the
// starts that belong to no task.
func printTokens(out io.Writer, report *tokens.Report) {
	if len(report.Tasks)+len(report.Sessions)+len(report.Unlabelled) == 0 {
		fmt.Fprintln(out, "no tokens are recorded in this clone; the plugin records them when a role or a session ends")
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TASK\tTOTAL\tWRITING\tWORKFLOW\tSHARE\tSTARTS\tCOMPLETE")
	for _, t := range report.Tasks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d%%\t%d\t%s\n", t.Task, commas(t.Total), commas(t.Writing), commas(t.Flow.Total), t.FlowShare, t.Starts, yesNo(t.Complete))
	}
	_ = w.Flush()
	printSessions(out, report.Sessions, report.Unlabelled)
	fmt.Fprintln(out, "`kitsu tokens --task <task>` prints the roles and the stages of one task")
}

func yesNo(yes bool) string {
	if yes {
		return "yes"
	}
	return "no: every sum is at least the number shown"
}

func printSessions(out io.Writer, sessions []tokens.Session, unlabelled []tokens.Unlabelled) {
	for _, s := range sessions {
		line := fmt.Sprintf("session %s: %s tokens of its own", s.Session, atLeast(s.Entry))
		switch {
		case s.Shared:
			line += fmt.Sprintf(", shared by the tasks %s and counted in the total of none", strings.Join(s.Tasks, ", "))
		case len(s.Tasks) == 1:
			line += fmt.Sprintf(", counted as the coordination of %s", s.Tasks[0])
		default:
			line += ", of no task"
		}
		fmt.Fprintln(out, line)
	}
	for _, u := range unlabelled {
		if u.Total.Unknown {
			fmt.Fprintf(out, "a start in session %s whose record gives no label and no count: it may belong to any task of that session (%s)\n", u.Session, u.Record)
			continue
		}
		fmt.Fprintf(out, "a start without a label in session %s: %s tokens, in no task (%s)\n", u.Session, atLeast(u.Entry), u.Record)
	}
}

// printTask prints the report of one task.
func printTask(out io.Writer, t *tokens.Task, sessions []tokens.Session, unlabelled []tokens.Unlabelled) {
	total := commas(t.Total)
	if !t.Complete {
		total = "at least " + total
	}
	fmt.Fprintf(out, "%s: %s tokens in %s\n", t.Task, total, plural(t.Starts, "start of a role", "starts of roles"))
	// With an unknown start the share is a share of what is known.
	known := ""
	if !t.Complete {
		known = " known"
	}
	fmt.Fprintf(out, "%s of %s%s tokens, %d%%, went to the workflow and not to the first writing of the specification, the design, the tests and the code\n",
		commas(t.Flow.Total), commas(t.Total), known, t.FlowShare)
	if len(t.UnknownRecords) > 0 {
		fmt.Fprintf(out, "the final token counts for %s are unknown, so every sum is at least the number shown:\n", unknownWhat(t))
		for _, record := range t.UnknownRecords {
			fmt.Fprintf(out, "  %s\n", record)
		}
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "\nwriting\t%s\t\n", commas(t.Writing))
	fmt.Fprintf(w, "workflow\t%s\t\n", commas(t.Flow.Total))
	fmt.Fprintf(w, "  coordination\t%s\t\n  review\t%s\t\n  lookup\t%s\t\n  git\t%s\t\n  rework\t%s\t\n",
		commas(t.Flow.Coordination), commas(t.Flow.Review), commas(t.Flow.Lookup), commas(t.Flow.Git), commas(t.Flow.Rework))
	_ = w.Flush()
	w = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\nROLE\tSTARTS\tINPUT\tCACHE WRITE\tCACHE READ\tOUTPUT\tTOTAL\tMODEL")
	for _, e := range t.Roles {
		fmt.Fprintf(w, "%s\t%d\t%s\n", e.Role, e.Starts, kinds(e))
	}
	fmt.Fprintln(w, "\t\t\t\t\t\t\t\nSTAGE\tROUND\tINPUT\tCACHE WRITE\tCACHE READ\tOUTPUT\tTOTAL\tROLE")
	for _, e := range t.Stages {
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", e.Stage, e.Round, commas(e.Input), commas(e.CacheWrite), commas(e.CacheRead), commas(e.Output), atLeast(e), e.Role)
	}
	_ = w.Flush()
	fmt.Fprintln(out)
	printSessions(out, sessions, unlabelled)
	fmt.Fprintln(out, "workflow tokens are coordination (the session itself), review, lookup, git and rework (a repair or a later managed writing round)")
}

// unknownWhat names what has unknown tokens: starts of roles, the session
// itself, or both.
func unknownWhat(t *tokens.Task) string {
	starts, sessions := t.Unknown()
	var parts []string
	if starts > 0 {
		parts = append(parts, plural(starts, "start", "starts"))
	}
	if sessions == 1 {
		parts = append(parts, "the session itself")
	} else if sessions > 1 {
		parts = append(parts, fmt.Sprintf("%d sessions themselves", sessions))
	}
	return strings.Join(parts, " and of ")
}

// kinds are the columns of the four kinds, their sum and the models.
func kinds(e tokens.Entry) string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", commas(e.Input), commas(e.CacheWrite), commas(e.CacheRead), commas(e.Output), atLeast(e), strings.Join(e.Models, ", "))
}

func (v *harnessView) loadUsage(repo *gitx.Repo, events []harness.Event) {
	starts, damaged, err := tokens.Load(tokensDir(repo))
	if err != nil {
		v.Coverage, v.UsageError = "unavailable", err.Error()
		return
	}
	report := tokens.Build(starts, damaged)
	v.observeUsage(report)
	if v.Captured == nil {
		v.ActorSessions = actorSessionUsage(events, report.Sessions)
	}
}

// A missing coordinator receipt must not erase the worker tokens we did see.
func (v *harnessView) observeUsage(report *tokens.Report) {
	v.Coverage = "unavailable"
	task, sessions, _, found := report.Find(v.Task)
	if !found {
		return
	}
	known := slices.ContainsFunc(task.Roles, func(e tokens.Entry) bool { return !e.Total.Unknown }) ||
		slices.ContainsFunc(sessions, func(s tokens.Session) bool { return !s.Shared && !s.Total.Unknown })
	if !known {
		return
	}
	v.Captured = &task.Total
	v.Coverage = "partial"
	if task.Complete && len(task.SharedSessions) == 0 {
		v.Coverage = "complete for attributed notices"
	}
}
