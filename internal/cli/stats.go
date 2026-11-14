package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/stats"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
)

// defaultPeriod is the period `cw stats` counts without --since.
const defaultPeriod = 30 * 24 * time.Hour

// parsePeriod reads a period such as 7d, 2w, 36h or 90m.
func parsePeriod(text string) (time.Duration, error) {
	units := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	if len(text) < 2 {
		return 0, fmt.Errorf("%q is not a period; write a number and a unit, such as 7d, 2w, 36h or 90m", text)
	}
	unit, known := units[text[len(text)-1]]
	n, err := strconv.Atoi(text[:len(text)-1])
	if !known || err != nil || n <= 0 || n > 100000 {
		return 0, fmt.Errorf("%q is not a period; write a number and a unit, such as 7d, 2w, 36h or 90m", text)
	}
	return time.Duration(n) * unit, nil
}

func runStats(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("stats")
	since := flags.String("since", "", "")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash {
		return usagef("stats takes no arguments; usage: cw stats [--since <period>] [--json]")
	}
	period := defaultPeriod
	if *since != "" {
		if period, err = parsePeriod(*since); err != nil {
			return usagef("%v", err)
		}
	}
	s, err := open(ctx, cfg, false)
	if err != nil {
		return err
	}
	now := time.Now()
	report, err := stats.Collect(s.store, now.Add(-period), now)
	if err != nil {
		return failf("the records in %s cannot be read: %v", s.store.Dir, err)
	}
	if *asJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return failf("%v", err)
		}
		fmt.Fprintln(cfg.Out, string(data))
		return nil
	}
	printStats(cfg.Out, report)
	return nil
}

// counts prints a map of counts in a fixed order: the names given first,
// then any other name, sorted.
func counts(m map[string]int, first ...string) string {
	names := slices.Clone(first)
	for _, name := range slices.Sorted(maps.Keys(m)) {
		if !slices.Contains(first, name) {
			names = append(names, name)
		}
	}
	var parts []string
	for _, name := range names {
		if n, ok := m[name]; ok {
			parts = append(parts, fmt.Sprintf("%s %d", name, n))
		}
	}
	return strings.Join(parts, ", ")
}

func printStats(w io.Writer, r *stats.Report) {
	const day = "2 Jan 2006 15:04"
	fmt.Fprintf(w, "from %s to %s UTC, from the records of this repository\n\n", r.Since.Format(day), r.Until.Format(day))
	fmt.Fprintln(w, "runs of each check, by outcome")
	if len(r.Runs) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, id := range slices.Sorted(maps.Keys(r.Runs)) {
		fmt.Fprintf(w, "  %s: %s\n", id, counts(r.Runs[id], store.Passed, store.Failed, store.TimedOut, store.SourceChanged, store.Interrupted))
	}
	fmt.Fprintln(w, "\npasses after a failure on the same checked content")
	if len(r.PassAfterFailure) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, id := range slices.Sorted(maps.Keys(r.PassAfterFailure)) {
		fmt.Fprintf(w, "  %s: %d\n", id, r.PassAfterFailure[id])
	}
	fmt.Fprintln(w, "\nhook decisions")
	if len(r.Decisions) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, g := range r.Decisions {
		outcome := g.Outcome
		if g.Mode == store.Observe && g.Outcome == "denied" {
			outcome = "would have been denied"
		}
		fmt.Fprintf(w, "  %s, %s mode: %d %s\n", g.Hook, g.Mode, g.Count, outcome)
		for _, reason := range slices.Sorted(maps.Keys(g.Reasons)) {
			line := fmt.Sprintf("      %s: %d", reason, g.Reasons[reason])
			if checks := g.Checks[reason]; len(checks) > 0 {
				line += " (" + counts(checks) + ")"
			}
			fmt.Fprintln(w, line)
		}
	}
	if r.Unreadable > 0 {
		fmt.Fprintf(w, "\n%d records could not be read and are not counted\n", r.Unreadable)
	}
}
