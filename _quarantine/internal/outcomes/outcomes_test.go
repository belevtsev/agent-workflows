package outcomes

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func f64(value float64) *float64 { return &value }
func i64(value int64) *int64     { return &value }

func sample(id, variant string, duration float64) Observation {
	return Observation{
		ObservationID: id, TaskID: "task-" + id, CaseID: "case-" + id,
		Project: Project{Kind: "go", Identity: "local-go-project"}, Variant: variant,
		Workload: "bounded-command-invocation", Source: Source{Revision: "actual-head", Fingerprint: "sha256:snapshot-a"},
		Environment: "runtime-a", Evidence: []string{"local:observer-" + id},
		Scope: "synthetic_acceptance", Status: "succeeded", DurationMS: f64(duration),
	}
}

func writeDocument(t *testing.T, root, file string, observations ...Observation) {
	t.Helper()
	data, err := json.Marshal(Document{SchemaVersion, observations})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func analyze(t *testing.T, observations ...Observation) (Report, error) {
	t.Helper()
	root := t.TempDir()
	writeDocument(t, root, "observations.json", observations...)
	return Summarize(t.Context(), Options{root, []string{"observations.json"}, "baseline", "candidate"})
}

func findSummary(t *testing.T, report Report, scope, variant, kind string) Summary {
	t.Helper()
	for _, summary := range report.Summaries {
		if summary.Scope == scope && summary.Variant == variant && summary.ProjectKind == kind {
			return summary
		}
	}
	t.Fatalf("missing summary %s/%s/%s", scope, variant, kind)
	return Summary{}
}

func findComparison(t *testing.T, report Report, scope, kind string) ComparisonSummary {
	t.Helper()
	for _, summary := range report.Comparisons.Summaries {
		if summary.Scope == scope && summary.ProjectKind == kind {
			return summary
		}
	}
	t.Fatalf("missing comparison %s/%s", scope, kind)
	return ComparisonSummary{}
}

func requireFloat(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Fatalf("got %v, want %g", got, want)
	}
}

// These twenty observations are synthetic acceptance fixtures. Their known
// deltas verify arithmetic and coverage, not real-project productivity.
func TestSyntheticAcceptanceTwentyObservations(t *testing.T) {
	var observations []Observation
	for _, kind := range []string{"go", "terraform", "roblox", "node", "python"} {
		for run := range 2 {
			for _, variant := range []string{"baseline", "candidate"} {
				id := fmt.Sprintf("%s-%d-%s", kind, run, variant)
				duration := float64(100 + run*10)
				if variant == "candidate" {
					duration -= 20
				}
				o := sample(id, variant, duration)
				o.Project = Project{kind, "local-" + kind + "-project"}
				o.CaseID = fmt.Sprintf("%s-case-%d", kind, run)
				o.Quality = &Quality{CheckedReferences: i64(2), IncorrectReferences: i64(0), DefectsFound: i64(1), DefectsMissed: i64(0)}
				if kind == "go" && run == 0 {
					o.Usage = &Usage{InputTokens: i64(0), OutputTokens: i64(0), APIRequests: i64(0)}
				}
				observations = append(observations, o)
			}
		}
	}
	report, err := analyze(t, observations...)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Seen != 20 || report.Counts.Succeeded != 20 || report.Comparisons.ComparablePairs != 10 {
		t.Fatalf("incorrect fixture coverage: %+v", report.Counts)
	}
	base := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	if base.Projects != 5 || base.Succeeded != 10 || len(report.Summaries) != 12 {
		t.Fatalf("lost project/kind coverage: %+v", base)
	}
	requireFloat(t, base.Metrics["duration_ms"].Total, 1050)
	usage := base.Metrics["input_tokens"]
	if usage.Known != 1 || usage.Unknown != 9 {
		t.Fatalf("invented usage coverage: %+v", usage)
	}
	requireFloat(t, usage.Total, 0)
	if base.Cost.Known != 0 || base.Cost.Unknown != 10 || len(base.Cost.Totals) != 0 {
		t.Fatalf("invented costs: %+v", base.Cost)
	}
	requireFloat(t, base.ReferenceErrorRate.Numerator, 0)
	requireFloat(t, base.ReferenceErrorRate.Denominator, 20)
	paired := findComparison(t, report, "synthetic_acceptance", "all")
	requireFloat(t, paired.Metrics["duration_ms"].Delta, -200)
	requireFloat(t, paired.Metrics["duration_ms"].RelativeChange, -200.0/1050)
	if paired.Metrics["duration_ms"].KnownPairs != 10 || paired.Metrics["input_tokens"].KnownPairs != 1 || paired.UnknownCostPairs != 10 {
		t.Fatalf("incorrect pair denominators: %+v", paired)
	}
	if paired.Metrics["rework"].Delta != nil || paired.Metrics["rework"].KnownPairs != 0 || paired.Metrics["rework"].UnknownPairs != 10 {
		t.Fatal("missing rework became zero improvement")
	}
}

