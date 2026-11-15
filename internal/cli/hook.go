package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/gate"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// runHook decides for git whether a commit, a merge commit or a push goes
// through. It decides from the records alone: it executes no check, waits
// for none and changes nothing but its own record of the decision.
func runHook(ctx context.Context, cfg Config, args []string) error {
	known := strings.Join(gate.Hooks, ", ")
	if len(args) == 0 {
		return usagef("hook needs the name of a hook: %s", known)
	}
	name := args[0]
	if !gate.IsHook(name) {
		return usagef("unknown hook %q; kitsu decides for %s", name, known)
	}
	s, err := open(ctx, cfg, false)
	if err != nil {
		// Without a repository nothing can be judged, and a hook that
		// cannot judge denies.
		fmt.Fprintf(cfg.Err, "kitsu: this %s is denied: %v\n", name, err)
		return exit(ExitDenied)
	}
	judge := &gate.Gate{Repo: s.repo, Store: s.store, Now: time.Now()}
	mode, modeErr := s.store.Mode()
	var rulings []gate.Ruling
	switch {
	case modeErr != nil:
		rulings = []gate.Ruling{gate.Cannot(name, store.CauseStore,
			fmt.Sprintf("the records in %s cannot be read: %v", s.store.Dir, modeErr))}
	case name == "pre-push":
		remote := ""
		if len(args) > 1 {
			remote = args[1]
		}
		rulings = judge.Push(ctx, remote, cfg.In)
	default:
		rulings = []gate.Ruling{judge.Commit(ctx, name)}
	}
	denied := false
	for _, ruling := range rulings {
		for _, note := range ruling.Notes {
			fmt.Fprintln(cfg.Err, "kitsu:", note)
		}
		if ruling.Verdict != nil {
			s.warn(ruling.Verdict.Warnings)
		}
		// The decision is kept for `kitsu stats`. A record that cannot be
		// written changes no decision.
		if err := s.store.Record(context.WithoutCancel(ctx), ruling.Decision(mode, judge.Now)); err != nil && modeErr == nil {
			fmt.Fprintf(cfg.Err, "kitsu: warning: this decision could not be recorded: %v\n", err)
		}
		switch {
		case ruling.Allowed:
		case mode == store.Observe:
			fmt.Fprint(cfg.Err, ruling.Observed())
		default:
			denied = true
			fmt.Fprint(cfg.Err, ruling.Denial(s.store))
		}
	}
	if denied {
		return exit(ExitDenied)
	}
	return nil
}
