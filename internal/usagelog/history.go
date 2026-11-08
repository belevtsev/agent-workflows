package usagelog

import (
	"context"
	"errors"
)

// The cost history is a JSONL file of per-request token records. Each line is
// one HistoryRecord identified by (source, session_id, request_id); the file
// stores token counts only, so dollars are recomputed from a price table when
// the history is read.
const (
	// HistorySchemaVersion is the "v" field of every history record.
	HistorySchemaVersion = 1
	// HistoryRecordKind is the "kind" field of every history record.
	HistoryRecordKind = "cw.metrics_request"
	// HistoryAppendKind is the kind of an AppendResult.
	HistoryAppendKind = "cw.metrics_history_append"
	// HistoryReportKind is the kind of a HistoryReport.
	HistoryReportKind = "cw.metrics_history"

	// MaxHistoryBytes bounds the history file; an append that would grow it
	// past this size fails without writing.
	MaxHistoryBytes = 1 << 30
	// MaxHistoryLineBytes bounds one history line, newline excluded; a longer
	// line is damaged.
	MaxHistoryLineBytes = 4096
	// MaxHistoryLabelBytes bounds each text field of a record.
	MaxHistoryLabelBytes = 256
	// MaxHistoryDamagedListed bounds HistoryReport.Damaged; DamagedLines still
	// counts every damaged line.
	MaxHistoryDamagedListed = 100
)

// HistoryRecord is one line of the history: the token counts of one
// de-duplicated provider request. It holds no prompt, tool input, file content
// or agent description.
type HistoryRecord struct {
	V         int    `json:"v"`
	Kind      string `json:"kind"`
	Source    string `json:"source"` // claude_transcript | codex_rollout
	SessionID string `json:"session_id"`
	// RequestID is the import's de-duplication key: the Claude requestId,
	// "message:<id>" when only a message id exists, or "response:<id>" for a
	// Codex response id.
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentRole string `json:"agent_role"` // main | subagent
	// AgentType is the Claude subagent type or the Codex agent role, truncated
	// to MaxHistoryLabelBytes; empty when unknown.
	AgentType string `json:"agent_type,omitempty"`
	Model     string `json:"model"`
	// Timestamp is the request line's time in UTC RFC 3339; empty when the
	// line had no parseable timestamp.
	Timestamp  string `json:"timestamp,omitempty"`
	ImportedAt string `json:"imported_at"`

	UncachedInputTokens     int64  `json:"uncached_input_tokens"`
	CacheWrite5mTokens      int64  `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens      int64  `json:"cache_write_1h_tokens"`
	CacheWriteUnsplitTokens int64  `json:"cache_write_unsplit_tokens"`
	CacheReadTokens         int64  `json:"cache_read_tokens"`
	OutputTokens            int64  `json:"output_tokens"`
	ThinkingTokens          *int64 `json:"thinking_tokens"`
}

// AppendResult reports one AppendHistory call. Every request the import
// counted lands in exactly one of Appended, AlreadyPresent, Conflicting,
// Unkeyed or Rejected, so they sum to Requests.
type AppendResult struct {
	SchemaVersion     int      `json:"schema_version"`
	Kind              string   `json:"kind"`
	History           string   `json:"history"`
	Source            string   `json:"source"`
	SessionID         string   `json:"session_id"`
	ImportStatus      string   `json:"import_status"` // the import report's status
	IncompleteReasons []string `json:"incomplete_reasons"`
	// Requests is the number of distinct requests the import counted.
	Requests int64 `json:"requests"`
	// Appended requests were written as new lines.
	Appended int64 `json:"appended"`
	// AlreadyPresent requests had an identical record in the history.
	AlreadyPresent int64 `json:"already_present"`
	// Conflicting requests had a record with the same identity but different
	// token counts; the stored record is kept and nothing is written.
	Conflicting int64 `json:"conflicting"`
	// Unkeyed requests had no request or message id and cannot be
	// de-duplicated, so they are never written.
	Unkeyed int64 `json:"unkeyed"`
	// Rejected requests had a field outside the record bounds.
	Rejected int64 `json:"rejected"`
	// HistoryDamagedLines counts existing history lines that were skipped
	// while reading identities.
	HistoryDamagedLines int64 `json:"history_damaged_lines"`
}

// HistoryFilter selects records. Empty fields match everything. Since and
// Until are YYYY-MM-DD dates compared with the record's UTC date, both
// inclusive; a record without a timestamp never matches a date bound.
type HistoryFilter struct {
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`
	Since     string `json:"since,omitempty"`
	Until     string `json:"until,omitempty"`
}

// DamagedLine is one skipped history line, numbered from 1.
type DamagedLine struct {
	Line   int64  `json:"line"`
	Reason string `json:"reason"`
}

// HistoryGroup aggregates the records sharing Key. Dollars is the
// price-table estimate for the group's tokens, or unknown with a reason.
type HistoryGroup struct {
	Key string `json:"key"`
	Tokens
	Dollars Amount `json:"dollars"`
}

// HistoryReport aggregates the valid records matching a filter.
type HistoryReport struct {
	SchemaVersion int           `json:"schema_version"`
	Kind          string        `json:"kind"`
	History       string        `json:"history"`
	Filter        HistoryFilter `json:"filter"`
	// Lines counts nonblank lines read.
	Lines int64 `json:"lines"`
	// Records counts valid, distinct records matching the filter.
	Records int64 `json:"records"`
	// DuplicateLines counts valid lines whose identity appeared earlier in
	// the file; only the first is counted.
	DuplicateLines int64 `json:"duplicate_lines"`
	// DamagedLines counts lines skipped as unreadable or invalid.
	DamagedLines int64         `json:"damaged_lines"`
	Damaged      []DamagedLine `json:"damaged"`
	// PriceTableEvidence echoes the supplied price table's evidence.
	PriceTableEvidence string `json:"price_table_evidence,omitempty"`
	// Totals has an empty Key.
	Totals HistoryGroup `json:"totals"`
	// ByModel, ByDay (UTC YYYY-MM-DD, or "unknown") and BySession are sorted
	// by Key.
	ByModel   []HistoryGroup `json:"by_model"`
	ByDay     []HistoryGroup `json:"by_day"`
	BySession []HistoryGroup `json:"by_session"`
}

var errNotImplemented = errors.New("not implemented")

// AppendHistory imports the transcript selected by options and appends each
// keyed request that the history does not already hold, as one line per
// request, under an exclusive lock that every writer takes. The history and
// its parent directory are created when missing, with modes 0600 and 0700. It
// fails with ErrInvalidOptions when options request a composition or carry a
// price table, and fails without writing when the import fails, the history
// path is not a regular file (a symlink included), or the append would exceed
// MaxHistoryBytes.
func AppendHistory(ctx context.Context, path string, options Options) (AppendResult, error) {
	return AppendResult{}, errNotImplemented
}

// ReadHistory reads the history at path, under a shared lock, and aggregates
// the valid records matching filter. Damaged lines are counted and listed,
// never silently dropped. With prices, each group carries price-table dollars.
// It fails with ErrInvalidOptions for a malformed date or Since after Until,
// and with an error wrapping os.ErrNotExist when the history does not exist.
func ReadHistory(ctx context.Context, path string, filter HistoryFilter, prices *PriceTable) (HistoryReport, error) {
	return HistoryReport{}, errNotImplemented
}
