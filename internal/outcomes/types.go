// Package outcomes aggregates explicitly supplied workflow observations. It does
// not inspect projects, contact providers, or infer usage from elapsed time.
package outcomes

import "errors"

const (
	SchemaVersion   = 1
	MaxFiles        = 100
	MaxFileBytes    = 2 << 20
	MaxTotalBytes   = 16 << 20
	MaxObservations = 5000
	MaxMetric       = 1e12
)

var ErrInvalidInput = errors.New("metrics input contains invalid files or observations")

type Options struct {
	InputRoot string
	Files     []string
	Baseline  string
	Candidate string
}

// Document is the versioned, deliberately small interchange format. Optional
// numbers use pointers so an observed zero is distinct from an unknown value.
type Document struct {
	SchemaVersion int           `json:"schema_version"`
	Observations  []Observation `json:"observations"`
}

type Project struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
}

type Source struct {
	Revision    string `json:"revision,omitempty"`
	Fingerprint string `json:"fingerprint"`
}

type Observation struct {
	ObservationID string   `json:"observation_id"`
	CaseID        string   `json:"case_id"`
	TaskID        string   `json:"task_id"`
	Project       Project  `json:"project"`
	Variant       string   `json:"variant"`
	Workload      string   `json:"workload"`
	Source        Source   `json:"source"`
	Environment   string   `json:"environment"`
	Evidence      []string `json:"evidence"`
	Scope         string   `json:"scope"`
	Status        string   `json:"status"`
	DurationMS    *float64 `json:"duration_ms,omitempty"`
	Usage         *Usage   `json:"usage,omitempty"`
	Cost          *Cost    `json:"cost,omitempty"`
	Quality       *Quality `json:"quality,omitempty"`
}

// Usage counters are actual provider counts; nil is unknown. InputTokens is
// total input: uncached input plus cache writes plus cache reads. The optional
// breakdown must sum to InputTokens when all of it is known, and thinking
// tokens are a subset of OutputTokens.
type Usage struct {
	InputTokens         *int64 `json:"input_tokens,omitempty"`
	OutputTokens        *int64 `json:"output_tokens,omitempty"`
	APIRequests         *int64 `json:"api_requests,omitempty"`
	UncachedInputTokens *int64 `json:"uncached_input_tokens,omitempty"`
	CacheWrite5mTokens  *int64 `json:"cache_write_5m_tokens,omitempty"`
	CacheWrite1hTokens  *int64 `json:"cache_write_1h_tokens,omitempty"`
	CacheReadTokens     *int64 `json:"cache_read_tokens,omitempty"`
	ThinkingTokens      *int64 `json:"thinking_tokens,omitempty"`
}

type Quality struct {
	CheckedReferences   *int64 `json:"checked_references,omitempty"`
	IncorrectReferences *int64 `json:"incorrect_references,omitempty"`
	DefectsFound        *int64 `json:"defects_found,omitempty"`
	DefectsMissed       *int64 `json:"defects_missed,omitempty"`
	Rework              *int64 `json:"rework,omitempty"`
}

// Cost is either an actual billed amount with evidence, or a calculation from
// complete actual usage and all explicitly supplied rates. A zero request
// rate must be explicit; omitted prices and usage never become zero.
type Cost struct {
	Currency string   `json:"currency"`
	Amount   *float64 `json:"amount,omitempty"`
	Evidence string   `json:"evidence,omitempty"`
	Rates    *Rates   `json:"rates,omitempty"`
}

// Rates without cache rates price total input_tokens at InputPerMillion. When
// any cache rate is present all three are required, InputPerMillion applies
// to uncached input only, and the cache counters must be known.
type Rates struct {
	InputPerMillion        *float64 `json:"input_per_million"`
	OutputPerMillion       *float64 `json:"output_per_million"`
	APIRequest             *float64 `json:"api_request"`
	CacheWrite5mPerMillion *float64 `json:"cache_write_5m_per_million,omitempty"`
	CacheWrite1hPerMillion *float64 `json:"cache_write_1h_per_million,omitempty"`
	CacheReadPerMillion    *float64 `json:"cache_read_per_million,omitempty"`
	Evidence               string   `json:"evidence"`
}

type Limits struct {
	MaxFiles        int     `json:"max_files"`
	MaxFileBytes    int     `json:"max_file_bytes"`
	MaxTotalBytes   int     `json:"max_total_bytes"`
	MaxObservations int     `json:"max_observations"`
	MaxMetric       float64 `json:"max_metric"`
}

func limits() Limits {
	return Limits{MaxFiles, MaxFileBytes, MaxTotalBytes, MaxObservations, MaxMetric}
}

type Counts struct {
	FilesRequested int `json:"files_requested"`
	FilesRead      int `json:"files_read"`
	Seen           int `json:"observations_seen"`
	Accepted       int `json:"accepted"`
	Invalid        int `json:"invalid"`
	Succeeded      int `json:"succeeded"`
	Failed         int `json:"failed"`
}

