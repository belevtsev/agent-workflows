// Package usagelog reads one Claude Code session transcript or one Codex
// rollout, read-only, and reports per-agent token usage. Import's dollars come
// only from an amount the transcript itself recorded (Claude cost-state) or
// from an explicitly supplied price table, reported separately so an estimate
// is never presented as a recorded amount. EstimateSession prices each request
// from a table, by default the dated built-in one (BuiltInPrices), and never
// uses the cost-state.
package usagelog

import "github.com/belevtsev/agent-workflows/v3/internal/outcomes"

const (
	// ReportSchemaVersion versions the JSON import report.
	ReportSchemaVersion = 1
	ReportKind          = "cw.usage_import"

	SourceClaude = "claude_transcript"
	SourceCodex  = "codex_rollout"

	StatusComplete   = "complete"
	StatusIncomplete = "incomplete"

	BasisRecorded   = "recorded_cost_state"
	BasisPriceTable = "price_table"

	// MaxLineBytes bounds one JSONL line; longer lines are skipped and counted.
	MaxLineBytes = 16 << 20
	// MaxTotalBytes bounds all transcript bytes read by one import.
	MaxTotalBytes = 2 << 30
	// MaxFiles bounds transcript files read by one import (main plus agents).
	MaxFiles = 2000
	// MaxRequests bounds distinct requests kept for de-duplication.
	MaxRequests = 1_000_000
	// MaxScanFiles bounds Codex rollout files inspected during child discovery.
	MaxScanFiles = 20000
	// MaxScanLineBytes bounds the session_meta line read from each scanned rollout.
	MaxScanLineBytes = MaxLineBytes
	// MaxSmallFileBytes bounds price tables and Claude subagent meta files.
	MaxSmallFileBytes = 1 << 20
)

// Limits echoes the bounds applied to an import.
type Limits struct {
	MaxLineBytes     int64 `json:"max_line_bytes"`
	MaxTotalBytes    int64 `json:"max_total_bytes"`
	MaxFiles         int   `json:"max_files"`
	MaxRequests      int   `json:"max_requests"`
	MaxScanFiles     int   `json:"max_scan_files"`
	MaxScanLineBytes int64 `json:"max_scan_line_bytes"`
}

func limits() Limits {
	return Limits{MaxLineBytes, MaxTotalBytes, MaxFiles, MaxRequests, MaxScanFiles, MaxScanLineBytes}
}

// Report is the deterministic import result.
type Report struct {
	SchemaVersion     int             `json:"schema_version"`
	Kind              string          `json:"kind"`
	Source            string          `json:"source"`
	SessionID         string          `json:"session_id"`
	Status            string          `json:"status"`
	IncompleteReasons []string        `json:"incomplete_reasons"`
	Limits            Limits          `json:"limits"`
	Files             []FileRead      `json:"files"`
	Skipped           Skipped         `json:"skipped"`
	Anomalies         Anomalies       `json:"anomalies"`
	ChildDiscovery    *ChildDiscovery `json:"child_discovery,omitempty"`
	Agents            []Agent         `json:"agents"`
	Models            []ModelUsage    `json:"models"`
	Totals            Tokens          `json:"totals"`
	Dollars           Dollars         `json:"dollars"`
	// Usage is observation-ready (outcomes.Usage) for the whole session; null
	// with UsageUnknownReason when the import is incomplete.
	Usage              *outcomes.Usage `json:"usage"`
	UsageUnknownReason string          `json:"usage_unknown_reason,omitempty"`
	// Cost is present only for a recorded amount; price-table dollars stay in
	// Dollars so measured and estimated amounts remain distinct.
	Cost  *outcomes.Cost `json:"cost,omitempty"`
	Notes []string       `json:"notes"`
}

// FileRead identifies one transcript file that contributed to the report.
type FileRead struct {
	Path      string `json:"path"` // Relative to the session directory or sessions root.
	AgentID   string `json:"agent_id"`
	BytesRead int64  `json:"bytes_read"`
	Lines     int64  `json:"lines"`
	Complete  bool   `json:"complete"`
}

