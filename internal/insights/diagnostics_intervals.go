package insights

import (
	"cmp"
	"slices"
	"time"
)

type diagnosticSpan struct{ start, end time.Time }

func diagnosticIntervals(stages []ManagedStage, task string) IntervalSummary {
	r := IntervalSummary{Sources: []string{}}
	spans := []diagnosticSpan{}
	for _, s := range stages {
		if task != "" && s.Task != task {
			continue
		}
		r.Sources = diagnosticSource(r.Sources, s.Source)
		r.Sources = diagnosticSource(r.Sources, s.ReceiptSource)
		for _, source := range s.ReceiptSources {
			r.Sources = diagnosticSource(r.Sources, source)
		}
		if s.Damaged {
			r.Unknown++
			continue
		}
		if s.Status == "abandoned" {
			r.Abandoned++
		}
		if s.Ended == nil {
			r.Unfinished++
			continue
		}
		if s.Prepared.IsZero() || s.Ended.Before(s.Prepared) {
			r.Unknown++
			continue
		}
		r.Closed++
		spans = append(spans, diagnosticSpan{s.Prepared, *s.Ended})
	}
	r.Complete = r.Closed > 0 && r.Unfinished == 0 && r.Unknown == 0
	r.ObservedSeconds = unionDiagnosticSpans(spans)
	return r
}

func unionDiagnosticSpans(spans []diagnosticSpan) float64 {
	if len(spans) == 0 {
		return 0
	}
	slices.SortFunc(spans, func(a, b diagnosticSpan) int { return cmp.Or(a.start.Compare(b.start), a.end.Compare(b.end)) })
	first, last := spans[0].start, spans[0].end
	var seconds float64
	for _, span := range spans[1:] {
		if span.start.After(last) {
			seconds += last.Sub(first).Seconds()
			first, last = span.start, span.end
		} else if span.end.After(last) {
			last = span.end
		}
	}
	return seconds + last.Sub(first).Seconds()
}

func diagnosticChecks(in DiagnosticInput, task string) CheckSummary {
	r := CheckSummary{Outcomes: map[string]int{}, Reuse: "unknown; evidence references are not execution or reuse events", Sources: []string{}}
	refs := slices.Clone(in.References)
	for _, stage := range in.Stages {
		if !stage.Damaged {
			refs = append(refs, stage.Checks...)
		}
	}
	associated := map[[2]string]bool{}
	for _, ref := range refs {
		if task != "" && ref.Task != task || ref.RunID == "" {
			continue
		}
		r.References++
		key := [2]string{ref.CheckID, ref.RunID}
		if associated[key] {
			r.RepeatedReferences++
		}
		associated[key] = true
		r.Sources = diagnosticSource(r.Sources, ref.Source)
	}
	spans := []diagnosticSpan{}
	seen := map[[2]string]bool{}
	for _, run := range in.Checks {
		record := run.Record
		key := [2]string{record.ID, record.RunID}
		if seen[key] {
			continue
		}
		seen[key] = true
		if task != "" && !associated[key] {
			continue
		}
		r.Runs++
		if associated[key] {
			r.AssociatedRuns++
		} else {
			r.UnassociatedRuns++
		}
		outcome := record.Outcome
		if outcome == "" {
			outcome = "unfinished_unknown"
		}
		r.Outcomes[outcome]++
		r.Sources = diagnosticSource(r.Sources, run.Source)
		if record.Outcome == "" || record.StartedAt.IsZero() || record.EndedAt == nil || record.EndedAt.Before(record.StartedAt) {
			r.UnknownIntervals++
			continue
		}
		spans = append(spans, diagnosticSpan{record.StartedAt, *record.EndedAt})
	}
	for key := range associated {
		if !seen[key] {
			r.MissingReferences++
		}
	}
	r.ObservedSeconds = unionDiagnosticSpans(spans)
	return r
}

func diagnosticSource(sources []string, source string) []string {
	if source != "" && len(sources) < diagnosticSamples && !slices.Contains(sources, source) {
		sources = append(sources, source)
	}
	return sources
}