type FileIssue struct {
	File    string `json:"file"`
	Problem string `json:"problem"`
}

type SampleIssue struct {
	File          string   `json:"file"`
	Index         int      `json:"index"` // One-based position within the file.
	ObservationID string   `json:"observation_id"`
	Problems      []string `json:"problems"`
}

type Coverage struct {
	Known   int `json:"known"`
	Unknown int `json:"unknown"`
}

type Measure struct {
	Coverage
	Total *float64 `json:"total"`
	Mean  *float64 `json:"mean"`
}

type Ratio struct {
	Coverage
	Numerator   *float64 `json:"numerator"`
	Denominator *float64 `json:"denominator"`
	Value       *float64 `json:"value"`
}

type CostTotal struct {
	Currency        string  `json:"currency"`
	Samples         int     `json:"samples"`
	ObservedSamples int     `json:"observed_samples"`
	DerivedSamples  int     `json:"derived_samples"`
	Total           float64 `json:"total"`
}

type Costs struct {
	Coverage
	Totals []CostTotal `json:"totals"`
}

type Summary struct {
	Scope                string             `json:"scope"`
	Variant              string             `json:"variant"`
	ProjectKind          string             `json:"project_kind"` // "all" or an input kind.
	Projects             int                `json:"projects"`
	Succeeded            int                `json:"succeeded"`
	Failed               int                `json:"failed"`
	DistinctSources      int                `json:"distinct_sources"`
	DistinctEnvironments int                `json:"distinct_environments"`
	Metrics              map[string]Measure `json:"metrics"`
	ReferenceErrorRate   Ratio              `json:"reference_error_rate"`
	DefectMissRate       Ratio              `json:"defect_miss_rate"`
	Cost                 Costs              `json:"cost"`
}

// PairedMeasure includes only pairs for which both values are known. A null
// relative change means the baseline denominator is zero or no pair is known.
type PairedMeasure struct {
	KnownPairs                  int      `json:"known_pairs"`
	UnknownPairs                int      `json:"unknown_pairs"`
	BaselineTotal               *float64 `json:"baseline_total"`
	CandidateTotal              *float64 `json:"candidate_total"`
	Delta                       *float64 `json:"candidate_minus_baseline"`
	RelativeChange              *float64 `json:"relative_change"`
	RelativeChangeUnknownReason string   `json:"relative_change_unknown_reason,omitempty"`
}

type PairedCost struct {
	Currency string `json:"currency"`
	PairedMeasure
	OtherCurrencyPairs int `json:"other_currency_pairs"`
}

type ComparisonSummary struct {
	Scope                 string                   `json:"scope"`
	ProjectKind           string                   `json:"project_kind"`
	ComparablePairs       int                      `json:"comparable_pairs"`
	Metrics               map[string]PairedMeasure `json:"metrics"`
	Costs                 []PairedCost             `json:"costs"`
	UnknownCostPairs      int                      `json:"unknown_cost_pairs"`
	CurrencyMismatchPairs int                      `json:"currency_mismatch_pairs"`
}

type Pair struct {
	CaseID               string   `json:"case_id"`
	Project              Project  `json:"project"`
	Scope                string   `json:"scope"`
	Workload             string   `json:"workload"`
	BaselineID           string   `json:"baseline_id"`
	CandidateID          string   `json:"candidate_id"`
	BaselineSource       Source   `json:"baseline_source"`
	CandidateSource      Source   `json:"candidate_source"`
	BaselineEnvironment  string   `json:"baseline_environment"`
	CandidateEnvironment string   `json:"candidate_environment"`
	Comparable           bool     `json:"comparable"`
	Exclusions           []string `json:"exclusions"`
}

type Unmatched struct {
	ObservationID string  `json:"observation_id"`
	CaseID        string  `json:"case_id"`
	Project       Project `json:"project"`
	Scope         string  `json:"scope"`
	Workload      string  `json:"workload"`
	Variant       string  `json:"variant"`
	Reason        string  `json:"reason"`
}

type Comparisons struct {
	Baseline           string              `json:"baseline"`
	Candidate          string              `json:"candidate"`
	MatchedCases       int                 `json:"matched_cases"`
	ComparablePairs    int                 `json:"comparable_pairs"`
	NotSelectedSamples int                 `json:"not_selected_samples"`
	Pairs              []Pair              `json:"pairs"`
	Unmatched          []Unmatched         `json:"unmatched"`
	Summaries          []ComparisonSummary `json:"summaries"`
}

type Report struct {
	SchemaVersion  int           `json:"schema_version"`
	Status         string        `json:"status"`
	Counts         Counts        `json:"counts"`
	Limits         Limits        `json:"limits"`
	FileErrors     []FileIssue   `json:"file_errors"`
	InvalidSamples []SampleIssue `json:"invalid_samples"`
	Summaries      []Summary     `json:"summaries"`
	Comparisons    Comparisons   `json:"comparisons"`
	Interpretation []string      `json:"interpretation"`
}