// Skipped counts transcript entries that did not contribute usage.
type Skipped struct {
	MalformedLines          int64 `json:"malformed_lines"`
	OversizedLines          int64 `json:"oversized_lines"`
	InvalidEntries          int64 `json:"invalid_entries"`
	SyntheticEntries        int64 `json:"synthetic_entries"`
	NoUsageEntries          int64 `json:"no_usage_entries"`
	DuplicateEntries        int64 `json:"duplicate_entries"`
	OtherSessionEntries     int64 `json:"other_session_entries"`
	IgnoredCostStates       int64 `json:"ignored_cost_states"`
	IgnoredTokenCountEvents int64 `json:"ignored_token_count_events"`
	UnreadableFiles         int64 `json:"unreadable_files"`
	MetaErrors              int64 `json:"meta_errors"`
}

// Anomalies counts included entries whose data needed an interpretation.
type Anomalies struct {
	ConflictingDuplicates               int64 `json:"conflicting_duplicates"`
	EntriesWithoutRequestID             int64 `json:"entries_without_request_id"`
	CacheSplitMismatchRequests          int64 `json:"cache_split_mismatch_requests"`
	CodexCacheWriteOutsideInputRequests int64 `json:"codex_cache_write_outside_input_requests"`
	RequestsWithoutModel                int64 `json:"requests_without_model"`
}

// ChildDiscovery describes how Codex descendant rollouts were found.
type ChildDiscovery struct {
	Status          string `json:"status"` // scanned | not_attempted
	Reason          string `json:"reason,omitempty"`
	SessionsRoot    string `json:"sessions_root,omitempty"`
	FilesScanned    int    `json:"files_scanned"`
	UnreadableFiles int    `json:"unreadable_files"`
	// OtherFormatFiles are readable rollouts whose first line is a JSON
	// object but not session_meta (legacy format); they cannot be children.
	OtherFormatFiles int  `json:"other_format_files"`
	RootFound        bool `json:"root_found"`
	Truncated        bool `json:"truncated"`
	Descendants      int  `json:"descendants"`
}

// Tokens are de-duplicated per-request counters. TotalInputTokens is
// uncached + all cache writes + cache reads. Thinking tokens are a subset of
// output tokens; ThinkingTokens is null when no request reported them.
type Tokens struct {
	Requests                 int64  `json:"requests"`
	UncachedInputTokens      int64  `json:"uncached_input_tokens"`
	CacheWrite5mTokens       int64  `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens       int64  `json:"cache_write_1h_tokens"`
	CacheWriteUnsplitTokens  int64  `json:"cache_write_unsplit_tokens"`
	CacheReadTokens          int64  `json:"cache_read_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	ThinkingTokens           *int64 `json:"thinking_tokens"`
	ThinkingReportedRequests int64  `json:"thinking_reported_requests"`
	TotalInputTokens         int64  `json:"total_input_tokens"`
}

// ModelUsage is a token breakdown for one model; Dollars is price-table based.
type ModelUsage struct {
	Model string `json:"model"`
	Tokens
	Dollars Amount `json:"dollars"`
}

// Amount is a dollar amount with its basis, or null with a reason.
type Amount struct {
	Amount        *float64 `json:"amount"`
	Basis         string   `json:"basis,omitempty"`
	UnknownReason string   `json:"unknown_reason,omitempty"`
}

// Agent is one main or child agent of the session.
type Agent struct {
	AgentID           string   `json:"agent_id"`
	Role              string   `json:"role"` // main | subagent
	Files             []string `json:"files"`
	Description       string   `json:"description,omitempty"`
	AgentType         string   `json:"agent_type,omitempty"`
	ConfiguredModel   string   `json:"configured_model,omitempty"`
	ConfiguredEffort  string   `json:"configured_effort,omitempty"`
	PickedEffortModel string   `json:"picked_effort_model,omitempty"`
	Nickname          string   `json:"nickname,omitempty"`
	AgentRole         string   `json:"agent_role,omitempty"`
	AgentPath         string   `json:"agent_path,omitempty"`
	ParentID          string   `json:"parent_id,omitempty"`
	Depth             int      `json:"depth"`
	ModelsSeen        []string `json:"models_seen"`
	EffortsSeen       []string `json:"efforts_seen"`
	Tokens
	ByModel []ModelUsage `json:"by_model"`
	Dollars Amount       `json:"dollars"`
}