func TestPartialFailuresAndQualityDenominators(t *testing.T) {
	first, partial := sample("first", "baseline", 10), sample("partial", "baseline", 20)
	first.Usage = &Usage{InputTokens: i64(12)}
	first.Quality = &Quality{CheckedReferences: i64(6), IncorrectReferences: i64(1), DefectsFound: i64(3), DefectsMissed: i64(1)}
	partial.Quality = &Quality{IncorrectReferences: i64(2), DefectsFound: i64(100)}
	failed := sample("failed", "baseline", 999999)
	failed.Status, failed.Usage = "failed", &Usage{InputTokens: i64(999999)}
	invalid := sample("invalid", "baseline", 888888)
	invalid.Quality = &Quality{CheckedReferences: i64(1), IncorrectReferences: i64(2)}
	report, err := analyze(t, first, partial, failed, invalid)
	if !errors.Is(err, ErrInvalidInput) || report.Counts.Accepted != 3 || report.Counts.Invalid != 1 || report.Counts.Succeeded != 2 || report.Counts.Failed != 1 {
		t.Fatalf("partial outcomes lost: %+v, %v", report.Counts, err)
	}
	summary := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	if summary.Failed != 1 || summary.Succeeded != 2 {
		t.Fatalf("failure inclusion not disclosed: %+v", summary)
	}
	requireFloat(t, summary.Metrics["duration_ms"].Total, 30)
	requireFloat(t, summary.Metrics["input_tokens"].Total, 12)
	if summary.Metrics["input_tokens"].Known != 1 || summary.Metrics["input_tokens"].Unknown != 1 {
		t.Fatal("failed samples entered successful coverage")
	}
	if summary.ReferenceErrorRate.Known != 1 || summary.ReferenceErrorRate.Unknown != 1 {
		t.Fatal("partial reference outcomes entered rate denominator")
	}
	requireFloat(t, summary.ReferenceErrorRate.Numerator, 1)
	requireFloat(t, summary.ReferenceErrorRate.Denominator, 6)
	requireFloat(t, summary.ReferenceErrorRate.Value, 1.0/6)
	requireFloat(t, summary.DefectMissRate.Numerator, 1)
	requireFloat(t, summary.DefectMissRate.Denominator, 4)
	if summary.DefectMissRate.Known != 1 || summary.DefectMissRate.Unknown != 1 {
		t.Fatal("missing defect outcomes became perfect quality")
	}
}

