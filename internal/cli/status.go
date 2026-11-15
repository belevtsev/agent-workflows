package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/gate"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// statusReport is the object `kitsu status --json` prints. No field is removed
// or changes its meaning while the declaration version is 1.
type statusReport struct {
	Tree          string        `json:"tree"`
	Verified      bool          `json:"verified"`
	IndexTree     string        `json:"index_tree"`
	IndexVerified bool          `json:"index_verified"`
	Mode          string        `json:"mode"`
	Hooks         string        `json:"hooks"`
	Checks        []statusCheck `json:"checks"`
	NotStaged     []string      `json:"not_staged,omitempty"`
	NotStagedMore int           `json:"not_staged_more,omitempty"`
	// IndexProblem says why the tree of the index cannot be judged at all.
	IndexProblem string   `json:"index_problem,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

type statusCheck struct {
	ID         string     `json:"id"`
	State      string     `json:"state"`
	Running    bool       `json:"running,omitempty"`
	RunID      string     `json:"run_id,omitempty"`
	Tree       string     `json:"tree,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	ExitStatus *int       `json:"exit_status,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Log        string     `json:"log,omitempty"`
}

// stateWord is a state as JSON holds it: one word, like an outcome.
func stateWord(state string) string { return strings.ReplaceAll(state, " ", "_") }

func runStatus(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("status")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash {
		return usagef("status takes no arguments; usage: kitsu status [--json]")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	verdict, err := s.judgeWorktree(ctx, nil)
	if err != nil {
		return failf("%v", err)
	}
	report, index, err := s.statusReport(ctx, verdict)
	if err != nil {
		return failf("%v", err)
	}
	if *asJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return failf("%v", err)
		}
		fmt.Fprintln(cfg.Out, string(data))
	} else {
		s.warn(report.Warnings)
		printStatus(cfg, s.store, verdict, index, report)
	}
	if !verdict.Verified {
		return exit(ExitNotVerified)
	}
	return nil
}

// statusReport gathers what status prints. index is the verdict of the tree
// of the index, or nil when that tree is the snapshot or cannot be judged.
func (s *session) statusReport(ctx context.Context, verdict *evidence.Verdict) (statusReport, *evidence.Verdict, error) {
	report := statusReport{Tree: verdict.Tree, Verified: verdict.Verified, Hooks: gate.State(ctx, s.repo), Warnings: verdict.Warnings}
	mode, err := s.store.Mode()
	if err != nil {
		return report, nil, &evidence.StoreError{Dir: s.store.Dir, Err: err}
	}
	report.Mode = mode
	for _, c := range verdict.Checks {
		entry := statusCheck{ID: c.ID, State: stateWord(c.State), Running: c.Running}
		if r := c.Deciding; r != nil {
			entry.RunID, entry.Tree, entry.Outcome, entry.ExitStatus = r.RunID, r.Tree, r.Outcome, r.ExitStatus
			entry.EndedAt, entry.Log = r.EndedAt, s.store.Abs(r.Log)
		}
		report.Checks = append(report.Checks, entry)
	}
	if report.Hooks != gate.Installed {
		report.Warnings = append(report.Warnings, hooksAdvice(ctx, s.repo))
	}
	indexTree, err := s.repo.IndexTree(ctx, s.store.Tmp())
	if err != nil {
		report.Warnings = append(report.Warnings, "the tree of the index cannot be written: "+err.Error())
		return report, nil, nil
	}
	report.IndexTree, report.IndexVerified = indexTree, verdict.Verified
	if indexTree == verdict.Tree {
		return report, nil, nil
	}
	if report.NotStaged, report.NotStagedMore, err = s.repo.DiffPaths(ctx, indexTree, verdict.Tree); err != nil {
		return report, nil, err
	}
	index, err := evidence.Judge(ctx, s.repo, s.store, indexTree, time.Now(), nil)
	report.IndexVerified = err == nil && index.Verified
	if err != nil {
		// The index cannot be judged at all, for example because it holds
		// no declaration: say that, and not that runs are missing.
		report.IndexProblem = err.Error()
		index = nil
	}
	return report, index, nil
}

func printStatus(cfg Config, st *store.Store, verdict *evidence.Verdict, index *evidence.Verdict, report statusReport) {
	out := cfg.Out
	switch {
	case len(verdict.Checks) == 0:
		fmt.Fprintln(out, "no checks are declared, so every commit is allowed")
	case verdict.Verified:
		fmt.Fprintln(out, "verified: every declared check has a passing run on this content")
	case report.Hooks != gate.Installed:
		fmt.Fprintln(out, "not verified, but no hook of kitsu judges a commit in this clone yet")
	default:
		fmt.Fprintln(out, "not verified: a commit of this content would be denied")
	}
	printChecks(out, st, verdict.Checks)
	if report.Mode == store.Observe {
		fmt.Fprintln(out, "mode: observe (the hooks record what they would decide and allow everything)")
	}
	switch {
	case report.IndexTree == "":
	case report.IndexTree == report.Tree:
		fmt.Fprintln(out, "index: holds the same content as the working tree")
	default:
		fmt.Fprintf(out, "index: differs from the working tree; not staged:\n")
		for _, path := range report.NotStaged {
			fmt.Fprintf(out, "  %s\n", decl.Plain(path))
		}
		fmt.Fprint(out, more(report.NotStagedMore))
		if report.IndexVerified {
			fmt.Fprintln(out, "a commit of the index alone would be allowed")
		} else if report.IndexProblem != "" {
			fmt.Fprintf(out, "a commit of the index alone would be denied: %s\n", report.IndexProblem)
			fmt.Fprintln(out, "stage everything with `git add -A`")
		} else {
			fmt.Fprintln(out, "a commit of the index alone would be denied: its content has no passing run of every check")
			if index != nil {
				printChecks(out, st, index.Missing())
			}
			fmt.Fprintln(out, "stage everything with `git add -A`, or run the checks on what you will commit")
		}
	}
	if !verdict.Verified {
		fmt.Fprintln(out, "run `kitsu run` to produce the missing evidence")
	}
}

// printChecks lists checks with their state and, where a run decides it,
// the log of that run.
func printChecks(out interface{ Write([]byte) (int, error) }, st *store.Store, checks []evidence.Check) {
	width := 0
	for _, c := range checks {
		width = max(width, len(c.ID))
	}
	for _, c := range checks {
		line := fmt.Sprintf("  %-*s  %s", width, c.ID, gate.Describe(st, c))
		fmt.Fprintln(out, line)
	}
}
