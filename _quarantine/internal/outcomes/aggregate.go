package outcomes

import (
	"cmp"
	"math"
	"slices"
)

var metricNames = []string{
	"duration_ms", "input_tokens", "output_tokens", "api_requests",
	"uncached_input_tokens", "cache_write_5m_tokens", "cache_write_1h_tokens", "cache_read_tokens", "thinking_tokens",
	"checked_references", "incorrect_references", "defects_found", "defects_missed", "rework",
}

func counters(o Observation) map[string]*int64 {
	values := map[string]*int64{
		"input_tokens": nil, "output_tokens": nil, "api_requests": nil,
		"uncached_input_tokens": nil, "cache_write_5m_tokens": nil, "cache_write_1h_tokens": nil, "cache_read_tokens": nil, "thinking_tokens": nil,
		"checked_references": nil, "incorrect_references": nil, "defects_found": nil, "defects_missed": nil, "rework": nil,
	}
	if o.Usage != nil {
		u := o.Usage
		values["input_tokens"], values["output_tokens"], values["api_requests"] = u.InputTokens, u.OutputTokens, u.APIRequests
		values["uncached_input_tokens"], values["cache_write_5m_tokens"], values["cache_write_1h_tokens"] = u.UncachedInputTokens, u.CacheWrite5mTokens, u.CacheWrite1hTokens
		values["cache_read_tokens"], values["thinking_tokens"] = u.CacheReadTokens, u.ThinkingTokens
	}
	if o.Quality != nil {
		q := o.Quality
		values["checked_references"], values["incorrect_references"] = q.CheckedReferences, q.IncorrectReferences
		values["defects_found"], values["defects_missed"], values["rework"] = q.DefectsFound, q.DefectsMissed, q.Rework
	}
	return values
}

func measurements(o Observation) map[string]*float64 {
	values := make(map[string]*float64, len(metricNames))
	values["duration_ms"] = o.DurationMS
	for name, counter := range counters(o) {
		values[name] = nil
		if counter != nil {
			value := float64(*counter)
			values[name] = &value
		}
	}
	return values
}

type summaryKey struct{ scope, variant, kind string }

func summaries(entries []entry) []Summary {
	groups := make(map[summaryKey][]entry)
	for _, e := range entries {
		o := e.observation
		for _, kind := range []string{"all", o.Project.Kind} {
			key := summaryKey{o.Scope, o.Variant, kind}
			groups[key] = append(groups[key], e)
		}
	}
	keys := make([]summaryKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b summaryKey) int {
		return cmp.Or(cmp.Compare(a.scope, b.scope), cmp.Compare(a.variant, b.variant), cmp.Compare(a.kind, b.kind))
	})
	var result []Summary
	for _, key := range keys {
		result = append(result, summarizeGroup(key, groups[key]))
	}
	return result
}

func summarizeGroup(key summaryKey, entries []entry) Summary {
	summary := Summary{Scope: key.scope, Variant: key.variant, ProjectKind: key.kind, Metrics: make(map[string]Measure, len(metricNames))}
	for _, name := range metricNames {
		summary.Metrics[name] = Measure{}
	}
	projects, environments := make(map[string]bool), make(map[string]bool)
	type sourceKey struct {
		project string
		source  Source
	}
	sources := make(map[sourceKey]bool)
	costs := make(map[string]CostTotal)
	for _, e := range entries {
		o := e.observation
		projects[o.Project.Identity], environments[o.Environment] = true, true
		sources[sourceKey{o.Project.Identity, o.Source}] = true
		if o.Status != "succeeded" {
			summary.Failed++
			continue
		}
		summary.Succeeded++
		for name, value := range measurements(o) {
			measure := summary.Metrics[name]
			if value == nil {
				measure.Unknown++
			} else {
				measure.Known++
				add(&measure.Total, *value)
			}
			summary.Metrics[name] = measure
		}
		if q := o.Quality; q != nil {
			addRatio(&summary.ReferenceErrorRate, q.IncorrectReferences, q.CheckedReferences, nil)
			addRatio(&summary.DefectMissRate, q.DefectsMissed, q.DefectsFound, q.DefectsMissed)
		} else {
			summary.ReferenceErrorRate.Unknown++
			summary.DefectMissRate.Unknown++
		}
		if e.cost == nil {
			summary.Cost.Unknown++
		} else {
			summary.Cost.Known++
			cost := costs[o.Cost.Currency]
			cost.Currency = o.Cost.Currency
			cost.Samples++
			cost.Total += *e.cost
			if e.derivedCost {
				cost.DerivedSamples++
			} else {
				cost.ObservedSamples++
			}
			costs[o.Cost.Currency] = cost
		}
	}
	for name, measure := range summary.Metrics {
		if measure.Known > 0 {
			mean := *measure.Total / float64(measure.Known)
			measure.Mean = &mean
		}
		summary.Metrics[name] = measure
	}
	finishRatio(&summary.ReferenceErrorRate)
	finishRatio(&summary.DefectMissRate)
	for _, cost := range costs {
		summary.Cost.Totals = append(summary.Cost.Totals, cost)
	}
	slices.SortFunc(summary.Cost.Totals, func(a, b CostTotal) int { return cmp.Compare(a.Currency, b.Currency) })
	summary.Projects, summary.DistinctSources, summary.DistinctEnvironments = len(projects), len(sources), len(environments)
	return summary
}