func TestDirtySnapshotsAtSameActualGitRevisionAreValidAndNotComparable(t *testing.T) {
	base, candidate := sample("base", "baseline", 100), sample("candidate", "candidate", 1)
	base.CaseID, candidate.CaseID = "same-real-case", "same-real-case"
	base.Scope, candidate.Scope = "real_project_diagnostic", "real_project_diagnostic"
	candidate.Source.Fingerprint = "sha256:dirty-files-changed-at-same-head"
	report, err := analyze(t, base, candidate)
	if err != nil || report.Counts.Accepted != 2 || report.Counts.Invalid != 0 {
		t.Fatalf("legitimate dirty evolution rejected: %+v %v", report, err)
	}
	if report.Comparisons.MatchedCases != 1 || report.Comparisons.ComparablePairs != 0 || len(report.Comparisons.Summaries) != 0 {
		t.Fatal("different source snapshots generated a numerical comparison")
	}
	pair := report.Comparisons.Pairs[0]
	if !slices.Equal(pair.Exclusions, []string{"source_differs"}) || pair.BaselineSource.Revision != pair.CandidateSource.Revision {
		t.Fatalf("source difference not preserved: %+v", pair)
	}
}

func TestUnversionedProjectUsesKnownFingerprintWithoutInventedRevision(t *testing.T) {
	base, candidate := sample("base", "baseline", 2), sample("candidate", "candidate", 1)
	base.Source.Revision, candidate.Source.Revision = "", ""
	base.CaseID, candidate.CaseID = "same", "same"
	report, err := analyze(t, base, candidate)
	if err != nil || report.Counts.Accepted != 2 || report.Comparisons.ComparablePairs != 1 {
		t.Fatalf("known source fingerprint without Git revision rejected: %+v %v", report, err)
	}
	if report.Comparisons.Pairs[0].BaselineSource.Revision != "" {
		t.Fatal("invented an unknown source revision")
	}
}

func TestPairingBoundariesAndAmbiguousVariants(t *testing.T) {
	var observations []Observation
	addPair := func(id string, change func(*Observation)) {
		base, candidate := sample(id+"-base", "baseline", 10), sample(id+"-candidate", "candidate", 5)
		base.CaseID, candidate.CaseID = id, id
		if change != nil {
			change(&candidate)
		}
		observations = append(observations, base, candidate)
	}
	addPair("matching", nil)
	addPair("changed-source", func(o *Observation) { o.Source.Revision, o.Source.Fingerprint = "next-head", "sha256:next-snapshot" })
	addPair("changed-env", func(o *Observation) { o.Environment = "runtime-b" })
	addPair("failed-candidate", func(o *Observation) { o.Status = "failed" })
	addPair("changed-workload", func(o *Observation) { o.Workload = "different-command" })
	addPair("changed-project", func(o *Observation) { o.Project.Identity = "another-go-project" })
	addPair("changed-scope", func(o *Observation) { o.Scope = "real_project_diagnostic" })
	addPair("ambiguous", nil)
	extra := sample("extra-base", "baseline", 3)
	extra.CaseID = "ambiguous"
	observations = append(observations, extra, sample("unmatched", "baseline", 3), sample("other", "alternative", 3))
	report, err := analyze(t, observations...)
	if err != nil {
		t.Fatal(err)
	}
	comparisons := report.Comparisons
	if comparisons.MatchedCases != 4 || comparisons.ComparablePairs != 1 || comparisons.NotSelectedSamples != 1 || len(comparisons.Unmatched) != 10 {
		t.Fatalf("incorrect pairing boundaries: %+v", comparisons)
	}
	seen := make(map[string]bool)
	for _, pair := range comparisons.Pairs {
		for _, reason := range pair.Exclusions {
			seen[reason] = true
		}
	}
	for _, reason := range []string{"source_differs", "environment_differs", "candidate_failed"} {
		if !seen[reason] {
			t.Fatalf("missing exclusion %s", reason)
		}
	}
	paired := findComparison(t, report, "synthetic_acceptance", "all")
	requireFloat(t, paired.Metrics["duration_ms"].BaselineTotal, 10)
	requireFloat(t, paired.Metrics["duration_ms"].CandidateTotal, 5)
}

