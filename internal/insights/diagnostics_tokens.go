package insights

import (
	"cmp"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

type diagnosticAccounting struct {
	starts                []tokens.Start
	report, uncached      *tokens.Report
	conflicts, duplicates int
	isolatedSessions      bool
}

// BuildDiagnostics joins only stored identities; temporal overlap never assigns
// repository check runs to a task. tokens.Build owns token categorization.
func BuildDiagnostics(in DiagnosticInput) Diagnostics {
	a := prepareDiagnosticAccounting(in)
	d := Diagnostics{Version: 1, Task: in.Task, Tasks: []TaskDiagnostic{}, Candidates: []Consumer{}, Unknowns: []string{
		"Missing, shared and unlabelled usage is not estimated or assigned to tasks.",
		"Managed spans include waiting and coordination; worker execution, unmanaged time and true task critical path are unknown.",
		"Check references prove evidence association; repeated references do not prove a reuse invocation or a saved execution.",
		"Largest captured consumers are investigation candidates; token cost alone does not establish a causal bottleneck.",
	}}
	d.DuplicateNotices = a.duplicates
	d.DamagedRecords = len(in.DamagedTokens) + len(in.DamagedRecords) + a.conflicts
	if a.conflicts > 0 {
		d.Unknowns = append(d.Unknowns, "Invalid or conflicting captured counters were excluded; their usage remains unknown.")
	}
	d.UnknownSources = diagnosticUnknownSources(in, a.starts)
	for _, task := range diagnosticTaskNames(in, a.report) {
		t := a.task(task, len(in.DamagedTokens) == 0)
		t.ManagedIntervals, t.Checks = diagnosticIntervals(in.Stages, task), diagnosticChecks(in, task)
		d.Tasks = append(d.Tasks, t)
	}
	fillDiagnosticTotals(&d, in, a)
	if in.Task != "" {
		d.CapturedTokens, d.UncachedTokens = d.Tasks[0].CapturedTokens, d.Tasks[0].UncachedTokens
	}
	slices.SortFunc(d.Candidates, func(a, b Consumer) int {
		return cmp.Or(cmp.Compare(b.Known, a.Known), strings.Compare(a.Host+"/"+a.Session+"/"+a.Agent, b.Host+"/"+b.Session+"/"+b.Agent))
	})
	if len(d.Candidates) > diagnosticSamples {
		d.Candidates = d.Candidates[:diagnosticSamples]
	}
	d.ManagedIntervals, d.Checks = diagnosticIntervals(in.Stages, in.Task), diagnosticChecks(in, in.Task)
	return d
}

func prepareDiagnosticAccounting(in DiagnosticInput) diagnosticAccounting {
	starts, duplicates, conflicts := diagnosticStarts(in.Starts)
	accounting, isolated := isolateDiagnosticSessions(starts)
	uncached := slices.Clone(accounting)
	for i, s := range uncached {
		uncached[i].Models = map[string]tokens.Counts{}
		for model, counts := range s.Models {
			counts.CacheRead, counts.CacheWrite = 0, 0
			uncached[i].Models[model] = counts
		}
	}
	return diagnosticAccounting{starts: starts, report: tokens.Build(accounting, in.DamagedTokens), uncached: tokens.Build(uncached, in.DamagedTokens), duplicates: duplicates, conflicts: conflicts, isolatedSessions: isolated}
}

// The existing accountant groups by session alone. When hosts collide, assign
// all host/session pairs temporary unique IDs; original IDs remain in sources.
func isolateDiagnosticSessions(starts []tokens.Start) ([]tokens.Start, bool) {
	hosts := map[string]string{}
	isolated := false
	for _, s := range starts {
		if host, exists := hosts[s.Session]; exists && host != s.Host {
			isolated = true
		}
		hosts[s.Session] = s.Host
	}
	accounting := slices.Clone(starts)
	if !isolated {
		return accounting, false
	}
	sessions := map[[2]string]string{}
	for i := range accounting {
		key := [2]string{accounting[i].Host, accounting[i].Session}
		if _, exists := sessions[key]; !exists {
			sessions[key] = "diagnostic-session-" + strconv.Itoa(len(sessions))
		}
		accounting[i].Session = sessions[key]
	}
	return accounting, true
}

func diagnosticUnknownSources(in DiagnosticInput, starts []tokens.Start) []string {
	sources := []string{}
	for _, source := range append(slices.Clone(in.DamagedTokens), in.DamagedRecords...) {
		sources = diagnosticSource(sources, source)
	}
	for _, start := range starts {
		if !start.Known {
			sources = diagnosticSource(sources, start.Record)
		}
	}
	return sources
}

func diagnosticTaskNames(in DiagnosticInput, report *tokens.Report) []string {
	if in.Task != "" {
		return []string{in.Task}
	}
	tasks := map[string]bool{}
	for _, t := range report.Tasks {
		tasks[t.Task] = true
	}
	for _, stage := range in.Stages {
		if stage.Task != "" {
			tasks[stage.Task] = true
		}
	}
	for _, ref := range in.References {
		if ref.Task != "" {
			tasks[ref.Task] = true
		}
	}
	names := make([]string, 0, len(tasks))
	for task := range tasks {
		names = append(names, task)
	}
	slices.Sort(names)
	return names
}

func (a diagnosticAccounting) task(task string, undamaged bool) TaskDiagnostic {
	t := TaskDiagnostic{Task: task, Shares: []TokenShare{}}
	captured, sessions, unlabelled, ok := a.report.Find(task)
	if !ok {
		t.CapturedTokens, t.UncachedTokens = tokenCounter(0, false, false), tokenCounter(0, false, false)
		return t
	}
	u, _, _, _ := a.uncached.Find(task)
	// Capture completeness is the existing accountant's contract. A separate
	// flag says whether attribution excludes shared or unlabelled overhead.
	complete := captured.Complete && a.conflicts == 0 && (!a.isolatedSessions || undamaged)
	known := slices.ContainsFunc(captured.Roles, func(e tokens.Entry) bool { return !e.Total.Unknown }) || slices.ContainsFunc(sessions, func(s tokens.Session) bool { return !s.Shared && !s.Total.Unknown })
	t.CapturedTokens = tokenCounter(captured.Total, complete, known)
	t.UncachedTokens = tokenCounter(u.Total, complete, known)
	t.SharedSessions, t.UnknownRecords = len(captured.SharedSessions), len(captured.UnknownRecords)
	t.AttributionComplete = complete && t.SharedSessions == 0 && len(unlabelled) == 0
	t.Shares = diagnosticShares(captured)
	return t
}

func diagnosticShares(captured *tokens.Task) []TokenShare {
	shares := []TokenShare{{Category: "writing", Known: captured.Writing}, {Category: "review", Known: captured.Flow.Review}, {Category: "rework", Known: captured.Flow.Rework}, {Category: "git", Known: captured.Flow.Git}, {Category: "coordination", Known: captured.Flow.Coordination}, {Category: "lookup", Known: captured.Flow.Lookup}}
	for i := range shares {
		if captured.Total > 0 {
			shares[i].Percent = float64(shares[i].Known) * 100 / float64(captured.Total)
		}
	}
	return shares
}

func fillDiagnosticTotals(d *Diagnostics, in DiagnosticInput, a diagnosticAccounting) {
	var total, noCache int64
	known, complete := false, len(in.DamagedTokens) == 0 && a.conflicts == 0 && len(a.starts) > 0
	terminals := map[[2]string]bool{}
	for _, start := range a.starts {
		if start.Agent == tokens.SessionAgent {
			terminals[[2]string{start.Host, start.Session}] = true
		}
	}
	for _, task := range a.report.Tasks {
		complete = complete && task.Complete
	}
	for _, s := range a.starts {
		if s.Agent != tokens.SessionAgent && !terminals[[2]string{s.Host, s.Session}] {
			complete = false
		}
		if s.Agent != tokens.SessionAgent && s.Task == "" {
			d.UnlabelledStarts++
		}
		complete = complete && s.Known
		if !s.Known {
			continue
		}
		n, u := diagnosticCount(s)
		total, noCache, known = total+n, noCache+u, true
		if in.Task == "" || s.Task == in.Task {
			d.Candidates = append(d.Candidates, Consumer{Task: s.Task, Stage: s.Stage, Role: cmp.Or(tokens.RoleOf(s.Stage), s.Role), Round: s.Round, Host: s.Host, Session: s.Session, Agent: s.Agent, Known: n, Record: s.Record})
		}
	}
	for _, session := range a.report.Sessions {
		if session.Shared {
			d.SharedSessions++
		}
	}
	d.CapturedTokens, d.UncachedTokens = tokenCounter(total, complete, known), tokenCounter(noCache, complete, known)
}

func tokenCounter(n int64, complete, known bool) TokenCounter {
	status := "lower_bound"
	if !known {
		status = "unknown"
	} else if complete {
		status = "known"
	}
	return TokenCounter{Known: n, Complete: complete && known, Status: status}
}

func diagnosticStarts(input []tokens.Start) ([]tokens.Start, int, int) {
	starts := []tokens.Start{}
	seen := map[[3]string]int{}
	originals := map[[3]string]tokens.Start{}
	duplicates, conflicts := 0, 0
	var aggregate int64
	for _, s := range input {
		key := [3]string{s.Host, s.Session, s.Agent}
		if at, ok := seen[key]; ok {
			duplicates++
			if !reflect.DeepEqual(originals[key], s) {
				conflicts++
				starts[at].Known, starts[at].Models, starts[at].Why = false, nil, "conflicting captured notices"
			}
			continue
		}
		seen[key], originals[key] = len(starts), s
		sum, valid := validDiagnosticCount(s)
		if s.Known && sum > math.MaxInt64-aggregate {
			valid = false
		}
		if !valid {
			s.Known, s.Models, s.Why = false, nil, "invalid captured counters"
			conflicts++
		}
		if !s.Known {
			s.Models = nil
		} else {
			aggregate += sum
		}
		starts = append(starts, s)
	}
	return starts, duplicates, conflicts
}

func validDiagnosticCount(s tokens.Start) (int64, bool) {
	if s.Host == "" || s.Session == "" || s.Agent == "" || s.Known && s.Models == nil {
		return 0, false
	}
	var sum int64
	for _, c := range s.Models {
		for _, n := range []int64{c.Input, c.CacheWrite, c.CacheRead, c.Output} {
			if n < 0 || n > math.MaxInt64-sum {
				return 0, false
			}
			sum += n
		}
	}
	return sum, true
}

func diagnosticCount(s tokens.Start) (int64, int64) {
	var total, uncached int64
	for _, c := range s.Models {
		total += c.Sum()
		uncached += c.Input + c.Output
	}
	return total, uncached
}