// Dollars holds session-level recorded and price-table amounts side by side.
type Dollars struct {
	Currency                string             `json:"currency"`
	Recorded                *RecordedDollars   `json:"recorded"`
	RecordedUnknownReason   string             `json:"recorded_unknown_reason,omitempty"`
	PriceTable              *PriceTableDollars `json:"price_table"`
	PriceTableUnknownReason string             `json:"price_table_unknown_reason,omitempty"`
	// RecordedMinusPriceTable compares recorded total with the price-table total
	// of attributed tokens plus unattributed recorded tokens.
	RecordedMinusPriceTable *float64 `json:"recorded_minus_price_table"`
	DifferenceUnknownReason string   `json:"difference_unknown_reason,omitempty"`
}

// RecordedDollars is the last Claude cost-state of the main transcript.
type RecordedDollars struct {
	Basis               string           `json:"basis"`
	Evidence            string           `json:"evidence"`
	CostStateEntries    int              `json:"cost_state_entries"`
	Total               float64          `json:"total"`
	HasUnknownModelCost *bool            `json:"has_unknown_model_cost"`
	Models              []RecordedModel  `json:"models"`
	Reconciliation      []Reconciliation `json:"reconciliation"`
	UnattributedModels  []string         `json:"unattributed_models"`
	UnattributedTotal   *float64         `json:"unattributed_total"`
}

// RecordedModel is one cost-state modelUsage entry. Absent fields are null.
type RecordedModel struct {
	Model              string   `json:"model"`
	Cost               *float64 `json:"cost"`
	InputTokens        *int64   `json:"input_tokens"` // Uncached input, as recorded.
	OutputTokens       *int64   `json:"output_tokens"`
	ThinkingTokens     *int64   `json:"thinking_tokens"`
	CacheReadTokens    *int64   `json:"cache_read_tokens"`
	CacheWriteTokens   *int64   `json:"cache_write_tokens"`
	CacheWrite5mTokens *int64   `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens *int64   `json:"cache_write_1h_tokens"`
	WebSearchRequests  *int64   `json:"web_search_requests"`
	Attributed         bool     `json:"attributed"`
}

// Reconciliation compares transcript-attributed tokens with recorded tokens.
// Status is equal, differs, not_compared (no recorded token field to check),
// recorded_only (unattributed) or transcript_only.
type Reconciliation struct {
	Model                   string           `json:"model"`
	Status                  string           `json:"status"`
	TranscriptMinusRecorded map[string]int64 `json:"transcript_minus_recorded"`
	NotCompared             []string         `json:"not_compared"`
}

// PriceTableDollars is computed from a supplied price table only.
type PriceTableDollars struct {
	Basis    string `json:"basis"`
	Currency string `json:"currency"`
	Evidence string `json:"evidence"`
	// Total covers transcript-attributed tokens.
	Total              *float64      `json:"total"`
	TotalUnknownReason string        `json:"total_unknown_reason,omitempty"`
	Models             []ModelAmount `json:"models"`
	// UnattributedRecorded prices recorded-only models from cost-state tokens.
	UnattributedRecorded              []ModelAmount `json:"unattributed_recorded"`
	UnattributedRecordedTotal         *float64      `json:"unattributed_recorded_total"`
	UnattributedRecordedUnknownReason string        `json:"unattributed_recorded_unknown_reason,omitempty"`
}

// ModelAmount is one model's dollar amount or the reason it is unknown.
type ModelAmount struct {
	Model string `json:"model"`
	Amount
}