func TestZeroBaselineAndUnknownMeasurementsNeverFabricateSavings(t *testing.T) {
	base, candidate := sample("base", "baseline", 0), sample("candidate", "candidate", 0)
	base.CaseID, candidate.CaseID = "same", "same"
	base.Quality, candidate.Quality = &Quality{CheckedReferences: i64(0), IncorrectReferences: i64(0)}, &Quality{}
	report, err := analyze(t, base, candidate)
	if err != nil {
		t.Fatal(err)
	}
	paired := findComparison(t, report, "synthetic_acceptance", "all")
	requireFloat(t, paired.Metrics["duration_ms"].Delta, 0)
	if paired.Metrics["duration_ms"].RelativeChange != nil || paired.Metrics["input_tokens"].Delta != nil || len(paired.Costs) != 0 {
		t.Fatal("zero baseline or absent usage invented a saving")
	}
	summary := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	if summary.ReferenceErrorRate.Known != 1 || summary.ReferenceErrorRate.Value != nil {
		t.Fatal("zero checked references became zero error rate")
	}
}

func TestFiniteMeasurementsCannotProduceNonfiniteRelativeChange(t *testing.T) {
	base, candidate := sample("base", "baseline", math.SmallestNonzeroFloat64), sample("candidate", "candidate", MaxMetric)
	base.CaseID, candidate.CaseID = "same", "same"
	report, err := analyze(t, base, candidate)
	if err != nil {
		t.Fatal(err)
	}
	measure := findComparison(t, report, "synthetic_acceptance", "all").Metrics["duration_ms"]
	if measure.RelativeChange != nil || measure.RelativeChangeUnknownReason != "nonfinite_ratio" {
		t.Fatalf("nonfinite ratio not represented as unknown: %+v", measure)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatal("finite inputs produced unencodable report", err)
	}
}

func TestDuplicateObservationOrTaskIDsExcludeAllConflictingSamples(t *testing.T) {
	for _, conflict := range []string{"observation", "task"} {
		t.Run(conflict, func(t *testing.T) {
			base, candidate := sample("base", "baseline", 10), sample("candidate", "candidate", 1)
			if conflict == "observation" {
				candidate.ObservationID = base.ObservationID
			} else {
				candidate.TaskID = base.TaskID
			}
			// Conflicting repeated source data must not allow first-wins aggregation.
			candidate.Source.Fingerprint = "sha256:conflicting-repeated-source"
			report, err := analyze(t, base, candidate)
			if !errors.Is(err, ErrInvalidInput) || report.Counts.Invalid != 2 || report.Counts.Accepted != 0 || len(report.Summaries) != 0 {
				t.Fatalf("conflicting duplicate accepted: %+v %v", report, err)
			}
		})
	}
}

func TestCostsRequireActualBillingOrCompleteUsageAndExplicitRates(t *testing.T) {
	actual, derived, unknown := sample("actual", "baseline", 1), sample("derived", "baseline", 1), sample("unknown", "baseline", 1)
	actual.Cost = &Cost{Currency: "USD", Amount: f64(.12), Evidence: "local:actual-billing-row"}
	derived.Usage = &Usage{InputTokens: i64(2000), OutputTokens: i64(1000), APIRequests: i64(2)}
	derived.Cost = &Cost{Currency: "USD", Rates: &Rates{InputPerMillion: f64(2), OutputPerMillion: f64(3), APIRequest: f64(.01), Evidence: "local:explicit-reliable-rates"}}
	report, err := analyze(t, actual, derived, unknown)
	if err != nil {
		t.Fatal(err)
	}
	summary := findSummary(t, report, "synthetic_acceptance", "baseline", "all")
	if summary.Cost.Known != 2 || summary.Cost.Unknown != 1 || len(summary.Cost.Totals) != 1 {
		t.Fatalf("incorrect cost coverage: %+v", summary.Cost)
	}
	cost := summary.Cost.Totals[0]
	if cost.ObservedSamples != 1 || cost.DerivedSamples != 1 || math.Abs(cost.Total-.147) > 1e-12 {
		t.Fatalf("wrong explicit cost: %+v", cost)
	}
	for _, mutate := range []func(*Observation){
		func(o *Observation) { o.Usage.InputTokens = nil },
		func(o *Observation) { o.Usage.APIRequests = nil },
		func(o *Observation) { o.Cost.Rates.APIRequest = nil },
		func(o *Observation) { o.Cost.Rates.Evidence = "" },
		func(o *Observation) { o.Cost.Amount = f64(.5) },
	} {
		copy := derived
		usage, cost, rates := *derived.Usage, *derived.Cost, *derived.Cost.Rates
		copy.Usage, copy.Cost, cost.Rates = &usage, &cost, &rates
		mutate(&copy)
		report, err := analyze(t, copy)
		if !errors.Is(err, ErrInvalidInput) || report.Counts.Invalid != 1 {
			t.Fatalf("incomplete/ambiguous derived billing accepted: %+v %v", copy, err)
		}
	}
}

