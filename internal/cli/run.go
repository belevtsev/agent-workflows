package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/runner"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func runRun(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("run")
	force := flags.Bool("force", false, "")
	baseRev := flags.String("base", "", "")
	compact, asJSON := flags.Bool("compact", false, ""), flags.Bool("json", false, "")
	budget := flags.Int("max-bytes", contextpack.DefaultBytes, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if p.dash {
		return usagef("run takes check ids only; usage: kitsu run [<id>...] [--force] [--base <rev>]")
	}
	if err := compactBudget(*budget); err != nil {
		return err
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	var base *evidence.Base
	if *baseRev != "" {
		if base, err = evidence.LoadBase(ctx, s.repo, *baseRev); err != nil {
			return failf("%v", err)
		}
	}
	first, err := s.judgeWorktree(ctx, base)
	if err != nil {
		return failf("%v", err)
	}
	ids, err := selectChecks(first, p.pos)
	if err != nil {
		return err
	}
	if *compact || *asJSON {
		return runCompactChecks(ctx, s, ids, *force, base, *budget)
	}
	s.store.Sweep(ctx, time.Now())
	if len(first.Checks) == 0 {
		fmt.Fprintln(cfg.Out, "no checks are declared, so there is nothing to run")
		return nil
	}
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		passed, err := s.runOne(ctx, id, *force, base)
		if err != nil {
			return err
		}
		if !passed {
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintf(cfg.Out, "not verified: %d of %d checks did not pass; fix what each log shows, then run `kitsu run` again\n", failed, len(ids))
		return exit(ExitNotVerified)
	}
	return s.summary(ctx, base)
}

// selectChecks returns the ids to consider, in the order of the declaration.
func selectChecks(verdict *evidence.Verdict, named []string) ([]string, error) {
	declared := map[string]bool{}
	for _, c := range verdict.Checks {
		declared[c.ID] = true
	}
	wanted := map[string]bool{}
	for _, id := range named {
		if !declared[id] {
			return nil, usagef("no check `%s` is declared in kitsu.toml", id)
		}
		wanted[id] = true
	}
	var ids []string
	for _, c := range verdict.Checks {
		if len(named) == 0 || wanted[c.ID] {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}

// runOne takes a snapshot and executes the check unless it is satisfied for
// that snapshot. It reports whether the check now has a passing run.
func (s *session) runOne(ctx context.Context, id string, force bool, base *evidence.Base) (bool, error) {
	verdict, err := s.judgeWorktree(ctx, base)
	if err != nil {
		return false, failf("%v", err)
	}
	idx := slices.IndexFunc(verdict.Checks, func(check evidence.Check) bool { return check.ID == id })
	if idx < 0 {
		return false, failf("kitsu.toml changed during the run: the check `%s` is no longer declared", id)
	}
	check := &verdict.Checks[idx]
	s.warn(verdict.Warnings)
	if s.compact != nil {
		s.compact.begin(check, force)
	}
	out := s.cfg.Out
	if check.State == evidence.Satisfied && !force {
		if check.Implied {
			fmt.Fprintf(out, "== %s: satisfied, not run: its checked content is the same as in the base revision\n", id)
		} else {
			fmt.Fprintf(out, "== %s: satisfied, not run again: run %s passed on this content\n", id, check.Deciding.RunID)
		}
		return true, nil
	}
	job := runner.Job{Check: check.Check, Tree: verdict.Tree, Key: check.Key, Out: out, Err: s.cfg.Err}
	record, err := s.executeJob(ctx, job, true)
	return record.Outcome == store.Passed, err
}

func (s *session) executeJob(ctx context.Context, job runner.Job, printStatus bool) (store.Record, error) {
	var err error
	if job.Program, err = checkExecutable(s.repo.Root, job.Check); err != nil {
		return store.Record{}, err
	}
	if printStatus {
		fmt.Fprintf(job.Out, "== %s: %s\n", job.Check.ID, decl.Plain(strings.Join(job.Check.Run, " ")))
	}
	result, err := runner.Execute(ctx, s.repo, s.store, job)
	s.warn(result.Warnings)
	if err == nil && printStatus {
		if s.compact != nil {
			s.compact.finish(result.Record)
		}
		fmt.Fprintf(job.Out, "== %s: %s\n", job.Check.ID, describeRun(s.store, result.Record))
	}
	return result.Record, err
}

// describeRun says how a run ended, with the outcome as its first word.
func describeRun(st *store.Store, r store.Record) string {
	took := r.EndedAt.Sub(r.StartedAt).Round(time.Second)
	text := r.Outcome
	switch r.Outcome {
	case store.Passed:
		return fmt.Sprintf("passed in %s", took)
	case store.Failed:
		if r.ExitStatus != nil {
			text += fmt.Sprintf(" with exit status %d", *r.ExitStatus)
		}
	case store.TimedOut:
		text += fmt.Sprintf(" after %s", took)
	}
	if r.Detail != "" {
		text += ": " + r.Detail
	}
	return text + "\n   log: " + st.Abs(r.Log)
}

// summary says, after every executed check passed, whether the working tree
// as a whole is verified now.
func (s *session) summary(ctx context.Context, base *evidence.Base) error {
	verdict, err := s.judgeWorktree(ctx, base)
	if err != nil {
		return failf("%v", err)
	}
	if verdict.Verified {
		fmt.Fprintln(s.cfg.Out, "every declared check has a passing run on this content")
		return nil
	}
	var names []string
	for _, c := range verdict.Missing() {
		names = append(names, c.ID)
	}
	fmt.Fprintf(s.cfg.Out, "not yet verified; still missing: %s\n", strings.Join(names, ", "))
	return nil
}

func checkExecutable(root string, check decl.Check) (string, error) {
	// On Windows the program is looked up here and started by its full path:
	// cmd.exe, which runs a batch file, would look a bare name up again, the
	// folder first. A bare name is found on PATH only, as on the other
	// systems, and a path is read from the repository root.
	program := ""
	if name := check.Run[0]; runtime.GOOS == "windows" {
		places := []string{filepath.Join(root, name)}
		if filepath.IsAbs(name) {
			places[0] = name
		} else if !strings.ContainsAny(name, `:/\`) {
			places = filepath.SplitList(os.Getenv("PATH"))
			for i, dir := range places {
				places[i] = filepath.Join(dir, name)
			}
		}
		for _, place := range places {
			found, err := exec.LookPath(place)
			if err != nil || !filepath.IsAbs(place) {
				continue
			}
			if refusal := runner.BatchRefusal(found, check.Run[1:]); refusal != "" {
				return "", failf("the check `%s` was not started: %s", check.ID, refusal)
			}
			program = found
			break
		}
	}
	return program, nil
}
