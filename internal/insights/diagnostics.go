// Package insights builds offline projections of existing local evidence.
package insights

import (
	"time"

	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

const diagnosticSamples = 8

// TokenCounter reports captured usage, never an estimate of missing usage.
type TokenCounter struct {
	Known    int64  `json:"known"`
	Complete bool   `json:"complete"`
	Status   string `json:"status"`
}

// TokenShare uses captured total tokens as its denominator.
type TokenShare struct {
	Category string  `json:"category"`
	Known    int64   `json:"known"`
	Percent  float64 `json:"percent_of_captured"`
}

// Consumer is an investigation candidate, not a causal bottleneck.
type Consumer struct {
	Task    string `json:"task,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Role    string `json:"role,omitempty"`
	Round   int    `json:"round,omitempty"`
	Host    string `json:"host"`
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Known   int64  `json:"known_tokens"`
	Record  string `json:"record"`
}

// IntervalSummary describes prepared-to-receipt spans, including waiting and
// coordinator work. It cannot measure worker execution or a task critical path.
type IntervalSummary struct {
	ObservedSeconds float64  `json:"observed_seconds_lower_bound"`
	Complete        bool     `json:"managed_records_complete"`
	Closed          int      `json:"closed"`
	Unfinished      int      `json:"unfinished"`
	Abandoned       int      `json:"abandoned"`
	Unknown         int      `json:"unknown"`
	Sources         []string `json:"source_samples"`
}

type CheckSummary struct {
	Runs               int            `json:"actual_runs"`
	Outcomes           map[string]int `json:"outcomes"`
	AssociatedRuns     int            `json:"explicitly_associated_runs"`
	UnassociatedRuns   int            `json:"unassociated_runs"`
	References         int            `json:"evidence_references"`
	RepeatedReferences int            `json:"repeated_evidence_references"`
	MissingReferences  int            `json:"missing_run_references"`
	Reuse              string         `json:"reuse_execution"`
	ObservedSeconds    float64        `json:"observed_run_seconds_lower_bound"`
	UnknownIntervals   int            `json:"unknown_run_intervals"`
	Sources            []string       `json:"source_samples"`
}

type TaskDiagnostic struct {
	Task                string          `json:"task"`
	CapturedTokens      TokenCounter    `json:"captured_tokens"`
	UncachedTokens      TokenCounter    `json:"uncached_tokens"`
	AttributionComplete bool            `json:"attribution_complete"`
	SharedSessions      int             `json:"shared_sessions"`
	UnknownRecords      int             `json:"unknown_records"`
	Shares              []TokenShare    `json:"shares"`
	ManagedIntervals    IntervalSummary `json:"managed_intervals"`
	Checks              CheckSummary    `json:"checks"`
}

type Diagnostics struct {
	Version          int              `json:"version"`
	Task             string           `json:"task,omitempty"`
	CapturedTokens   TokenCounter     `json:"captured_tokens"`
	UncachedTokens   TokenCounter     `json:"uncached_tokens"`
	Tasks            []TaskDiagnostic `json:"tasks"`
	DuplicateNotices int              `json:"duplicate_notices"`
	UnlabelledStarts int              `json:"unlabelled_starts"`
	SharedSessions   int              `json:"shared_sessions"`
	DamagedRecords   int              `json:"damaged_records"`
	ManagedIntervals IntervalSummary  `json:"managed_intervals"`
	Checks           CheckSummary     `json:"checks"`
	Candidates       []Consumer       `json:"investigation_candidates"`
	Unknowns         []string         `json:"unknowns"`
	UnknownSources   []string         `json:"unknown_source_samples"`
}

// ManagedStage contains only identities, times and original check references.
type ManagedStage struct {
	Task, ID, Source, ReceiptSource, Status string
	Prepared                                time.Time
	Ended                                   *time.Time
	Checks                                  []CheckReference
	ReceiptSources                          []string
	Damaged                                 bool
}

type CheckReference struct {
	Task, CheckID, RunID, Source string
}

type CheckRun struct {
	Record store.Record
	Source string
}

type DiagnosticInput struct {
	Task           string
	Starts         []tokens.Start
	DamagedTokens  []string
	Stages         []ManagedStage
	Checks         []CheckRun
	References     []CheckReference
	DamagedRecords []string
}