func TestCostComparisonDoesNotMixCurrenciesOrUnknowns(t *testing.T) {
	var observations []Observation
	for index, currency := range []string{"USD", "EUR", "USD", "USD"} {
		base, candidate := sample(fmt.Sprintf("b%d", index), "baseline", 2), sample(fmt.Sprintf("c%d", index), "candidate", 1)
		base.CaseID, candidate.CaseID = fmt.Sprint(index), fmt.Sprint(index)
		base.Cost = &Cost{Currency: currency, Amount: f64(1), Evidence: "local:invoice"}
		candidate.Cost = &Cost{Currency: currency, Amount: f64(.5), Evidence: "local:invoice"}
		if index == 2 {
			candidate.Cost.Currency = "EUR"
		}
		if index == 3 {
			candidate.Cost = nil
		}
		observations = append(observations, base, candidate)
	}
	report, err := analyze(t, observations...)
	if err != nil {
		t.Fatal(err)
	}
	paired := findComparison(t, report, "synthetic_acceptance", "all")
	if paired.ComparablePairs != 4 || paired.UnknownCostPairs != 1 || paired.CurrencyMismatchPairs != 1 || len(paired.Costs) != 2 {
		t.Fatalf("currency coverage lost: %+v", paired)
	}
	for _, cost := range paired.Costs {
		if cost.KnownPairs != 1 || cost.UnknownPairs != 2 || cost.OtherCurrencyPairs != 1 {
			t.Fatalf("cost denominators wrong: %+v", cost)
		}
		requireFloat(t, cost.Delta, -.5)
	}
}