func add(target **float64, value float64) {
	if *target == nil {
		*target = new(float64)
	}
	**target += value
}

// For defect misses the denominator is found+missed. Requiring all operands
// prevents a partial quality report from silently becoming a perfect score.
func addRatio(ratio *Ratio, numerator, denominator, denominatorExtra *int64) {
	if numerator == nil || denominator == nil {
		ratio.Unknown++
		return
	}
	ratio.Known++
	add(&ratio.Numerator, float64(*numerator))
	add(&ratio.Denominator, float64(*denominator))
	if denominatorExtra != nil {
		add(&ratio.Denominator, float64(*denominatorExtra))
	}
}

func finishRatio(ratio *Ratio) {
	if ratio.Denominator != nil && *ratio.Denominator > 0 {
		value := *ratio.Numerator / *ratio.Denominator
		ratio.Value = &value
	}
}

type caseKey struct{ scope, kind, project, caseID, workload string }
type pairEntries struct{ baseline, candidate entry }

func comparisons(entries []entry, baseline, candidate string) Comparisons {
	result := Comparisons{Baseline: baseline, Candidate: candidate}
	groups := make(map[caseKey][]entry)
	for _, e := range entries {
		o := e.observation
		if o.Variant != baseline && o.Variant != candidate {
			result.NotSelectedSamples++
			continue
		}
		key := caseKey{o.Scope, o.Project.Kind, o.Project.Identity, o.CaseID, o.Workload}
		groups[key] = append(groups[key], e)
	}
	keys := make([]caseKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b caseKey) int {
		return cmp.Or(cmp.Compare(a.scope, b.scope), cmp.Compare(a.kind, b.kind), cmp.Compare(a.project, b.project), cmp.Compare(a.caseID, b.caseID), cmp.Compare(a.workload, b.workload))
	})
	summaryGroups := make(map[summaryKey][]pairEntries)
	for _, key := range keys {
		var base, cand []entry
		for _, e := range groups[key] {
			if e.observation.Variant == baseline {
				base = append(base, e)
			} else {
				cand = append(cand, e)
			}
		}
		if len(base) != 1 || len(cand) != 1 {
			reason := "ambiguous_variant"
			if len(base) == 0 {
				reason = "missing_baseline"
			} else if len(cand) == 0 {
				reason = "missing_candidate"
			}
			for _, e := range groups[key] {
				o := e.observation
				result.Unmatched = append(result.Unmatched, Unmatched{o.ObservationID, o.CaseID, o.Project, o.Scope, o.Workload, o.Variant, reason})
			}
			continue
		}
		b, c := base[0].observation, cand[0].observation
		pair := Pair{CaseID: b.CaseID, Project: b.Project, Scope: b.Scope, Workload: b.Workload,
			BaselineID: b.ObservationID, CandidateID: c.ObservationID,
			BaselineSource: b.Source, CandidateSource: c.Source,
			BaselineEnvironment: b.Environment, CandidateEnvironment: c.Environment}
		if b.Source != c.Source {
			pair.Exclusions = append(pair.Exclusions, "source_differs")
		}
		if b.Environment != c.Environment {
			pair.Exclusions = append(pair.Exclusions, "environment_differs")
		}
		if b.Status != "succeeded" {
			pair.Exclusions = append(pair.Exclusions, "baseline_failed")
		}
		if c.Status != "succeeded" {
			pair.Exclusions = append(pair.Exclusions, "candidate_failed")
		}
		pair.Comparable = len(pair.Exclusions) == 0
		result.Pairs = append(result.Pairs, pair)
		result.MatchedCases++
		if pair.Comparable {
			result.ComparablePairs++
			for _, kind := range []string{"all", b.Project.Kind} {
				summaryKey := summaryKey{scope: b.Scope, kind: kind}
				summaryGroups[summaryKey] = append(summaryGroups[summaryKey], pairEntries{base[0], cand[0]})
			}
		}
	}
	summaryKeys := make([]summaryKey, 0, len(summaryGroups))
	for key := range summaryGroups {
		summaryKeys = append(summaryKeys, key)
	}
	slices.SortFunc(summaryKeys, func(a, b summaryKey) int { return cmp.Or(cmp.Compare(a.scope, b.scope), cmp.Compare(a.kind, b.kind)) })
	for _, key := range summaryKeys {
		result.Summaries = append(result.Summaries, summarizePairs(key, summaryGroups[key]))
	}
	slices.SortFunc(result.Unmatched, func(a, b Unmatched) int { return cmp.Compare(a.ObservationID, b.ObservationID) })
	return result
}

