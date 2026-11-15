// Package stats counts what the records of one repository hold: runs per
// check, hook decisions, and how often a check passed after it had failed
// on the same content.
package stats

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/gatecub/v3/internal/store"
)

// Group counts the decisions of one hook in one mode with one outcome.
type Group struct {
	Hook    string `json:"hook"`
	Mode    string `json:"mode"`
	Outcome string `json:"outcome"`
	Count   int    `json:"count"`
	// Reasons counts each denial once under every reason it had: the state
	// of a check that was not satisfied (failed, not_run, timed_out), or
	// the cause that prevented a decision.
	Reasons map[string]int `json:"reasons,omitempty"`
	// Checks counts, per state, how often each check was not satisfied.
	Checks map[string]map[string]int `json:"checks,omitempty"`
}

// Report is what `gatecub stats` prints.
type Report struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
	// Runs counts the runs of each check by outcome.
	Runs map[string]map[string]int `json:"runs"`
	// PassAfterFailure counts, per check, the passing runs that followed a
	// failed or timed-out run of the same command on the same checked
	// content. Nothing the check reads had changed, so each one is a result
	// that did not repeat.
	PassAfterFailure map[string]int `json:"pass_after_failure"`
	Decisions        []Group        `json:"decisions"`
	// Unreadable counts the decision lines and run records that could not
	// be read.
	Unreadable int `json:"unreadable"`
}

// Collect counts the records made at or after since.
func Collect(st *store.Store, since, now time.Time) (*Report, error) {
	report := &Report{
		Since: since.UTC().Truncate(time.Second), Until: now.UTC().Truncate(time.Second),
		Runs: map[string]map[string]int{}, PassAfterFailure: map[string]int{}, Decisions: []Group{},
	}
	records, damaged, err := st.AllRecords()
	if err != nil {
		return nil, err
	}
	report.Unreadable = len(damaged)
	report.countRuns(records, since)
	decisions, unreadable, err := st.Decisions(since)
	if err != nil {
		return nil, err
	}
	report.Unreadable += unreadable
	report.countDecisions(decisions)
	return report, nil
}

// inPeriod says whether a run belongs to the period: it ended in it, or it
// has no end and started in it.
func inPeriod(r store.Record, since time.Time) bool {
	if r.EndedAt != nil {
		return !r.EndedAt.Before(since)
	}
	return !r.StartedAt.Before(since)
}

func (r *Report) countRuns(records []store.Record, since time.Time) {
	// One history per check, checked content, command and paths.
	type content struct{ id, key, run, paths string }
	byContent := map[content][]store.Record{}
	for _, record := range records {
		if record.Running || record.Outcome == "" {
			continue
		}
		if inPeriod(record, since) {
			if r.Runs[record.ID] == nil {
				r.Runs[record.ID] = map[string]int{}
			}
			r.Runs[record.ID][record.Outcome]++
		}
		if store.Decisive(record.Outcome) {
			at := content{record.ID, record.ContentKey, strings.Join(record.Run, "\x00"), strings.Join(record.Paths, "\x00")}
			byContent[at] = append(byContent[at], record)
		}
	}
	for at, runs := range byContent {
		slices.SortFunc(runs, func(a, b store.Record) int { return cmp.Compare(a.Seq, b.Seq) })
		for i := 1; i < len(runs); i++ {
			previous, run := runs[i-1], runs[i]
			if run.Outcome == store.Passed && previous.Outcome != store.Passed && inPeriod(run, since) {
				r.PassAfterFailure[at.id]++
			}
		}
	}
}

func (r *Report) countDecisions(decisions []store.Decision) {
	type key struct{ hook, mode, outcome string }
	groups := map[key]*Group{}
	for _, d := range decisions {
		k := key{d.Hook, d.Mode, d.Outcome}
		group := groups[k]
		if group == nil {
			group = &Group{Hook: d.Hook, Mode: d.Mode, Outcome: d.Outcome}
			groups[k] = group
		}
		group.Count++
		if d.Outcome != "denied" {
			continue
		}
		if group.Reasons == nil {
			group.Reasons = map[string]int{}
		}
		if d.Cause != "" {
			group.Reasons[d.Cause]++
		}
		seen := map[string]bool{}
		for id, state := range d.Checks {
			if !seen[state] {
				seen[state] = true
				group.Reasons[state]++
			}
			if group.Checks == nil {
				group.Checks = map[string]map[string]int{}
			}
			if group.Checks[state] == nil {
				group.Checks[state] = map[string]int{}
			}
			group.Checks[state][id]++
		}
	}
	for _, group := range groups {
		r.Decisions = append(r.Decisions, *group)
	}
	slices.SortFunc(r.Decisions, func(a, b Group) int {
		return cmp.Or(cmp.Compare(a.Hook, b.Hook), cmp.Compare(a.Mode, b.Mode), cmp.Compare(a.Outcome, b.Outcome))
	})
}