func TestStrictJSONAndMetricBounds(t *testing.T) {
	valid, err := json.Marshal(Document{SchemaVersion, []Observation{sample("one", "baseline", 1)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		input []byte
	}{
		{"unknown version", bytes.Replace(valid, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1)},
		{"unknown envelope field", append([]byte(`{"unexpected":1,`), valid[1:]...)},
		{"unknown observation field", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1,"unknown_metric":4`), 1)},
		{"unknown nested field", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1,"usage":{"estimated_tokens":100}`), 1)},
		{"duplicate member", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1,"duration_ms":2`), 1)},
		{"negative duration", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":-1`), 1)},
		{"oversized duration", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1000000000001`), 1)},
		{"overflow duration", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1e9999`), 1)},
		{"nonfinite duration", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":"NaN"`), 1)},
		{"fractional counter", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1,"usage":{"input_tokens":1.5}`), 1)},
		{"negative counter", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1,"quality":{"rework":-1}`), 1)},
		{"oversized counter", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":1,"usage":{"input_tokens":1000000000001}`), 1)},
		{"null successful duration", bytes.Replace(valid, []byte(`"duration_ms":1`), []byte(`"duration_ms":null`), 1)},
		{"trailing document", append(append([]byte(nil), valid...), []byte(` {}`)...)},
		{"empty observations", []byte(`{"schema_version":1,"observations":[]}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "bad.json"), test.input, 0600); err != nil {
				t.Fatal(err)
			}
			report, err := Summarize(t.Context(), Options{root, []string{"bad.json"}, "baseline", "candidate"})
			if !errors.Is(err, ErrInvalidInput) || report.Counts.Accepted != 0 || report.Status != "invalid_input" {
				t.Fatalf("invalid input accepted: %+v %v", report, err)
			}
		})
	}
	for _, number := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, MaxMetric + 1} {
		e := entry{observation: sample("bounded", "baseline", number)}
		e.validate()
		if len(e.problems) == 0 {
			t.Fatalf("nonfinite/unbounded in-memory metric %v accepted", number)
		}
	}
	zero := sample("zero", "baseline", 0)
	zero.Usage = &Usage{InputTokens: i64(int64(MaxMetric)), OutputTokens: i64(0)}
	if report, err := analyze(t, zero); err != nil || report.Counts.Accepted != 1 {
		t.Fatal("valid numeric boundaries rejected", err)
	}
}

func TestUnknownsAndNamedVariantsArePreserved(t *testing.T) {
	root := t.TempDir()
	observation := sample("named", "experimental", 1)
	data, err := json.Marshal(Document{SchemaVersion, []Observation{observation}})
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"duration_ms":1`), []byte(`"duration_ms":1,"usage":{"input_tokens":null},"quality":null,"cost":null`), 1)
	if err := os.WriteFile(filepath.Join(root, "unknown.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Summarize(t.Context(), Options{root, []string{"unknown.json"}, "control", "experimental"})
	if err != nil {
		t.Fatal(err)
	}
	summary := findSummary(t, report, "synthetic_acceptance", "experimental", "all")
	if summary.Metrics["input_tokens"].Unknown != 1 || summary.Metrics["input_tokens"].Total != nil || summary.Cost.Unknown != 1 {
		t.Fatal("explicit null became zero")
	}
	if report.Comparisons.Baseline != "control" || len(report.Comparisons.Unmatched) != 1 || report.Comparisons.Unmatched[0].Reason != "missing_baseline" {
		t.Fatal("named variants lost")
	}
}

func TestReadOnlyBoundariesSpacesAndPartialFileFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "observations with spaces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := "pilot data.json"
	writeDocument(t, root, file, sample("one", "baseline", 1))
	before, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Summarize(t.Context(), Options{root, []string{filepath.Join(root, file), "missing.json"}, "baseline", "candidate"})
	if !errors.Is(err, ErrInvalidInput) || report.Counts.Accepted != 1 || len(report.FileErrors) != 1 {
		t.Fatalf("good file lost on independent read failure: %+v %v", report, err)
	}
	after, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) || beforeInfo.Mode() != afterInfo.Mode() || len(files) != 1 {
		t.Fatal("read-only aggregation changed inputs or created files")
	}
	outside := t.TempDir()
	writeDocument(t, outside, "outside.json", sample("outside", "baseline", 1))
	for _, path := range []string{filepath.Join(outside, "outside.json"), "../outside.json", "sub/../pilot data.json", "."} {
		report, err := Summarize(t.Context(), Options{root, []string{path}, "baseline", "candidate"})
		if !errors.Is(err, ErrInvalidInput) || report.Counts.FilesRead != 0 {
			t.Fatalf("boundary escape or directory %q read: %+v %v", path, report, err)
		}
	}
	for _, test := range []struct{ name, target, input string }{
		{"inside-link", filepath.Join(root, file), "inside-link"},
		{"outside-link", filepath.Join(outside, "outside.json"), "outside-link"},
		{"directory-link", outside, filepath.Join("directory-link", "outside.json")},
	} {
		if err := os.Symlink(test.target, filepath.Join(root, test.name)); err != nil {
			t.Fatal(err)
		}
		report, err := Summarize(t.Context(), Options{root, []string{test.input}, "baseline", "candidate"})
		if !errors.Is(err, ErrInvalidInput) || report.Counts.FilesRead != 0 {
			t.Fatalf("symlink followed: %+v %v", report, err)
		}
	}
}