func summarizePairs(key summaryKey, entries []pairEntries) ComparisonSummary {
	summary := ComparisonSummary{Scope: key.scope, ProjectKind: key.kind, ComparablePairs: len(entries), Metrics: make(map[string]PairedMeasure, len(metricNames))}
	for _, name := range metricNames {
		summary.Metrics[name] = PairedMeasure{}
	}
	costs := make(map[string]PairedCost)
	for _, pair := range entries {
		base, cand := measurements(pair.baseline.observation), measurements(pair.candidate.observation)
		for _, name := range metricNames {
			measure := summary.Metrics[name]
			if base[name] == nil || cand[name] == nil {
				measure.UnknownPairs++
			} else {
				addPair(&measure, *base[name], *cand[name])
			}
			summary.Metrics[name] = measure
		}
		if pair.baseline.cost == nil || pair.candidate.cost == nil {
			summary.UnknownCostPairs++
		} else if pair.baseline.observation.Cost.Currency != pair.candidate.observation.Cost.Currency {
			summary.CurrencyMismatchPairs++
		} else {
			currency := pair.baseline.observation.Cost.Currency
			cost := costs[currency]
			cost.Currency = currency
			addPair(&cost.PairedMeasure, *pair.baseline.cost, *pair.candidate.cost)
			costs[currency] = cost
		}
	}
	for name, measure := range summary.Metrics {
		finishPair(&measure)
		summary.Metrics[name] = measure
	}
	for _, cost := range costs {
		cost.UnknownPairs = summary.UnknownCostPairs + summary.CurrencyMismatchPairs
		cost.OtherCurrencyPairs = len(entries) - cost.KnownPairs - cost.UnknownPairs
		finishPair(&cost.PairedMeasure)
		summary.Costs = append(summary.Costs, cost)
	}
	slices.SortFunc(summary.Costs, func(a, b PairedCost) int { return cmp.Compare(a.Currency, b.Currency) })
	return summary
}

func addPair(measure *PairedMeasure, baseline, candidate float64) {
	measure.KnownPairs++
	add(&measure.BaselineTotal, baseline)
	add(&measure.CandidateTotal, candidate)
}

func finishPair(measure *PairedMeasure) {
	if measure.KnownPairs == 0 {
		measure.RelativeChangeUnknownReason = "no_known_pairs"
		return
	}
	delta := *measure.CandidateTotal - *measure.BaselineTotal
	measure.Delta = &delta
	if *measure.BaselineTotal > 0 {
		change := delta / *measure.BaselineTotal
		if math.IsInf(change, 0) || math.IsNaN(change) {
			measure.RelativeChangeUnknownReason = "nonfinite_ratio"
		} else {
			measure.RelativeChange = &change
		}
	} else {
		measure.RelativeChangeUnknownReason = "zero_baseline"
	}
}