func TestReadLimitsAndDuplicateFiles(t *testing.T) {
	root := t.TempDir()
	writeDocument(t, root, "one.json", sample("one", "baseline", 1))
	if report, err := Summarize(t.Context(), Options{root, []string{"one.json", "./one.json"}, "baseline", "candidate"}); !errors.Is(err, ErrInvalidInput) || report.Counts.FilesRead != 1 || len(report.FileErrors) != 1 {
		t.Fatal("duplicate paths inflated observations", err)
	}
	if err := os.WriteFile(filepath.Join(root, "huge.json"), bytes.Repeat([]byte(" "), MaxFileBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if report, err := Summarize(t.Context(), Options{root, []string{"huge.json"}, "baseline", "candidate"}); !errors.Is(err, ErrInvalidInput) || report.Counts.FilesRead != 0 {
		t.Fatal("oversized file read", err)
	}
	many := `{"schema_version":1,"observations":[` + strings.Repeat("null,", MaxObservations) + "null]}"
	if err := os.WriteFile(filepath.Join(root, "many.json"), []byte(many), 0600); err != nil {
		t.Fatal(err)
	}
	if report, err := Summarize(t.Context(), Options{root, []string{"many.json"}, "baseline", "candidate"}); !errors.Is(err, ErrInvalidInput) || report.Counts.Accepted != 0 || !strings.Contains(report.FileErrors[0].Problem, "observation count") {
		t.Fatal("observation read bound absent", err)
	}
	if _, err := Summarize(t.Context(), Options{root, make([]string, MaxFiles+1), "baseline", "candidate"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("file count bound absent", err)
	}
	var files []string
	for index := range MaxTotalBytes/MaxFileBytes + 1 {
		file := fmt.Sprintf("padded-%d.json", index)
		writeDocument(t, root, file, sample(fmt.Sprint(index), "baseline", 1))
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, bytes.Repeat([]byte(" "), MaxFileBytes-len(data))...)
		if err := os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	report, err := Summarize(t.Context(), Options{root, files, "baseline", "candidate"})
	if !errors.Is(err, ErrInvalidInput) || report.Counts.Accepted != MaxTotalBytes/MaxFileBytes || len(report.FileErrors) != 1 {
		t.Fatalf("total-byte read bound failed: %+v %v", report.Counts, err)
	}
}

func TestCancellationStopsBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := Summarize(ctx, Options{"/nonexistent/no-io", []string{"missing"}, "baseline", "candidate"})
	if !errors.Is(err, context.Canceled) || report.Status != "cancelled" || report.Counts.FilesRead != 0 {
		t.Fatalf("cancelled work performed I/O: %+v %v", report, err)
	}
	_, err = (contextReader{ctx, panicReader{}}).Read(make([]byte, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatal("reader ignored cancellation", err)
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("cancelled input reader was called") }

func TestSchemaExampleAndDefensiveCopy(t *testing.T) {
	var schema struct {
		Examples []Document `json:"examples"`
	}
	if err := json.Unmarshal(Schema(), &schema); err != nil || len(schema.Examples) != 1 {
		t.Fatal("schema lacks usable example", err)
	}
	example := schema.Examples[0]
	if example.SchemaVersion != 1 || len(example.Observations) != 1 {
		t.Fatal("schema example not versioned")
	}
	if _, err := analyze(t, example.Observations...); err != nil {
		t.Fatal("schema example rejected", err)
	}
	copy := Schema()
	copy[0] = 'x'
	if Schema()[0] != '{' {
		t.Fatal("schema output exposed mutable state")
	}
}

func TestDeterministicReport(t *testing.T) {
	base, candidate := sample("base", "baseline", 2), sample("candidate", "candidate", 1)
	base.CaseID, candidate.CaseID = "same", "same"
	first, err := analyze(t, base, candidate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := analyze(t, candidate, base)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("input order changed valid aggregate results")
	}
	// A context deadline is preserved for callers rather than masked as validation.
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(io.EOF)
	if _, err := Summarize(ctx, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity changed", err)
	}
}
